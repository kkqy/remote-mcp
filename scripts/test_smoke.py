"""验证冒烟脚本的生命周期、跨平台编码和成功报告时机。"""
from contextlib import ExitStack, redirect_stdout
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time
from types import SimpleNamespace
import unittest
from unittest.mock import MagicMock, patch

import smoke
from smoke import read_startup, stop_service


class LifecycleTest(unittest.TestCase):
    def start(self, code):
        service = subprocess.Popen(
            [sys.executable, "-c", code], stdout=subprocess.PIPE, text=True, encoding="utf-8",
        )
        self.addCleanup(lambda: stop_service(service, timeout=0.2))
        return service

    def test_startup_timeout(self):
        service = self.start("import time; time.sleep(60)")
        start = time.monotonic()
        with self.assertRaises(TimeoutError):
            read_startup(service, timeout=0.1)
        self.assertLess(time.monotonic() - start, 2)
        stop_service(service, timeout=0.2)
        self.assertIsNotNone(service.poll())

    def test_exited_service(self):
        service = self.start("raise SystemExit(3)")
        service.wait(timeout=5)
        stop_service(service, timeout=0.2)
        self.assertEqual(service.returncode, 3)

    @unittest.skipIf(os.name == "nt", "Windows terminate 为强制结束，无 SIGTERM 忽略路径")
    def test_force_stop(self):
        service = self.start(
            "import signal, time; signal.signal(signal.SIGTERM, signal.SIG_IGN); "
            "print('{}', flush=True); time.sleep(60)"
        )
        self.assertEqual(read_startup(service), {})
        start = time.monotonic()
        stop_service(service, timeout=0.1)
        self.assertLess(time.monotonic() - start, 2)
        self.assertIsNotNone(service.poll())


class EncodingTest(unittest.TestCase):
    def run_main(self, output, *, anonymous=False, diagnostic=None):
        """模拟 cp1252 默认编码，保留 main 中的文件、解码和最终日志检查。"""
        original_open = Path.open
        expected = b"remote-mcp-debug-result\n"
        endpoint = "http://127.0.0.1:8080/mcp"
        token = "" if anonymous else "smoke-test-token"

        def open_file(path, *args, **kwargs):
            if path.name == "server.log":
                kwargs.setdefault("encoding", "cp1252")
            return original_open(path, *args, **kwargs)

        def start_service(*args, **kwargs):
            entry = {"type": "remote", "url": endpoint, "enabled": True, "oauth": False}
            if token:
                entry["headers"] = {"Authorization": "Bearer " + token}
            config = {"$schema": "https://opencode.ai/config.json", "mcp": {"remote-mcp": entry}, "说明": "启动成功"}
            service = MagicMock()
            service.stdout = io.TextIOWrapper(
                io.BytesIO(json.dumps(config, ensure_ascii=False).encode("utf-8")),
                encoding=kwargs.get("encoding", "cp1252"),
            )
            service.poll.return_value = 0
            data = diagnostic if diagnostic is not None else "启动成功，允许匿名访问".encode("utf-8")
            os.write(kwargs["stderr"].fileno(), data)
            return service

        def transfer(command, **kwargs):
            source, target = map(Path, command[-2:])
            shutil.copyfile(source, target)
            result = {"ok": True, "sha256": hashlib.sha256(source.read_bytes()).hexdigest(), "说明": "传输成功"}
            encoding = kwargs.get("encoding", "cp1252")
            return subprocess.CompletedProcess(
                command, 0, json.dumps(result, ensure_ascii=False).encode("utf-8").decode(encoding),
                "启动传输".encode("utf-8").decode(encoding),
            )

        def rpc(request, **kwargs):
            payload = json.loads(request.data)
            method = payload["method"]
            result = {}
            if method == "initialize":
                result = {"protocolVersion": "2025-11-25"}
            elif method == "tools/list":
                result = {"tools": [{}] * 22}
            elif method == "tools/call":
                name = payload["params"]["name"]
                arguments = payload["params"]["arguments"]
                if name == "port_forward_create":
                    content = {"id": "forward", "listen_address": "127.0.0.1:9999"}
                elif name == "port_forward_status":
                    content = {"state": "running"}
                elif name == "port_forward_stop":
                    content = {"state": "stopped"}
                elif name == "process_start":
                    if arguments["request_id"] == "smoke-run":
                        Path(arguments["args"][-1]).write_bytes(expected)
                    content = {"id": "process", "exit_code": 0}
                elif name == "process_read":
                    content = {"valid_utf8": True}
                else:
                    raise AssertionError("未预期的工具调用: " + name)
                result = {"structuredContent": content}
            response = MagicMock()
            response.__enter__.return_value = response
            response.headers = {}
            response.read.return_value = json.dumps({"result": result}).encode("utf-8")
            return response

        listener = MagicMock()
        listener.__enter__.return_value = listener
        listener.getsockname.return_value = ("127.0.0.1", 9999)
        incoming = MagicMock()
        incoming.__enter__.return_value = incoming
        incoming.recv.side_effect = [b"forward-request\x00\xff", b""]
        listener.accept.return_value = (incoming, None)
        outgoing = MagicMock()
        outgoing.__enter__.return_value = outgoing
        outgoing.recv.side_effect = [b"forward-response\x00\xff", b""]
        argv = ["smoke.py"] + (["--no-token"] if anonymous else [])
        with ExitStack() as stack:
            stack.enter_context(patch.object(Path, "open", open_file))
            stack.enter_context(patch.object(smoke.subprocess, "Popen", start_service))
            stack.enter_context(patch.object(smoke.subprocess, "run", transfer))
            stack.enter_context(patch.object(smoke.urllib.request, "urlopen", rpc))
            stack.enter_context(patch.object(smoke.socket, "socket", return_value=listener))
            stack.enter_context(patch.object(smoke.socket, "create_connection", return_value=outgoing))
            stack.enter_context(patch.object(smoke.uuid, "uuid4", return_value=SimpleNamespace(hex="test-token")))
            stack.enter_context(patch.object(sys, "argv", argv))
            stack.enter_context(redirect_stdout(output))
            smoke.main()

    def test_utf8_with_non_utf8_default(self):
        for anonymous in (False, True):
            with self.subTest(anonymous=anonymous):
                output = io.StringIO()
                self.run_main(output, anonymous=anonymous)
                summary = json.loads(output.getvalue())
                self.assertTrue(summary["ok"])
                self.assertEqual(summary["authentication"], "anonymous" if anonymous else "token")

    def test_failed_log_check_does_not_report_success(self):
        output = io.StringIO()
        with self.assertRaisesRegex(AssertionError, "普通日志泄漏凭据"):
            self.run_main(output, diagnostic=b"smoke-test-token")
        self.assertEqual(output.getvalue(), "")

    def test_invalid_utf8_is_not_hidden(self):
        output = io.StringIO()
        with self.assertRaises(UnicodeDecodeError):
            self.run_main(output, diagnostic=b"\xff")
        self.assertEqual(output.getvalue(), "")

    def test_cleanup_failure_does_not_report_success(self):
        def fail_stop(service):
            stop_service(service)
            raise OSError("模拟服务清理失败")

        output = io.StringIO()
        with patch.object(smoke, "stop_service", fail_stop):
            with self.assertRaisesRegex(OSError, "模拟服务清理失败"):
                self.run_main(output)
        self.assertEqual(output.getvalue(), "")


if __name__ == "__main__":
    unittest.main()
