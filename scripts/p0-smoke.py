#!/usr/bin/env python3
"""以真实本机二进制和独立 JSON-RPC 验证远程调试 P0，不访问 GUI 或公网。"""
import argparse
import base64
import hashlib
import http.server
import json
import os
import platform
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import threading
import time
import urllib.request
import uuid

from smoke import read_startup, stop_service


class Protocol:
    """直接构造协议请求，避免客户端与服务复用错误字段定义。"""
    def __init__(self, endpoint, token):
        self.endpoint = endpoint
        self.sequence = 0
        self.headers = {
            "Content-Type": "application/json",
            "Accept": "application/json, text/event-stream",
            "MCP-Protocol-Version": "2025-11-25",
        }
        if token:
            self.headers["Authorization"] = "Bearer " + token
        initialized = self.rpc("initialize", {
            "protocolVersion": "2025-11-25", "capabilities": {},
            "clientInfo": {"name": "independent-p0-smoke", "version": "1"},
        })
        assert initialized["protocolVersion"] == "2025-11-25"
        self.rpc("notifications/initialized", {}, notification=True)

    def rpc(self, method, params, notification=False):
        self.sequence += 1
        payload = {"jsonrpc": "2.0", "method": method, "params": params}
        if not notification:
            payload["id"] = self.sequence
        request = urllib.request.Request(
            self.endpoint, data=json.dumps(payload).encode("utf-8"), headers=self.headers,
        )
        with urllib.request.urlopen(request, timeout=15) as response:
            session = response.headers.get("Mcp-Session-Id")
            if session:
                self.headers["Mcp-Session-Id"] = session
            body = response.read(2 * 1024 * 1024 + 1)
        assert len(body) <= 2 * 1024 * 1024, "Protocol response exceeded the limit"
        envelope = json.loads(body.decode("utf-8")) if body else {}
        assert "error" not in envelope, "JSON-RPC call failed"
        return envelope.get("result", {})

    def tool(self, name, arguments, failed=False):
        result = self.rpc("tools/call", {"name": name, "arguments": arguments})
        assert bool(result.get("isError")) == failed, "Unexpected tool error status: " + name
        if "structuredContent" in result:
            return result["structuredContent"]
        assert failed, "Tool response is missing structuredContent"
        return json.loads(result["content"][0]["text"])


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def check_tools(protocol):
    listed = protocol.rpc("tools/list", {})["tools"]
    tools = {tool["name"]: tool for tool in listed}
    assert len(tools) == len(listed), "Duplicate registered tool names"
    # 工具范围由远端服务平台决定，不能使用 Python 宿主系统代替。
    environment = protocol.tool("environment_inspect", {"runtimes": ["python"]})
    assert environment.get("ok") is True and environment.get("os") in ("linux", "windows"), "Unsupported or unconfirmed service platform"
    windows = environment["os"] == "windows"
    assert len(tools) == (40 if windows else 33), "Unexpected registered tool count for the service platform"
    gui_tools = {"gui_status", "gui_open", "gui_close", "gui_screenshot", "gui_mouse", "gui_key", "gui_text"}
    discovered_gui = {name for name in tools if name.startswith("gui_")}
    assert discovered_gui == (gui_tools if windows else set()), "GUI tools do not match the service platform"
    for name in (
        "environment_inspect", "process_inspect", "network_listeners", "network_probe",
        "file_list", "file_read", "file_search", "file_patch", "log_open", "log_read", "log_close",
    ):
        assert tools[name]["inputSchema"] and tools[name]["outputSchema"]
    for name in ("process_read", "terminal_read", "log_read"):
        assert "wait_ms" in tools[name]["inputSchema"]["properties"]


def check_files(protocol, root):
    directory = root / "files"
    directory.mkdir()
    path = directory / "private-file-path-012345.txt"
    original = "中文首行\r\nprivate-query-012345\r\n末尾无换行".encode("utf-8")
    path.write_bytes(original)
    listing = protocol.tool("file_list", {"path": str(directory), "limit": 1})
    assert len(listing["entries"]) == 1
    read = protocol.tool("file_read", {"path": str(path), "start_line": 2, "line_count": 1})
    assert read["text"] == "private-query-012345\r\n" and read["sha256"] == sha256(original)
    found = protocol.tool("file_search", {"path": str(directory), "query": "private-query-012345"})
    assert len(found["matches"]) == 1 and found["matches"][0]["line"] == 2
    edits = [{"start_line": 2, "delete_count": 1, "text": "新的中文行\r\n"}]
    arguments = {"path": str(path), "expected_sha256": read["sha256"], "edits": edits}
    patched = protocol.tool("file_patch", arguments)
    expected = "中文首行\r\n新的中文行\r\n末尾无换行".encode("utf-8")
    assert path.read_bytes() == expected and patched["sha256"] == sha256(expected)
    conflict = protocol.tool("file_patch", arguments, failed=True)
    assert conflict["code"] == "conflict" and conflict["message"]
    assert path.read_bytes() == expected, "A conflicting patch damaged the existing file"
    assert not list(directory.glob(".remote-mcp-patch-*")), "Patch temporary files were retained"


def check_wait(protocol, root):
    worker = root / "worker.py"
    worker.write_text(
        "import pathlib, sys, time\n"
        "gate, ready, exit_gate = map(pathlib.Path, sys.argv[1:])\n"
        "ready.write_bytes(b'ready')\n"
        "deadline = time.monotonic() + 15\n"
        "def wait_for(path):\n"
        "    while not path.exists() and time.monotonic() < deadline:\n"
        "        time.sleep(0.01)\n"
        "    if not path.exists():\n"
        "        sys.exit(2)\n"
        "wait_for(gate)\n"
        "print('wait-output-012345', flush=True)\n"
        "wait_for(exit_gate)\n",
        encoding="utf-8",
    )
    for kind in ("process", "terminal"):
        gate = root / (kind + "-gate")
        ready = root / (kind + "-ready")
        exit_gate = root / (kind + "-exit")
        arguments = {"request_id": "p0-smoke-" + kind, "command": sys.executable, "args": [str(worker), str(gate), str(ready), str(exit_gate)]}
        if kind == "process":
            arguments["background"] = True
        resource = protocol.tool("process_start" if kind == "process" else "terminal_open", arguments)
        try:
            deadline = time.monotonic() + 5
            while not ready.exists() and time.monotonic() < deadline:
                time.sleep(0.01)
            assert ready.exists(), "Waiting-read fixture did not become ready"
            read_arguments = {"id": resource["id"]}
            if kind == "process":
                read_arguments["stream"] = "stdout"
            immediate = protocol.tool(kind + "_read", read_arguments)
            assert immediate["reason"] == "immediate"
            # 输出闸门保持关闭，超时不会依赖进程调度或输出到达的偶然顺序。
            read_arguments.update(cursor=immediate["next_cursor"], wait_ms=150)
            started = time.monotonic()
            startup_data = base64.b64decode(immediate["data_base64"], validate=True)
            assert b"wait-output-012345" not in startup_data, "Waiting-read fixture produced output before its gate opened"
            while time.monotonic() - started < 5:
                waiting = time.monotonic()
                empty = protocol.tool(kind + "_read", read_arguments)
                if empty["reason"] == "timeout":
                    assert 0.1 <= time.monotonic() - waiting < 5, "Waiting read exceeded its timeout budget"
                    break
                # ConPTY 可以异步产生初始化控制序列，先有界消耗，闸门仍不允许业务输出。
                assert kind == "terminal" and empty["reason"] == "output"
                startup_data += base64.b64decode(empty["data_base64"], validate=True)
                assert len(startup_data) <= 65536 and b"wait-output-012345" not in startup_data
                read_arguments["cursor"] = empty["next_cursor"]
            else:
                raise AssertionError("Waiting-read fixture did not become idle")
            assert empty["reason"] == "timeout" and not empty["data_base64"]
            assert empty["next_cursor"] == read_arguments["cursor"]
            timer = threading.Timer(0.1, lambda: gate.write_bytes(b"go"))
            timer.start()
            try:
                read_arguments.update(wait_ms=3000)
                data = b""
                deadline = time.monotonic() + 5
                while b"wait-output-012345" not in data and time.monotonic() < deadline:
                    output = protocol.tool(kind + "_read", read_arguments)
                    assert output["reason"] == "output" and output["valid_utf8"]
                    data += base64.b64decode(output["data_base64"], validate=True)
                    assert len(data) <= 65536, "Waiting-read fixture output exceeded the limit"
                    read_arguments["cursor"] = output["next_cursor"]
                assert b"wait-output-012345" in data
            finally:
                timer.cancel()
                timer.join()
            # 显式允许退出，继续消耗可能分块到达的终端尾部数据，直到退出结果。
            exit_gate.write_bytes(b"exit")
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline:
                ended = protocol.tool(kind + "_read", read_arguments)
                read_arguments["cursor"] = ended["next_cursor"]
                if ended["reason"] == "exit":
                    assert not ended["data_base64"]
                    break
                assert ended["reason"] == "output", "Waiting read did not observe fixture exit"
            else:
                raise AssertionError("Waiting read did not finish after fixture exit")
            status = protocol.tool(kind + "_status", {"id": resource["id"]})
            assert status["state"] == "exited" and status["exit_code"] == 0, "Waiting-read fixture failed"
        finally:
            protocol.tool("process_stop" if kind == "process" else "terminal_close", {"id": resource["id"]})


def check_logs(protocol, root):
    path = root / "private-log-path-012345"
    path.write_bytes("旧代\n".encode("utf-8"))
    opened = protocol.tool("log_open", {"path": str(path)})
    try:
        def append():
            with path.open("ab") as file:
                file.write("追加中文\n".encode("utf-8"))
        timer = threading.Timer(0.1, append)
        timer.start()
        try:
            output = protocol.tool("log_read", {"id": opened["id"], "generation": opened["generation"], "cursor": opened["end_cursor"], "wait_ms": 3000})
            assert output["reason"] == "output" and output["text"] == "追加中文\n"
        finally:
            timer.cancel()
            timer.join()
        path.rename(root / "old-log")
        path.write_bytes("新代中文\n".encode("utf-8"))
        rotated = protocol.tool("log_read", {"id": opened["id"], "generation": output["generation"], "cursor": output["next_cursor"]})
        assert rotated["rotated"] and rotated["generation"] != output["generation"] and rotated["text"] == "新代中文\n"
        # 同一 inode 缩短到上一游标之前，保证采样可以观察截断。
        path.write_bytes("短\n".encode("utf-8"))
        truncated = protocol.tool("log_read", {"id": opened["id"], "generation": rotated["generation"], "cursor": rotated["next_cursor"]})
        assert truncated["truncated"] and not truncated["rotated"]
        assert truncated["generation"] != rotated["generation"] and truncated["start_cursor"] == 0
        assert truncated["text"] == "短\n" and truncated["next_cursor"] == len("短\n".encode("utf-8"))
        idle = protocol.tool("log_read", {"id": opened["id"], "generation": truncated["generation"], "cursor": truncated["next_cursor"], "wait_ms": 150})
        assert idle["reason"] == "timeout" and not idle["data_base64"]
        assert idle["generation"] == truncated["generation"] and idle["next_cursor"] == truncated["next_cursor"]
    finally:
        closed = protocol.tool("log_close", {"id": opened["id"]})
        assert closed["closed"], "Log resource failed to close"


def check_inspection(protocol, service_pid):
    environment = protocol.tool("environment_inspect", {"runtimes": ["python"]})
    assert environment["ok"] and environment["os"] and environment["arch"]
    processes = protocol.tool("process_inspect", {"root_pid": service_pid})
    assert any(process["pid"] == service_pid for process in processes["processes"])

    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            self.send_response(204)
            self.end_headers()
        def log_message(self, *_):
            pass
    target = http.server.HTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=target.serve_forever, daemon=True)
    thread.start()
    try:
        listeners = protocol.tool("network_listeners", {"port": target.server_port, "pid": os.getpid()})
        assert any(os.getpid() in row["pids"] and row["port"] == target.server_port for row in listeners["listeners"])
        dns = protocol.tool("network_probe", {"mode": "dns", "target": "localhost"})
        assert dns["ok"] and dns["addresses"]
        endpoint = "http://127.0.0.1:" + str(target.server_port) + "/private-url-012345"
        response = protocol.tool("network_probe", {"mode": "http", "target": endpoint})
        assert response["ok"] and response["status_code"] == 204
        tls = protocol.tool("network_probe", {"mode": "tls", "target": "127.0.0.1:" + str(target.server_port)}, failed=True)
        assert tls["code"] == "tls_failed" and tls["failed_stage"] == "tls" and len(tls["stages"]) >= 3
    finally:
        target.shutdown()
        target.server_close()
        thread.join(timeout=5)
        assert not thread.is_alive(), "Local HTTP fixture failed to close"
    with socket.socket() as closed:
        closed.bind(("127.0.0.1", 0))
        port = closed.getsockname()[1]
    failure = protocol.tool("network_probe", {"mode": "tcp", "target": "127.0.0.1:" + str(port)}, failed=True)
    assert failure["code"] == "tcp_failed" and failure["failed_stage"] == "tcp" and len(failure["stages"]) >= 2
    return {"os": environment["os"], "arch": environment["arch"]}


def write_report(path, summary):
    """只发布有限最终摘要，同目录临时文件失败时清理。"""
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", dir=path.parent, prefix=".p0-report-", delete=False) as report:
            temporary = Path(report.name)
            json.dump(summary, report, ensure_ascii=False, indent=2)
            report.write("\n")
        temporary.replace(path)
        temporary = None
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description="Validate P0 inspection, waiting reads, log rotation and hash-checked text patches using a local binary.")
    parser.add_argument("--bin-dir", default="dist/linux-amd64", help="Directory containing the server binary")
    parser.add_argument("--no-token", action="store_true", help="Validate anonymous mode")
    parser.add_argument("--report-file", type=Path, help="Write the final bounded UTF-8 JSON summary after all checks and cleanup succeed; remove any previous report before this run")
    args = parser.parse_args()
    if args.report_file is not None:
        args.report_file.unlink(missing_ok=True)
    binary = Path(args.bin_dir).resolve() / ("remote-mcp.exe" if os.name == "nt" else "remote-mcp")
    token = "" if args.no_token else "fictional-p0-smoke-" + uuid.uuid4().hex
    environment = os.environ.copy()
    environment["REMOTE_MCP_TOKEN"] = token
    with tempfile.TemporaryDirectory(prefix="remote-mcp-p0-smoke-") as directory:
        root = Path(directory)
        with (root / "server.log").open("w+", encoding="utf-8") as logs:
            service = subprocess.Popen([str(binary), "--listen", "127.0.0.1:0"], stdout=subprocess.PIPE, stderr=logs, text=True, encoding="utf-8", env=environment)
            try:
                entry = read_startup(service)["mcp"]["remote-mcp"]
                assert entry["oauth"] is False and entry["type"] == "remote"
                if token:
                    assert entry["headers"] == {"Authorization": "Bearer " + token}
                else:
                    assert "headers" not in entry
                protocol = Protocol(entry["url"], token)
                check_tools(protocol)
                check_files(protocol, root)
                check_wait(protocol, root)
                check_logs(protocol, root)
                service_platform = check_inspection(protocol, service.pid)
            finally:
                running_before_stop = service.poll() is None
                stop_service(service)
            # Windows terminate 使用 TerminateProcess；进程和终端已逐项关闭，不能宣称服务优雅退出。
            expected_termination = os.name == "nt" and running_before_stop and service.returncode == 1
            assert service.returncode == 0 or expected_termination, "The service failed to stop cleanly"
            logs.flush()
            logs.seek(0)
            content = logs.read()
            for forbidden in (token, str(root), "private-query-012345", "新的中文行", "追加中文", "新代中文", "private-url-012345", "wait-output-012345"):
                if forbidden:
                    assert forbidden not in content, "Ordinary logs contain credentials or user content"
            for code in ("conflict", "tls_failed", "tcp_failed"):
                assert "error_code=" + code in content, "Failure diagnostics are missing a stable code"
            assert "error_message=" in content
    summary = {"ok": True, "inspection": True, "waiting_reads": True, "log_rotation": True, "hash_patch": True, "gui_exercised": False, "token_enabled": bool(token),
               "log_truncation": True, "waiting_timeout": True, "waiting_exit": True,
               "platform": service_platform["os"], "arch": service_platform["arch"],
               "host_platform": sys.platform, "host_arch": platform.machine(),
               "authentication": "token" if token else "anonymous", "protocol_version": "2025-11-25",
               "service_shutdown": "terminate_process" if expected_termination else "graceful"}
    if args.report_file is not None:
        write_report(args.report_file, summary)
    print(json.dumps(summary))


if __name__ == "__main__":
    main()
