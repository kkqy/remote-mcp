#!/usr/bin/env python3
"""Validate a remote Windows GUI through HTTP, save screenshots, and optionally use a caller-prepared dedicated target plan."""
import argparse
import base64
import json
import os
from pathlib import Path
import platform
import ssl
import stat
import struct
import sys
import time
import urllib.request
import uuid
import zlib


GUI_TOOLS = {"gui_status", "gui_open", "gui_close", "gui_screenshot", "gui_mouse", "gui_key", "gui_text"}
GUI_ERROR_CODES = {"invalid_argument", "unsupported", "no_gui", "permission_denied", "dependency_missing",
                   "authorization_cancelled", "authorization_timeout", "session_closed", "not_found", "conflict",
                   "busy", "stale_capture", "capture_failed", "timeout", "limit_exceeded", "input_failed"}


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


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--execute", action="store_true", help="Explicitly enable remote Windows screenshots and optional input")
    parser.add_argument("--url", default="http://127.0.0.1:8080/mcp", help="MCP URL of an already running Windows service")
    parser.add_argument("--token-file", help="Credential file; defaults to REMOTE_MCP_TOKEN")
    parser.add_argument("--no-token", action="store_true", help="Ignore the environment token and use anonymous mode")
    parser.add_argument("--ca-file", help="Trusted CA file for the service certificate")
    parser.add_argument("--display-id", help="Display to capture; defaults to the service selection")
    parser.add_argument("--output-dir", default=".tmp/gui-evidence", help="Directory for screenshots and metadata")
    parser.add_argument("--input-plan", help="Caller-prepared dedicated target and input plan JSON; coordinates are relative to the first screenshot")
    parser.add_argument("--desktop-label", default="Unspecified", help="Record the desktop and its version, such as Windows 10 22H2")
    parser.add_argument("--authorize-wait-seconds", type=authorize_wait, default=125, help="Test client authorization wait limit, from 1 to 605 seconds; align it with the service authorization timeout")
    args = parser.parse_args()
    if not args.execute:
        parser.print_help()
        print("\nNot executed: only --execute connects to the desktop; ordinary CI does not invoke native GUI operations.")
        return 0
    if args.no_token and args.token_file:
        parser.error("--no-token and --token-file cannot be used together")
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
    primary_failure = None
    report = {"acceptance_complete": False, "run_completed": False, "host_platform": platform.platform(), "target_platform": "windows", "desktop": args.desktop_label,
              "checks": [], "submitted_operations": [], "pending": ["Verify image display in an actual MCP client", "Validate multiple displays, mixed scaling, and layout changes", "Validate session interruption and disconnection"]}
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
        status, _ = client.tool("gui_status", {})
        if status.get("backend") != "windows":
            raise RuntimeError("This GUI validation requires a Windows service")
        opened, _ = client.tool("gui_open", {"request_id": "gui-smoke-" + uuid.uuid4().hex, "wait_ms": 10000})
        gui_id = opened["id"]
        deadline = time.monotonic() + args.authorize_wait_seconds
        while opened.get("state") == "authorizing" and time.monotonic() < deadline:
            time.sleep(1)
            opened, _ = client.tool("gui_status", {"id": gui_id})
        if opened.get("state") != "ready":
            raise RuntimeError("The GUI session did not enter ready state; check authorization on the target machine")
        capture = screenshot(client, gui_id, args.display_id, output, "before")
        report["checks"].append("Standard MCP image, PNG scanlines, coordinates, and capture metadata")
        if plan:
            verification = plan.get("verify_text_file")
            if verification and Path(verification["path"]).exists():
                raise RuntimeError("The text validation file must not exist before the run, to avoid reading a stale artifact")
            for index, operation in enumerate(plan["operations"]):
                arguments = dict(operation["arguments"], id=gui_id)
                if operation["tool"] == "gui_mouse":
                    arguments["capture_id"] = capture["capture_id"]
                result, _ = client.tool(operation["tool"], arguments)
                report["submitted_operations"].append({"tool": operation["tool"], "submitted": result.get("submitted"),
                                                       "mode": result.get("mode"), "clipboard_restore": result.get("clipboard_restore")})
                screenshot(client, gui_id, args.display_id, output, "after-" + str(index + 1))
            if verification:
                evidence = Path(verification["path"])
                deadline = time.monotonic() + 10
                while not evidence.exists() and time.monotonic() < deadline:
                    time.sleep(0.1)
                if evidence.stat().st_size > 1024 * 1024 or evidence.read_text(encoding="utf-8") != verification["expected"]:
                    raise RuntimeError("The UTF-8 content saved by the target application does not match the expected content")
                report["checks"].append("The UTF-8 text actually saved by the target application matches the expected content")
            else:
                report["pending"].append("Only input event submission was checked; target application text and mouse behavior require manual verification")
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
                report["checks"].append("GUI session close and repeated close")
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
