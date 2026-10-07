# Remote MCP

**English** | [简体中文](README.zh-CN.md)

A Go MCP service deployed on a remote machine. Agents connect over a local network or VPN to transfer files, run commands, and operate real interactive terminals. The service includes two programs: the `remote-mcp` server and the `remote-mcp-transfer` local file transfer helper.

## Downloads and automated releases

Download official versions from [GitHub Releases](../../releases). Each archive contains only the `remote-mcp` server and the `remote-mcp-transfer` helper; Windows filenames end in `.exe`.

| Platform | Asset name (using `v1.2.3` as an example) |
| --- | --- |
| Linux amd64 | `remote-mcp-v1.2.3-linux-amd64.tar.gz` |
| Linux arm64 | `remote-mcp-v1.2.3-linux-arm64.tar.gz` |
| Windows amd64 | `remote-mcp-v1.2.3-windows-amd64.zip` |
| Windows arm64 | `remote-mcp-v1.2.3-windows-arm64.zip` |

Each release includes `SHA256SUMS` with the SHA-256 hashes of the four archives. On Linux, download all assets, run `sha256sum -c SHA256SUMS`, then extract with `tar -xzf <archive>`; tar preserves the executable permissions of both programs. On Windows, extract the zip and run the `.exe` files. Use `Get-FileHash <archive> -Algorithm SHA256` to compare each hash with the manifest.

Maintainers should first merge and push the release workflow and version code to `main`, then create and push a tag from a commit already merged into `main`, for example:

```sh
git switch main
git pull --ff-only
git tag -a v1.2.3 -m "Release v1.2.3"
git push origin v1.2.3
```

The built-in `GITHUB_TOKEN` has no additional Workflows permission. GitHub may reject release creation if the tagged commit changes workflows that have not yet been merged into the default branch. This process uses no PAT; complete the merge above first.

Branch pushes and pull requests continue to run validation without publishing a release; only `v*` tag pushes enter the release flow. The packaging script requires a tag starting with `v`, followed by a letter or digit, then only letters, digits, dots, underscores or hyphens, with a maximum total length of 64 characters. Other names fail explicitly. The release job creates a draft and uploads the four archives and checksum file only after native Linux and Windows validation and all four build combinations succeed. It publishes the release only after every upload succeeds. Only this job has `contents: write` through `GITHUB_TOKEN`. Releases for the same ref run serially without cancelling an active publication; existing binary and P0 validation artifacts are retained.

An existing published release or draft for the same tag is never automatically overwritten. Rerun validation failures in Actions. If draft creation or upload is interrupted, inspect the tag and draft assets in Releases, manually delete **only that failed draft**, keep the tag, and rerun the Actions run. Do not delete a published release or use asset overwrite options. A final publication request that times out may already have succeeded: check the release state first. If it is published, use a new version tag for subsequent changes instead of rerunning to overwrite it.

On 2026-10-07, both the [main Actions run](https://github.com/kkqy/remote-mcp/actions/runs/37503153099) and the [v0.1.0 tag Actions run](https://github.com/kkqy/remote-mcp/actions/runs/37503152596) completed successfully. The public [v0.1.0 release](https://github.com/kkqy/remote-mcp/releases/tag/v0.1.0) has four archives and `SHA256SUMS`, and its assets were checked. These CI and release results do not establish native Windows/arm64, multiple-monitor, mixed-DPI or remaining GUI acceptance. Developers can package locally with `python3 scripts/release.py package --tag v1.2.3 --dist-dir dist --output-dir .tmp/release`; the output directory must not already exist. Keep temporary files and build caches in the project `.tmp/` directory.

## Build and start

The module requires Go 1.25 or later and builds with Go 1.27 by default. CGO is not required:

```sh
go build -o bin/remote-mcp ./cmd/remote-mcp
go build -o bin/remote-mcp-transfer ./cmd/remote-mcp-transfer
```

Start directly when no token is configured. On Linux:

```sh
./bin/remote-mcp
```

Windows PowerShell:

```powershell
.\remote-mcp.exe
```

Token authentication is optional. When `REMOTE_MCP_TOKEN` is unset or empty and `--token-file` is not specified, the service allows anonymous access: any client that can reach it can use the file and command permissions of the account running the service. If a token was previously set in your terminal, clear it before starting with `unset REMOTE_MCP_TOKEN` in Bash or `$env:REMOTE_MCP_TOKEN = $null` in PowerShell.

For authentication, configure a nonempty token before startup. On Linux:

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

`terminal_resize` accepts `id`, `columns` and `rows`. The foreground application handles Ctrl+C input; use `terminal_close` to force cleanup. Windows uses ConPTY and Linux uses PTY. Terminals are reclaimed after 30 minutes without calls by default; continuous output does not extend this period. After a brief network disconnection, continue using the original ID.

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

Supported platforms are Linux and Windows on amd64/arm64; macOS support has been removed. The build script generates `dist/{linux,windows}-{amd64,arm64}/`, with two programs per combination. GitHub Actions runs native and race tests on Linux and Windows, upload/run/download cycles with real binaries, and all four build combinations. A configured CI job is not evidence that it has run successfully.

Linux also requires `procps` (or the distribution's equivalent package), which provides `/bin/ps`, to identify and clean up additional terminal job groups.

Minimum target systems are Windows 10 1809+/Server 2019+, or Linux 3.2+ with PTY support. These minimums come from Go 1.27 and ConPTY requirements; they do not mean every older system version has been tested.

The current development environment is Linux/amd64. A Windows/amd64 VM passed native package tests and real-binary cycles, including terminals, file replacement and process-tree cleanup. Windows/arm64 has only been built. There is no frontend, public relay, tenant isolation, dedicated breakpoint debugging protocol or automatic system-service installation.

Earlier Windows CI exposed a ConPTY bug that reused host handles when standard input/output was redirected. Startup parameters were corrected, with regressions for interaction, Ctrl+C and idle cleanup under pipe and file redirection. Those regressions passed on the Windows/amd64 VM. That VM validation record predates the successful Actions runs linked above; those runs do not imply that the VM tests were repeated.

The recorded Linux/amd64 validation passed race tests, static analysis, an upload/run/download cycle with real binaries, and an actual 257 MiB bidirectional MCP transfer. The large-file test took approximately 29.74 seconds, with a sampled peak heap increase of about 9.46 MiB and matching SHA-256 hashes at both ends. A sampled heap increase is not equivalent to operating system RSS.

## Remote debugging: inspection, output waits and text patches

These capabilities share `/mcp`, authentication and service-account permissions with the existing upload, execution and terminal tools. They add no CLI options.

| Tool | Purpose and main parameters |
| --- | --- |
| `environment_inspect` | System, architecture, kernel and hostname; optional `runtimes` selects a subset of go/node/python/java/dotnet |
| `process_inspect` | Bounded PID/PPID/name snapshot; optional `root_pid` returns that process and its descendants |
| `network_listeners` | IPv4/IPv6 TCP LISTEN addresses, ports and visible PIDs; optional `pid/port/limit` |
| `network_probe` | `mode` is dns/tcp/tls/http; `target` is a hostname, host:port or HTTP(S) URL as appropriate |
| `process_read`, `terminal_read` | Optional `wait_ms`; omitted or 0 reads immediately, maximum 30000 ms |
| `log_open/read/close` | Follow an existing regular log file; read uses `id/generation/cursor/limit/wait_ms` |
| `file_list` | Paginate one directory with `path/offset/limit`; returns `next_offset` and limit reasons |
| `file_read` | Read UTF-8 lines with `path/start_line/line_count/max_bytes`; returns the whole-file `sha256` |
| `file_search` | Case-sensitive literal substring search with `path/query`; configurable depth, entry, byte and match budgets |
| `file_patch` | Modify an existing regular UTF-8 file with `path/expected_sha256/edits` after verifying its original contents |

Inspection has a default total budget of 5 seconds, with `timeout_ms` capped at 10000 and at most 4 concurrent inspections. Snapshots return 256 entries by default, up to 4096 per call; internal PID/FD scans are also bounded. `partial/warnings/truncated/visibility` indicate permission, dependency, scan-limit or visibility gaps. An empty list does not prove that no objects exist on the machine. Linux uses the current `/proc` namespace; Windows uses Toolhelp and IP Helper. Missing dependencies or permissions produce English codes and specific reasons. Runtime probes use the service account's PATH without collecting full environment variables or process command lines.

Network probes share one deadline across DNS→TCP→TLS→HTTP. Failures retain `stages/failed_stage/code/message` in `structuredContent`; literal IP addresses may skip DNS. TLS verifies certificates using system CAs. HTTP sends one GET without proxies, redirects or response-body reads. HTTP 4xx/5xx responses are valid protocol responses; the caller determines application health. For example:

```json
{"name":"network_probe","arguments":{"mode":"http","target":"http://127.0.0.1:8080/health","timeout_ms":3000}}
```

Read results distinguish `immediate/output/exit/timeout/cancelled` through `reason`. Raw Base64 bytes, byte cursors, UTF-8 views and truncation flags are retained. HTTP cancellation ends only the current wait, not the application process. Pass the previous `next_cursor` back before waiting for new output:

```json
{"name":"process_read","arguments":{"id":"resource-id","stream":"stdout","cursor":128,"wait_ms":10000}}
```

Log resources default to a maximum of 32, are reclaimed after 10 minutes of inactivity, check file changes about every 100 ms, and return at most 64 KiB per read. `log_open` returns `generation/end_cursor`: use cursor=0 to read from the beginning, or end_cursor to read only later appends. Pass the previous `generation/next_cursor` to `log_read`. Path replacement or shortening resets the generation and offset and returns `rotated/truncated`. Waiting reads can also report `reason=rotated/truncated`; immediate reads retain `immediate`, with changes indicated by boolean flags. Truncation followed by restoration between samples, or an in-place rewrite without a size decrease, may go undetected. This is not a file auditing protocol. Explicit `log_close` or service shutdown ends the resource.

File reads and modifications default to a maximum of 8 MiB, with at most 64 KiB of returned text. `next_line` is the first line not fully returned. For `truncated/reason=byte_limit`, increase `max_bytes` within the limit and reread. Lines longer than 64 KiB have only a bounded UTF-8 preview; skip such a line with `end_line+1` instead of blindly concatenating repeated prefixes. Use the download tools for full bytes. Search matches literal text within individual lines, never across lines, with limits of 10000 entries, 64 MiB scanned, 8 MiB per file, 16 levels and 1000 matches, plus a response-byte budget. Long lines are fully matched within the scan budget even if the preview omits the matching text. `issues/truncated/reason` explain invalid UTF-8, permission errors or budget limits. Directory pagination follows filesystem enumeration order; concurrent changes can move offsets. It does not promise a stable snapshot or follow symbolic links.

Patch line numbers start at 1 and must increase strictly without overlap in **original-file** coordinates; `total_lines+1` appends. `text` supplies the exact replacement bytes, including caller-provided LF or CRLF; newlines are not added automatically. Read the whole-file hash before sending a line patch, for example when the original second line uses CRLF:

```json
{"name":"file_read","arguments":{"path":"/srv/app/config.txt","start_line":1,"line_count":20}}
```

```json
{"name":"file_patch","arguments":{"path":"/srv/app/config.txt","expected_sha256":"<sha256-from-file_read>","edits":[{"start_line":2,"delete_count":1,"text":"新的配置\r\n"}]}}
```

A wrong hash or observed external modification returns `conflict` and preserves the existing target. A temporary file in the same directory is verified, synced and closed before atomic replacement; failures clean up the temporary name. This module serializes patches. After request disconnection or wait cancellation, call `file_read` again to check the actual hash before proceeding; do not blindly retry the old hash. A non-cooperating external writer can still change the target between final hash verification and publication, so this is not strict filesystem CAS. Permission preservation uses portable platform permission bits, not a complete copy of custom Windows ACLs. Cancellation is checked between filesystem operations and cannot forcibly interrupt a blocked kernel operation.

Regular logs record only the tool, duration, result, and controlled English error codes and reasons. They exclude file contents, query, edits, URLs, runtime version output and credentials. Local Linux tests and the four CGO-free builds are recorded separately. Native PID, port, terminal and file behavior passed on a Windows/amd64 VM; Windows/arm64 remains build-only. P0 additions do not close the remaining native GUI acceptance gaps. DAP, debugging-session aggregation, diagnostic bundles, reverse tunnels and additional GUI capabilities are deferred.

Real-binary local validation without GUI or public-network access, using locally built Go binaries on Linux:

```sh
CGO_ENABLED=0 go build -o bin/ ./cmd/...
python3 scripts/smoke.py --bin-dir bin
python3 scripts/smoke.py --bin-dir bin --no-token
python3 scripts/p0-smoke.py --bin-dir bin --report-file dist/p0-token.json
python3 scripts/p0-smoke.py --bin-dir bin --no-token --report-file dist/p0-anonymous.json
```

Windows PowerShell:

```powershell
$env:CGO_ENABLED = '0'
go build -o bin/ ./cmd/...
Remove-Item Env:CGO_ENABLED
python scripts/smoke.py --bin-dir bin
python scripts/smoke.py --bin-dir bin --no-token
python scripts/p0-smoke.py --bin-dir bin --report-file dist/p0-token.json
python scripts/p0-smoke.py --bin-dir bin --no-token --report-file dist/p0-anonymous.json
```

Existing four-combination build outputs can also be tested with directories such as `--bin-dir dist/linux-amd64`; the directory must match the running system and architecture. P0 smoke tests cover tool discovery, the current PID and listening port, loopback network success and failure, Chinese CRLF patches and conflict preservation, immediate/output-wait/timeout/exit reads for processes and real terminals, and log append/rotation/same-file truncation. Success is reported only after service and test-resource cleanup and regular-log filtering pass. Optional `--report-file` writes a bounded, strictly UTF-8 JSON report. Each run deletes any old report first; failure leaves neither stale success nor an early success result. Reports contain only check booleans, protocol and authentication mode, server system/architecture and Python host system/architecture, excluding credentials, user content, paths, hostnames and process lists. Windows stops the service with `TerminateProcess` and reports `service_shutdown=terminate_process`; this does not establish graceful signal-driven shutdown.

The Linux/Windows CI entry point runs four smoke rounds: existing functionality and P0, each with token and anonymous modes. Native binary builds explicitly disable CGO, race checks retain their required configuration, and all four cross-build combinations run separately. P0 reports are stored in artifacts named for each runner platform, with evidence for each successful mode. Earlier Linux/amd64 and Windows/amd64 native validation passed all four rounds; the Windows VM passed 111 top-level tests across nine packages, with 4 platform- or condition-specific skips. This is a historical VM record, distinct from the successful main and v0.1.0 Actions runs linked above. The VM lacked Python, so it used the Go helper below rather than the Python smoke scripts. Windows/arm64 has only build results, the VM did not run Windows race detection, and GUI acceptance remains separate.

If the target Windows machine has no Python, deploy a precompiled Go helper through an **authorized** existing MCP endpoint from your local machine:

```sh
python3 scripts/windows-native-smoke.py \
  --url http://windows-host:8080/mcp \
  --bin-dir dist/windows-amd64 \
  --report-file dist/windows-native.json
```

The script uses a unique temporary directory and a separate loopback service, cleans up afterward, then confirms that the original endpoint remains available. It preserves the original service and configuration. `--skip-package-tests` runs only the four service rounds; repeat `--package` to select packages, or use `--package-only` for package tests alone. Package-only reports retain `protocol_smoke=false` and do not establish four-round success. Reports contain bounded check results and uploaded-artifact hashes, without credentials, directories or user content.

## Windows desktop screenshots and input

GUI operations are supported only on Windows. Linux retains file, process, PTY terminal, TCP forwarding and remote debugging tools, with no `gui_*` tools or `gui-*` startup options.

Windows GUI uses the existing `/mcp` endpoint with anonymous or optional token authentication and controls only the service account's current interactive desktop. Start the service within a logged-in graphical session; background services, locked screens and secure desktops are not guaranteed to be accessible. UIPI may reject input to applications running with higher privileges. In anonymous mode, reachable clients can also request desktop operations. Desktop unavailability does not prevent other tools from starting.

| Tool | Parameters and purpose |
| --- | --- |
| `gui_status` | `{}` inspects the backend and capabilities; `{"id":"session-id"}` inspects a session, displays and unavailable reasons |
| `gui_open` | `{"request_id":"desktop-001","wait_ms":10000}` opens the current user's graphical session |
| `gui_close` | `{"id":"session-id"}` releases resources; repeated close is safe during record retention |
| `gui_screenshot` | `id`, optional `display_id/region`; captures a whole display or region as PNG |
| `gui_mouse` | `id/capture_id/action/x/y`; move, click, double-click, drag, and horizontal/vertical scroll |
| `gui_key` | `{"id":"session-id","keys":["Ctrl","A"]}` submits a key or chord and releases the keys pressed by this call |
| `gui_text` | `id/text`, optional `mode/paste_keys/allow_clipboard_replace`; submits UTF-8 text, including Chinese and emoji |

Sessions are deduplicated by `request_id`; the same ID with different parameters returns `conflict`. The service allows at most one authorizing or ready session, and returns `busy` while an operation is running. HTTP/MCP disconnection does not destroy graphical resources; sessions default to reclamation after 30 minutes without calls. Normal shutdown releases desktop handles, and restart invalidates old IDs.

### Screenshots and coordinates

After opening a session, request a screenshot, for example:

```json
{"id":"session-id","display_id":"display-id","region":{"x":100,"y":80,"width":640,"height":480}}
```

Omitting `display_id` selects the default or first display; omitting `region` captures the whole display. Screenshots are not scaled or stitched into a combined desktop. The response contains a standard MCP `image` content block (`mimeType: "image/png"`) and `structuredContent` metadata: `capture_id/display_id/width/height/region/logical_bounds/layout_generation/captured_at/captured_at_source/frame_sequence/freshness`. Structured results do not duplicate the PNG. Displaying it requires client support for MCP image content.

Mouse coordinates use pixels within the returned screenshot, with `(0,0)` at its top-left corner. For example, click `(120,50)` in the region above:

```json
{"id":"session-id","capture_id":"capture-id-from-this-screenshot","action":"click","x":120,"y":50,"button":"left"}
```

The service adds the crop offset and converts through the actual display mapping; callers must not add the desktop origin or scale again. Drag endpoints refer to the same screenshot. Multiple displays, negative desktop coordinates and DPI use the actual mapping. Layout changes, expiration or eviction return `stale_capture`; take a new screenshot to locate the target again.

Native screenshots use `captured_at_source: acquired_at`. An input result with `submitted` means only that events were submitted; take another screenshot or inspect the actual control to confirm application acceptance. Cancellation or failure may already have applied some input, indicated by `input_may_have_applied`; do not blindly retry.

### Chinese text input and limits

```json
{"id":"session-id","text":"你好，图形桌面 😀","mode":"auto"}
```

On Windows, `auto/direct` uses native Unicode input without reading or modifying the clipboard. Explicit `mode: "clipboard"` returns `unsupported`; retained paste-related parameters do not imply clipboard-mode support. Regular logs contain neither images nor input text.

The following startup options are available only on Windows, and values must be positive:

| Option | Default |
| --- | --- |
| `--gui-idle` | `30m` |
| `--gui-authorize-timeout` | `2m` |
| `--gui-operation-timeout` | `30s` |
| `--gui-max-pixels` | 16777216 original display pixels |
| `--gui-max-png-bytes` | 16777216 (16 MiB) |

Final-state records default to 10-minute retention and a maximum of 256. Each session retains up to 64 coordinate records, valid for 5 minutes. Text is limited to 64 KiB of UTF-8, chords to 8 keys, and drags to 10 seconds.

### Real Windows desktop validation

CI without a desktop can validate only code and builds. After starting the service on the target Windows desktop, run the following from a Linux or Windows Python host:

```sh
python3 scripts/gui-smoke.py --execute \
  --url http://WINDOWS_HOST:8080/mcp --no-token \
  --desktop-label 'Windows' --output-dir .tmp/gui-capture-evidence
```

This command uses independent JSON-RPC to check tool discovery, the backend, PNG image blocks, scanlines and capture metadata, then closes the session. It defaults to queries and screenshots only. Token mode uses `REMOTE_MCP_TOKEN` or `--token-file`; HTTPS can use `--ca-file`. Without `--execute`, it shows help without connecting. The output directory must not already exist.

Use `--input-plan .tmp/plan.json` to operate a dedicated application that the caller has prepared on the selected display. Plans contain `target` and `operations`, restricted to `gui_mouse/gui_key/gui_text`; the script supplies the session ID and first screenshot ID. For example:

```json
{
  "target":"A prepared and focused dedicated test editor",
  "operations":[
    {"tool":"gui_mouse","arguments":{"action":"click","x":320,"y":200}},
    {"tool":"gui_text","arguments":{"text":"中文测试"}}
  ]
}
```

By default, plans verify only returned event results; application outcomes require separate confirmation. If a plan saves a file, add `path/expected` under `verify_text_file` to check a previously nonexistent UTF-8 artifact readable by the script. Transfer remote files separately; remote paths are not read as local files.

When Windows has no Python, deploy a Go helper with a dedicated native window through an authorized anonymous MCP endpoint from your local machine:

```sh
python3 scripts/windows-gui-smoke.py --execute \
  --url http://WINDOWS_HOST:8080/mcp \
  --bin-dir dist/windows-amd64 \
  --report-file .tmp/windows-gui-evidence.json
```

The script checks SHA-256 hashes of local artifacts, remote files and the actual running programs. Local caches and packages stay in the project `.tmp/`; remote deployment uses `.tmp/` under the MCP service working directory, which must be writable. A temporary loopback service runs token and anonymous rounds. Input targets only the helper's own Win32 window after verifying foreground identity and input focus. It checks four-color markers in cropped PNGs, real mouse and wheel events, select-all and clearing, and actual Chinese/emoji text in the EDIT control. Native Unicode input never accesses the clipboard. The window, session, temporary service and deployment directory are cleaned up, and the original MCP service is checked again before publishing the report. The existing native/P0 entry point still defaults to four rounds.

On 2026-10-07, the current build completed the token and anonymous native cycles above on Windows/amd64 with one display at 100% DPI. This does not cover native Windows/arm64, multiple monitors, mixed DPI, layout changes, permission revocation or image display in an actual MCP client. Reports retain `acceptance_complete: false`; builds, simulated protocol results and real desktop evidence are recorded separately. Historical Linux GUI research is retained for traceability, outside the current supported scope and deliverables.

Regular logs for failed MCP tools retain `tool/elapsed/failed` and include `error_code/error_message`; program-owned errors and prompts are in English. GUI failures may also include `input_may_have_applied/clipboard_restore`. For example, explicitly requesting clipboard mode on Windows returns `unsupported` and logs a specific English reason. Successful calls have no error fields. Logs accept safe business explanations only from known built-in tool domains. SDK parameter failures are categorized, such as missing required fields or type mismatches; unknown tools and exceptions use safe classifications without printing raw arguments, contents or error payloads. Error explanations are limited to 1024 bytes, control characters are sanitized, and nonempty tokens and recognizable input contents are redacted. Future custom tools need a separate safe logging contract before logging original error reasons.
