"""真实 GUI 验证入口的离线边界回归，不连接或控制用户桌面。"""
import importlib.util
import hashlib
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
clipboard_spec = importlib.util.spec_from_file_location("gui_clipboard", Path(__file__).with_name("gui_clipboard.py"))
clipboard_module = importlib.util.module_from_spec(clipboard_spec)
clipboard_spec.loader.exec_module(clipboard_module)


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
        message = "Portal 尚未提供可靠的剪贴板所有权及格式快照"
        response = {"isError": True, "structuredContent": {"code": "clipboard_preservation_unavailable", "message": message}}
        client.rpc = lambda *args: response
        with self.assertRaisesRegex(RuntimeError, message):
            client.tool("gui_text", {})
        for name, invalid in [("file_stat", message), ("gui_text", "x" * 513), ("gui_text", "含控制符\n不能转发")]:
            response["structuredContent"]["message"] = invalid
            with self.assertRaises(RuntimeError) as captured:
                client.tool(name, {})
            self.assertNotIn(invalid, str(captured.exception))
        response["structuredContent"] = []
        with self.assertRaisesRegex(RuntimeError, "缺少结构化结果"):
            client.tool("gui_text", {})

    def test_no_execute_has_no_network_or_desktop_effect(self):
        result = subprocess.run([sys.executable, str(Path(__file__).with_name("gui-smoke.py")), "--url", "invalid-url"],
                                capture_output=True, text=True, encoding="utf-8", timeout=5)
        self.assertEqual(result.returncode, 0)
        self.assertIn("未执行", result.stdout)

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
        with tempfile.TemporaryDirectory() as directory:
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

    def test_fixture_points_bind_visible_markers(self):
        colors = [(241, 13, 78), (23, 214, 185), (217, 74, 7), (73, 60, 241)]
        origins = [(30, 40), (610, 40), (30, 440), (610, 440)]

        def pixel(x, y):
            for index, (left, top) in enumerate(origins):
                if left <= x < left + 20 and top <= y < top + 20:
                    return colors[index]
            return (230, 230, 230)

        point = module.fixture_points(png(660, 480, pixel))
        self.assertEqual(point(30, 30), {"x": 39.5, "y": 49.5})
        self.assertEqual(point(320, 130), {"x": 329.5, "y": 149.5})
        with self.assertRaises(RuntimeError):
            module.fixture_points(png(30, 30, lambda x, y: colors[0]))

    def test_fixture_requires_actual_text_and_target_events(self):
        events = [{"type": kind, "x": x, "y": y, "delta_y": 120} for kind, x, y in
                  [("press", 150, 260), ("release", 150, 260), ("double_click", 320, 260), ("drag", 500, 320), ("scroll", 400, 260)]]
        result = {"text": "中文验证", "events": events}
        self.assertTrue(module.fixture_verified(result, "中文验证"))
        result["text"] = "旧内容中文验证"
        self.assertFalse(module.fixture_verified(result, "中文验证"))
        result["text"] = "中文验证"
        events[2]["x"] = 10
        self.assertFalse(module.fixture_verified(result, "中文验证"))

    def test_inputs_only_plan_never_calls_text_or_prepares_clipboard(self):
        plan, expected = module.fixture_plan(lambda x, y: {"x": x, "y": y}, False, inputs_only=True)
        self.assertEqual(expected, "")
        self.assertEqual({operation["tool"] for operation in plan["operations"]}, {"gui_key", "gui_mouse"})
        self.assertEqual([operation["arguments"]["keys"] for operation in plan["operations"] if operation["tool"] == "gui_key"],
                         [["Meta" if sys.platform == "darwin" else "Ctrl", "A"], ["Backspace"]])
        for arguments in [["--fixture-inputs-only"], ["--fixture", "--fixture-inputs-only", "--refresh-clipboard-offer"],
                          ["--fixture", "--fixture-inputs-only", "--allow-clipboard-replace"]]:
            result = subprocess.run([sys.executable, str(Path(__file__).with_name("gui-smoke.py")), "--execute", *arguments],
                                    capture_output=True, text=True, encoding="utf-8", timeout=5)
            self.assertEqual(result.returncode, 2)

    def test_inputs_only_run_verifies_empty_field_and_retains_text_pending(self):
        calls = []
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "evidence"
            evidence = output / "fixture.json"
            fixture = mock.Mock()
            fixture.poll.return_value = None

            def start_fixture(arguments, **kwargs):
                self.assertIn("--inputs-only", arguments)
                evidence.write_text(json.dumps({"platform": "offscreen", "text": "组合键验证", "events": []}))
                return fixture

            class Client:
                def __init__(self, *args):
                    pass

                def rpc(self, method, *args, **kwargs):
                    if method == "initialize":
                        return {"protocolVersion": "2025-11-25"}
                    if method == "tools/list":
                        return {"tools": [{"name": name} for name in module.GUI_TOOLS]}
                    return {}

                def tool(self, name, arguments):
                    calls.append(name)
                    if name == "gui_open":
                        return {"id": "fixture-session", "state": "ready"}, {}
                    if name == "gui_close":
                        return {"state": "closed"}, {}
                    if name == "gui_key" and arguments["keys"] == ["Backspace"]:
                        events = [{"type": kind, "x": x, "y": y, "delta_y": 120} for kind, x, y in
                                  [("press", 150, 260), ("release", 150, 260), ("double_click", 320, 260), ("drag", 500, 320), ("scroll", 400, 260)]]
                        evidence.write_text(json.dumps({"platform": "offscreen", "text": "", "events": events}))
                    return {"submitted": True}, {}

            def screenshot(client, gui_id, display_id, output, label):
                (output / (label + ".png")).write_bytes(b"mock-image")
                return {"capture_id": "capture"}

            argv = ["gui-smoke.py", "--execute", "--fixture", "--fixture-inputs-only", "--no-token", "--authorize-wait-seconds", "605", "--output-dir", str(output)]
            with mock.patch.object(sys, "argv", argv), mock.patch.object(module.os, "environ", {}), \
                 mock.patch.object(module, "Client", Client), mock.patch.object(module.subprocess, "Popen", side_effect=start_fixture), \
                 mock.patch.object(module, "sample_clipboard", side_effect=AssertionError("仅键鼠不得读取剪贴板")), \
                 mock.patch.object(module, "fixture_request", return_value={"ok": True}) as request, \
                 mock.patch.object(module, "screenshot", side_effect=screenshot), \
                 mock.patch.object(module, "fixture_points", return_value=lambda x, y: {"x": x, "y": y}), mock.patch("builtins.print"):
                self.assertEqual(module.main(), 0)
            self.assertNotIn("gui_text", calls)
            self.assertEqual(request.call_args_list[0], mock.call(output, fixture, "focus", timeout_seconds=605))
            self.assertTrue(all(call == mock.call(output, fixture, "focus") for call in request.call_args_list[1:]))
            report = json.loads((output / "report.json").read_text())
            self.assertTrue(report["run_completed"])
            self.assertFalse(report["acceptance_complete"])
            self.assertTrue(report["fixture_inputs_only"])
            self.assertTrue(any("中文实际输入及默认剪贴板恢复待验收" in pending for pending in report["pending"]))
            fixture.terminate.assert_called_once()

    def test_clipboard_summary_keeps_only_hashes_and_limits_formats(self):
        secret = "原内容不得记录".encode("utf-8")
        summary = clipboard_module.summarize(["text/plain"], lambda _: secret)
        self.assertEqual(summary["formats"], [{"mime": "text/plain", "length": len(secret), "sha256": hashlib.sha256(secret).hexdigest()}])
        self.assertNotIn(secret.decode("utf-8"), json.dumps(summary, ensure_ascii=False))
        reads = []
        with self.assertRaises(RuntimeError):
            clipboard_module.summarize([str(i) for i in range(65)], lambda mime: reads.append(mime))
        self.assertEqual(reads, [])
        with self.assertRaises(RuntimeError):
            clipboard_module.summarize(["a", "b"], lambda _: b"123", max_bytes=5)
        self.assertEqual(clipboard_module.summarize([], lambda _: self.fail("空剪贴板不读取格式")), {"formats": [], "total_bytes": 0})

    def test_clipboard_sample_requires_fresh_matching_hashes(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            baseline = {"formats": [], "total_bytes": 0}

            class Fixture:
                summary = baseline

                def poll(self):
                    request = json.loads((output / "clipboard-request.json").read_text(encoding="utf-8"))
                    (output / "clipboard-response.json").write_text(json.dumps({"id": request["id"], "ok": True, "summary": self.summary}), encoding="utf-8")
                    return None

            fixture = Fixture()
            self.assertEqual(module.sample_clipboard(output, fixture, "baseline"), baseline)
            self.assertEqual(module.sample_clipboard(output, fixture, "after-close", baseline), baseline)
            fixture.summary = {"formats": [{"mime": "text/plain", "length": 1, "sha256": "changed"}], "total_bytes": 1}
            with self.assertRaisesRegex(RuntimeError, "哈希已变化"):
                module.sample_clipboard(output, fixture, "after-text", baseline)

    def test_clipboard_offer_refresh_requires_identical_stable_snapshot(self):
        original = {"text/plain": "原内容只留内存".encode("utf-8"), "application/example": b"\x00\x01"}
        baseline, copied = clipboard_module.copy_snapshot(list(original), original.__getitem__)
        published = []
        snapshot = lambda: (baseline, copied, 7)
        self.assertEqual(clipboard_module.republish_same(snapshot, lambda data: published.append(data), baseline), baseline)
        self.assertEqual(published, [original])
        self.assertNotIn("原内容只留内存", json.dumps(baseline, ensure_ascii=False))
        for reads in [[(baseline, copied, 7), (baseline, copied, 8)],
                      [(baseline, copied, 7), ({"formats": [], "total_bytes": 0}, {}, 7)]]:
            published.clear()
            with self.assertRaises(RuntimeError):
                clipboard_module.republish_same(iter(reads).__next__, lambda data: published.append(data), baseline)
            self.assertEqual(published, [])
        published.clear()
        with self.assertRaises(RuntimeError):
            clipboard_module.republish_same(mock.Mock(side_effect=RuntimeError("失焦或读取不可靠")), lambda data: published.append(data), baseline)
        self.assertEqual(published, [])
        with self.assertRaises(RuntimeError):
            clipboard_module.copy_snapshot(list(original), original.__getitem__, max_bytes=1)
        with self.assertRaises(RuntimeError):
            clipboard_module.copy_snapshot([str(i) for i in range(65)], lambda _: self.fail("格式超限不能读取"))

    def test_clipboard_offer_refresh_rejects_post_publish_difference(self):
        baseline = {"formats": [], "total_bytes": 0}
        reads = iter([(baseline, {}, 1), (baseline, {}, 1), ({"formats": [{"mime": "new"}], "total_bytes": 1}, {}, 2)])
        published = []
        with self.assertRaisesRegex(RuntimeError, "refresh_post_changed"):
            clipboard_module.republish_same(reads.__next__, lambda data: published.append(data), baseline)
        self.assertEqual(published, [{}])

    def test_clipboard_refresh_accepts_only_qt_equal_charset_alias(self):
        # 复现 KDE 第五轮的四格式各14字节，经 Qt 发布成为五格式的情形。
        data = {mime: b"x" * 14 for mime in ["STRING", "TEXT", "UTF8_STRING", "text/plain"]}
        original, contents = clipboard_module.copy_snapshot(list(data), data.__getitem__)
        offered = dict(data, **{clipboard_module.UTF8_ALIAS: b"x" * 14})
        refreshed = clipboard_module.summarize(list(offered), offered.__getitem__)
        reads = iter([(original, contents, 1), (original, contents, 1), (refreshed, offered, 2)])
        published = []
        self.assertEqual(clipboard_module.republish_same(reads.__next__, published.append, original), refreshed)
        self.assertEqual(published, [data])
        self.assertTrue(clipboard_module.validate_original_formats(refreshed, original, True))
        self.assertEqual(refreshed["total_bytes"], 70)
        with self.assertRaises(RuntimeError):
            clipboard_module.validate_original_formats(refreshed, original)
        for changed in [dict(offered, **{"text/html": b"x" * 14}),
                        dict(offered, **{clipboard_module.UTF8_ALIAS: b"y" * 14}),
                        dict(offered, **{"TEXT": b"y" * 14}),
                        {mime: value for mime, value in offered.items() if mime != "STRING"}]:
            summary = clipboard_module.summarize(list(changed), changed.__getitem__)
            with self.assertRaisesRegex(RuntimeError, "refresh_post_changed"):
                clipboard_module.validate_original_formats(summary, original, True)
        # 原本就有 charset 格式时仍必须保留，不能套用新增别名例外。
        with self.assertRaises(RuntimeError):
            clipboard_module.validate_original_formats(original, refreshed, True)

    def test_clipboard_original_baseline_is_independently_retained(self):
        original = clipboard_module.summarize(["text/plain"], lambda _: b"same")
        refreshed = clipboard_module.summarize(["text/plain", clipboard_module.UTF8_ALIAS], lambda _: b"same")
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            with mock.patch.object(module, "fixture_request", return_value={"summary": refreshed}):
                self.assertEqual(module.sample_clipboard(output, None, "after-close", refreshed, original), refreshed)
            check = json.loads((output / "clipboard-original-check-after-close.json").read_text())
            self.assertEqual(check, {"original_formats_preserved": True, "qt_derived_alias_present": True})

    def test_clipboard_refresh_errors_identify_safe_stage(self):
        baseline = {"formats": [], "total_bytes": 0}
        snapshot = lambda: (baseline, {}, 1)
        with self.assertRaisesRegex(RuntimeError, "refresh_pre_read_unreliable"):
            clipboard_module.republish_same(mock.Mock(side_effect=RuntimeError("不能泄露读取原因")), mock.Mock(), baseline)
        with self.assertRaisesRegex(RuntimeError, "refresh_publish_failed"):
            clipboard_module.republish_same(snapshot, mock.Mock(side_effect=RuntimeError("不能泄露发布原因")), baseline)
        reads = mock.Mock(side_effect=[snapshot(), snapshot(), RuntimeError("不能泄露原内容")])
        with self.assertRaisesRegex(RuntimeError, "refresh_post_read_unreliable"):
            clipboard_module.republish_same(reads, mock.Mock(), baseline)
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)

            class Fixture:
                def poll(self):
                    request = json.loads((output / "clipboard-request.json").read_text())
                    (output / "clipboard-response.json").write_text(json.dumps({"id": request["id"], "ok": False,
                        "error": "refresh_post_changed"}))
                    return None

            with self.assertRaisesRegex(RuntimeError, "refresh_post_changed"):
                module.fixture_request(output, Fixture(), "refresh_offer", baseline)

    def test_clipboard_rescue_never_infers_external_owner_from_equal_text(self):
        original, data = clipboard_module.copy_snapshot(["text/plain"], lambda _: b"original")
        current = clipboard_module.summarize(["text/plain"], lambda _: b"test text")
        hashes = [{"length": 9, "sha256": hashlib.sha256(b"test text").hexdigest()}]
        publish = mock.Mock()
        self.assertEqual(clipboard_module.rescue_snapshot(lambda: (current, {}, 2), publish, data, original, hashes, lambda: False), "needs_user")
        publish.assert_not_called()
        user_new = clipboard_module.summarize(["text/plain"], lambda _: b"user new content")
        self.assertEqual(clipboard_module.rescue_snapshot(lambda: (user_new, {}, 2), publish, data, original, hashes, lambda: True), "needs_user")
        publish.assert_not_called()
        changed = iter([(current, {}, 2), (current, {}, 3)])
        self.assertEqual(clipboard_module.rescue_snapshot(changed.__next__, publish, data, original, hashes, lambda: True), "needs_user")
        publish.assert_not_called()
        reads = iter([(current, {}, 2), (current, {}, 2), (original, {}, 3)])
        self.assertEqual(clipboard_module.rescue_snapshot(reads.__next__, publish, data, original, hashes, lambda: True), "rescued")
        publish.assert_called_once_with(data)

    def test_clipboard_rescue_preserved_data_does_not_publish(self):
        baseline, data = clipboard_module.copy_snapshot(["text/plain"], lambda _: b"original")
        publish = mock.Mock()
        self.assertEqual(clipboard_module.rescue_snapshot(lambda: (baseline, data, 1), publish, data, baseline, [], lambda: False), "original_preserved")
        publish.assert_not_called()
        with self.assertRaisesRegex(RuntimeError, "rescue_read_unreliable"):
            clipboard_module.rescue_snapshot(mock.Mock(side_effect=RuntimeError("读取原内容异常")), publish, data, baseline, [], lambda: True)
        publish.assert_not_called()

    def test_clipboard_control_marker_never_republishes_automatically(self):
        data = {"text/plain": b"original", clipboard_module.KDE_CONTROL_MIME: b"1"}
        baseline, copied = clipboard_module.copy_snapshot(list(data), data.__getitem__)
        publish = mock.Mock()
        with self.assertRaisesRegex(RuntimeError, "refresh_control_format_unsupported"):
            clipboard_module.republish_same(lambda: (baseline, copied, 1), publish, baseline)
        publish.assert_not_called()
        current = clipboard_module.summarize(["text/plain"], lambda _: b"test text")
        hashes = [{"length": 9, "sha256": hashlib.sha256(b"test text").hexdigest()}]
        with self.assertRaisesRegex(RuntimeError, "rescue_control_format_unsupported"):
            clipboard_module.rescue_snapshot(lambda: (current, {}, 2), publish, copied, baseline, hashes, lambda: True)
        publish.assert_not_called()

    def test_qt_provider_reference_in_isolated_offscreen_process(self):
        # 可选Qt验证只连接offscreen平台，绝不读写宿主Wayland/X11剪贴板。
        if importlib.util.find_spec("PySide6") is None:
            self.skipTest("可选PySide6未安装")
        script = '''
import gc, tempfile
from pathlib import Path
from unittest import mock
from PySide6.QtCore import Qt
from PySide6.QtTest import QTest
from PySide6.QtWidgets import QApplication, QLineEdit
import shiboken6
from gui_fixture import Target
from gui_clipboard import KDE_CONTROL_MIME
app = QApplication([])
assert app.platformName() == "offscreen"
with tempfile.TemporaryDirectory() as directory:
    inputs = Target(Path(directory) / "inputs.json", inputs_only=True)
    assert inputs.field.echoMode() == QLineEdit.EchoMode.Password
    app.clipboard().setText("isolated-baseline")
    QTest.keyClick(inputs.field, Qt.Key.Key_A, Qt.KeyboardModifier.ControlModifier)
    QTest.keyClick(inputs.field, Qt.Key.Key_Backspace)
    assert inputs.field.text() == ""
    assert app.clipboard().text() == "isolated-baseline"
    target = Target(Path(directory) / "fixture.json")
    target.field.setText("可见中文")
    QTest.keyClick(target.field, Qt.Key.Key_A, Qt.KeyboardModifier.ControlModifier)
    assert target.field.selectedText() == "可见中文"
    assert target.field.echoMode() == QLineEdit.EchoMode.Normal
    assert app.clipboard().text() == "isolated-baseline"
    target.publish_original_offer({"application/example": b"test"})
    offered = target.offered_data
    gc.collect()
    assert offered is not None and shiboken6.isValid(offered)
    assert not shiboken6.ownedByPython(offered)
    assert len(offered.data("application/example")) == 4
    clipboard = target.clipboard
    target.clipboard = mock.Mock()
    target.clipboard.ownsClipboard.return_value = True
    target.release_offered_data()
    assert target.offered_data is offered
    target.clipboard.ownsClipboard.return_value = False
    backup = target.original_snapshot
    target.original_snapshot = None
    target.release_offered_data()
    assert target.offered_data is offered
    target.original_snapshot = backup
    target.release_offered_data()
    assert target.offered_data is None and target.original_snapshot == backup
    try:
        target.publish_clipboard_snapshot({KDE_CONTROL_MIME: b"1"})
    except RuntimeError:
        pass
    else:
        raise AssertionError("控制格式未被拒绝")
    target.clipboard.setMimeData.assert_not_called()
    target.clipboard = clipboard
    clipboard.clear()
'''
        environment = module.os.environ.copy()
        environment["QT_QPA_PLATFORM"] = "offscreen"
        result = subprocess.run([sys.executable, "-c", script], cwd=Path(__file__).parent, env=environment,
                                capture_output=True, text=True, encoding="utf-8", timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_failed_text_keeps_fixture_memory_backup_for_user(self):
        baseline = {"formats": [], "total_bytes": 0}

        class Client:
            def __init__(self, *args):
                pass

            def rpc(self, method, *args, **kwargs):
                if method == "initialize":
                    return {"protocolVersion": "2025-11-25"}
                if method == "tools/list":
                    return {"tools": [{"name": name} for name in module.GUI_TOOLS]}
                return {}

            def tool(self, name, *args):
                if name == "gui_text":
                    raise RuntimeError("默认恢复失败")
                if name == "gui_open":
                    return {"id": "fixture-session", "state": "ready"}, {}
                if name == "gui_close":
                    return {"state": "closed"}, {}
                return {"submitted": True}, {}

        fixture = mock.Mock()
        fixture.poll.return_value = None
        fixture.pid = 123

        def start_fixture(arguments, **kwargs):
            Path(arguments[-1]).write_text(json.dumps({"platform": "wayland", "text": "", "events": []}))
            return fixture

        def screenshot(client, gui_id, display_id, output, label):
            (output / (label + ".png")).write_bytes(b"mock-image")
            return {"capture_id": "capture"}

        def request(output, process, operation, baseline=None, **kwargs):
            return {"summary": baseline, "recovery": "needs_user", "ok": True}

        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "evidence"
            argv = ["gui-smoke.py", "--execute", "--no-token", "--fixture", "--refresh-clipboard-offer", "--output-dir", str(output)]
            with mock.patch.object(sys, "argv", argv), mock.patch.object(module, "Client", Client), \
                 mock.patch.object(module.subprocess, "Popen", side_effect=start_fixture), \
                 mock.patch.object(module, "sample_clipboard", return_value=baseline), \
                 mock.patch.object(module, "fixture_request", side_effect=request), \
                 mock.patch.object(module, "screenshot", side_effect=screenshot), \
                 mock.patch.object(module, "fixture_points", return_value=lambda x, y: {"x": x, "y": y}):
                with self.assertRaisesRegex(RuntimeError, "默认恢复失败"):
                    module.main()
            report = json.loads((output / "report.json").read_text())
            self.assertEqual(report["clipboard_recovery"], "needs_user")
            self.assertTrue(report["fixture_retained"])
            self.assertEqual(report["fixture_pid"], 123)
            fixture.terminate.assert_not_called()
            fixture.kill.assert_not_called()
            fixture.wait.assert_not_called()

    def test_cleanup_failure_keeps_primary_failure(self):
        class Client:
            def __init__(self, *args):
                pass

            def rpc(self, method, *args, **kwargs):
                if method == "initialize":
                    return {"protocolVersion": "2025-11-25"}
                if method == "tools/list":
                    return {"tools": [{"name": name} for name in module.GUI_TOOLS]}
                return {}

            def tool(self, name, *args):
                if name == "gui_close":
                    raise RuntimeError("清理失败")
                if name == "gui_open":
                    return {"id": "fixture-session", "state": "ready"}, {}
                return {}, {}

        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "evidence"
            argv = ["gui-smoke.py", "--execute", "--no-token", "--output-dir", str(output)]
            with mock.patch.object(sys, "argv", argv), mock.patch.object(module, "Client", Client), mock.patch.object(module, "screenshot", side_effect=RuntimeError("原始失败")):
                with self.assertRaisesRegex(RuntimeError, "原始失败"):
                    module.main()
            report = json.loads((output / "report.json").read_text(encoding="utf-8"))
            self.assertFalse(report["run_completed"])
            self.assertEqual(report["failure"], "原始失败")
            self.assertEqual(report["cleanup_failures"], ["清理失败"])

    def test_focus_response_waits_until_active(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)

            class Fixture:
                calls = 0

                def poll(self):
                    self.calls += 1
                    request = json.loads((output / "clipboard-request.json").read_text(encoding="utf-8"))
                    document = {"id": request["id"], "ok": self.calls > 1}
                    if self.calls == 1:
                        document["error"] = "not_focused"
                    (output / "clipboard-response.json").write_text(json.dumps(document), encoding="utf-8")
                    return None

            fixture = Fixture()
            self.assertTrue(module.fixture_request(output, fixture, "focus")["ok"])
            self.assertEqual(fixture.calls, 2)

    def test_focus_timeout_has_own_error_and_bounded_wait(self):
        with tempfile.TemporaryDirectory() as directory:
            fixture = mock.Mock()
            fixture.poll.return_value = None
            for value in [0, 606, True]:
                with self.assertRaisesRegex(RuntimeError, "不超过 605"):
                    module.fixture_request(Path(directory), fixture, "focus", timeout_seconds=value)
            with mock.patch.object(module.time, "monotonic", side_effect=[0, 606]):
                with self.assertRaisesRegex(RuntimeError, "聚焦超时") as captured:
                    module.fixture_request(Path(directory), fixture, "focus", timeout_seconds=605)
            self.assertNotIn("剪贴板", str(captured.exception))


if __name__ == "__main__":
    unittest.main()
