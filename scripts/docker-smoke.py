#!/usr/bin/env python3
"""在 Linux host 网络容器中验证镜像运行依赖、协议与正常停止。"""
import argparse
import base64
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import urllib.error
import uuid

from smoke import read_startup, stop_service


# 复用已有独立 JSON-RPC 客户端，服务仍来自待验收镜像。
spec = importlib.util.spec_from_file_location("p0_smoke", Path(__file__).with_name("p0-smoke.py"))
p0 = importlib.util.module_from_spec(spec)
spec.loader.exec_module(p0)


def docker(*args, **kwargs):
    return subprocess.run(
        ["docker", *args], check=True, capture_output=True, text=True,
        encoding="utf-8", timeout=60, **kwargs,
    )


def check_compose():
    """解析默认值和自定义值，防止模板失去 host 网络或配置入口。"""
    environment = {key: value for key, value in os.environ.items() if not key.startswith("REMOTE_MCP_")}
    for overrides in ({}, {
        "REMOTE_MCP_IMAGE": "remote-mcp:smoke",
        "REMOTE_MCP_LISTEN": "127.0.0.1:18080",
        "REMOTE_MCP_TOKEN": "compose-smoke-token",
        "REMOTE_MCP_DATA_DIR": str(Path(".tmp").resolve()),
        "REMOTE_MCP_UID": "1234", "REMOTE_MCP_GID": "2345",
    }):
        configured = json.loads(docker(
            "compose", "--env-file", "/dev/null", "config", "--format", "json",
            env=environment | overrides,
        ).stdout)["services"]["remote-mcp"]
        assert configured["network_mode"] == "host"
        assert not configured.get("ports") and not configured.get("networks")
        assert configured["init"] and configured["restart"] == "unless-stopped"
        assert configured["command"] == ["--listen", overrides.get("REMOTE_MCP_LISTEN", "0.0.0.0:8080")]
        assert configured["environment"]["REMOTE_MCP_TOKEN"] == overrides.get("REMOTE_MCP_TOKEN", "")
        assert configured["image"] == overrides.get("REMOTE_MCP_IMAGE", "ghcr.io/kkqy/remote-mcp:latest")
        assert configured["user"] == ("1234:2345" if overrides else "10001:10001")
        assert configured["volumes"][0]["target"] == "/workspace"
        expected = overrides.get("REMOTE_MCP_DATA_DIR", str(Path("data").resolve()))
        assert configured["volumes"][0]["source"] == expected


def check_round(image, root, token):
    container = "remote-mcp-smoke-" + uuid.uuid4().hex
    environment = os.environ | {"REMOTE_MCP_TOKEN": token}
    follower = None
    created = False
    with (root / "server.log").open("w+", encoding="utf-8") as logs:
        try:
            docker(
                "create", "--name", container, "--network", "host", "--init",
                "--user", f"{os.getuid()}:{os.getgid()}",
                "--mount", f"type=bind,source={root},target=/workspace",
                "--env", "REMOTE_MCP_TOKEN", image, "--listen", "127.0.0.1:0",
                env=environment,
            )
            created = True
            # 创建成功后即登记清理，启动阶段失败也不会遗留容器。
            docker("start", container)
            follower = subprocess.Popen(
                ["docker", "logs", "--follow", container], stdout=subprocess.PIPE,
                stderr=logs, text=True, encoding="utf-8",
            )
            entry = read_startup(follower)["mcp"]["remote-mcp"]
            assert entry["type"] == "remote" and entry["oauth"] is False
            assert entry.get("headers", {}) == ({"Authorization": "Bearer " + token} if token else {})
            endpoint = entry["url"]
            if token:
                for invalid in ("", "incorrect-token"):
                    try:
                        p0.Protocol(endpoint, invalid)
                    except urllib.error.HTTPError as error:
                        assert error.code == 401
                    else:
                        raise AssertionError("Container accepted invalid credentials")
            protocol = p0.Protocol(endpoint, token)
            names = {tool["name"] for tool in protocol.rpc("tools/list", {})["tools"]}
            assert {"terminal_open", "process_start", "upload_create", "download_open"} <= names
            assert not any(name.startswith("gui_") for name in names)
            docker("exec", container, "/bin/ps", "-axo", "pid=,ppid=,pgid=,sess=")
            assert (root / ".tmp").is_dir()

            # 两次传输均调用镜像内的真实辅助命令，经 MCP 写入并读取挂载目录。
            expected = "容器文件传输\n".encode("utf-8") + bytes(range(256))
            (root / "source.bin").write_bytes(expected)
            for operation, source, target in (
                ("upload", "/workspace/source.bin", "/workspace/uploaded.bin"),
                ("download", "/workspace/uploaded.bin", "/workspace/downloaded.bin"),
            ):
                result = json.loads(docker(
                    "exec", container, "remote-mcp-transfer", operation,
                    "--url", endpoint, source, target,
                ).stdout)
                assert result["ok"] and result["sha256"] == hashlib.sha256(expected).hexdigest()
            assert (root / "downloaded.bin").read_bytes() == expected
            process = protocol.tool("process_start", {
                "request_id": "container-process", "command": "/bin/sh",
                "args": ["-c", "printf process-ok"], "wait_ms": 10000,
            })
            assert process["exit_code"] == 0
            output = protocol.tool("process_read", {"id": process["id"], "stream": "stdout"})
            assert base64.b64decode(output["data_base64"], validate=True) == b"process-ok"

            terminal = protocol.tool("terminal_open", {"request_id": "container-terminal"})
            # 将标记拆成两段，避免把终端输入回显误认为真实命令输出。
            command = "test -t 0 && test -t 1 && printf '%s%s\\n' 'terminal-' 'verified'\n"
            protocol.tool("terminal_write", {
                "id": terminal["id"], "data_base64": base64.b64encode(command.encode()).decode(),
            })
            output, cursor = b"", 0
            deadline = time.monotonic() + 15
            while b"terminal-verified" not in output and time.monotonic() < deadline:
                chunk = protocol.tool("terminal_read", {"id": terminal["id"], "cursor": cursor, "wait_ms": 1000})
                output += base64.b64decode(chunk["data_base64"], validate=True)
                cursor = chunk["next_cursor"]
                assert len(output) <= 64 * 1024 and not chunk["truncated"]
            assert b"terminal-verified" in output, "Real terminal command did not complete"
            protocol.tool("terminal_close", {"id": terminal["id"]})

            terminal = protocol.tool("terminal_open", {
                "request_id": "container-child", "command": "/bin/bash",
                # 原子发布完整 PID，避免宿主读到刚创建但尚未写入的空文件。
                "args": ["-c", "sleep 300 & echo $! > /workspace/child.pid.pending; mv /workspace/child.pid.pending /workspace/child.pid; wait"],
            })
            deadline = time.monotonic() + 10
            while not (root / "child.pid").exists() and time.monotonic() < deadline:
                time.sleep(0.05)
            child = int((root / "child.pid").read_text(encoding="utf-8").strip())
            docker("exec", container, "test", "-d", f"/proc/{child}")
            protocol.tool("terminal_close", {"id": terminal["id"]})
            deadline = time.monotonic() + 10
            while time.monotonic() < deadline:
                try:
                    docker("exec", container, "test", "!", "-d", f"/proc/{child}")
                    break
                except subprocess.CalledProcessError:
                    time.sleep(0.05)
            else:
                raise AssertionError("Terminal child process survived closure")

            # 保留活跃进程和终端，确认 SIGTERM 能完成服务清理并以零状态退出。
            protocol.tool("process_start", {
                "request_id": "container-shutdown-process", "command": "sleep", "args": ["300"], "background": True,
            })
            protocol.tool("terminal_open", {"request_id": "container-shutdown-terminal"})
            assert json.loads(docker("inspect", container).stdout)[0]["State"]["Running"]
            docker("stop", "--time", "30", container)
            state = json.loads(docker("inspect", container).stdout)[0]["State"]
            assert not state["Running"] and not state["OOMKilled"] and state["ExitCode"] == 0
            follower.wait(timeout=10)
            assert follower.returncode == 0
            logs.flush()
            logs.seek(0)
            diagnostic = logs.read()
            if token:
                assert token not in diagnostic, "Regular logs leaked credentials"
            else:
                assert "anonymous access is allowed" in diagnostic
            assert (root / "uploaded.bin").read_bytes() == expected
        finally:
            try:
                if created:
                    docker("rm", "--force", container)
            finally:
                if follower is not None:
                    stop_service(follower)


def main():
    parser = argparse.ArgumentParser(description="Validate a Linux container image over host networking.")
    parser.add_argument("--image", default="remote-mcp:smoke")
    args = parser.parse_args()
    check_compose()
    config = json.loads(docker("image", "inspect", args.image).stdout)[0]["Config"]
    assert config["User"] == "10001:10001" and config["WorkingDir"] == "/workspace"
    base = Path(".tmp/docker-smoke").resolve()
    base.mkdir(parents=True, exist_ok=True)
    for token in ("container-smoke-" + uuid.uuid4().hex, ""):
        with tempfile.TemporaryDirectory(prefix="round-", dir=base) as directory:
            check_round(args.image, Path(directory), token)
    print(json.dumps({"ok": True, "authentication": ["token", "anonymous"], "network": "host", "service_shutdown": "graceful"}))


if __name__ == "__main__":
    main()
