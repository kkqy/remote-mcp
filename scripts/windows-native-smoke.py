#!/usr/bin/env python3
"""通过已授权 MCP 入口在 Windows 独立临时目录运行原生验收，不需要远端 Python。"""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
import zipfile

PACKAGES = ("internal/config", "internal/execution", "internal/fileops", "internal/forwarding",
            "internal/inspection", "internal/logstream", "internal/server", "internal/transfer",
            "cmd/remote-mcp-transfer")
LIMIT = 2 * 1024 * 1024


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *_args, **_kwargs):
        raise RuntimeError("MCP redirects are forbidden")


class Protocol:
    """仅连接用户指定入口；禁止代理及重定向，响应有界且严格 UTF-8。"""
    def __init__(self, endpoint):
        parsed = urllib.parse.urlsplit(endpoint)
        if parsed.scheme not in ("http", "https") or not parsed.hostname or parsed.username or parsed.password or parsed.query or parsed.fragment:
            raise ValueError("Invalid MCP endpoint")
        self.endpoint = endpoint
        self.sequence = 0
        self.headers = {"Content-Type": "application/json", "Accept": "application/json, text/event-stream",
                        "MCP-Protocol-Version": "2025-11-25"}
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        initialized = self.rpc("initialize", {"protocolVersion": "2025-11-25", "capabilities": {},
                                               "clientInfo": {"name": "windows-native-validation", "version": "1"}})
        assert initialized["protocolVersion"] == "2025-11-25"
        self.rpc("notifications/initialized", {}, notification=True)

    def rpc(self, method, params, notification=False):
        self.sequence += 1
        payload = {"jsonrpc": "2.0", "method": method, "params": params}
        if not notification:
            payload["id"] = self.sequence
        request = urllib.request.Request(self.endpoint, data=json.dumps(payload).encode("utf-8"), headers=self.headers)
        with self.opener.open(request, timeout=20) as response:
            session = response.headers.get("Mcp-Session-Id")
            if session:
                self.headers["Mcp-Session-Id"] = session
            raw = response.read(LIMIT + 1)
        assert len(raw) <= LIMIT, "MCP response exceeded the limit"
        envelope = json.loads(raw.decode("utf-8")) if raw else {}
        assert "error" not in envelope, "JSON-RPC call failed"
        return envelope.get("result", {})

    def tool(self, name, arguments):
        result = self.rpc("tools/call", {"name": name, "arguments": arguments})
        assert not result.get("isError"), "MCP tool failed: " + name
        output = result["structuredContent"]
        assert output.get("ok", True), "MCP business operation failed: " + name
        return output


def quote_ps(value):
    return "'" + value.replace("'", "''") + "'"


def run_remote(protocol, command, arguments, directory=None, timeout=180):
    """只登记本次创建的进程；超时和失败时也结束并核对终态。"""
    args = {"request_id": "native-" + uuid.uuid4().hex, "command": command, "args": arguments,
            "background": True, "timeout_ms": 0}
    if directory:
        args["dir"] = directory
    process = protocol.tool("process_start", args)
    streams = {"stdout": bytearray(), "stderr": bytearray()}
    cursors = {"stdout": 0, "stderr": 0}
    deadline = time.monotonic() + timeout
    try:
        while True:
            status = protocol.tool("process_status", {"id": process["id"]})
            fully_read = True
            for stream in streams:
                output = protocol.tool("process_read", {"id": process["id"], "stream": stream,
                                                          "cursor": cursors[stream], "limit": 65536})
                assert not output["truncated"], "Remote output was truncated"
                streams[stream].extend(base64.b64decode(output["data_base64"], validate=True))
                assert len(streams[stream]) <= LIMIT, "Remote output exceeded the limit"
                cursors[stream] = output["next_cursor"]
                fully_read = fully_read and output["next_cursor"] >= output["end_cursor"]
            if status["state"] == "exited" and fully_read:
                break
            if time.monotonic() >= deadline:
                raise TimeoutError("Remote validation exceeded its deadline")
            time.sleep(0.1)
        return status["exit_code"], {name: bytes(value).decode("utf-8") for name, value in streams.items()}
    finally:
        protocol.tool("process_stop", {"id": process["id"]})
        deadline = time.monotonic() + 5
        while protocol.tool("process_status", {"id": process["id"]})["state"] != "exited":
            assert time.monotonic() < deadline, "Remote process cleanup failed"
            time.sleep(0.05)


def powershell(protocol, code, timeout=30):
    code = "[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false);$OutputEncoding=[Text.UTF8Encoding]::new($false);" + code
    encoded = base64.b64encode(code.encode("utf-16le")).decode("ascii")
    exit_code, output = run_remote(protocol, "powershell.exe", ["-NoProfile", "-NonInteractive", "-EncodedCommand", encoded], timeout=timeout)
    if exit_code != 0:
        Path("/tmp/remote-mcp-native-powershell-failure.txt").write_text(output["stderr"], encoding="utf-8")
        raise RuntimeError("Remote PowerShell operation failed")
    return output["stdout"]


def upload(protocol, source, destination):
    with source.open("rb") as source_file:
        digest = hashlib.file_digest(source_file, "sha256").hexdigest()
    created = protocol.tool("upload_create", {"request_id": "native-upload-" + uuid.uuid4().hex,
                                              "path": destination, "size": source.stat().st_size, "sha256": digest})
    complete = False
    try:
        offset = 0
        with source.open("rb") as payload:
            while chunk := payload.read(min(created["chunk_size"], 256 * 1024)):
                protocol.tool("upload_write", {"id": created["id"], "offset": offset,
                                               "data": base64.b64encode(chunk).decode("ascii")})
                offset += len(chunk)
        finished = protocol.tool("upload_finish", {"id": created["id"]})
        assert finished["sha256"] == digest and finished["size"] == source.stat().st_size, "Upload verification failed"
        complete = True
    finally:
        if not complete:
            protocol.tool("upload_cancel", {"id": created["id"]})


def prepare_archive(repository, binaries, target, arch, packages):
    """仅打包本任务二进制及生产源码，不携带仓库配置、凭据或工作日志。"""
    environment = os.environ.copy()
    environment.update(GOOS="windows", GOARCH=arch, CGO_ENABLED="0")
    helper = target.parent / "native-helper.exe"
    subprocess.run(["go", "build", "-trimpath", "-o", str(helper), "./scripts/native-smoke"],
                   cwd=repository, env=environment, check=True, timeout=120, capture_output=True)
    files = [(helper, "native-helper.exe")]
    for name in ("remote-mcp.exe", "remote-mcp-transfer.exe"):
        files.append((binaries / name, name))
    if packages:
        for package in packages:
            binary = target.parent / (package.replace("/", "-") + ".test.exe")
            subprocess.run(["go", "test", "-c", "-trimpath", "-o", str(binary), "./" + package],
                           cwd=repository, env=environment, check=True, timeout=120, capture_output=True)
            files.append((binary, package + "/native.test.exe"))
        for directory in ("cmd", "internal"):
            files.extend((path, str(path.relative_to(repository))) for path in (repository / directory).rglob("*.go"))
    with zipfile.ZipFile(target, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        for source, name in files:
            archive.write(source, name)
    with zipfile.ZipFile(target) as archive:
        with archive.open("remote-mcp.exe") as server_binary, archive.open("native-helper.exe") as helper_binary:
            return {"server_sha256": hashlib.file_digest(server_binary, "sha256").hexdigest(),
                    "helper_sha256": hashlib.file_digest(helper_binary, "sha256").hexdigest()}


def validate_smoke_summary(summary, arch):
    """拒绝缺轮、重复轮或只有包测试的摘要，不以单个 ok 代替真实四轮。"""
    assert isinstance(summary, dict) and summary.get("ok") is True, "Invalid native smoke summary"
    assert summary.get("platform") == "windows" and summary.get("arch") == arch, "Native smoke platform mismatch"
    assert summary.get("protocol_version") == "2025-11-25", "Native smoke protocol mismatch"
    assert summary.get("native_conpty") is True and summary.get("gui_exercised") is False, "Native smoke capability mismatch"
    rounds = summary.get("rounds")
    assert isinstance(rounds, list) and len(rounds) == 4, "Native smoke must finish four rounds"
    expected = {(suite, mode) for suite in ("legacy", "p0") for mode in ("token", "anonymous")}
    seen = set()
    for result in rounds:
        assert isinstance(result, dict), "Invalid native smoke round"
        pair = (result.get("suite"), result.get("authentication"))
        assert pair in expected and pair not in seen, "Missing or duplicate native smoke round"
        assert result.get("ok") is True and result.get("service_shutdown") == "terminate_process", "Native smoke round failed cleanup"
        seen.add(pair)
    assert seen == expected, "Native smoke coverage is incomplete"
    summary["protocol_smoke"] = True


def main():
    parser = argparse.ArgumentParser(description="Validate Windows binaries through an explicitly authorized anonymous MCP endpoint.")
    parser.add_argument("--url", required=True, help="Authorized existing Windows MCP endpoint; it is kept running")
    parser.add_argument("--bin-dir", default="dist/windows-amd64", type=Path)
    parser.add_argument("--report-file", type=Path, required=True)
    parser.add_argument("--skip-package-tests", action="store_true", help="Run only four native protocol smoke rounds")
    parser.add_argument("--package", action="append", choices=PACKAGES, help="Run selected native test packages; default runs all listed packages")
    parser.add_argument("--package-only", action="store_true", help="Run native package tests without starting smoke services")
    args = parser.parse_args()
    assert not (args.skip_package_tests and (args.package or args.package_only)), "Conflicting package validation options"
    selected_packages = () if args.skip_package_tests else tuple(args.package or PACKAGES)
    args.report_file.unlink(missing_ok=True)
    protocol = Protocol(args.url)
    tools = {tool["name"] for tool in protocol.rpc("tools/list", {})["tools"]}
    assert {"process_start", "process_read", "process_status", "process_stop", "upload_create", "upload_write", "upload_finish", "upload_cancel"} <= tools
    discovery = json.loads(powershell(protocol, "[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false);"
                                          "[ordered]@{platform=[Environment]::OSVersion.Platform.ToString();"
                                          "arch=[Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString();"
                                          "temp=[IO.Path]::GetTempPath()}|ConvertTo-Json -Compress"))
    assert discovery["platform"] == "Win32NT", "Remote platform is not Windows"
    arch = {"X64": "amd64", "Arm64": "arm64"}.get(discovery["arch"])
    assert arch and args.bin_dir.name == "windows-" + arch, "Remote architecture does not match the binary directory"
    root = discovery["temp"].rstrip("\\/") + "\\remote-mcp-native-" + uuid.uuid4().hex
    root_literal = quote_ps(root)
    state_file = Path(tempfile.gettempdir()) / ("remote-mcp-native-state-" + uuid.uuid4().hex + ".json")
    state_file.write_text(json.dumps({"root": root}), encoding="utf-8")
    repository = Path(__file__).resolve().parents[1]
    tests = []
    with tempfile.TemporaryDirectory(prefix="remote-mcp-native-deploy-") as directory:
        archive = Path(directory) / "native.zip"
        artifact_hashes = prepare_archive(repository, args.bin_dir.resolve(), archive, arch, selected_packages)
        print(json.dumps({"stage": "prepared", "platform": "windows", "arch": arch, "archive_bytes": archive.stat().st_size}), flush=True)
        powershell(protocol, "$ErrorActionPreference='Stop';[IO.Directory]::CreateDirectory(" + root_literal + ")|Out-Null")
        try:
            upload(protocol, archive, root + "\\native.zip")
            powershell(protocol, "$ErrorActionPreference='Stop';Expand-Archive -LiteralPath " + quote_ps(root + "\\native.zip") + " -DestinationPath " + root_literal, timeout=60)
            print(json.dumps({"stage": "uploaded", "platform": "windows", "arch": arch}), flush=True)
            if selected_packages:
                for package in selected_packages:
                    path = root + "\\" + package.replace("/", "\\")
                    code, output = run_remote(protocol, path + "\\native.test.exe", ["-test.v", "-test.count=1", "-test.timeout=120s"], path)
                    lines = output["stdout"].splitlines()
                    tests.append({"package": package, "passed": sum(line.startswith("--- PASS:") for line in lines),
                                  "skipped": sum(line.startswith("--- SKIP:") for line in lines)})
                    if code != 0:
                        # 失败记录仅来自本任务测试夹具，不输出远程目录或原始错误内容。
                        failed = [line.split()[2].split("(")[0] for line in lines if line.startswith("--- FAIL:")]
                        Path("/tmp/remote-mcp-windows-native-failed-test.txt").write_text(output["stdout"] + "\n" + output["stderr"], encoding="utf-8")
                        print(json.dumps({"stage": "package_failed", "package": package, "tests": failed}), flush=True)
                        raise RuntimeError("Windows native package tests failed")
                    print(json.dumps({"stage": "package_passed", **tests[-1]}), flush=True)
            if args.package_only:
                summary = {"ok": True, "platform": "windows", "arch": arch, "protocol_smoke": False, "rounds": []}
            else:
                code, output = run_remote(protocol, root + "\\native-helper.exe", [root], root)
                if code != 0:
                    diagnostic = output["stderr"].strip()
                    assert len(diagnostic) <= 512 and root not in diagnostic, "Native diagnostic exceeded safe bounds"
                    print(json.dumps({"stage": "smoke_failed", "reason": diagnostic}), flush=True)
                    raise RuntimeError("Windows native protocol smoke failed")
                summary = json.loads(output["stdout"])
                validate_smoke_summary(summary, arch)
        finally:
            powershell(protocol, "$ErrorActionPreference='Stop';Remove-Item -LiteralPath " + root_literal +
                       " -Recurse -Force;if(Test-Path -LiteralPath " + root_literal + "){throw 'Temporary cleanup failed'}", timeout=60)
            state_file.unlink()
    preserved = Protocol(args.url)
    assert {tool["name"] for tool in preserved.rpc("tools/list", {})["tools"]} == tools, "Existing MCP service is unavailable after cleanup"
    assert artifact_hashes and artifact_hashes["server_sha256"], "Missing uploaded artifact hashes"
    summary.update(package_tests=tests, deployment_cleaned=True,
                   **artifact_hashes,
                   existing_service_preserved=True, python_smoke_executed=False,
                   gui_exercised=False)
    args.report_file.parent.mkdir(parents=True, exist_ok=True)
    temporary = args.report_file.with_name(".native-report-" + uuid.uuid4().hex)
    try:
        temporary.write_text(json.dumps(summary, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        temporary.replace(args.report_file)
    finally:
        temporary.unlink(missing_ok=True)
    print(json.dumps(summary), flush=True)


if __name__ == "__main__":
    main()
