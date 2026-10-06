"""验证 P0 冒烟的报告、严格编码、凭据过滤与清理边界。"""
from contextlib import ExitStack, redirect_stdout
import importlib.util
import io
import json
import os
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import MagicMock, patch


spec = importlib.util.spec_from_file_location("p0_smoke", Path(__file__).with_name("p0-smoke.py"))
p0 = importlib.util.module_from_spec(spec)
spec.loader.exec_module(p0)


class ReportTest(unittest.TestCase):
    def run_main(self, report, output, *, anonymous=False, diagnostic=None, stop_error=None, inspection_error=None, windows=False, exited=False):
        """保留真实启动解码、日志校验及报告发布，只替换需要服务的业务调用。"""
        original_open = Path.open
        token = "" if anonymous else "fictional-p0-smoke-test-token"
        content = diagnostic if diagnostic is not None else (
            "Startup succeeded\nerror_code=conflict error_message=File changed\n"
            "error_code=tls_failed error_message=TLS failed\n"
            "error_code=tcp_failed error_message=TCP failed\n"
        ).encode("utf-8")

        def open_file(path, *args, **kwargs):
            if path.name == "server.log":
                kwargs.setdefault("encoding", "cp1252")
            return original_open(path, *args, **kwargs)

        def start_service(*args, **kwargs):
            self.assertEqual(kwargs["env"]["REMOTE_MCP_TOKEN"], token)
            entry = {"oauth": False, "type": "remote", "url": "http://127.0.0.1:8080/mcp"}
            if token:
                entry["headers"] = {"Authorization": "Bearer " + token}
            config = {"mcp": {"remote-mcp": entry}, "说明": "中文数据"}
            service = MagicMock()
            service.stdout = io.TextIOWrapper(io.BytesIO(json.dumps(config, ensure_ascii=False).encode("utf-8")), encoding=kwargs.get("encoding", "cp1252"))
            service.poll.return_value = 1 if exited else None
            service.returncode = 1 if exited else None
            os.write(kwargs["stderr"].fileno(), content)
            return service

        def stop_service(service):
            service.stdout.close()
            service.returncode = 1 if windows or exited else 0
            if stop_error:
                raise stop_error

        argv = ["p0-smoke.py"] + (["--report-file", str(report)] if report is not None else []) + (["--no-token"] if anonymous else [])
        with ExitStack() as stack:
            stack.enter_context(patch.object(Path, "open", open_file))
            stack.enter_context(patch.object(p0.subprocess, "Popen", start_service))
            stack.enter_context(patch.object(p0, "stop_service", stop_service))
            protocol = stack.enter_context(patch.object(p0, "Protocol"))
            for name in ("check_tools", "check_files", "check_wait", "check_logs"):
                stack.enter_context(patch.object(p0, name))
            stack.enter_context(patch.object(p0, "check_inspection", return_value={"os": "windows" if windows else "linux", "arch": "amd64"}, side_effect=inspection_error))
            stack.enter_context(patch.object(p0.uuid, "uuid4", return_value=SimpleNamespace(hex="test-token")))
            stack.enter_context(patch.object(sys, "argv", argv))
            stack.enter_context(redirect_stdout(output))
            if windows:
                stack.enter_context(patch.object(p0, "os", SimpleNamespace(name="nt", environ=os.environ)))
            p0.main()
            self.assertEqual(protocol.call_args.args[1], token)

    def test_reports_match_stdout_for_both_authentication_modes(self):
        for anonymous in (False, True):
            with self.subTest(anonymous=anonymous), tempfile.TemporaryDirectory() as directory:
                report = Path(directory) / "报告" / "result.json"
                output = io.StringIO()
                self.run_main(report, output, anonymous=anonymous)
                result = json.loads(report.read_text(encoding="utf-8"))
                self.assertEqual(result, json.loads(output.getvalue()))
                self.assertTrue(result["ok"])
                self.assertEqual(result["authentication"], "anonymous" if anonymous else "token")
                self.assertEqual((result["platform"], result["arch"]), ("linux", "amd64"))
                self.assertIn("host_arch", result)
                self.assertLess(report.stat().st_size, 2048)
                self.assertNotIn("fictional-p0-smoke-test-token", report.read_text(encoding="utf-8"))

    def test_report_is_optional(self):
        output = io.StringIO()
        self.run_main(None, output)
        self.assertTrue(json.loads(output.getvalue())["ok"])

    def test_failures_remove_previous_success_and_emit_no_success(self):
        cases = (
            ({"diagnostic": b"\xff"}, UnicodeDecodeError),
            ({"diagnostic": b"fictional-p0-smoke-test-token"}, AssertionError),
            ({"stop_error": OSError("Cleanup failed")}, OSError),
            ({"inspection_error": AssertionError("Inspection failed")}, AssertionError),
            ({"diagnostic": b"Missing diagnostics"}, AssertionError),
        )
        for arguments, error in cases:
            with self.subTest(arguments=arguments), tempfile.TemporaryDirectory() as directory:
                report = Path(directory) / "result.json"
                report.write_text('{"ok":true}', encoding="utf-8")
                output = io.StringIO()
                with self.assertRaises(error):
                    self.run_main(report, output, **arguments)
                self.assertFalse(report.exists())
                self.assertEqual(output.getvalue(), "")

    def test_windows_termination_is_explicit_and_early_exit_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            report = Path(directory) / "result.json"
            output = io.StringIO()
            self.run_main(report, output, windows=True)
            self.assertEqual(json.loads(output.getvalue())["service_shutdown"], "terminate_process")
            output = io.StringIO()
            with self.assertRaisesRegex(AssertionError, "failed to stop"):
                self.run_main(report, output, windows=True, exited=True)
            self.assertFalse(report.exists())
            self.assertEqual(output.getvalue(), "")

    def test_report_publish_failure_cleans_temporary_and_emits_no_success(self):
        with tempfile.TemporaryDirectory() as directory:
            report = Path(directory) / "result.json"
            output = io.StringIO()
            with patch.object(Path, "replace", side_effect=OSError("Report publication failed")):
                with self.assertRaisesRegex(OSError, "publication failed"):
                    self.run_main(report, output)
            self.assertFalse(report.exists())
            self.assertEqual(list(Path(directory).iterdir()), [])
            self.assertEqual(output.getvalue(), "")

    def test_stale_report_removal_failure_does_not_start_service(self):
        with tempfile.TemporaryDirectory() as directory:
            report = Path(directory) / "result.json"
            report.write_text('{"ok":true}', encoding="utf-8")
            output = io.StringIO()
            with patch.object(Path, "unlink", side_effect=PermissionError("Report removal denied")), patch.object(p0.subprocess, "Popen") as start, patch.object(sys, "argv", ["p0-smoke.py", "--report-file", str(report)]), redirect_stdout(output):
                with self.assertRaisesRegex(PermissionError, "removal denied"):
                    p0.main()
                start.assert_not_called()
            self.assertEqual(output.getvalue(), "")


class ProtocolTest(unittest.TestCase):
    def test_authorization_is_omitted_in_anonymous_mode(self):
        for token in ("", "fictional-token"):
            with self.subTest(token=token), patch.object(p0.urllib.request, "urlopen") as request:
                response = request.return_value.__enter__.return_value
                response.headers = {}
                response.read.side_effect = [b'{"result":{"protocolVersion":"2025-11-25"}}', b""]
                p0.Protocol("http://127.0.0.1:8080/mcp", token)
                for call in request.call_args_list:
                    self.assertEqual(call.args[0].get_header("Authorization"), "Bearer " + token if token else None)


if __name__ == "__main__":
    unittest.main()
