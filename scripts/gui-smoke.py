#!/usr/bin/env python3
"""显式连接真实桌面，保存截图并按调用方提供的专用目标计划验证输入。"""
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
        raise argparse.ArgumentTypeError("授权等待须为 1 至 605 秒的整数") from None
    if seconds < 1 or seconds > 605:
        raise argparse.ArgumentTypeError("授权等待须为 1 至 605 秒的整数")
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
            raise RuntimeError("协议响应超过验证脚本上限")
        document = json.loads(body.decode("utf-8")) if body else {}
        if document.get("error"):
            raise RuntimeError("JSON-RPC 调用失败")
        return document.get("result", {})

    def tool(self, name, arguments):
        result = self.rpc("tools/call", {"name": name, "arguments": arguments})
        metadata = result.get("structuredContent", {})
        if not isinstance(metadata, dict):
            raise RuntimeError("工具缺少结构化结果：" + name)
        if result.get("isError") or metadata.get("ok") is False:
            # 仅保留 GUI 契约的有界固定错误说明，不转发图片、参数、剪贴板或其他工具消息。
            code = metadata.get("code", "tool_failed")
            description = "工具调用失败：" + name + "（" + str(code) + "）"
            message = metadata.get("message")
            if name in GUI_TOOLS and code in GUI_ERROR_CODES and isinstance(message, str) and 0 < len(message) <= 512 and all(ord(character) >= 32 and ord(character) != 127 for character in message):
                description += "：" + message
            raise RuntimeError(description)
        return metadata, result


def decode_png(data):
    """校验 PNG 块、CRC 和全部解压扫描行；不只读取文件头。"""
    if not data.startswith(b"\x89PNG\r\n\x1a\n"):
        raise RuntimeError("截图不是 PNG")
    cursor, compressed, dimensions, ended = 8, bytearray(), None, False
    while cursor < len(data):
        if cursor + 12 > len(data):
            raise RuntimeError("PNG 块被截断")
        size = struct.unpack_from(">I", data, cursor)[0]
        kind = data[cursor + 4:cursor + 8]
        content = data[cursor + 8:cursor + 8 + size]
        if cursor + size + 12 > len(data):
            raise RuntimeError("PNG 数据被截断")
        checksum = struct.unpack_from(">I", data, cursor + 8 + size)[0]
        if zlib.crc32(kind + content) & 0xffffffff != checksum:
            raise RuntimeError("PNG CRC 校验失败")
        if kind == b"IHDR":
            if dimensions is not None or size != 13 or cursor != 8:
                raise RuntimeError("PNG 头无效")
            width, height, depth, color, compression, filtering, interlace = struct.unpack(">IIBBBBB", content)
            if width <= 0 or height <= 0 or width * height > 16777216:
                raise RuntimeError("PNG 像素超过脚本上限")
            if compression or filtering or interlace:
                raise RuntimeError("验证脚本仅支持非交错标准 PNG")
            channels = {0: 1, 2: 3, 3: 1, 4: 2, 6: 4}.get(color)
            valid_depths = {0: {1, 2, 4, 8, 16}, 2: {8, 16}, 3: {1, 2, 4, 8}, 4: {8, 16}, 6: {8, 16}}
            if channels is None or depth not in valid_depths[color]:
                raise RuntimeError("PNG 像素格式无效")
            row_bytes = (width * depth * channels + 7) // 8
            dimensions = (width, height, row_bytes)
        elif kind == b"IDAT":
            compressed.extend(content)
        elif kind == b"IEND":
            if size or cursor + 12 != len(data):
                raise RuntimeError("PNG 结束块无效")
            ended = True
        cursor += size + 12
    if not dimensions or not ended or not compressed:
        raise RuntimeError("PNG 缺少必需块")
    width, height, row_bytes = dimensions
    expected = height * (row_bytes + 1)
    decoder = zlib.decompressobj()
    raw = decoder.decompress(compressed, expected + 1)
    if len(raw) != expected or not decoder.eof or decoder.unused_data:
        raise RuntimeError("PNG 扫描行长度不符")
    if any(raw[y * (row_bytes + 1)] > 4 for y in range(height)):
        raise RuntimeError("PNG 扫描行过滤类型无效")
    return width, height


def screenshot(client, gui_id, display_id, output, label):
    arguments = {"id": gui_id}
    if display_id:
        arguments["display_id"] = display_id
    metadata, response = client.tool("gui_screenshot", arguments)
    images = [item for item in response.get("content", []) if item.get("type") == "image"]
    if len(images) != 1 or images[0].get("mimeType") != "image/png":
        raise RuntimeError("截图必须返回一个标准 MCP PNG 图片块")
    data = base64.b64decode(images[0]["data"], validate=True)
    width, height = decode_png(data)
    # 兼容结果外层携带 capture 字段与直接返回截图元数据两种结构。
    capture = metadata.get("capture", metadata)
    for key in ("capture_id", "display_id", "region", "logical_bounds", "layout_generation", "captured_at", "captured_at_source", "frame_sequence", "freshness"):
        if key not in capture:
            raise RuntimeError("截图缺少坐标或采集元数据：" + key)
    if capture.get("width") != width or capture.get("height") != height:
        raise RuntimeError("截图图片尺寸与结构化结果不符")
    (output / (label + ".png")).write_bytes(data)
    (output / (label + ".json")).write_text(json.dumps(capture, ensure_ascii=False, indent=2), encoding="utf-8")
    return capture


def load_plan(path):
    if not path:
        return None
    plan = json.loads(Path(path).read_text(encoding="utf-8"))
    if not isinstance(plan, dict) or not isinstance(plan.get("target"), str) or not plan["target"].strip():
        raise RuntimeError("输入计划必须声明已准备好的专用目标 target")
    operations = plan.get("operations")
    if not isinstance(operations, list) or not operations or len(operations) > 64:
        raise RuntimeError("输入计划需要 1 至 64 个 operations")
    for operation in operations:
        if not isinstance(operation, dict) or operation.get("tool") not in {"gui_mouse", "gui_key", "gui_text"}:
            raise RuntimeError("输入计划只能调用 GUI 输入工具")
        arguments = operation.get("arguments")
        if not isinstance(arguments, dict) or "id" in arguments or "capture_id" in arguments:
            raise RuntimeError("输入计划 arguments 不得指定会话或截图 ID")
    verification = plan.get("verify_text_file")
    if verification is not None and (not isinstance(verification, dict) or not isinstance(verification.get("path"), str) or not isinstance(verification.get("expected"), str)):
        raise RuntimeError("verify_text_file 必须指定 path 和 expected")
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
                raise RuntimeError("专用窗口自动定位需要 RGB/RGBA 8 位截图")
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
            raise RuntimeError("截图未完整包含专用窗口四个标记；请选择正确显示器并保持窗口可见")
        centers.append((total_x / count, total_y / count))
    top_left, top_right, bottom_left, bottom_right = centers
    scale_x, scale_y = (top_right[0] - top_left[0]) / 580, (bottom_left[1] - top_left[1]) / 400
    if scale_x <= 0 or scale_y <= 0 or abs(bottom_right[0] - top_right[0]) > 2 or abs(bottom_right[1] - bottom_left[1]) > 2:
        raise RuntimeError("专用窗口标记坐标不一致，拒绝输入")

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
    return {"target": "专用原生验证窗口", "operations": operations}, text


def fixture_verified(result, expected):
    def occurred(kind, x, y):
        return any(event["type"] == kind and abs(event.get("x", -1000) - x) < 10 and abs(event.get("y", -1000) - y) < 10
                   and (kind != "scroll" or event.get("delta_y", 0) != 0) for event in result["events"])

    return (result["text"] == expected and occurred("press", 150, 260) and occurred("release", 150, 260)
            and occurred("double_click", 320, 260) and occurred("drag", 500, 320) and occurred("scroll", 400, 260))


def fixture_request(output, fixture, operation, baseline=None, test_hashes=None, test_closed=False, timeout_seconds=10):
    """通过专用窗口确认焦点或读取剪贴板，等待有上限。"""
    if not isinstance(timeout_seconds, (int, float)) or isinstance(timeout_seconds, bool) or not 0 < timeout_seconds <= 605:
        raise RuntimeError("专用窗口等待须大于 0 且不超过 605 秒")
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
                        raise RuntimeError("剪贴板准备失败：" + document["error"])
                    raise RuntimeError("剪贴板无法可靠读取或超过验证上限，默认恢复验证中止")
                return document
        time.sleep(0.05)
    if operation == "focus":
        raise RuntimeError("专用窗口聚焦超时：请完成系统授权并保持窗口运行及可见")
    raise RuntimeError("剪贴板操作等待超时：专用窗口须获得焦点及桌面读取许可，且保持运行")


def sample_clipboard(output, fixture, label, baseline=None, original_baseline=None):
    """请求专用窗口重新读取系统剪贴板，只保存格式、长度和哈希。"""
    summary = fixture_request(output, fixture, "clipboard")["summary"]
    (output / ("clipboard-" + label + ".json")).write_text(json.dumps(summary, ensure_ascii=False, indent=2), encoding="utf-8")
    if baseline is not None and summary != baseline:
        raise RuntimeError("剪贴板格式、长度或哈希已变化：" + label)
    if original_baseline is not None:
        validate_original_formats(summary, original_baseline, allow_qt_alias=True)
        (output / ("clipboard-original-check-" + label + ".json")).write_text(json.dumps({"original_formats_preserved": True,
            "qt_derived_alias_present": summary != original_baseline}), encoding="utf-8")
    return summary


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--execute", action="store_true", help="显式启用真实桌面授权、截图及可选输入")
    parser.add_argument("--url", default="http://127.0.0.1:8080/mcp", help="已经启动的服务 MCP URL")
    parser.add_argument("--token-file", help="凭据文件；默认使用 REMOTE_MCP_TOKEN")
    parser.add_argument("--no-token", action="store_true", help="忽略环境 Token，使用匿名模式")
    parser.add_argument("--ca-file", help="服务证书的受信 CA 文件")
    parser.add_argument("--display-id", help="截图目标显示器；默认由服务选择")
    parser.add_argument("--output-dir", default="gui-evidence", help="截图与元数据保存目录")
    parser.add_argument("--input-plan", help="调用方准备的专用目标与输入计划 JSON，坐标相对首次截图")
    parser.add_argument("--fixture", action="store_true", help="启动可选 PySide6 专用窗口，自动定位并核验鼠标及中文")
    parser.add_argument("--fixture-inputs-only", action="store_true", help="仅专用窗口验证键鼠及清空固定字段；不调用文字工具或写剪贴板")
    parser.add_argument("--allow-clipboard-replace", action="store_true", help="仅用于专用窗口，明确允许无法保存时替换剪贴板")
    parser.add_argument("--refresh-clipboard-offer", action="store_true", help="专用窗口显式重新发布完全相同剪贴板数据，准备 Portal 初始格式通知")
    parser.add_argument("--desktop-label", default="未注明", help="记录桌面及其版本，如 GNOME 49 Wayland")
    parser.add_argument("--authorize-wait-seconds", type=authorize_wait, default=125, help="测试客户端授权等待上限，1至605秒；须配合服务授权期限")
    args = parser.parse_args()
    if not args.execute:
        parser.print_help()
        print("\n未执行：只有 --execute 才连接桌面；普通 CI 不调用真实 GUI。")
        return 0
    if args.no_token and args.token_file:
        parser.error("--no-token 与 --token-file 不能同时指定")
    if args.fixture and args.input_plan:
        parser.error("--fixture 与 --input-plan 不能同时指定")
    if args.fixture_inputs_only and (not args.fixture or args.refresh_clipboard_offer or args.allow_clipboard_replace):
        parser.error("--fixture-inputs-only 须与 --fixture 并用，不能刷新或允许替换剪贴板")
    if args.allow_clipboard_replace and not args.fixture:
        parser.error("--allow-clipboard-replace 仅用于 --fixture；自定义计划在 gui_text 参数声明")
    if args.refresh_clipboard_offer and (not args.fixture or args.allow_clipboard_replace):
        parser.error("--refresh-clipboard-offer 仅用于 --fixture 默认保留验证，不能与允许替换同时使用")
    plan = load_plan(args.input_plan)
    token = "" if args.no_token else os.environ.get("REMOTE_MCP_TOKEN", "")
    if args.token_file:
        token_file = Path(args.token_file)
        with token_file.open("rb") as credential:
            info = os.fstat(credential.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_size > 8192:
                raise RuntimeError("凭据文件必须是不超过 8 KiB 的普通文件")
            if os.name != "nt" and info.st_mode & 0o077:
                raise RuntimeError("凭据文件须仅当前账号可读")
            contents = credential.read(8193)
            if len(contents) > 8192:
                raise RuntimeError("凭据文件超过 8 KiB")
            token = contents.decode("utf-8").strip()
        if not token:
            raise RuntimeError("显式凭据文件不能为空")
    if token and (len(token) > 8192 or any(ord(character) < 33 or ord(character) > 126 for character in token)):
        raise RuntimeError("Token 格式无效")
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
              "checks": [], "submitted_operations": [], "pending": ["五类真实桌面分别验收", "实际 MCP 客户端图片展示", "多屏混合缩放、布局变化", "剪贴板恢复与竞争、撤销和断线"]}
    try:
        initialized = client.rpc("initialize", {"protocolVersion": "2025-11-25", "capabilities": {},
                                                "clientInfo": {"name": "independent-gui-smoke", "version": "1"}})
        if initialized.get("protocolVersion") != "2025-11-25":
            raise RuntimeError("MCP 协议版本不符")
        client.rpc("notifications/initialized", {}, notification=True)
        names = {item["name"] for item in client.rpc("tools/list", {}).get("tools", [])}
        if not GUI_TOOLS <= names:
            raise RuntimeError("GUI 工具注册不完整")
        report["checks"].append("独立 JSON-RPC 初始化和七个 GUI 工具发现")
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
                raise RuntimeError("专用窗口无法启动，请安装可选验证依赖 PySide6 并核对桌面")
            if args.fixture_inputs_only:
                initial = json.loads((output / "fixture.json").read_text(encoding="utf-8"))
                if initial.get("text") != "组合键验证":
                    raise RuntimeError("仅键鼠窗口缺少固定初始字段，不能核验实际清空")
                report["fixture_inputs_only"] = True
                report["pending"].append("本次未调用 gui_text；中文实际输入及默认剪贴板恢复待验收")
            elif not args.allow_clipboard_replace:
                clipboard_baseline = sample_clipboard(output, fixture, "baseline")
        opened, _ = client.tool("gui_open", {"request_id": "gui-smoke-" + uuid.uuid4().hex, "wait_ms": 10000})
        gui_id = opened["id"]
        deadline = time.monotonic() + args.authorize_wait_seconds
        while opened.get("state") == "authorizing" and time.monotonic() < deadline:
            time.sleep(1)
            opened, _ = client.tool("gui_status", {"id": gui_id})
        if opened.get("state") != "ready":
            raise RuntimeError("图形会话未进入 ready，需核对目标机授权")
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
            report["checks"].append("原格式字节摘要一致；" + ("新增 Qt 派生 charset 别名" if alias_added else "无新增格式") + "；未允许替换")
            report["qt_derived_alias_added"] = alias_added
            report["clipboard_offer_refreshed"] = True
            # 让 Portal 异步格式通知抵达服务；有界等待后仍由默认保护作最终判断。
            offer_deadline = time.monotonic() + 2
            while time.monotonic() < offer_deadline:
                status, _ = client.tool("gui_status", {"id": gui_id})
                reason = status.get("capabilities", {}).get("reasons", {}).get("text", "")
                if "尚未提供当前剪贴板格式" not in reason:
                    break
                time.sleep(0.05)
        capture = screenshot(client, gui_id, args.display_id, output, "before")
        report["checks"].append("标准 MCP 图片、PNG 扫描行、坐标与采集元数据")
        if args.fixture:
            point = fixture_points((output / "before.png").read_bytes())
            plan, fixture_expected = fixture_plan(point, args.allow_clipboard_replace, inputs_only=args.fixture_inputs_only)
        if plan:
            verification = plan.get("verify_text_file")
            if verification and Path(verification["path"]).exists():
                raise RuntimeError("文字验收文件必须事先不存在，避免读取旧产物")
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
                    report["checks"].append("文本输入后原剪贴板各格式长度和哈希一致")
                screenshot(client, gui_id, args.display_id, output, "after-" + str(index + 1))
            if verification:
                evidence = Path(verification["path"])
                deadline = time.monotonic() + 10
                while not evidence.exists() and time.monotonic() < deadline:
                    time.sleep(0.1)
                if evidence.stat().st_size > 1024 * 1024 or evidence.read_text(encoding="utf-8") != verification["expected"]:
                    raise RuntimeError("目标应用保存的 UTF-8 内容与预期不符")
                report["checks"].append("目标应用实际保存的 UTF-8 文本与预期一致")
            elif not args.fixture:
                report["pending"].append("输入仅核对事件结果；目标应用文字与鼠标行为需人工检查")
            if args.fixture:
                deadline = time.monotonic() + 5
                while True:
                    fixture_result = json.loads((output / "fixture.json").read_text(encoding="utf-8"))
                    complete = fixture_verified(fixture_result, fixture_expected)
                    if complete or time.monotonic() >= deadline:
                        break
                    time.sleep(0.1)
                if not complete:
                    raise RuntimeError("专用窗口实际鼠标事件或中文内容未通过核验")
                if (os.environ.get("XDG_SESSION_TYPE") == "wayland" or os.environ.get("WAYLAND_DISPLAY")) and fixture_result["platform"] != "wayland":
                    raise RuntimeError("Wayland 原生验收不能用 XWayland 窗口替代")
                if args.fixture_inputs_only:
                    report["checks"].append("仅键鼠：原生窗口点击、双击、拖拽、滚动、全选及删除组合键，密码回显字段实际清空；测试未调用剪贴板读写或选区复制")
                else:
                    report["checks"].append("原生专用窗口的点击、双击、拖拽、滚动、全选组合键和实际中文内容")
                report["fixture_platform"] = fixture_result["platform"]
        else:
            report["pending"].append("未提供输入计划，鼠标键盘与中文实际输入待验证")
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
                    raise RuntimeError("图形会话未可靠关闭")
                close_confirmed = True
                report["checks"].append("图形会话关闭和重复关闭")
                if clipboard_baseline is not None:
                    sample_clipboard(output, fixture, "after-close", clipboard_baseline, original_clipboard_baseline)
                    report["checks"].append("图形会话关闭后原剪贴板各格式仍可读取且哈希一致")
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
                            report["pending"].append("验证窗口保留有界原剪贴板内存备份；请用户确认恢复后再关闭")
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
        print("GUI 验证失败：" + (str(error) if isinstance(error, RuntimeError) else type(error).__name__), file=sys.stderr)
        sys.exit(1)
