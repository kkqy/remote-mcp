#!/usr/bin/env python3
"""Connect explicitly to a native desktop, save screenshots, and validate input using a caller-provided dedicated target plan."""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import platform
import ssl
import stat
import struct
import subprocess
import sys
import time
import urllib.request
import uuid
import zlib

from gui_clipboard import REFRESH_ERRORS, RESCUE_ERRORS, validate_original_formats


GUI_TOOLS = {"gui_status", "gui_open", "gui_close", "gui_screenshot", "gui_mouse", "gui_key", "gui_text"}
GUI_ERROR_CODES = {"invalid_argument", "unsupported", "no_gui", "permission_denied", "dependency_missing",
                   "authorization_cancelled", "authorization_timeout", "session_closed", "not_found", "conflict",
                   "busy", "stale_capture", "capture_failed", "timeout", "limit_exceeded", "input_failed",
                   "clipboard_preservation_unavailable", "clipboard_restore_failed"}


def authorize_wait(value):
    try:
        seconds = int(value)
    except ValueError:
        raise argparse.ArgumentTypeError("Authorization wait must be an integer between 1 and 605 seconds") from None
    if seconds < 1 or seconds > 605:
        raise argparse.ArgumentTypeError("Authorization wait must be an integer between 1 and 605 seconds")
    return seconds


class Client:
    def __init__(self, url, token, ca_file=None):
        self.url = url
        self.headers = {"Content-Type": "application/json", "Accept": "application/json, text/event-stream",
                        "MCP-Protocol-Version": "2025-11-25"}
        if token:
            self.headers["Authorization"] = "Bearer " + token
        self.sequence = 0
        self.tls = ssl.create_default_context(cafile=ca_file)

    def rpc(self, method, params, notification=False):
        self.sequence += 1
        payload = {"jsonrpc": "2.0", "method": method, "params": params}
        if not notification:
            payload["id"] = self.sequence
        request = urllib.request.Request(self.url, data=json.dumps(payload).encode("utf-8"), headers=self.headers)
        with urllib.request.urlopen(request, timeout=45, context=self.tls) as response:
            session = response.headers.get("Mcp-Session-Id")
            if session:
                self.headers["Mcp-Session-Id"] = session
            body = response.read(24 * 1024 * 1024 + 1)
        if len(body) > 24 * 1024 * 1024:
            raise RuntimeError("The protocol response exceeds the validation script limit")
        document = json.loads(body.decode("utf-8")) if body else {}
        if document.get("error"):
            raise RuntimeError("The JSON-RPC call failed")
        return document.get("result", {})

    def tool(self, name, arguments):
        result = self.rpc("tools/call", {"name": name, "arguments": arguments})
        metadata = result.get("structuredContent", {})
        if not isinstance(metadata, dict):
            raise RuntimeError("The tool response has no structured result: " + name)
        if result.get("isError") or metadata.get("ok") is False:
            # 仅保留 GUI 契约的有界固定错误说明，不转发图片、参数、剪贴板或其他工具消息。
            code = metadata.get("code", "tool_failed")
            description = "Tool call failed: " + name + " (" + str(code) + ")"
            message = metadata.get("message")
            if name in GUI_TOOLS and code in GUI_ERROR_CODES and isinstance(message, str) and 0 < len(message) <= 512 and all(ord(character) >= 32 and ord(character) != 127 for character in message):
                description += ": " + message
            raise RuntimeError(description)
        return metadata, result


def decode_png(data):
    """校验 PNG 块、CRC 和全部解压扫描行；不只读取文件头。"""
    if not data.startswith(b"\x89PNG\r\n\x1a\n"):
        raise RuntimeError("The screenshot is not a PNG")
    cursor, compressed, dimensions, ended = 8, bytearray(), None, False
    while cursor < len(data):
        if cursor + 12 > len(data):
            raise RuntimeError("A PNG chunk is truncated")
        size = struct.unpack_from(">I", data, cursor)[0]
        kind = data[cursor + 4:cursor + 8]
        content = data[cursor + 8:cursor + 8 + size]
        if cursor + size + 12 > len(data):
            raise RuntimeError("PNG data is truncated")
        checksum = struct.unpack_from(">I", data, cursor + 8 + size)[0]
        if zlib.crc32(kind + content) & 0xffffffff != checksum:
            raise RuntimeError("PNG CRC validation failed")
        if kind == b"IHDR":
            if dimensions is not None or size != 13 or cursor != 8:
                raise RuntimeError("Invalid PNG header")
            width, height, depth, color, compression, filtering, interlace = struct.unpack(">IIBBBBB", content)
            if width <= 0 or height <= 0 or width * height > 16777216:
                raise RuntimeError("The PNG pixel count exceeds the script limit")
            if compression or filtering or interlace:
                raise RuntimeError("The validation script supports only standard noninterlaced PNG images")
            channels = {0: 1, 2: 3, 3: 1, 4: 2, 6: 4}.get(color)
            valid_depths = {0: {1, 2, 4, 8, 16}, 2: {8, 16}, 3: {1, 2, 4, 8}, 4: {8, 16}, 6: {8, 16}}
            if channels is None or depth not in valid_depths[color]:
                raise RuntimeError("Invalid PNG pixel format")
            row_bytes = (width * depth * channels + 7) // 8
            dimensions = (width, height, row_bytes)
        elif kind == b"IDAT":
            compressed.extend(content)
        elif kind == b"IEND":
            if size or cursor + 12 != len(data):
                raise RuntimeError("Invalid PNG end chunk")
            ended = True
        cursor += size + 12
    if not dimensions or not ended or not compressed:
        raise RuntimeError("The PNG is missing required chunks")
    width, height, row_bytes = dimensions
    expected = height * (row_bytes + 1)
    decoder = zlib.decompressobj()
    raw = decoder.decompress(compressed, expected + 1)
    if len(raw) != expected or not decoder.eof or decoder.unused_data:
        raise RuntimeError("The PNG scanline length does not match")
    if any(raw[y * (row_bytes + 1)] > 4 for y in range(height)):
        raise RuntimeError("Invalid PNG scanline filter type")
    return width, height


def screenshot(client, gui_id, display_id, output, label):
    arguments = {"id": gui_id}
    if display_id:
        arguments["display_id"] = display_id
    metadata, response = client.tool("gui_screenshot", arguments)
    images = [item for item in response.get("content", []) if item.get("type") == "image"]
    if len(images) != 1 or images[0].get("mimeType") != "image/png":
        raise RuntimeError("A screenshot must return exactly one standard MCP PNG image block")
    data = base64.b64decode(images[0]["data"], validate=True)
    width, height = decode_png(data)
    # 兼容结果外层携带 capture 字段与直接返回截图元数据两种结构。
    capture = metadata.get("capture", metadata)
    for key in ("capture_id", "display_id", "region", "logical_bounds", "layout_generation", "captured_at", "captured_at_source", "frame_sequence", "freshness"):
        if key not in capture:
            raise RuntimeError("The screenshot is missing coordinate or capture metadata: " + key)
    if capture.get("width") != width or capture.get("height") != height:
        raise RuntimeError("The screenshot dimensions do not match the structured result")
    (output / (label + ".png")).write_bytes(data)
    (output / (label + ".json")).write_text(json.dumps(capture, ensure_ascii=False, indent=2), encoding="utf-8")
    return capture


def load_plan(path):
    if not path:
        return None
    plan = json.loads(Path(path).read_text(encoding="utf-8"))
    if not isinstance(plan, dict) or not isinstance(plan.get("target"), str) or not plan["target"].strip():
        raise RuntimeError("The input plan must declare a prepared dedicated target")
    operations = plan.get("operations")
    if not isinstance(operations, list) or not operations or len(operations) > 64:
        raise RuntimeError("The input plan requires 1 to 64 operations")
    for operation in operations:
        if not isinstance(operation, dict) or operation.get("tool") not in {"gui_mouse", "gui_key", "gui_text"}:
            raise RuntimeError("The input plan may call only GUI input tools")
        arguments = operation.get("arguments")
        if not isinstance(arguments, dict) or "id" in arguments or "capture_id" in arguments:
            raise RuntimeError("Input plan arguments must not specify session or capture IDs")
    verification = plan.get("verify_text_file")
    if verification is not None and (not isinstance(verification, dict) or not isinstance(verification.get("path"), str) or not isinstance(verification.get("expected"), str)):
        raise RuntimeError("verify_text_file must specify path and expected")
    return plan


def fixture_points(data):
    """从真实截图的四个纯色标记定位专用窗口，适配桌面缩放。"""
    width, height = decode_png(data)
    position, compressed, color_type = 8, bytearray(), None
    while position < len(data):
        size = struct.unpack_from(">I", data, position)[0]
        kind, content = data[position + 4:position + 8], data[position + 8:position + 8 + size]
        if kind == b"IHDR":
            depth, color_type = content[8], content[9]
            if depth != 8 or color_type not in {2, 6}:
                raise RuntimeError("Automatic fixture location requires an 8-bit RGB or RGBA screenshot")
        if kind == b"IDAT":
            compressed.extend(content)
        position += size + 12
    channels = 3 if color_type == 2 else 4
    row_size = width * channels
    raw = zlib.decompress(compressed)
    previous = bytearray(row_size)
    markers = {(241, 13, 78): [0, 0, 0], (23, 214, 185): [0, 0, 0], (217, 74, 7): [0, 0, 0], (73, 60, 241): [0, 0, 0]}
    for y in range(height):
        start = y * (row_size + 1)
        filtering = raw[start]
        row = bytearray(raw[start + 1:start + 1 + row_size])
        for offset in range(row_size):
            left = row[offset - channels] if offset >= channels else 0
            above = previous[offset]
            upper_left = previous[offset - channels] if offset >= channels else 0
            if filtering == 1:
                predictor = left
            elif filtering == 2:
                predictor = above
            elif filtering == 3:
                predictor = (left + above) // 2
            elif filtering == 4:
                candidate = left + above - upper_left
                distances = [abs(candidate - left), abs(candidate - above), abs(candidate - upper_left)]
                predictor = [left, above, upper_left][distances.index(min(distances))]
            else:
                predictor = 0
            row[offset] = (row[offset] + predictor) & 255
        for x in range(width):
            offset = x * channels
            rgb = tuple(row[offset:offset + 3])
            if rgb in markers:
                counts = markers[rgb]
                counts[0] += 1
                counts[1] += x
                counts[2] += y
        previous = row
    centers = []
    for count, total_x, total_y in markers.values():
        if count < 64 or count > 40000:
            raise RuntimeError("The screenshot does not contain all four fixture markers; select the correct display and keep the window visible")
        centers.append((total_x / count, total_y / count))
    top_left, top_right, bottom_left, bottom_right = centers
    scale_x, scale_y = (top_right[0] - top_left[0]) / 580, (bottom_left[1] - top_left[1]) / 400
    if scale_x <= 0 or scale_y <= 0 or abs(bottom_right[0] - top_right[0]) > 2 or abs(bottom_right[1] - bottom_left[1]) > 2:
        raise RuntimeError("The fixture marker coordinates are inconsistent; input is refused")

    def point(x, y):
        return {"x": top_left[0] + (x - 30) * scale_x, "y": top_left[1] + (y - 30) * scale_y}

    return point


def fixture_plan(point, allow_replace, inputs_only=False):
    text = "中文图形验证成功"
    operations = [
        {"tool": "gui_mouse", "arguments": {"action": "click", **point(320, 130)}},
        {"tool": "gui_text", "arguments": {"text": "旧内容", "allow_clipboard_replace": allow_replace}},
        {"tool": "gui_key", "arguments": {"keys": ["Ctrl" if sys.platform != "darwin" else "Meta", "A"]}},
        {"tool": "gui_text", "arguments": {"text": text, "allow_clipboard_replace": allow_replace}},
        {"tool": "gui_mouse", "arguments": {"action": "click", **point(150, 260)}},
        {"tool": "gui_mouse", "arguments": {"action": "double_click", **point(320, 260)}},
        {"tool": "gui_mouse", "arguments": {"action": "drag", **point(100, 320), "end_x": point(500, 320)["x"], "end_y": point(500, 320)["y"], "duration_ms": 500}},
        {"tool": "gui_mouse", "arguments": {"action": "scroll", **point(400, 260), "scroll_y": 2}},
    ]
    if inputs_only:
        operations = [operations[0], operations[2], {"tool": "gui_key", "arguments": {"keys": ["Backspace"]}}, *operations[4:]]
        text = ""
    return {"target": "Dedicated native validation window", "operations": operations}, text


def fixture_verified(result, expected):
    def occurred(kind, x, y):
        return any(event["type"] == kind and abs(event.get("x", -1000) - x) < 10 and abs(event.get("y", -1000) - y) < 10
                   and (kind != "scroll" or event.get("delta_y", 0) != 0) for event in result["events"])

    return (result["text"] == expected and occurred("press", 150, 260) and occurred("release", 150, 260)
            and occurred("double_click", 320, 260) and occurred("drag", 500, 320) and occurred("scroll", 400, 260))


def fixture_request(output, fixture, operation, baseline=None, test_hashes=None, test_closed=False, timeout_seconds=10):
    """通过专用窗口确认焦点或读取剪贴板，等待有上限。"""
    if not isinstance(timeout_seconds, (int, float)) or isinstance(timeout_seconds, bool) or not 0 < timeout_seconds <= 605:
        raise RuntimeError("Fixture wait must be greater than 0 and no more than 605 seconds")
    request_id = uuid.uuid4().hex
    request = output / "clipboard-request.json"
    temporary = request.with_suffix(".tmp")
    document = {"id": request_id, "operation": operation}
    if baseline is not None:
        document["baseline"] = baseline
    if test_hashes is not None:
        document["test_hashes"] = test_hashes
    if operation == "rescue_original":
        document["test_closed"] = test_closed
    temporary.write_text(json.dumps(document), encoding="utf-8")
    temporary.replace(request)
    response = output / "clipboard-response.json"
    deadline = time.monotonic() + timeout_seconds
    while fixture.poll() is None and time.monotonic() < deadline:
        if response.exists():
            document = json.loads(response.read_text(encoding="utf-8"))
            if document.get("id") == request_id:
                if not document.get("ok"):
                    if document.get("error") == "not_focused":
                        time.sleep(0.05)
                        continue
                    if document.get("error") in REFRESH_ERRORS | RESCUE_ERRORS:
                        raise RuntimeError("Clipboard preparation failed: " + document["error"])
                    raise RuntimeError("The clipboard cannot be read reliably or exceeds the validation limit; default restoration validation is aborted")
                return document
        time.sleep(0.05)
    if operation == "focus":
        raise RuntimeError("Fixture focus timed out; complete system authorization and keep the window running and visible")
    raise RuntimeError("The clipboard operation timed out; the fixture must have focus and desktop read permission and remain running")


def sample_clipboard(output, fixture, label, baseline=None, original_baseline=None):
    """请求专用窗口重新读取系统剪贴板，只保存格式、长度和哈希。"""
    summary = fixture_request(output, fixture, "clipboard")["summary"]
    (output / ("clipboard-" + label + ".json")).write_text(json.dumps(summary, ensure_ascii=False, indent=2), encoding="utf-8")
    if baseline is not None and summary != baseline:
        raise RuntimeError("Clipboard formats, lengths, or hashes have changed: " + label)
    if original_baseline is not None:
        validate_original_formats(summary, original_baseline, allow_qt_alias=True)
        (output / ("clipboard-original-check-" + label + ".json")).write_text(json.dumps({"original_formats_preserved": True,
            "qt_derived_alias_present": summary != original_baseline}), encoding="utf-8")
    return summary


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--execute", action="store_true", help="Explicitly enable native desktop authorization, screenshots, and optional input")
    parser.add_argument("--url", default="http://127.0.0.1:8080/mcp", help="MCP URL of an already running service")
    parser.add_argument("--token-file", help="Credential file; defaults to REMOTE_MCP_TOKEN")
    parser.add_argument("--no-token", action="store_true", help="Ignore the environment token and use anonymous mode")
    parser.add_argument("--ca-file", help="Trusted CA file for the service certificate")
    parser.add_argument("--display-id", help="Display to capture; defaults to the service selection")
    parser.add_argument("--output-dir", default="gui-evidence", help="Directory for screenshots and metadata")
    parser.add_argument("--input-plan", help="Caller-prepared dedicated target and input plan JSON; coordinates are relative to the first screenshot")
    parser.add_argument("--fixture", action="store_true", help="Start the optional PySide6 fixture, locate it automatically, and verify mouse input and Chinese text")
    parser.add_argument("--fixture-inputs-only", action="store_true", help="Verify only mouse and keyboard input and clearing the fixed fixture field; do not call text tools or write the clipboard")
    parser.add_argument("--allow-clipboard-replace", action="store_true", help="Fixture only: explicitly allow clipboard replacement when preservation is unavailable")
    parser.add_argument("--refresh-clipboard-offer", action="store_true", help="Explicitly republish identical clipboard data from the fixture to prepare the initial Portal format notification")
    parser.add_argument("--desktop-label", default="Unspecified", help="Record the desktop and its version, such as GNOME 49 Wayland")
    parser.add_argument("--authorize-wait-seconds", type=authorize_wait, default=125, help="Test client authorization wait limit, from 1 to 605 seconds; align it with the service authorization timeout")
    args = parser.parse_args()
    if not args.execute:
        parser.print_help()
        print("\nNot executed: only --execute connects to the desktop; ordinary CI does not invoke native GUI operations.")
        return 0
    if args.no_token and args.token_file:
        parser.error("--no-token and --token-file cannot be used together")
    if args.fixture and args.input_plan:
        parser.error("--fixture and --input-plan cannot be used together")
    if args.fixture_inputs_only and (not args.fixture or args.refresh_clipboard_offer or args.allow_clipboard_replace):
        parser.error("--fixture-inputs-only requires --fixture and cannot refresh or allow replacement of the clipboard")
    if args.allow_clipboard_replace and not args.fixture:
        parser.error("--allow-clipboard-replace requires --fixture; custom plans declare it in gui_text arguments")
    if args.refresh_clipboard_offer and (not args.fixture or args.allow_clipboard_replace):
        parser.error("--refresh-clipboard-offer requires default-preservation validation with --fixture and cannot be used with clipboard replacement")
    plan = load_plan(args.input_plan)
    token = "" if args.no_token else os.environ.get("REMOTE_MCP_TOKEN", "")
    if args.token_file:
        token_file = Path(args.token_file)
        with token_file.open("rb") as credential:
            info = os.fstat(credential.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_size > 8192:
                raise RuntimeError("The credential file must be a regular file no larger than 8 KiB")
            if os.name != "nt" and info.st_mode & 0o077:
                raise RuntimeError("The credential file must be readable only by the current account")
            contents = credential.read(8193)
            if len(contents) > 8192:
                raise RuntimeError("The credential file exceeds 8 KiB")
            token = contents.decode("utf-8").strip()
        if not token:
            raise RuntimeError("An explicit credential file must not be empty")
    if token and (len(token) > 8192 or any(ord(character) < 33 or ord(character) > 126 for character in token)):
        raise RuntimeError("Invalid token format")
    output = Path(args.output_dir).resolve()
    output.mkdir(mode=0o700, parents=True, exist_ok=False)
    client = Client(args.url, token, args.ca_file)
    gui_id = None
    fixture = None
    fixture_expected = None
    clipboard_baseline = None
    original_clipboard_baseline = None
    refresh_attempted = False
    test_hashes = []
    close_confirmed = False
    primary_failure = None
    report = {"acceptance_complete": False, "run_completed": False, "platform": platform.platform(), "desktop": args.desktop_label,
              "checks": [], "submitted_operations": [], "pending": ["Validate all five native desktop environments separately", "Verify image display in an actual MCP client", "Validate multiple displays, mixed scaling, and layout changes", "Validate clipboard restoration and ownership races, authorization revocation, and disconnection"]}
    try:
        initialized = client.rpc("initialize", {"protocolVersion": "2025-11-25", "capabilities": {},
                                                "clientInfo": {"name": "independent-gui-smoke", "version": "1"}})
        if initialized.get("protocolVersion") != "2025-11-25":
            raise RuntimeError("The MCP protocol version does not match")
        client.rpc("notifications/initialized", {}, notification=True)
        names = {item["name"] for item in client.rpc("tools/list", {}).get("tools", [])}
        if not GUI_TOOLS <= names:
            raise RuntimeError("GUI tool registration is incomplete")
        report["checks"].append("Independent JSON-RPC initialization and discovery of all seven GUI tools")
        client.tool("gui_status", {})
        if args.fixture:
            environment = os.environ.copy()
            if environment.get("XDG_SESSION_TYPE") == "wayland" or environment.get("WAYLAND_DISPLAY"):
                environment["QT_QPA_PLATFORM"] = "wayland"
            fixture_args = [sys.executable, str(Path(__file__).with_name("gui_fixture.py")), "--evidence", str(output / "fixture.json")]
            if args.fixture_inputs_only:
                fixture_args.append("--inputs-only")
            fixture = subprocess.Popen(fixture_args,
                                       env=environment, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            deadline = time.monotonic() + 10
            while not (output / "fixture.json").exists() and time.monotonic() < deadline and fixture.poll() is None:
                time.sleep(0.1)
            if not (output / "fixture.json").exists():
                raise RuntimeError("The fixture could not start; install the optional PySide6 validation dependency and check the desktop session")
            if args.fixture_inputs_only:
                initial = json.loads((output / "fixture.json").read_text(encoding="utf-8"))
                if initial.get("text") != "组合键验证":
                    raise RuntimeError("The input-only fixture is missing its fixed initial field; actual clearing cannot be verified")
                report["fixture_inputs_only"] = True
                report["pending"].append("gui_text was not called in this run; actual Chinese text input and default clipboard restoration remain pending")
            elif not args.allow_clipboard_replace:
                clipboard_baseline = sample_clipboard(output, fixture, "baseline")
        opened, _ = client.tool("gui_open", {"request_id": "gui-smoke-" + uuid.uuid4().hex, "wait_ms": 10000})
        gui_id = opened["id"]
        deadline = time.monotonic() + args.authorize_wait_seconds
        while opened.get("state") == "authorizing" and time.monotonic() < deadline:
            time.sleep(1)
            opened, _ = client.tool("gui_status", {"id": gui_id})
        if opened.get("state") != "ready":
            raise RuntimeError("The GUI session did not enter ready state; check authorization on the target machine")
        if fixture is not None:
            # Portal ready之后仍可能有系统模态许可；先等待窗口焦点，再定位或准备剪贴板。
            fixture_request(output, fixture, "focus", timeout_seconds=args.authorize_wait_seconds)
        if args.refresh_clipboard_offer:
            refresh_attempted = True
            refreshed = fixture_request(output, fixture, "refresh_offer", clipboard_baseline)["summary"]
            validate_original_formats(refreshed, clipboard_baseline, allow_qt_alias=True)
            original_clipboard_baseline = clipboard_baseline
            # Qt/合成器可能稍后规范化格式；仅在准备阶段再次接受经核验的唯一别名。
            refreshed = sample_clipboard(output, fixture, "after-refresh", original_baseline=original_clipboard_baseline)
            alias_added = validate_original_formats(refreshed, original_clipboard_baseline, allow_qt_alias=True)
            clipboard_baseline = refreshed
            report["checks"].append("Original format byte digests match; " + ("a Qt-derived charset alias was added" if alias_added else "no formats were added") + "; replacement was not allowed")
            report["qt_derived_alias_added"] = alias_added
            report["clipboard_offer_refreshed"] = True
            # 让 Portal 异步格式通知抵达服务；有界等待后仍由默认保护作最终判断。
            offer_deadline = time.monotonic() + 2
            while time.monotonic() < offer_deadline:
                status, _ = client.tool("gui_status", {"id": gui_id})
                reason = status.get("capabilities", {}).get("reasons", {}).get("text", "")
                if "has not provided the current clipboard formats" not in reason:
                    break
                time.sleep(0.05)
        capture = screenshot(client, gui_id, args.display_id, output, "before")
        report["checks"].append("Standard MCP image, PNG scanlines, coordinates, and capture metadata")
        if args.fixture:
            point = fixture_points((output / "before.png").read_bytes())
            plan, fixture_expected = fixture_plan(point, args.allow_clipboard_replace, inputs_only=args.fixture_inputs_only)
        if plan:
            verification = plan.get("verify_text_file")
            if verification and Path(verification["path"]).exists():
                raise RuntimeError("The text validation file must not exist before the run, to avoid reading a stale artifact")
            for index, operation in enumerate(plan["operations"]):
                if fixture is not None:
                    fixture_request(output, fixture, "focus")
                arguments = dict(operation["arguments"], id=gui_id)
                if operation["tool"] == "gui_mouse":
                    arguments["capture_id"] = capture["capture_id"]
                if operation["tool"] == "gui_text" and clipboard_baseline is not None:
                    sample_clipboard(output, fixture, "before-text-" + str(index + 1), clipboard_baseline, original_clipboard_baseline)
                    encoded = arguments["text"].encode("utf-8")
                    test_hashes.append({"length": len(encoded), "sha256": hashlib.sha256(encoded).hexdigest()})
                result, _ = client.tool(operation["tool"], arguments)
                report["submitted_operations"].append({"tool": operation["tool"], "submitted": result.get("submitted"),
                                                       "mode": result.get("mode"), "clipboard_restore": result.get("clipboard_restore")})
                if operation["tool"] == "gui_text" and clipboard_baseline is not None:
                    sample_clipboard(output, fixture, "after-text-" + str(index + 1), clipboard_baseline, original_clipboard_baseline)
                    report["checks"].append("Original clipboard format lengths and hashes match after text input")
                screenshot(client, gui_id, args.display_id, output, "after-" + str(index + 1))
            if verification:
                evidence = Path(verification["path"])
                deadline = time.monotonic() + 10
                while not evidence.exists() and time.monotonic() < deadline:
                    time.sleep(0.1)
                if evidence.stat().st_size > 1024 * 1024 or evidence.read_text(encoding="utf-8") != verification["expected"]:
                    raise RuntimeError("The UTF-8 content saved by the target application does not match the expected content")
                report["checks"].append("The UTF-8 text actually saved by the target application matches the expected content")
            elif not args.fixture:
                report["pending"].append("Only input event submission was checked; target application text and mouse behavior require manual verification")
            if args.fixture:
                deadline = time.monotonic() + 5
                while True:
                    fixture_result = json.loads((output / "fixture.json").read_text(encoding="utf-8"))
                    complete = fixture_verified(fixture_result, fixture_expected)
                    if complete or time.monotonic() >= deadline:
                        break
                    time.sleep(0.1)
                if not complete:
                    raise RuntimeError("Actual mouse events or Chinese text in the fixture failed validation")
                if (os.environ.get("XDG_SESSION_TYPE") == "wayland" or os.environ.get("WAYLAND_DISPLAY")) and fixture_result["platform"] != "wayland":
                    raise RuntimeError("Native Wayland validation cannot use an XWayland window as a substitute")
                if args.fixture_inputs_only:
                    report["checks"].append("Mouse and keyboard only: native window clicks, double-clicks, dragging, scrolling, select-all and deletion keys cleared the password-echo field; the test did not read or write the clipboard or copy selections")
                else:
                    report["checks"].append("Clicks, double-clicks, dragging, scrolling, select-all shortcuts, and actual Chinese text in the native fixture")
                report["fixture_platform"] = fixture_result["platform"]
        else:
            report["pending"].append("No input plan was provided; actual mouse, keyboard, and Chinese text input remain pending")
        report["run_completed"] = True
    except Exception as error:
        primary_failure = error
        report["failure"] = str(error) if isinstance(error, RuntimeError) else type(error).__name__
        raise
    finally:
        try:
            if gui_id:
                closed, _ = client.tool("gui_close", {"id": gui_id})
                again, _ = client.tool("gui_close", {"id": gui_id})
                if closed.get("state") != "closed" or again.get("state") != "closed":
                    raise RuntimeError("The GUI session was not reliably closed")
                close_confirmed = True
                report["checks"].append("GUI session close and repeated close")
                if clipboard_baseline is not None:
                    sample_clipboard(output, fixture, "after-close", clipboard_baseline, original_clipboard_baseline)
                    report["checks"].append("All original clipboard formats remain readable with matching hashes after the GUI session closes")
        except Exception as error:
            report["run_completed"] = False
            report.setdefault("cleanup_failures", []).append(str(error) if isinstance(error, RuntimeError) else type(error).__name__)
            if primary_failure is None:
                primary_failure = error
                raise
        finally:
            try:
                if fixture:
                    retain_fixture = False
                    if refresh_attempted and primary_failure is not None and fixture.poll() is None:
                        # 内存备份不随业务失败销毁；无法证明外部所有权时交由用户恢复。
                        try:
                            result = fixture_request(output, fixture, "rescue_original", test_hashes=test_hashes, test_closed=close_confirmed)
                            recovery = result["recovery"]
                            retain_fixture = recovery in {"needs_user", "rescued"} or result.get("backup_provider_owned", False) or not close_confirmed
                            report["clipboard_recovery"] = recovery
                        except Exception:
                            retain_fixture = True
                            report["clipboard_recovery"] = "needs_user_unconfirmed"
                        if retain_fixture:
                            report["fixture_retained"] = True
                            report["fixture_pid"] = fixture.pid
                            report["pending"].append("The fixture retains a bounded in-memory backup of the original clipboard; confirm restoration before closing it")
                    if not retain_fixture:
                        if fixture.poll() is None:
                            fixture.terminate()
                        try:
                            fixture.wait(timeout=5)
                        except subprocess.TimeoutExpired:
                            fixture.kill()
                            fixture.wait(timeout=5)
            except Exception as error:
                report["run_completed"] = False
                report.setdefault("cleanup_failures", []).append(str(error) if isinstance(error, RuntimeError) else type(error).__name__)
                if primary_failure is None:
                    primary_failure = error
                    raise
            finally:
                (output / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps(report, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as error:
        # 不输出 traceback，避免参数、图片、输入计划及凭据意外进入日志。
        print("GUI validation failed: " + (str(error) if isinstance(error, RuntimeError) else type(error).__name__), file=sys.stderr)
        sys.exit(1)
