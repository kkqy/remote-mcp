# Remote MCP

**English** | [简体中文](README.zh-CN.md)

A Go MCP service deployed on a remote machine. Agents connect over a local network or VPN to transfer files, run commands, and operate real interactive terminals. The service includes two programs: the `remote-mcp` server and the `remote-mcp-transfer` local file transfer helper.

## Build and start

The module requires Go 1.25 or later and builds with Go 1.27 by default. CGO is not required:

```sh
go build -o bin/remote-mcp ./cmd/remote-mcp
go build -o bin/remote-mcp-transfer ./cmd/remote-mcp-transfer
```

You can start the server directly without configuring a token. Linux/macOS:

```sh
./bin/remote-mcp
```

Windows PowerShell:

```powershell
.\remote-mcp.exe
```

Token authentication is optional. When `REMOTE_MCP_TOKEN` is unset or empty and `--token-file` is not specified, the service allows anonymous access: any client that can reach it can use the file and command permissions of the account running the service. If a token was previously set in your terminal, clear it before starting with `unset REMOTE_MCP_TOKEN` in Bash or `$env:REMOTE_MCP_TOKEN = $null` in PowerShell.

To enable authentication, configure a nonempty token before starting. Linux/macOS:

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

VPN addresses receive separate configurations when available. **Choose a configuration reachable from your agent**; all listed addresses belong to the same service. Each block is a separate JSON object; concatenating them does not produce a single JSON document. Anonymous mode omits `headers` entirely. With authentication enabled, the configuration adds `"headers": {"Authorization": "Bearer <current token>"}` containing the actual token, so treat it as a credential. Explanatory messages in Chinese and regular logs go to stderr. Regular logs do not record tokens, environment variable values, file contents, or terminal input/output.

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

`terminal_resize` accepts `id`, `columns`, and `rows`. The foreground program handles Ctrl+C input itself; use `terminal_close` for forced cleanup. Windows uses ConPTY, while Linux/macOS use PTY. By default, terminals are reclaimed after 30 minutes without API call activity; continuous output does not extend this period. After a brief network disconnection, use the original ID to continue.

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

The build script generates `dist/{linux,darwin,windows}-{amd64,arm64}/`, with both programs in each combination. GitHub Actions runs native tests and race checks on Linux, Windows, and macOS, exercises an upload/run/download cycle with real binaries, and builds all six combinations. CI runs that have not actually been executed cannot be counted as passed local validation.

Linux also requires `procps` (or the distribution's equivalent package), which provides `/bin/ps`, to identify and clean up additional terminal job groups.

The minimum target systems are Windows 10 1809+/Server 2019+, macOS 13+, and Linux 3.2+ with PTY support. These minimums come from Go 1.27 and ConPTY requirements and do not mean every older OS version has been tested.

The current development environment is Linux/amd64. Native terminal behavior, file replacement, and process tree cleanup on Windows/macOS still require tests on those systems; cross-compilation only demonstrates that the programs build. The project does not include a frontend, public relay, multi-tenant isolation, a dedicated breakpoint debugging protocol, or automatic system service installation.

Windows CI previously exposed a problem where redirected host standard input/output caused ConPTY sessions to use host handles incorrectly. The startup parameters have been fixed, with regression tests added for interaction, Ctrl+C, and idle cleanup when the host uses pipe or file redirection. The Windows native results after the fix still need CI confirmation.

The recorded Linux/amd64 validation passed race tests, static analysis, an upload/run/download cycle with real binaries, and an actual 257 MiB bidirectional MCP transfer. The large-file test took approximately 29.74 seconds, with a sampled peak heap increase of about 9.46 MiB and matching SHA-256 hashes at both ends. A sampled heap increase is not equivalent to operating system RSS.
