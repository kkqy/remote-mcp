#!/usr/bin/env python3
"""用实际二进制验证上传脚本、独立 JSON-RPC 执行和下载产物。"""
import argparse
import hashlib
import json
import os
import platform
from pathlib import Path
import queue
import socket
import subprocess
import tempfile
import threading
import urllib.request
import uuid


def read_startup(service, timeout=15):
    """跨平台限时读取启动配置，服务无输出时也能进入清理。"""
    result = queue.Queue(maxsize=1)

    def read():
        document = ""
        try:
            while True:
                line = service.stdout.readline()
                if not line:
                    raise RuntimeError("Service startup failed")
                document += line
                if len(document) > 64 * 1024:
                    raise RuntimeError("Service startup configuration exceeds the limit")
                try:
                    result.put(json.loads(document))
                    return
                except json.JSONDecodeError:
                    pass
        except Exception as error:
            result.put(error)

    threading.Thread(target=read, daemon=True).start()
    try:
        value = result.get(timeout=timeout)
    except queue.Empty as error:
        raise TimeoutError("Timed out waiting for service startup configuration") from error
    if isinstance(value, Exception):
        raise value
    return value


def stop_service(service, timeout=15):
    """先请求退出，超时后强制结束并回收，已经退出的服务无需发信号。"""
    if service.poll() is None:
        try:
            service.terminate()
        except ProcessLookupError:
            pass
    try:
        service.wait(timeout=timeout)
    except subprocess.TimeoutExpired:
        service.kill()
        service.wait(timeout=5)
    service.stdout.close()


def main():
    parser = argparse.ArgumentParser(description="Validate upload, independent JSON-RPC execution, and download using real binaries.")
    parser.add_argument("--bin-dir", default="dist/linux-amd64", help="Directory containing both binaries")
    parser.add_argument("--no-token", action="store_true", help="Validate anonymous mode without a configured token")
    args = parser.parse_args()
    binary_dir = Path(args.bin_dir).resolve()
    suffix = ".exe" if os.name == "nt" else ""
    environment = os.environ.copy()
    token = "" if args.no_token else "smoke-" + uuid.uuid4().hex
    environment["REMOTE_MCP_TOKEN"] = token
    with tempfile.TemporaryDirectory(prefix="remote-mcp-smoke-") as directory:
        root = Path(directory)
        logs = (root / "server.log").open("w+", encoding="utf-8")
        service = subprocess.Popen(
            [str(binary_dir / ("remote-mcp" + suffix)), "--listen", "127.0.0.1:0"],
            stdout=subprocess.PIPE, stderr=logs, text=True, encoding="utf-8", env=environment,
        )
        try:
            config = read_startup(service)
            assert "mcpServers" not in config, "Startup configuration must not contain the legacy mcpServers format"
            assert config["$schema"] == "https://opencode.ai/config.json"
            entry = config["mcp"]["remote-mcp"]
            assert entry["type"] == "remote"
            assert entry["enabled"] is True
            assert entry["oauth"] is False
            endpoint = entry["url"]
            if token:
                assert entry["headers"] == {"Authorization": "Bearer " + token}
            else:
                assert "headers" not in entry, "Anonymous startup configuration must not contain headers"
            headers = {
                "Content-Type": "application/json",
                "Accept": "application/json, text/event-stream",
                "MCP-Protocol-Version": "2025-11-25",
            }
            if token:
                headers["Authorization"] = "Bearer " + token
            sequence = 0

            def rpc(method, params, notification=False):
                nonlocal sequence
                sequence += 1
                payload = {"jsonrpc": "2.0", "method": method, "params": params}
                if not notification:
                    payload["id"] = sequence
                request = urllib.request.Request(endpoint, data=json.dumps(payload).encode(), headers=headers)
                with urllib.request.urlopen(request, timeout=20) as response:
                    session = response.headers.get("Mcp-Session-Id")
                    if session:
                        headers["Mcp-Session-Id"] = session
                    body = response.read()
                result = json.loads(body) if body else {}
                if "error" in result:
                    raise RuntimeError("JSON-RPC call failed")
                return result.get("result", {})

            def tool(name, arguments):
                result = rpc("tools/call", {"name": name, "arguments": arguments})
                if result.get("isError"):
                    raise RuntimeError("Tool call failed: " + name)
                return result["structuredContent"]

            initialized = rpc("initialize", {
                "protocolVersion": "2025-11-25", "capabilities": {},
                "clientInfo": {"name": "independent-smoke", "version": "1"},
            })
            assert initialized["protocolVersion"] == "2025-11-25"
            rpc("notifications/initialized", {}, notification=True)
            assert len(rpc("tools/list", {})["tools"]) >= 22
            with socket.socket() as target:
                target.bind(("127.0.0.1", 0))
                target.listen()
                target.settimeout(5)
                response_bytes = b"forward-response\x00\xff"

                def respond():
                    with target.accept()[0] as connection:
                        connection.settimeout(5)
                        request_bytes = b""
                        while True:
                            chunk = connection.recv(4096)
                            if not chunk:
                                break
                            request_bytes += chunk
                        if request_bytes == b"forward-request\x00\xff":
                            connection.sendall(response_bytes)

                responder = threading.Thread(target=respond, daemon=True)
                responder.start()
                forward = tool("port_forward_create", {
                    "request_id": "smoke-forward", "target_host": "127.0.0.1",
                    "target_port": target.getsockname()[1],
                })
                host, port = forward["listen_address"].rsplit(":", 1)
                assert host == "127.0.0.1"
                with socket.create_connection((host, int(port)), timeout=5) as connection:
                    connection.sendall(b"forward-request\x00\xff")
                    connection.shutdown(socket.SHUT_WR)
                    data = b""
                    while True:
                        chunk = connection.recv(4096)
                        if not chunk:
                            break
                        data += chunk
                    assert data == response_bytes
                responder.join(timeout=5)
                assert not responder.is_alive()
                assert tool("port_forward_status", {"id": forward["id"]})["state"] == "running"
                assert tool("port_forward_stop", {"id": forward["id"]})["state"] == "stopped"
                with socket.socket() as released:
                    released.bind((host, int(port)))
            source = root / ("source.ps1" if os.name == "nt" else "source.sh")
            remote = root / ("uploaded.ps1" if os.name == "nt" else "uploaded.sh")
            artifact = root / "artifact.txt"
            downloaded = root / "downloaded.txt"
            expected = b"remote-mcp-debug-result\n"
            if os.name == "nt":
                source.write_text("[IO.File]::WriteAllText($args[0], \"remote-mcp-debug-result`n\", [Text.UTF8Encoding]::new($false))\n", encoding="utf-8")
            else:
                source.write_text('#!/bin/sh\nprintf "remote-mcp-debug-result\\n" > "$1"\nprintf "finished\\n"\n', encoding="utf-8")

            def transfer(operation, source_path, target_path):
                command = [str(binary_dir / ("remote-mcp-transfer" + suffix)), operation, "--url", endpoint, str(source_path), str(target_path)]
                completed = subprocess.run(command, env=environment, capture_output=True, text=True, encoding="utf-8", timeout=30, check=True)
                result = json.loads(completed.stdout)
                assert result["ok"]
                return result

            uploaded = transfer("upload", source, remote)
            assert uploaded["sha256"] == hashlib.sha256(source.read_bytes()).hexdigest()
            if os.name != "nt":
                result = tool("process_start", {"request_id": "smoke-chmod", "command": "chmod", "args": ["u+x", str(remote)], "wait_ms": 10000})
                assert result["exit_code"] == 0
                command, command_args = str(remote), [str(artifact)]
            else:
                command, command_args = "powershell.exe", ["-NoProfile", "-ExecutionPolicy", "Bypass", "-File", str(remote), str(artifact)]
            result = tool("process_start", {"request_id": "smoke-run", "command": command, "args": command_args, "wait_ms": 10000})
            assert result["exit_code"] == 0
            output = tool("process_read", {"id": result["id"], "stream": "stdout", "cursor": 0})
            assert output["valid_utf8"]
            received = transfer("download", artifact, downloaded)
            assert downloaded.read_bytes() == expected
            assert received["sha256"] == hashlib.sha256(expected).hexdigest()
        finally:
            try:
                stop_service(service)
                logs.seek(0)
                diagnostic = logs.read()
                if token:
                    assert token not in diagnostic, "Regular logs leaked credentials"
                else:
                    assert "anonymous access is allowed" in diagnostic, "Startup logs must identify anonymous mode"
            finally:
                logs.close()
    print(json.dumps({"ok": True, "authentication": "token" if token else "anonymous", "platform": platform.platform(), "architecture": platform.machine(), "bytes": len(expected), "sha256": received["sha256"]}, ensure_ascii=False))


if __name__ == "__main__":
    main()
