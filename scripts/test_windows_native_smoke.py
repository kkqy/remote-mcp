"""离线验证远程验收入口、尾部读取及成功报告的清理前提。"""
import base64
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("windows_native", Path(__file__).with_name("windows-native-smoke.py"))
native = importlib.util.module_from_spec(spec)
spec.loader.exec_module(native)


class FakeProtocol:
    instances = 0
    fail_preserved = False

    def __init__(self, _endpoint):
        type(self).instances += 1
        if self.fail_preserved and self.instances == 2:
            raise RuntimeError("Existing service failed")

    def rpc(self, *_args, **_kwargs):
        return {"tools": [{"name": name} for name in ("process_start", "process_read", "process_status", "process_stop", "upload_create", "upload_write", "upload_finish", "upload_cancel")]}


class ScriptTests(unittest.TestCase):
    def setUp(self):
        FakeProtocol.instances = 0
        FakeProtocol.fail_preserved = False

    def invoke(self, root, *, cleanup_failure=False, report_failure=False, failed_helper=False):
        binary_dir = root / "windows-amd64"
        binary_dir.mkdir()
        (binary_dir / "remote-mcp.exe").write_bytes(b"fake binary")
        report = root / "report.json"
        report.write_text('{"ok":true,"old":true}', encoding="utf-8")
        output = io.StringIO()
        def powershell(_protocol, code, **_kwargs):
            if "ConvertTo-Json" in code:
                return json.dumps({"platform": "Win32NT", "arch": "X64", "temp": "C:\\Temp\\"})
            if cleanup_failure and "Remove-Item" in code:
                raise RuntimeError("Cleanup failed")
            return ""
        def archive(_repository, _binaries, target, _arch, _tests):
            target.write_bytes(b"archive")
            return {"server_sha256": "fake-sha256", "helper_sha256": "fake-helper-sha256"}
        summary = {"ok": True, "platform": "windows", "arch": "amd64", "protocol_version": "2025-11-25",
                   "native_conpty": True, "gui_exercised": False,
                   "rounds": [{"suite": suite, "authentication": mode, "ok": True, "service_shutdown": "terminate_process"}
                              for suite in ("legacy", "p0") for mode in ("token", "anonymous")]}
        execution = (1, {"stdout": "", "stderr": "Native fixture failed"}) if failed_helper else (0, {"stdout": json.dumps(summary), "stderr": ""})
        with patch.object(native.tempfile, "gettempdir", return_value=str(root)), patch.object(native, "Protocol", FakeProtocol), patch.object(native, "powershell", side_effect=powershell), patch.object(native, "prepare_archive", side_effect=archive), patch.object(native, "upload"), patch.object(native, "run_remote", return_value=execution), patch.object(native.sys, "argv", ["windows-native-smoke.py", "--url", "http://authorized.example/mcp", "--bin-dir", str(binary_dir), "--report-file", str(report), "--skip-package-tests"]), contextlib.redirect_stdout(output):
            with patch.object(Path, "replace", side_effect=OSError("Report failed")) if report_failure else contextlib.nullcontext():
                try:
                    native.main()
                    return report, output.getvalue(), None
                except (RuntimeError, OSError) as error:
                    return report, output.getvalue(), error

    def test_success_after_cleanup_and_original_service_confirmation(self):
        with tempfile.TemporaryDirectory() as directory:
            report, output, error = self.invoke(Path(directory))
            self.assertIsNone(error)
            summary = json.loads(report.read_text(encoding="utf-8"))
            self.assertTrue(summary["deployment_cleaned"])
            self.assertTrue(summary["existing_service_preserved"])
            self.assertTrue(summary["protocol_smoke"])
            self.assertEqual(FakeProtocol.instances, 2)
            self.assertNotIn("C:\\Temp", output)

    def test_cleanup_failure_removes_old_report_and_never_announces_success(self):
        with tempfile.TemporaryDirectory() as directory:
            report, output, error = self.invoke(Path(directory), cleanup_failure=True)
            self.assertIsNotNone(error)
            self.assertFalse(report.exists())
            self.assertNotIn('"ok": true', output)

    def test_incomplete_or_duplicate_smoke_rounds_are_rejected(self):
        for rounds in ([], [{"suite": "p0", "authentication": "token", "ok": True, "service_shutdown": "terminate_process"}] * 4):
            with self.subTest(rounds=rounds):
                summary = {"ok": True, "platform": "windows", "arch": "amd64", "protocol_version": "2025-11-25",
                           "native_conpty": True, "gui_exercised": False, "rounds": rounds}
                with self.assertRaises(AssertionError):
                    native.validate_smoke_summary(summary, "amd64")
                self.assertNotIn("protocol_smoke", summary)

    def test_original_service_failure_prevents_success(self):
        FakeProtocol.fail_preserved = True
        with tempfile.TemporaryDirectory() as directory:
            report, output, error = self.invoke(Path(directory))
            self.assertIsNotNone(error)
            self.assertFalse(report.exists())
            self.assertNotIn('"ok": true', output)

    def test_report_failure_prevents_success_and_cleans_temporary_report(self):
        with tempfile.TemporaryDirectory() as directory:
            report, output, error = self.invoke(Path(directory), report_failure=True)
            self.assertIsNotNone(error)
            self.assertFalse(report.exists())
            self.assertFalse(list(Path(directory).glob(".native-report-*")))
            self.assertNotIn('"ok": true', output)

    def test_helper_failure_prevents_success(self):
        with tempfile.TemporaryDirectory() as directory:
            report, output, error = self.invoke(Path(directory), failed_helper=True)
            self.assertIsNotNone(error)
            self.assertFalse(report.exists())
            self.assertNotIn('"ok": true', output)

    def test_invalid_endpoints_are_rejected_before_connecting(self):
        for endpoint in ("file:///tmp/data", "http://user:password@example/mcp", "http://example/mcp?token=secret", "http://example/mcp#fragment"):
            with self.subTest(endpoint=endpoint), patch.object(native.urllib.request, "build_opener") as opened:
                with self.assertRaises(ValueError):
                    native.Protocol(endpoint)
                opened.assert_not_called()

    def test_redirects_are_rejected(self):
        with self.assertRaises(RuntimeError):
            native.NoRedirect().redirect_request(None, None, None, None, None, None)

    def test_remote_exit_drains_both_streams_to_end_cursor(self):
        class Process:
            reads = 0
            stopped = False
            def tool(self, name, args):
                if name == "process_start":
                    return {"id": "owned"}
                if name == "process_status":
                    return {"state": "exited", "exit_code": 0}
                if name == "process_stop":
                    self.stopped = True
                    return {}
                if name == "process_read":
                    if args["stream"] == "stderr":
                        return {"truncated": False, "data_base64": "", "next_cursor": 0, "end_cursor": 0}
                    self.reads += 1
                    data = b"head" if self.reads == 1 else b"tail"
                    return {"truncated": False, "data_base64": base64.b64encode(data).decode("ascii"), "next_cursor": 4 if self.reads == 1 else 8, "end_cursor": 8}
                raise AssertionError(name)
        process = Process()
        with patch.object(native.time, "sleep"):
            code, output = native.run_remote(process, "owned.exe", [])
        self.assertEqual((code, output["stdout"]), (0, "headtail"))
        self.assertTrue(process.stopped)

    def test_remote_invalid_utf8_still_stops_owned_process(self):
        class Process:
            stopped = False
            def tool(self, name, _args):
                if name == "process_start":
                    return {"id": "owned"}
                if name == "process_status":
                    return {"state": "exited", "exit_code": 0}
                if name == "process_stop":
                    self.stopped = True
                    return {}
                return {"truncated": False, "data_base64": "/w==", "next_cursor": 1, "end_cursor": 1}
        process = Process()
        with self.assertRaises(UnicodeDecodeError):
            native.run_remote(process, "owned.exe", [])
        self.assertTrue(process.stopped)


if __name__ == "__main__":
    unittest.main()
