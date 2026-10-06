"""真实 GUI 验证入口的离线边界回归，不连接或控制用户桌面。"""
import importlib.util
import json
from pathlib import Path
import struct
import subprocess
import sys
import tempfile
import unittest
from unittest import mock
import zlib


spec = importlib.util.spec_from_file_location("gui_smoke", Path(__file__).with_name("gui-smoke.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


WORKSPACE_TEMP = Path(__file__).resolve().parents[1] / ".tmp"
WORKSPACE_TEMP.mkdir(mode=0o700, exist_ok=True)


def png(width, height, pixel):
    def chunk(kind, data):
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data) & 0xffffffff)

    rows = b"".join(b"\x00" + b"".join(bytes(pixel(x, y)) for x in range(width)) for y in range(height))
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(rows)) + chunk(b"IEND", b"")


class GUISmokeTest(unittest.TestCase):
    def test_authorization_wait_is_bounded(self):
        self.assertEqual(module.authorize_wait("605"), 605)
        self.assertEqual(module.authorize_wait("1"), 1)
        for value in ["0", "606", "invalid", "1.5"]:
            with self.assertRaises(module.argparse.ArgumentTypeError):
                module.authorize_wait(value)

    def test_gui_error_preserves_only_bounded_contract_message(self):
        client = module.Client.__new__(module.Client)
        message = "Clipboard input is not supported by the Windows backend"
        response = {"isError": True, "structuredContent": {"code": "unsupported", "message": message}}
        client.rpc = lambda *args: response
        with self.assertRaisesRegex(RuntimeError, message):
            client.tool("gui_text", {})
        for name, invalid in [("file_stat", message), ("gui_text", "x" * 513), ("gui_text", "含控制符\n不能转发")]:
            response["structuredContent"]["message"] = invalid
            with self.assertRaises(RuntimeError) as captured:
                client.tool(name, {})
            self.assertNotIn(invalid, str(captured.exception))
        # 已删除的 Linux 剪贴板错误码不再获得固定消息白名单待遇。
        for code in ["clipboard_preservation_unavailable", "clipboard_restore_failed"]:
            response["structuredContent"] = {"code": code, "message": "untrusted removed-backend message"}
            with self.assertRaises(RuntimeError) as captured:
                client.tool("gui_text", {})
            self.assertNotIn("untrusted removed-backend message", str(captured.exception))
        response["structuredContent"] = []
        with self.assertRaisesRegex(RuntimeError, "no structured result"):
            client.tool("gui_text", {})

    def test_no_execute_has_no_network_or_desktop_effect(self):
        result = subprocess.run([sys.executable, str(Path(__file__).with_name("gui-smoke.py")), "--url", "invalid-url"],
                                capture_output=True, text=True, encoding="utf-8", timeout=5)
        self.assertEqual(result.returncode, 0)
        self.assertIn("Not executed", result.stdout)

    def test_png_validates_data_and_dimensions(self):
        data = png(5, 3, lambda x, y: (x, y, 7))
        self.assertEqual(module.decode_png(data), (5, 3))
        broken = bytearray(data)
        broken[-17] ^= 1
        with self.assertRaises(RuntimeError):
            module.decode_png(broken)
        with self.assertRaises(RuntimeError):
            module.decode_png(data[:-1])

    def test_plan_requires_explicit_target_and_owned_ids(self):
        with tempfile.TemporaryDirectory(dir=WORKSPACE_TEMP) as directory:
            path = Path(directory) / "plan.json"
            invalid = {"operations": [{"tool": "gui_key", "arguments": {"keys": ["A"]}}]}
            path.write_text(json.dumps(invalid), encoding="utf-8")
            with self.assertRaises(RuntimeError):
                module.load_plan(path)
            invalid["target"] = "专用编辑器"
            invalid["operations"][0]["arguments"]["id"] = "unowned"
            path.write_text(json.dumps(invalid), encoding="utf-8")
            with self.assertRaises(RuntimeError):
                module.load_plan(path)
            del invalid["operations"][0]["arguments"]["id"]
            path.write_text(json.dumps(invalid), encoding="utf-8")
            self.assertEqual(module.load_plan(path)["target"], "专用编辑器")

    def test_removed_linux_options_are_rejected_without_connecting(self):
        for option in ["--fixture", "--fixture-inputs-only", "--refresh-clipboard-offer", "--allow-clipboard-replace"]:
            with self.subTest(option=option), mock.patch.object(sys, "argv", ["gui-smoke.py", "--execute", option]), mock.patch.object(module, "Client") as client, mock.patch("sys.stderr"):
                with self.assertRaises(SystemExit) as captured:
                    module.main()
                self.assertEqual(captured.exception.code, 2)
                client.assert_not_called()

    def test_help_has_windows_scope_and_no_qt_requirements(self):
        result = subprocess.run([sys.executable, str(Path(__file__).with_name("gui-smoke.py")), "--help"],
                                capture_output=True, text=True, encoding="utf-8", timeout=5)
        self.assertEqual(result.returncode, 0)
        self.assertIn("Windows", result.stdout)
        for removed in ["Wayland", "Portal", "PySide6", "--fixture", "--refresh-clipboard-offer"]:
            self.assertNotIn(removed, result.stdout)

    def run_mock(self, output, *, backend="windows", tools=None, fail_capture=False, fail_close=False, plan=None):
        calls = []

        class Client:
            def __init__(self, *args):
                pass

            def rpc(self, method, *args, **kwargs):
                if method == "initialize":
                    return {"protocolVersion": "2025-11-25"}
                if method == "tools/list":
                    return {"tools": [{"name": name} for name in (module.GUI_TOOLS if tools is None else tools)]}
                return {}

            def tool(self, name, arguments):
                calls.append((name, arguments))
                if name == "gui_status":
                    return {"backend": backend}, {}
                if name == "gui_open":
                    return {"id": "test-session", "state": "ready"}, {}
                if name == "gui_close":
                    if fail_close:
                        raise RuntimeError("Session cleanup failed")
                    return {"state": "closed"}, {}
                return {"submitted": True}, {}

        def screenshot(client, gui_id, display_id, directory, label):
            if fail_capture:
                raise RuntimeError("Screenshot validation failed")
            return {"capture_id": "test-capture"}

        argv = ["gui-smoke.py", "--execute", "--no-token", "--output-dir", str(output)]
        if plan is not None:
            argv.extend(["--input-plan", str(plan)])
        with mock.patch.object(sys, "argv", argv), mock.patch.object(module, "Client", Client), mock.patch.object(module, "screenshot", side_effect=screenshot), mock.patch.object(module.platform, "platform", return_value="Linux-host"), mock.patch("builtins.print") as printed:
            try:
                result = module.main()
            except Exception as error:
                return calls, error, printed
            return calls, result, printed

    def test_linux_host_can_validate_remote_windows_and_repeated_close(self):
        with tempfile.TemporaryDirectory(dir=WORKSPACE_TEMP) as directory:
            output = Path(directory) / "evidence"
            calls, result, _ = self.run_mock(output)
            self.assertEqual(result, 0)
            self.assertEqual([name for name, _ in calls], ["gui_status", "gui_open", "gui_close", "gui_close"])
            report = json.loads((output / "report.json").read_text(encoding="utf-8"))
            self.assertTrue(report["run_completed"])
            self.assertFalse(report["acceptance_complete"])
            self.assertEqual(report["host_platform"], "Linux-host")
            self.assertEqual(report["target_platform"], "windows")
            self.assertFalse(any("Wayland" in pending or "X11" in pending for pending in report["pending"]))

    def test_non_windows_or_missing_tools_stop_before_gui_open(self):
        for backend, tools in [("wayland", None), ("windows", {"file_stat", "process_start"})]:
            with self.subTest(backend=backend, tools=tools), tempfile.TemporaryDirectory(dir=WORKSPACE_TEMP) as directory:
                output = Path(directory) / "evidence"
                calls, result, printed = self.run_mock(output, backend=backend, tools=tools)
                self.assertIsInstance(result, RuntimeError)
                self.assertFalse(any(name == "gui_open" for name, _ in calls))
                printed.assert_not_called()
                self.assertFalse(json.loads((output / "report.json").read_text(encoding="utf-8"))["run_completed"])

    def test_explicit_plan_preserves_chinese_text_and_uses_owned_capture(self):
        with tempfile.TemporaryDirectory(dir=WORKSPACE_TEMP) as directory:
            plan = Path(directory) / "plan.json"
            plan.write_text(json.dumps({"target": "专用测试窗口", "operations": [
                {"tool": "gui_mouse", "arguments": {"action": "click", "x": 10, "y": 20}},
                {"tool": "gui_text", "arguments": {"text": "中文🙂", "mode": "direct"}},
                {"tool": "gui_key", "arguments": {"keys": ["Ctrl", "A"]}}
            ]}), encoding="utf-8")
            output = Path(directory) / "evidence"
            calls, result, _ = self.run_mock(output, plan=plan)
            self.assertEqual(result, 0)
            mouse = next(arguments for name, arguments in calls if name == "gui_mouse")
            self.assertEqual(mouse["id"], "test-session")
            self.assertEqual(mouse["capture_id"], "test-capture")
            text = next(arguments for name, arguments in calls if name == "gui_text")
            self.assertEqual(text["text"], "中文🙂")
            self.assertEqual(text["mode"], "direct")
            report = json.loads((output / "report.json").read_text(encoding="utf-8"))
            self.assertEqual(len(report["submitted_operations"]), 3)
            self.assertFalse(report["acceptance_complete"])

    def test_cleanup_failure_keeps_primary_failure_and_no_success(self):
        for primary in [False, True]:
            with self.subTest(primary=primary), tempfile.TemporaryDirectory(dir=WORKSPACE_TEMP) as directory:
                output = Path(directory) / "evidence"
                calls, result, printed = self.run_mock(output, fail_capture=primary, fail_close=True)
                self.assertEqual(str(result), "Screenshot validation failed" if primary else "Session cleanup failed")
                printed.assert_not_called()
                report = json.loads((output / "report.json").read_text(encoding="utf-8"))
                self.assertFalse(report["run_completed"])
                self.assertEqual(report["cleanup_failures"], ["Session cleanup failed"])
                if primary:
                    self.assertEqual(report["failure"], "Screenshot validation failed")

    def test_screenshot_checks_single_png_dimensions_and_coordinate_metadata(self):
        data = png(5, 3, lambda x, y: (x, y, 7))
        capture = {"capture_id": "c", "display_id": "d", "width": 5, "height": 3, "region": {},
                   "logical_bounds": {}, "layout_generation": 1, "captured_at": "now", "captured_at_source": "capture",
                   "frame_sequence": 1, "freshness": "captured"}
        import base64
        image = {"type": "image", "mimeType": "image/png", "data": base64.b64encode(data).decode("ascii")}
        client = mock.Mock()
        with tempfile.TemporaryDirectory(dir=WORKSPACE_TEMP) as directory:
            output = Path(directory)
            client.tool.return_value = ({"capture": capture}, {"content": [image]})
            self.assertEqual(module.screenshot(client, "s", "d", output, "valid"), capture)
            self.assertEqual((output / "valid.png").read_bytes(), data)
            for metadata, images in [(capture, [image, image]), (dict(capture, width=6), [image]),
                                     ({key: value for key, value in capture.items() if key != "capture_id"}, [image])]:
                client.tool.return_value = (metadata, {"content": images})
                with self.assertRaises(RuntimeError):
                    module.screenshot(client, "s", None, output, "invalid")
            self.assertFalse((output / "invalid.png").exists())


if __name__ == "__main__":
    unittest.main()
