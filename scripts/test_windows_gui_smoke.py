"""离线核对 GUI 入口的显式开关、证据完整性和清理后成功顺序。"""
import contextlib
import copy
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("windows_gui", Path(__file__).with_name("windows-gui-smoke.py"))
gui = importlib.util.module_from_spec(spec)
spec.loader.exec_module(gui)
native = gui.native


def evidence():
    return {"utf8_text_verified": True, "region_verified": True, "markers_verified": True,
            "coordinate_events_verified": True, "foreground_guard": True, "repeated_close": True, "window_cleaned": True,
            "clipboard_accessed": False, "clicks": 3, "double_clicks": 1, "drags": 1,
            "select_all": 1, "backspaces": 1, "vertical_scroll": 2, "horizontal_scroll": 2,
            "width": 640, "height": 420, "display_count": 1,
            "png_before_sha256": "a" * 64, "png_after_sha256": "b" * 64}


def summary():
    return {"ok": True, "platform": "windows", "arch": "amd64", "protocol_version": "2025-11-25",
            "gui_exercised": True, "artifacts": {"server_sha256": "a" * 64, "helper_sha256": "b" * 64}, "clipboard_accessed": False, "acceptance_complete": False,
            "pending": ["multiple_displays_and_mixed_scaling", "layout_changes", "permission_revocation", "MCP_client_image_display"],
            "rounds": [{"suite": "gui", "authentication": mode, "ok": True, "service_shutdown": "terminate_process", "evidence": evidence()} for mode in ("token", "anonymous")]}


def failure_summary():
    return {"ok": False, "gui_diagnostic": {"stage": "initial_capture", "artifacts": {"server_sha256": "a" * 64, "helper_sha256": "b" * 64}, "cleanup_errors": None,
            "capture": {"region": {"x": 43, "y": 73, "width": 680, "height": 450}, "paint_count": 2, "paint_failure_bits": 0, "attempt": 61,
                        "dc": {"visible": True, "iconic": False, "cloaked_valid": True, "cloaked": 0,
                               "markers": [{"index": index, "window_rgb": [240, 20, 80], "window_valid": True, "desktop_rgb": [0, 0, 0], "desktop_valid": True} for index in range(4)]},
                        "markers": [{"index": index, "expected_rgb": expected, "actual_rgb": [255, 255, 255], "mismatched_pixels": 49, "matching_color_pixels": 0}
                                    for index, expected in enumerate(([240, 20, 80], [20, 200, 90], [30, 100, 240], [220, 170, 20]))]}}}


class GUITests(unittest.TestCase):
    def test_requires_explicit_execute_before_connecting(self):
        with patch.object(native.sys, "argv", ["windows-gui-smoke.py", "--url", "http://authorized.example/mcp", "--report-file", "unused.json"]), patch.object(native, "Protocol") as protocol, contextlib.redirect_stdout(io.StringIO()):
            gui.main()
            protocol.assert_not_called()

    def test_missing_fake_or_incomplete_evidence_cannot_pass(self):
        gui.validate_summary(summary(), "amd64")
        changes = [lambda s: s.update(acceptance_complete=True), lambda s: s.update(pending=[]),
                   lambda s: s["rounds"].pop(), lambda s: s["rounds"][1].update(authentication="token"),
                   lambda s: s["rounds"][0]["evidence"].update(window_cleaned=False),
                   lambda s: s["rounds"][0]["evidence"].update(clipboard_accessed=True),
                   lambda s: s["rounds"][0]["evidence"].update(utf8_text_verified=False),
                   lambda s: s["rounds"][0]["evidence"].update(clicks=0),
                   lambda s: s["rounds"][0]["evidence"].update(vertical_scroll=0),
                   lambda s: s["rounds"][0]["evidence"].update(png_after_sha256="a" * 64)]
        for change in changes:
            bad = summary()
            change(bad)
            with self.assertRaises(AssertionError):
                gui.validate_summary(bad, "amd64")

    def invoke(self, directory, failure=None):
        events = []
        report = directory / "report.json"
        report.write_text('{"ok":true,"old":true}')
        binaries = directory / "windows-amd64"
        tools = {"tools": [{"name": name} for name in ("process_start", "process_read", "process_status", "process_stop", "upload_create", "upload_write", "upload_finish", "upload_cancel")]}
        class Protocol:
            def __init__(self, _url):
                events.append("connected")
                if failure == "existing" and events.count("connected") == 2:
                    raise RuntimeError("Existing service failed")
            def rpc(self, *_args):
                return copy.deepcopy(tools)
        def powershell(_protocol, code, **_kwargs):
            self.assertNotIn("GetTempPath", code)
            if "CreateDirectory" in code or "Remove-Item" in code:
                self.assertIn("C:\\workspace\\.tmp\\remote-mcp-native-", code)
            if "Get-FileHash" in code:
                events.append("hash_verified")
                if failure == "hash":
                    return json.dumps({"server_sha256": "c" * 64, "helper_sha256": "b" * 64})
                return json.dumps({"server_sha256": "a" * 64, "helper_sha256": "b" * 64})
            if "ConvertTo-Json" in code:
                return json.dumps({"platform": "Win32NT", "arch": "X64", "work_directory": "C:\\workspace"})
            if "Remove-Item" in code:
                events.append("cleaned")
                self.assertIn("AddSeconds(5)", code)
                self.assertNotIn("Stop-Process", code)
                if failure in ("cleanup", "helper_and_cleanup"):
                    raise RuntimeError("Cleanup failed")
            return ""
        def archive(_repository, _binaries, target, _arch, packages):
            self.assertEqual(packages, ())
            target.write_bytes(b"archive")
            return {"server_sha256": "a" * 64, "helper_sha256": "b" * 64}
        def run_remote(_protocol, _command, arguments, _directory):
            events.append("helper")
            self.assertEqual(len(arguments), 4)
            self.assertEqual(arguments[2:], ["a" * 64, "b" * 64])
            self.assertEqual(arguments[1], "--gui")
            return (1, {"stdout": json.dumps(failure_summary()), "stderr": "Fixture markers were obscured or misplaced"}) if failure in ("helper", "helper_and_cleanup") else (0, {"stdout": json.dumps(summary()), "stderr": ""})
        stdout = io.StringIO()
        with patch.object(native.tempfile, "gettempdir", side_effect=AssertionError("禁止系统临时目录回退")), patch.object(native.sys, "argv", ["windows-gui-smoke.py", "--execute", "--url", "http://authorized.example/mcp", "--bin-dir", str(binaries), "--report-file", str(report)]), patch.object(native, "workspace_temp", return_value=directory), patch.object(native, "Protocol", Protocol), patch.object(native, "powershell", side_effect=powershell), patch.object(native, "prepare_archive", side_effect=archive), patch.object(native, "upload"), patch.object(native, "run_remote", side_effect=run_remote), contextlib.redirect_stdout(stdout):
            error = None
            try:
                gui.main()
            except (RuntimeError, AssertionError) as caught:
                error = caught
        return report, stdout.getvalue(), events, error

    def test_gui_summary_published_only_after_cleanup_and_original_service(self):
        with tempfile.TemporaryDirectory() as directory:
            report, output, events, error = self.invoke(Path(directory))
            self.assertIsNone(error)
            result = json.loads(report.read_text())
            self.assertTrue(result["gui_exercised"])
            self.assertTrue(result["deployment_cleaned"])
            self.assertTrue(result["existing_service_preserved"])
            self.assertFalse(result["acceptance_complete"])
            self.assertEqual(events[-2:], ["cleaned", "connected"])
            self.assertNotIn("C:\\Temp", output)

    def test_helper_cleanup_or_existing_service_failure_never_reports_success(self):
        for failure in ("helper", "cleanup", "existing"):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as directory:
                report, output, _events, error = self.invoke(Path(directory), failure)
                self.assertIsNotNone(error)
                self.assertFalse(report.exists())
                self.assertNotIn('"ok": true', output)

    def test_marker_failure_and_cleanup_failure_are_both_preserved(self):
        with tempfile.TemporaryDirectory() as directory:
            report, output, _events, error = self.invoke(Path(directory), "helper_and_cleanup")
            self.assertFalse(report.exists())
            self.assertIn("Fixture markers were obscured or misplaced", str(error.__cause__))
            self.assertIn('"gui_diagnostic"', output)
            self.assertIn('"cleanup_error"', output)
            self.assertNotIn('"ok": true', output)

    def test_hash_mismatch_never_launches_helper(self):
        with tempfile.TemporaryDirectory() as directory:
            report, output, events, error = self.invoke(Path(directory), "hash")
            self.assertIsNotNone(error)
            self.assertFalse(report.exists())
            self.assertNotIn("helper", events)
            self.assertIn("cleaned", events)
            self.assertNotIn('"ok": true', output)

    def test_diagnostic_rejects_text_or_unbounded_samples(self):
        gui.validate_diagnostic(failure_summary())
        for change in (lambda value: value["gui_diagnostic"].update(text="private-content"),
                       lambda value: value["gui_diagnostic"]["capture"]["markers"][0].update(actual_rgb=[256, 0, 0]),
                       lambda value: value["gui_diagnostic"]["capture"]["markers"][0].update(matching_color_pixels=1000001),
                       lambda value: value["gui_diagnostic"]["capture"].update(attempt=62),
                       lambda value: value["gui_diagnostic"]["capture"]["dc"].update(cloaked=8),
                       lambda value: value["gui_diagnostic"]["capture"]["dc"].update(hwnd="private-handle"),
                       lambda value: value["gui_diagnostic"]["capture"]["dc"]["markers"][0].update(desktop_rgb=[0, -1, 0]),
                       lambda value: value["gui_diagnostic"]["capture"]["dc"]["markers"][0].update(window_valid=1)):
            invalid = failure_summary()
            change(invalid)
            with self.assertRaises(AssertionError):
                gui.validate_diagnostic(invalid)


if __name__ == "__main__":
    unittest.main()
