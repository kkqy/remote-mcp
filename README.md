# Remote MCP

**English** | [简体中文](README.zh-CN.md)

A Go MCP service deployed on a remote machine. Agents connect over a local network or VPN to transfer files, run commands, and operate real interactive terminals. The service includes two programs: the `remote-mcp` server and the `remote-mcp-transfer` local file transfer helper.

## 下载与自动发布

可从仓库的 [GitHub Releases](../../releases) 下载正式版本。每个压缩包只含 `remote-mcp` 服务端和 `remote-mcp-transfer` 辅助命令；Windows 文件带 `.exe` 后缀。

| 平台 | 附件名（以 `v1.2.3` 为例） |
| --- | --- |
| Linux amd64 | `remote-mcp-v1.2.3-linux-amd64.tar.gz` |
| Linux arm64 | `remote-mcp-v1.2.3-linux-arm64.tar.gz` |
| Windows amd64 | `remote-mcp-v1.2.3-windows-amd64.zip` |
| Windows arm64 | `remote-mcp-v1.2.3-windows-arm64.zip` |

同一版本附带 `SHA256SUMS`，记录四个压缩包的实际 SHA-256。Linux 下载全部附件后可运行 `sha256sum -c SHA256SUMS`，再用 `tar -xzf <压缩包>` 解压；tar 保留两个程序的可执行权限。Windows 解压 zip 后运行 `.exe`，可用 `Get-FileHash <压缩包> -Algorithm SHA256` 对照校验文件。

维护者先将发布工作流和版本代码合入并推送 `main`，再从已合入 `main` 的提交创建并推送标签，例如：

```sh
git switch main
git pull --ff-only
git tag -a v1.2.3 -m "Release v1.2.3"
git push origin v1.2.3
```

内置 `GITHUB_TOKEN` 不具备额外的 Workflows 权限；标签目标若包含相对默认分支尚未合入的工作流修改，GitHub 可能拒绝创建 Release。本流程不使用 PAT，请先完成上述合入步骤。

普通分支 push 和 PR 继续验证，不发布版本；仅 `v*` 标签 push 进入发布流程。打包脚本要求标签以 `v` 开头、后续首字符为字母或数字、其余仅含字母/数字/点/下划线/连字符，总长最多 64 字符；其他名称会明确失败。Linux、Windows 原生验证和四组合构建全部成功后，发布 job 才创建草稿、上传四个包和校验文件，全部上传成功后公开为正式 Release。只有该 job 的 `GITHUB_TOKEN` 具有 `contents: write`，相同 ref 的发布串行且不取消正在执行的发布；原有二进制和 P0 验证 artifact 保留。

已有正式 Release 或同标签草稿不会自动覆盖。验证阶段失败时可在 Actions 重跑；若创建草稿或上传中断，先在 Releases 核对本次标签与草稿附件，再手动删除**本次失败草稿**，保留标签并重跑该 Actions run。不要删除已公开版本或使用附件覆盖参数。最终公开请求若超时，结果可能已生效，必须先查看 Release 状态；若已公开则不重跑覆盖，后续修改使用新版本标签。

发布构建不代表 Windows/arm64、多屏/混合 DPI 或 GUI 原生缺口已验收。当前自动发布链路尚未实际创建版本；实际发布需维护者推送选定标签。开发者可用 `python3 scripts/release.py package --tag v1.2.3 --dist-dir dist --output-dir .tmp/release` 本机打包，输出目录须不存在；临时文件与构建缓存均置于项目 `.tmp/`。

## Build and start

The module requires Go 1.25 or later and builds with Go 1.27 by default. CGO is not required:

```sh
go build -o bin/remote-mcp ./cmd/remote-mcp
go build -o bin/remote-mcp-transfer ./cmd/remote-mcp-transfer
```

未配置 Token 时可以直接启动。Linux：

```sh
./bin/remote-mcp
```

Windows PowerShell:

```powershell
.\remote-mcp.exe
```

Token authentication is optional. When `REMOTE_MCP_TOKEN` is unset or empty and `--token-file` is not specified, the service allows anonymous access: any client that can reach it can use the file and command permissions of the account running the service. If a token was previously set in your terminal, clear it before starting with `unset REMOTE_MCP_TOKEN` in Bash or `$env:REMOTE_MCP_TOKEN = $null` in PowerShell.

需要鉴权时，在启动前配置非空 Token。Linux：

```sh
export REMOTE_MCP_TOKEN='replace-with-a-random-ascii-token'
./bin/remote-mcp
```

Windows PowerShell:

```powershell
$env:REMOTE_MCP_TOKEN = 'replace-with-a-random-ascii-token'
.\remote-mcp.exe
```

Tokens must contain only printable ASCII characters with no whitespace and must not exceed 8 KiB. You can also use a credential file readable only by the account running the service:

```sh
chmod 600 /secure/remote-mcp.token
./bin/remote-mcp --token-file /secure/remote-mcp.token --listen 192.168.1.10:8080
```

The token file takes precedence over the environment variable; leading and trailing whitespace is trimmed when reading it. If an explicitly specified file is empty, unreadable, or has invalid contents or permissions, startup fails instead of falling back to anonymous access. A nonempty but invalid environment token also causes an error. On Windows, restrict token file access to the service account using file ACLs; Unix permission bits cannot substitute for Windows ACL checks. The service does not accept token values as command-line arguments, keeping credentials out of process arguments.

By default, the server listens on all IPv4 interfaces at `0.0.0.0:8080`. Use `--listen 127.0.0.1:8080` for local access only. For IPv6, use `--listen '[::]:8080'`; this listens only on IPv6 and does not assume dual-stack support. Port `0` lets the operating system assign a port, and the startup configuration shows the actual port. Stop the service with Ctrl+C or a normal termination signal to clean up managed processes, terminals, temporary transfer files, and handles. Temporary file cleanup cannot be guaranteed after a forced kill or power loss.

## Copy the startup configuration

Once the listener is ready, stdout prints a complete OpenCode configuration for each usable IP address on enabled network interfaces. Multiple addresses on the same interface are shown separately, with duplicates removed and a stable sort order. Without a token, an example configuration is:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "remote-mcp": {
      "type": "remote",
      "enabled": true,
      "oauth": false,
      "url": "http://192.168.1.10:8080/mcp"
    }
  }
}
```

VPN addresses receive separate configurations when available. **Choose a configuration reachable from your agent**; all listed addresses belong to the same service. Each block is a separate JSON object; concatenating them does not produce a single JSON document. Anonymous mode omits `headers` entirely. With authentication enabled, the configuration adds `"headers": {"Authorization": "Bearer <current token>"}` containing the actual token, so treat it as a credential. Explanatory messages in English and regular logs go to stderr. Regular logs do not record tokens, environment variable values, file contents, or terminal input/output.

When listening on all interfaces, the address list excludes disabled interfaces, loopback, unspecified, multicast, and IPv6 link-local addresses. Binding to a single IP shows only that address; an explicit loopback binding is labeled as local-only. If interface enumeration fails, the service keeps running and prompts you to configure the client manually, without using `0.0.0.0` or `::` as a connection URL. Listing an interface address does not establish routing, firewall access, or certificate trust on the agent side.

The configuration uses the [OpenCode remote MCP format](https://opencode.ai/docs/mcp-servers/#remote): `mcp` is the top-level key, the server type is `remote`, `enabled: true` enables the connection, and `oauth: false` disables the OAuth flow that this service does not provide. Merge one JSON block into your project or user `opencode.json`. For an existing configuration, merge only the `mcp.remote-mcp` entry and preserve other settings. Restart OpenCode, then use `opencode mcp list` to check the status. This format corresponds to OpenCode 1.18.34; it is not a universal configuration file defined by the MCP protocol. Other clients must map the same URL and optional Authorization value to their own configuration format. Clients must support Streamable HTTP and protocol version **2025-11-25**, plus Bearer tokens when authentication is enabled. The service provides no adapters for stdio, legacy SSE, or OAuth autodiscovery, and compatibility with every agent product has not been verified.

## Authentication and HTTPS

Without a configured token, Authorization is not checked. With a nonempty token configured, every MCP request is authenticated; missing or incorrect credentials return HTTP 401. A protocol session ID does not replace authentication. Authorized clients share the file and command permissions of the service account. The working directory is not a sandbox, and the program does not elevate privileges automatically. Process tree cleanup covers managed process groups, terminal jobs, and Windows Job Objects. Programs that deliberately detach from Unix sessions, for example, may not be fully cleaned up; this is not isolation for malicious programs. Use a dedicated account with the permissions needed for debugging.

An encrypted VPN can carry HTTP traffic. On a regular local network, you can configure HTTPS:

```sh
./bin/remote-mcp --tls-cert /certs/server.pem --tls-key /certs/server.key
```

The certificate must cover the selected IP address or the domain name manually configured by the client. Startup configurations then use `https://`; clients must still verify the certificate. The transfer helper supports `--ca-file /certs/ca.pem` to add a self-signed CA and provides no option to skip certificate verification.

Non-browser requests without an `Origin` header are allowed. Requests with an `Origin` header are rejected by default. If needed, configure an exact allowlist:

```sh
./bin/remote-mcp --allowed-origins https://agent.example,https://console.example
```

This option does not bypass authentication or automatically configure a browser CORS preflight policy. The firewall should allow the agent's local network or VPN to reach the listening port.

## Upload and download files

The helper runs on the agent's machine, reads actual bytes from local disk, and transfers them in chunks over MCP. File contents do not pass through model-generated text.

```sh
./bin/remote-mcp-transfer upload --url http://192.168.1.10:8080/mcp ./package.zip /remote-dir/package.zip
./bin/remote-mcp-transfer download --url http://192.168.1.10:8080/mcp /remote-dir/runtime.log ./runtime.log
```

By default, no token is required and the helper sends no Authorization header. If the server has authentication enabled, set the same `REMOTE_MCP_TOKEN` first or pass `--token-file`; an explicitly specified invalid file causes an error. `REMOTE_MCP_URL` can supply `--url`. Place options after `upload` or `download` and before the two path arguments. Existing destinations are replaced only with `--overwrite`. Uploads and downloads write to temporary files first and commit only after size and SHA-256 verification. Failures return a nonzero exit code. Progress goes to stderr, and the final JSON summary goes to stdout with `ok`, `size`, `sha256`, or an error reason.

The default maximum file size is 4 GiB, and the default raw chunk size is 256 KiB. Package directories with system tools first; archives are transferred as ordinary files. The service does not automatically extract archives, preserve file permissions or original timestamps, synchronize directories, or support user-initiated transfer resumption. After uploading a Unix executable, use the command tool to set its executable bit, or run an uploaded script through an interpreter.

## MCP tools

Clients can obtain the complete typed parameters through `tools/list`. Operation failures return MCP `isError` and stable error codes; an HTTP 200 protocol response does not imply tool success.

| Tool | Purpose |
| --- | --- |
| `file_stat` | Inspect path information |
| `upload_create` / `upload_write` / `upload_finish` / `upload_cancel` | Create an upload, write sequential chunks, verify and commit, or cancel |
| `download_open` / `download_read` / `download_close` | Open a download, read chunks by byte offset, and close it |
| `process_start` / `process_read` / `process_status` / `process_stop` | Run a process, read output, query status, and stop its process tree |
| `terminal_open` / `terminal_write` / `terminal_read` | Create a real terminal, send input, and read combined output |
| `terminal_resize` / `terminal_status` / `terminal_close` | Resize, inspect, and close a terminal |

Creating uploads, processes, and terminals requires a caller-generated `request_id`. Reusing the same ID with identical parameters returns the existing resource during the record retention period; changed parameters cause a conflict. Application resource IDs can access resources on the same service across MCP connections; token authentication is still required when enabled. Network disconnections do not automatically stop background processes or terminals, and restarting the service does not restore them.

### Commands and background processes

Example parameters for `process_start`:

```json
{
  "request_id": "debug-build-001",
  "command": "go",
  "args": ["test", "./..."],
  "dir": "/remote-project",
  "env": {"CGO_ENABLED": "0"},
  "wait_ms": 10000
}
```

The response includes a resource ID and status. `wait_ms` is limited to 10000 milliseconds; use the ID to continue reading after the wait window. Commands time out after 5 minutes by default. Set `background: true` and `timeout_ms: 0` to run a background service indefinitely. Pipes and redirection are interpreted only when you explicitly invoke a shell, such as `sh -c` on Unix or `powershell.exe -NoProfile -Command` on Windows.

`process_read` accepts `id`, `stream` (`stdout` or `stderr`), `cursor` (initially 0), and an optional `limit`. Continue with the returned `next_cursor`. The `start_cursor`, `end_cursor`, and `truncated` fields indicate whether the bounded buffer has discarded older output. `data_base64` contains the raw bytes, `text` provides a UTF-8 view, and `valid_utf8` indicates whether the bytes are valid UTF-8. For non-UTF-8 Windows program output, restore the bytes from Base64 and decode them using the program's actual encoding.

### Interactive terminals

```json
{"request_id":"debug-terminal-001","columns":120,"rows":30}
```

After calling `terminal_open`, use `id` and the Base64 field `data_base64` in subsequent `terminal_write` calls. For example, Unix `pwd` followed by a carriage return is `cHdkDQ==`; Ctrl+C is `Aw==`. `terminal_read` accepts `id`, `cursor`, and an optional `limit`. Terminal output is a combined stream that preserves ANSI/VT control sequences, with no separate stderr. Repeated input through the same ID preserves the working directory, environment variables, and foreground application state.

`terminal_resize` 接受 `id`、`columns` 和 `rows`。Ctrl+C 输入由前台程序自行响应，强制回收使用 `terminal_close`。Windows 使用 ConPTY，Linux 使用 PTY。终端默认无调用活动 30 分钟回收，持续输出不会延长这一时间；网络短暂断开后可凭原 ID 继续。

### A complete debugging workflow

1. Use the helper on the agent's machine to upload build artifacts. On Windows, use a path local to the remote machine, such as `C:\debug\app.exe`.
2. For a native Unix binary, first call `process_start` with `command: "chmod"` and `args: ["u+x", "/remote-dir/app"]`, then wait for a successful exit. Scripts can also run directly through interpreters such as `sh` or `python`.
3. Call `process_start` to run the remote program, or create a terminal with `terminal_open` and send commands. Read logs and use the output to guide further input or program changes.
4. Confirm the exit result with `process_status`, or explicitly stop background processes and close terminals.
5. Use the helper to download logs, dumps, or build artifacts, and verify the size and SHA-256 in the final JSON summary.

## Configurable default limits

Run `remote-mcp --help` for all options. Sizes are in bytes, and durations use Go duration syntax, such as `30s` or `5m`.

| Option | Default |
| --- | --- |
| `--listen` | `0.0.0.0:8080` |
| `--max-body-bytes` | 1048576 (1 MiB) |
| `--max-file-bytes` | 4294967296 (4 GiB) |
| `--chunk-bytes` | 262144 (256 KiB) |
| `--max-transfers` / `--transfer-idle` | 8 / 10m |
| `--transfer-retention` / `--max-transfer-records` | 10m / 1024 |
| `--command-timeout` / `--process-retention` | 5m / 10m |
| `--max-processes` / `--max-terminals` | 16 / 8 |
| `--terminal-idle` / `--sweep-interval` | 30m / 30s |
| `--output-bytes` / `--read-bytes` | 4194304 / 65536 |

When increasing the chunk size, also raise the request body limit to accommodate at least the Base64 expansion and 4 KiB of metadata. Reaching concurrent resource limits returns an error instead of allowing unbounded growth. Completed resources and deduplication records expire and are cleaned up; creation records for running resources remain protected.

## Validation and platform support

```sh
go vet ./...
go test ./...
go test -race ./...
bash scripts/build.sh
python3 scripts/smoke.py --bin-dir dist/linux-amd64
python3 scripts/smoke.py --bin-dir dist/linux-amd64 --no-token
```

当前仅支持 Linux 和 Windows 的 amd64/arm64，macOS 支持已移除。构建脚本生成 `dist/{linux,windows}-{amd64,arm64}/`，每种组合含两个程序。GitHub Actions 在 Linux、Windows 执行原生测试及竞态检查，并运行真实二进制上传、执行、下载闭环，另运行四组合构建；没有执行过的 CI 不能算验证通过。

Linux also requires `procps` (or the distribution's equivalent package), which provides `/bin/ps`, to identify and clean up additional terminal job groups.

目标系统下限为 Windows 10 1809+/Server 2019+、Linux 3.2+ 且具有 PTY。下限来自 Go 1.27 和 ConPTY 的要求，不表示每个旧系统版本均已测试。

当前开发环境为 Linux/amd64。Windows/amd64 虚拟机已通过原生包测试和真实二进制闭环，包含终端、文件替换及进程树清理；Windows/arm64 目前仅构建通过。没有前端、公网中转、多租户隔离、专用断点调试协议或自动安装系统服务功能。

Windows CI 曾发现宿主标准输入输出重定向导致 ConPTY 会话误用宿主句柄的问题，已修正启动参数，并增加管道及文件重定向宿主下的交互、Ctrl+C 和空闲回收回归。对应回归已在 Windows/amd64 虚拟机实际通过；本轮未触发远程 CI。

The recorded Linux/amd64 validation passed race tests, static analysis, an upload/run/download cycle with real binaries, and an actual 257 MiB bidirectional MCP transfer. The large-file test took approximately 29.74 seconds, with a sampled peak heap increase of about 9.46 MiB and matching SHA-256 hashes at both ends. A sampled heap increase is not equivalent to operating system RSS.

## 远程调试：巡检、等待日志与文本补丁

以下能力与已有上传、执行和终端工具共用 `/mcp`、鉴权及服务账号权限，无新增 CLI 参数。

| 工具 | 用途与主要参数 |
| --- | --- |
| `environment_inspect` | 系统、架构、内核、hostname；可选 `runtimes` 为 go/node/python/java/dotnet 的子集 |
| `process_inspect` | 有界 PID/PPID/name 快照；可选 `root_pid` 返回根进程及后代 |
| `network_listeners` | IPv4/IPv6 TCP LISTEN 地址、端口及可见 PID；可选 `pid/port/limit` |
| `network_probe` | `mode` 为 dns/tcp/tls/http，`target` 分别为 hostname、host:port 或 HTTP(S) URL |
| `process_read`、`terminal_read` | 新增 `wait_ms`，缺省或 0 保持立即读取，最大 30000 ms |
| `log_open/read/close` | 跟踪已有普通日志文件；read 使用 `id/generation/cursor/limit/wait_ms` |
| `file_list` | `path/offset/limit` 单目录分页，返回 `next_offset` 和限制原因 |
| `file_read` | `path/start_line/line_count/max_bytes` 按行读取 UTF-8，返回全文件 `sha256` |
| `file_search` | `path/query` 区分大小写的字面子串；可配置深度、条目、字节和命中预算 |
| `file_patch` | `path/expected_sha256/edits` 校验原内容后修改已有普通 UTF-8 文件 |

巡检默认总预算 5 秒，`timeout_ms` 最多 10000；最多 4 个并发巡检。快照默认返回 256 项，单次最多 4096，内部 PID/FD 扫描也有界。返回 `partial/warnings/truncated/visibility` 表示权限、依赖、扫描上限或可见性缺口；空列表不证明整机没有对象。Linux 使用当前 `/proc` namespace，Windows 使用 Toolhelp 与 IP Helper；缺少依赖或权限不足提供英文代码与具体原因。运行时探测使用服务账号 PATH，不采集完整环境变量或进程命令行。

网络探测沿 DNS→TCP→TLS→HTTP 共用一个 deadline。失败仍保留 `structuredContent` 中的 `stages/failed_stage/code/message`；IP 字面量可跳过 DNS。TLS 使用系统 CA 校验证书，HTTP 固定一次 GET，不走代理、不跟随重定向、不读取正文；HTTP 4xx/5xx 是有效协议响应，业务健康由调用方判断。例如：

```json
{"name":"network_probe","arguments":{"mode":"http","target":"http://127.0.0.1:8080/health","timeout_ms":3000}}
```

执行 read 的 `reason` 区分 `immediate/output/exit/timeout/cancelled`。原始 Base64 字节、字节游标、UTF-8 视图及截断标记继续保留；HTTP 取消只结束本次等待，不停止应用进程。先将上次 `next_cursor` 带回，再等待新输出：

```json
{"name":"process_read","arguments":{"id":"resource-id","stream":"stdout","cursor":128,"wait_ms":10000}}
```

日志资源默认最多 32 个，空闲 10 分钟回收，文件变化约每 100 ms 检查，单次最多 64 KiB。`log_open` 返回 `generation/end_cursor`：从头读取用 cursor=0，只读取以后追加内容用 end_cursor。调用 `log_read` 时同时传回上次 `generation/next_cursor`；路径替换或缩短会重置代际与偏移，返回 `rotated/truncated`。等待式结果还可用 `reason=rotated/truncated` 表示文件变化；立即读取仍是 `immediate`，变化看布尔标记。两个采样之间瞬间截断又恢复、同一文件原地重写且尺寸没有缩短，可能无法检测；它不是文件审计协议。显式 `log_close` 或服务关闭结束资源。

文件读取和修改默认最多 8 MiB，返回文本最多 64 KiB；`next_line` 是首个未完整返回的行。`truncated/reason=byte_limit` 时可在上限内提高 `max_bytes` 重读；超过 64 KiB 的长行仅提供有限 UTF-8 预览，跳过此行用 `end_line+1`，不要把重复返回的前缀盲目拼接。完整字节可使用原下载工具。搜索逐行匹配字面子串，不跨行匹配，最多 10000 条目、64 MiB 扫描、每文件 8 MiB、16 层和 1000 命中，结果也有字节预算。长行在文件扫描预算内完整匹配，返回预览可能没有展示命中位置；`issues/truncated/reason` 说明无效 UTF-8、权限失败或预算限制。目录分页采用文件系统枚举顺序，目录并发变化时 offset 会移动；不承诺稳定快照，也不跟随符号链接。

补丁行号从 1 开始，按**原文件**坐标严格递增且不重叠；`total_lines+1` 可追加。`text` 是确切替换字节，调用方自行携带 LF 或 CRLF，不自动补换行。先读取全文件 hash，再发送行补丁，例如原第二行使用 CRLF：

```json
{"name":"file_read","arguments":{"path":"/srv/app/config.txt","start_line":1,"line_count":20}}
```

```json
{"name":"file_patch","arguments":{"path":"/srv/app/config.txt","expected_sha256":"<sha256-from-file_read>","edits":[{"start_line":2,"delete_count":1,"text":"新的配置\r\n"}]}}
```

错误哈希或已观测到的外部修改返回 `conflict` 并保留现有目标。同目录临时文件校验、同步、关闭后才原子替换，失败清理临时名称；本模块补丁串行执行。请求断开或等待取消后，应重新 `file_read` 核对实际 hash，再决定下一步，不盲目重试旧 hash。最终哈希复核与发布之间仍存在外部非合作写入窗口，不提供严格文件系统 CAS。权限保持遵循平台可移植权限位，不能把它等同于 Windows 自定义 ACL 的完整复制。取消在文件系统操作之间检查，无法强制中断已阻塞的内核文件操作。

普通日志只记录工具、耗时、结果及受控英文错误码与原因，不记录文件内容、query、edits、URL、运行时版本输出或凭据。Linux 本机测试与四组无 CGO 构建分别验收，Windows/amd64 原生 PID、端口、终端和文件行为已通过虚拟机验收，Windows/arm64 仍仅构建通过；新增 P0 不消除 GUI 任务的原生验收缺口。DAP、调试会话聚合、诊断包、反向隧道和新增 GUI 留待后续。

真实本机二进制闭环（不访问 GUI 或公网），Linux 使用本机 Go 构建产物：

```sh
CGO_ENABLED=0 go build -o bin/ ./cmd/...
python3 scripts/smoke.py --bin-dir bin
python3 scripts/smoke.py --bin-dir bin --no-token
python3 scripts/p0-smoke.py --bin-dir bin --report-file dist/p0-token.json
python3 scripts/p0-smoke.py --bin-dir bin --no-token --report-file dist/p0-anonymous.json
```

Windows PowerShell：

```powershell
$env:CGO_ENABLED = '0'
go build -o bin/ ./cmd/...
Remove-Item Env:CGO_ENABLED
python scripts/smoke.py --bin-dir bin
python scripts/smoke.py --bin-dir bin --no-token
python scripts/p0-smoke.py --bin-dir bin --report-file dist/p0-token.json
python scripts/p0-smoke.py --bin-dir bin --no-token --report-file dist/p0-anonymous.json
```

已有四组合构建产物也可通过 `--bin-dir dist/linux-amd64` 等目录验证，目录必须对应正在运行的系统与架构。P0 冒烟覆盖工具发现、当前 PID 与监听端口、回环网络成功和失败、中文 CRLF 补丁与冲突保留、进程及真实终端的立即读取/输出等待/超时/退出、日志追加/轮转/同文件截断。服务及测试资源清理和普通日志过滤全部通过后，才输出成功摘要，并按可选 `--report-file` 保存严格 UTF-8 的有限 JSON 报告。每次运行先删除该位置的旧报告，失败不留下旧成功记录；报告只包含检查布尔值、协议及鉴权模式、服务端系统/架构和 Python 宿主系统/架构，不保存凭据、用户内容、路径、主机名或进程列表。Windows 服务停止使用 `TerminateProcess`，报告明确标注 `service_shutdown=terminate_process`，不表示服务收到信号后优雅退出。

现有 Linux/Windows CI 入口执行旧功能与 P0 的 Token/匿名四轮冒烟，原生构建步骤明确关闭 CGO，竞态检查保留所需配置，四组合构建继续独立执行；P0 报告保存到按 runner 平台命名的 artifact，成功模式分别留证。Linux/amd64 和 Windows/amd64 实际旧功能/P0 × Token/匿名四轮冒烟均通过，Windows 九个包合计 111 个顶层测试通过、4 个按平台或条件跳过。本轮未触发远程 CI；CI 配置或 artifact 名称不能代替实际结果。Windows 虚拟机缺少 Python，实际验收使用下面的 Go 助手方式，未在 Windows 执行上述 Python 冒烟脚本。Windows/arm64 只有构建结果，虚拟机没有运行 Windows race 检测，GUI 保留独立验收边界。

目标 Windows 没有 Python 时，在本机通过**已授权的**现有 MCP 入口部署预编译 Go 助手：

```sh
python3 scripts/windows-native-smoke.py \
  --url http://windows-host:8080/mcp \
  --bin-dir dist/windows-amd64 \
  --report-file dist/windows-native.json
```

该脚本只使用唯一临时目录和独立回环服务，完成后清理并重新确认原入口可用，保留原服务与配置。`--skip-package-tests` 仅跑四轮服务闭环；可重复 `--package` 选择包，`--package-only` 仅跑包测试，其报告 `protocol_smoke=false`，不能代表四轮成功。报告只含有限检查结果与上传产物哈希，不记录凭据、目录或用户内容。

## Windows 图形桌面截图与操作

图形操作仅支持 Windows。Linux 保留文件、进程、PTY 终端、TCP 转发及远程调试工具，不提供 `gui_*` 工具或 `gui-*` 启动参数。

Windows GUI 使用原有 `/mcp` 入口、匿名或可选 Token 模式，只操作服务运行账号的当前交互桌面。请在已登录的图形会话中启动服务；后台服务、锁屏和安全桌面不保证可访问。系统 UIPI 可能拒绝向更高权限的应用输入。匿名模式下，能够连接服务的客户端也可申请桌面操作；桌面不可用不影响其他工具启动。

| 工具 | 参数与用途 |
| --- | --- |
| `gui_status` | `{}` 只读查询后端及能力；`{"id":"会话 ID"}` 查询会话、显示器及不可用原因 |
| `gui_open` | `{"request_id":"desktop-001","wait_ms":10000}` 创建当前用户图形会话 |
| `gui_close` | `{"id":"会话 ID"}` 释放资源；记录保留期内允许重复关闭 |
| `gui_screenshot` | `id`、可选 `display_id/region`，获取整显示器或区域 PNG |
| `gui_mouse` | `id/capture_id/action/x/y`；支持移动、点击、双击、拖拽、横纵滚动 |
| `gui_key` | `{"id":"会话 ID","keys":["Ctrl","A"]}` 单键或组合键，自动释放本次按下的键 |
| `gui_text` | `id/text` 与可选 `mode/paste_keys/allow_clipboard_replace`，提交 UTF-8 文本，包括中文和 emoji |

会话按 `request_id` 去重；同 ID、不同参数返回 `conflict`。一次服务最多一个待授权或可操作会话，操作正在执行时返回 `busy`。HTTP/MCP 断线不会销毁图形资源；默认无调用活动 30 分钟回收。正常退出释放桌面句柄，重启后旧 ID 失效。

### 截图与坐标

打开会话后获取截图，例如：

```json
{"id":"会话 ID","display_id":"显示器 ID","region":{"x":100,"y":80,"width":640,"height":480}}
```

省略 `display_id` 时选择默认或首个显示器，省略 `region` 时获取整个显示器。截图不缩放、不拼接全部桌面。返回一个标准 MCP `image` 内容块（`mimeType: "image/png"`），以及 `structuredContent` 中的 `capture_id/display_id/width/height/region/logical_bounds/layout_generation/captured_at/captured_at_source/frame_sequence/freshness`。结构化结果不重复 PNG，图片展示需要客户端支持 MCP 图片内容。

鼠标坐标以返回截图内部像素为单位，左上角为 `(0,0)`。例如在上述区域图的 `(120,50)` 单击：

```json
{"id":"会话 ID","capture_id":"本次截图 ID","action":"click","x":120,"y":50,"button":"left"}
```

服务加入裁剪偏移并按实际显示器映射转换，调用方不再自行加桌面原点或缩放。拖拽终点相对同一截图。多屏、负桌面坐标和 DPI 使用实际映射；布局改变、记录到期或淘汰后返回 `stale_capture`，应重新截图定位。

原生截图的 `captured_at_source` 为 `acquired_at`。输入结果的 `submitted` 仅表示事件提交，应用是否接受应再次截图或检查实际控件。取消或失败可能已经产生部分输入，结果中的 `input_may_have_applied` 提示这一情况，不应盲目重试。

### 中文输入与限制

```json
{"id":"会话 ID","text":"你好，图形桌面 😀","mode":"auto"}
```

Windows 的 `auto/direct` 使用原生 Unicode 输入，不读取或修改剪贴板。显式 `mode: "clipboard"` 返回 `unsupported`；保留的粘贴相关参数不代表支持剪贴板模式。普通日志不记录图片或输入文字。

以下启动参数仅在 Windows 提供，值必须为正：

| 配置参数 | 默认值 |
| --- | --- |
| `--gui-idle` | `30m` |
| `--gui-authorize-timeout` | `2m` |
| `--gui-operation-timeout` | `30s` |
| `--gui-max-pixels` | 16777216 个原始显示器像素 |
| `--gui-max-png-bytes` | 16777216（16 MiB） |

终态默认保留 10 分钟、最多 256 条记录；每会话最多保留 64 份坐标记录、5 分钟有效。文字上限 UTF-8 64 KiB，组合键最多 8 个，拖拽最长 10 秒。

### 真实 Windows 桌面验证

普通无桌面 CI 只能验证代码和构建。目标 Windows 桌面启动服务后，可从 Linux 或 Windows 的 Python 宿主运行：

```sh
python3 scripts/gui-smoke.py --execute \
  --url http://WINDOWS_HOST:8080/mcp --no-token \
  --desktop-label 'Windows' --output-dir .tmp/gui-capture-evidence
```

该命令使用独立 JSON-RPC 检查工具发现、后端、PNG 图片块、扫描行和采集元数据，然后关闭会话；默认只查询和截图。Token 模式使用 `REMOTE_MCP_TOKEN` 或 `--token-file`，HTTPS 可用 `--ca-file`。不带 `--execute` 只显示帮助，不连接服务。输出目录须事先不存在。

可用 `--input-plan .tmp/plan.json` 操作调用方已准备并置于所选显示器的专用应用。计划含 `target` 和 `operations`，工具仅限 `gui_mouse/gui_key/gui_text`；脚本补入会话 ID 和首张截图 ID。例如：

```json
{
  "target":"已准备并获得焦点的专用测试编辑器",
  "operations":[
    {"tool":"gui_mouse","arguments":{"action":"click","x":320,"y":200}},
    {"tool":"gui_text","arguments":{"text":"中文测试"}}
  ]
}
```

计划默认只核验事件返回，应用结果需另行确认。若计划保存文件，可增加 `verify_text_file` 的 `path/expected`，核对事前不存在、脚本可读的 UTF-8 产物；远端文件需另行传输，远端路径不会作为本地文件读取。

Windows 没有 Python 时，可从本机通过已授权的匿名 MCP 入口部署 Go 专用原生窗口助手：

```sh
python3 scripts/windows-gui-smoke.py --execute \
  --url http://WINDOWS_HOST:8080/mcp \
  --bin-dir dist/windows-amd64 \
  --report-file .tmp/windows-gui-evidence.json
```

脚本核对本机产物、远端文件和实际启动程序的 SHA-256。本机缓存和打包文件放项目 `.tmp/`，远端部署放 MCP 服务工作目录的 `.tmp/`；需该目录可写。用临时回环服务完成 Token/匿名两轮；仅向自己创建且确认前台和输入焦点的 Win32 窗口操作。它验证裁剪 PNG 的四色标记、真实鼠标和滚轮事件、全选清空、中文/emoji 实际进入 EDIT 控件。原生 Unicode 路线不访问剪贴板；窗口、会话、临时服务和部署目录清理后复查原 MCP 服务，再发布报告。旧功能/P0 原生入口继续默认四轮。

2026-10-07 当前构建已在 Windows/amd64 单屏 100% DPI 环境完成上述 Token/匿名原生闭环；该结果不覆盖 Windows/arm64 原生运行、多屏/混合 DPI、布局变化、撤权或实际 MCP 客户端图片展示。报告保留 `acceptance_complete: false`，构建、模拟协议及真实桌面结果分别记录。历史 Linux GUI 研究仅供追溯，不属于当前支持范围或待交付项。

MCP 工具失败的普通日志保留 `tool/elapsed/failed`，并输出 `error_code/error_message`，程序自有错误原因及提示使用英文；GUI 失败还可包含 `input_may_have_applied/clipboard_restore`。例如 Windows 显式请求剪贴板模式会返回 `unsupported`，并记录具体英文原因。成功调用没有错误字段。日志只接收已知内置工具域的安全业务说明；SDK 参数失败按必填缺失、类型不符等类别说明，未知工具和异常使用安全分类，不打印原始参数、内容或错误载荷。错误说明最多 1024 字节，控制字符清理，非空 Token 及可识别的输入内容脱敏；未来自定义工具需单独建立安全日志契约才能输出原文原因。
