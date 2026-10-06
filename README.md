# Remote MCP

**English** | [简体中文](README.zh-CN.md)

A Go MCP service deployed on a remote machine. Agents connect over a local network or VPN to transfer files, run commands, and operate real interactive terminals. The service includes two programs: the `remote-mcp` server and the `remote-mcp-transfer` local file transfer helper.

程序自有错误、提示、CLI 帮助和 MCP 工具/参数说明使用英文；文档和源码注释继续使用中文。用户文件、终端输出及 GUI 输入保持原有字节或文本，支持中文。

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

## 图形桌面截图与操作

GUI 工具使用原有 `/mcp` 入口、匿名或可选 Token 模式，只操作服务运行账号的当前用户图形桌面。请在已登录的图形会话中启动服务；SSH、后台系统服务、锁屏或安全桌面不保证有可访问的图形会话。匿名模式下，能够连接服务的客户端也可申请桌面操作。GUI 依赖或权限缺失只影响 GUI 工具，文件、进程、终端和转发仍可使用。

| 工具 | 参数与用途 |
| --- | --- |
| `gui_status` | `{}` 只读查询后端及能力，不弹出授权；`{"id":"会话 ID"}` 查询状态、获授权显示器及缺失能力原因 |
| `gui_open` | `{"request_id":"desktop-001","wait_ms":10000}` 创建当前用户图形会话；状态为 `authorizing` 时继续查询 `gui_status` |
| `gui_close` | `{"id":"会话 ID"}` 释放桌面资源；记录保留期内允许重复关闭 |
| `gui_screenshot` | `id`、可选 `display_id` 与 `region`，获取整显示器或区域 PNG |
| `gui_mouse` | `id/capture_id/action/x/y`，支持 `move/click/double_click/drag/scroll`；拖拽增加 `end_x/end_y/duration_ms`，滚动增加 `scroll_x/scroll_y` |
| `gui_key` | `{"id":"会话 ID","keys":["Ctrl","V"]}` 单键或组合键，一次调用自动释放本次按下的键 |
| `gui_text` | `id/text` 与可选 `mode/paste_keys/allow_clipboard_replace`，提交 UTF-8 文本，包括中文 |

会话按 `request_id` 去重；同 ID、不同参数返回 `conflict`。一次服务最多一个待授权或可操作的图形会话，输入正在执行时返回 `busy`。HTTP/MCP 断线不会销毁图形资源；默认无调用活动 30 分钟回收。服务正常退出释放桌面、辅助采集进程和句柄，服务重启后旧 ID 失效。

### 授权与平台依赖

- **Windows**：当前交互桌面上的原生截图与输入。系统的 UIPI 权限限制可能拒绝对更高权限应用输入；不控制安全桌面或 Windows 服务会话。
- **macOS**：分别授予“屏幕录制”和“辅助功能”权限。采集权限不能代替输入权限；授权后可能需要按系统提示重新启动服务。
- **Linux X11**：运行账号需要连接当前 X 显示服务器，并具备 XTEST 输入扩展。能力以实际连接及扩展探测为准。
- **Linux Wayland**：首版完整操作目标为 GNOME/Mutter 和 KDE Plasma/KWin。需要当前用户会话 D-Bus、PipeWire、`xdg-desktop-portal` 及与桌面匹配的 GNOME/KDE Portal 后端，并安装 `gst-launch-1.0`、`gst-inspect-1.0` 和 `pipewiresrc/videoconvert/pngenc/fdsink` 插件。Go 二进制仍无需 CGO，桌面采集依赖系统组件。

Wayland 首次打开或授权失效时，目标机用户需在系统 Portal 对话框选择屏幕并确认输入权限；用户可以取消或撤销。工具只列出实际可访问或获授权的显示器和设备，不把请求的权限当作已经获授予。运行期间复用已授权会话，不把恢复令牌写盘；不能承诺重启后永久免提示。

Wayland 的 Portal 包名随发行版不同。Debian/Ubuntu 系可核对 `xdg-desktop-portal-gnome` 或 `xdg-desktop-portal-kde`、`gstreamer1.0-pipewire`、`gstreamer1.0-tools`、`gstreamer1.0-plugins-base`、`gstreamer1.0-plugins-good`；其他发行版安装提供相同命令和插件的包。用以下命令确认运行组件：

```sh
gst-inspect-1.0 pipewiresrc
gst-inspect-1.0 videoconvert
gst-inspect-1.0 pngenc
gst-inspect-1.0 fdsink
```

GNOME/KDE 的实际版本、权限和插件组合仍须逐台核验；基础服务的系统下限不等于 GUI 的兼容下限。Sway、Hyprland 等其他 Wayland 桌面按实际 Portal 能力报告，不保证完整输入控制；XWayland 窗口可用也不能证明原生 Wayland 桌面完整可控。缺失依赖、无图形会话或无权限时，检查 `gui_status` 的能力及原因。

### 截图与坐标

打开会话后获取截图，例如：

```json
{"id":"会话 ID","display_id":"显示器 ID","region":{"x":100,"y":80,"width":640,"height":480}}
```

省略 `display_id` 时选择默认或首个获授权显示器，省略 `region` 时获取整个显示器。截图不缩放，不拼接全部桌面，也不按窗口专用捕获。返回一个标准 MCP `image` 内容块（`mimeType: "image/png"`），以及 `structuredContent` 中的 `capture_id/display_id/width/height/region/logical_bounds/layout_generation/captured_at/captured_at_source/frame_sequence/freshness`。图片展示需要客户端支持 MCP 图片内容；结构化结果不重复完整 PNG。

鼠标坐标以**返回截图内部像素**为单位，左上角为 `(0,0)`。例如在上述区域图的 `(120,50)` 单击：

```json
{"id":"会话 ID","capture_id":"本次截图 ID","action":"click","x":120,"y":50,"button":"left"}
```

由服务加入区域偏移并转换到显示器逻辑坐标，调用方不再自行加桌面原点或缩放。拖拽终点也相对同一截图。多屏、负桌面坐标和混合缩放依赖实际显示器映射；布局改变、记录到期或被淘汰后，旧截图输入返回 `stale_capture`，应重新截图定位。

输入结果的 `submitted` 仅表示事件已提交；应用是否接受、文字是否进入控件，应再次截图核对。`freshness` 与 `captured_at_source` 说明时间来源和新鲜度。原生采集的 `captured_at_source` 为 `acquired_at`；Wayland GStreamer PNG 没有源帧时间戳，标记为 `received_at` 和 `latest_available`，`captured_at` 表示首帧接收时间，不能据此保证源画面已经在输入之后渲染。等待有上限，不把调用返回时间当作源画面采集时间。输入取消或失败可能已经产生部分操作，`input_may_have_applied` 用于提示这一情况，不应盲目重试。

Wayland 的绝对坐标还要求可用的逻辑布局监视器：GNOME 使用 Mutter DisplayConfig 的 `MonitorsChanged`，KDE 使用实际可读取的 KScreen `getConfig/configChanged`。逻辑缩放或排列变化、监视服务断线或不可用时，会关闭当前绝对输入映射，旧截图不能继续定位；需 `gui_close` 后重新 `gui_open` 授权。媒体像素尺寸不变也不能证明逻辑布局未变。KDE 上游接口会演进，不能将旧 KScreen D-Bus 路线视为所有版本通用；能力以运行时探测为准。

### 中文输入与剪贴板

```json
{"id":"会话 ID","text":"你好，图形桌面","mode":"auto"}
```

`mode` 为 `auto/direct/clipboard`；默认 `auto` 优先直接文本输入，否则采用剪贴板粘贴。Windows/macOS 当前使用原生 Unicode 输入，显式 `clipboard` 模式返回 `unsupported`；各后端实际能力见 `gui_status`。兼容模式先可靠保存原内容及 MIME 信息，粘贴后恢复；默认 `allow_clipboard_replace: false`。无法可靠保留时返回 `clipboard_preservation_unavailable`，不会直接覆盖剪贴板。明确接受替换时可设为 `true`，结果会标明替换与恢复状态。发现其他应用已经改变剪贴板时不覆盖其新内容；恢复失败明确报告，不把部分输入失败包装为无副作用。

部分 KDE Portal 在授权开始时仅订阅后续剪贴板变更，不发送现有格式；Clipboard v1 也没有查询全部现有格式的接口。此时默认输入返回 `clipboard_preservation_unavailable`，直到获得可确认的格式通知。服务不会自动改写剪贴板来探测初始内容。

普通应用默认粘贴键为 Windows/Linux 的 Ctrl+V、macOS 的 Meta+V。终端等不同目标可明确提供 `"paste_keys":["Ctrl","Shift","V"]`。剪贴板授权和键盘授权都需要可用；目标控件可能不接受粘贴。工具和普通日志不会返回原剪贴板内容，也不会记录图像、输入文本或 Portal 恢复令牌。

| 配置参数 | 默认值 |
| --- | --- |
| `--gui-idle` | `30m` |
| `--gui-authorize-timeout` | `2m` |
| `--gui-operation-timeout` | `30s` |
| `--gui-max-pixels` | 16777216 个原始显示器像素 |
| `--gui-max-png-bytes` | 16777216（16 MiB） |

以上必须为正值。GUI 终态默认保留 10 分钟、最多 256 条记录；每会话最多保留 64 份坐标元数据、5 分钟有效。文字上限 UTF-8 64 KiB，剪贴板快照上限 1 MiB。组合键最多 8 个，拖拽最长 10 秒。

### 真实桌面验证

普通无桌面 CI 只检查代码与构建，不能算 GUI 原生验收。先在目标桌面启动服务，再显式运行：

```sh
python3 scripts/gui-smoke.py --execute --url http://127.0.0.1:8080/mcp --no-token --desktop-label 'KDE Plasma Wayland' --output-dir /tmp/gui-capture-evidence
```

该命令只查询、打开会话及截图，不向未知前台窗口输入。脚本使用独立 JSON-RPC，检查工具发现、标准 PNG 图片块、全部 PNG 扫描行、坐标和采集元数据，并关闭会话。Token 模式使用 `REMOTE_MCP_TOKEN` 或 `--token-file`，HTTPS 可用 `--ca-file`。不带 `--execute` 只显示帮助，不访问 GUI。

完整基本操作可用可选验证依赖 **PySide6** 的专用窗口：

```sh
python3 scripts/gui-smoke.py --execute --fixture --url http://127.0.0.1:8080/mcp --no-token --desktop-label 'KDE Plasma Wayland' --output-dir /tmp/gui-input-evidence
```

脚本从真实截图的四个颜色标记定位窗口，验证点击、双击、拖拽、滚动、全选组合键和中文实际出现在控件；Wayland 强制使用原生 Qt Wayland 窗口。窗口必须完整可见且位于所选显示器，必要时使用 `--display-id`。用户自行确认系统授权，脚本不自动点击授权对话框。完整验证的可见中文字段接收全选快捷键后直接选中内容，避免 Qt 普通快捷键路径自动发布 PRIMARY；不会为验证而读取或回写 PRIMARY。剪贴板保留策略默认保持；只有明确接受替换时才增加 `--allow-clipboard-replace`。

仅验证键鼠时，可使用 `--fixture --fixture-inputs-only`：

```sh
python3 scripts/gui-smoke.py --execute --fixture --fixture-inputs-only --no-token --authorize-wait-seconds 605 --output-dir /tmp/gui-inputs-only-evidence
```

这个入口只向专用窗口执行点击、双击、拖拽、滚动、全选和 Backspace，核对固定字段实际清空，不调用 `gui_text`，不读取、重新发布或恢复剪贴板，也不能与刷新或允许替换并用。字段使用密码回显，阻止 Qt 普通全选自动复制到 PRIMARY 选区；报告只声明测试自身未调用剪贴板读写或选区复制，不保证外部合成器、剪贴板管理器或其他应用不会独立改变内容。中文输入及默认剪贴板恢复仍列为待验收。会话 ready 后先等待专用窗口获得焦点，上限使用授权等待参数；每次操作前的焦点确认仍只等待 10 秒。

默认专用窗口验证会只读采样原剪贴板，在文本输入前后及 `gui_close` 后重新核对各 MIME 的长度和 SHA-256，不保存原内容。格式超过 64 种、累计内容超过 1 MiB、读取超时或哈希发生变化时，验证失败。Qt 会先物化单个格式，采样上限约束摘要处理与保留，不能作为 Qt 底层读取量的保证。显式替换模式不计为原内容恢复验收。

在上述初始格式盲区环境，专用窗口测试可显式增加 `--refresh-clipboard-offer`。会话 ready 后，窗口聚焦读取全部 MIME（最多 64 种、累计 1 MiB），两次稳定读取均须与原基线哈希一致；仅在内存重新发布全部原格式及相同字节，并核对发布后摘要。Qt 可能自动新增 `text/plain;charset=utf-8`：只在原基线没有此格式，且它的长度和 SHA-256 与原 `text/plain` 完全相同时接受这个派生别名；任何其他新增、原格式移除或字节变化均失败。报告明确记录原格式摘要一致及新增别名，发布后的完整格式集作为输入恢复基线，同时独立保留原基线，在输入前后和关闭后核对全部原格式。该参数不能与 `--allow-clipboard-replace` 并用，不代表服务能够默认读取初始剪贴板。提供者由 Qt 管理，新的所有者或窗口退出时释放；发布前检查不能提供桌面协议未实现的原子所有权比较，发现变化即中止验收，不覆盖新内容。

若原快照包含 `application/x-kde-onlyReplaceEmpty`，默认文字输入、同内容刷新及自动救援均在发布前拒绝。它是 KDE 所有权控制标记，原样复制到非空剪贴板可能被取消，不能当作普通 MIME 数据，也不会被测试自动剥除。用户主动点击恢复按钮时，若备份含此标记，会额外询问是否仅恢复有效数据并省略该控制标记；确认后仍核对有效数据摘要，但明确记录原格式集合未完全恢复，不能作为完整保存恢复验收。

显式刷新还会将全部原格式字节保留在专用窗口的有界内存中，失去剪贴板所有权不清除备份，原内容不写盘或日志。失败后仅在会话已确认关闭、当前内容为空或与本次测试文字一致、读取版本稳定且 Qt 能确认本进程仍拥有提供者时自动救援；内容相等或 KDE 标记本身不能证明没有第三方所有者。无法确认时报告 `clipboard_recovery`、`fixture_retained` 和窗口 PID，保留窗口及备份，请用户点击“Restore original clipboard”（恢复原剪贴板）明确恢复后再关闭；关闭时会提醒备份尚未恢复。救援不改变本次验收失败结果。

若人工授权需要更长时间，验证脚本可指定 `--authorize-wait-seconds 605`（默认 125 秒，允许 1～605 秒），并将测试服务配置为 `--gui-authorize-timeout 10m`。这只延长会话授权及 ready 后首次窗口聚焦的有界等待，不自动批准或重复发起授权。

也可用 `--input-plan /路径/plan.json` 控制调用方准备的专用应用。计划包含目标说明、输入工具及参数；脚本补入会话 ID 和首张截图 ID，坐标相对首张整显示器截图。例如：

```json
{
  "target":"已准备并置于所选显示器的测试编辑器",
  "operations":[
    {"tool":"gui_mouse","arguments":{"action":"click","x":320,"y":200}},
    {"tool":"gui_text","arguments":{"text":"中文测试"}}
  ]
}
```

自定义计划默认只核验事件返回，需人工确认图像及文字。若计划包含保存操作，可增加 `"verify_text_file":{"path":"脚本可读且事前不存在的产物路径","expected":"中文测试"}` 核对应用实际保存的 UTF-8 文件。脚本与目标运行在不同机器时，产物需另行传输到可读路径，脚本不把远端路径视为本地文件。

截图与元数据写入指定的新目录，专用窗口证据写入 `fixture.json`，最终 `report.json` 分列已核验与待验收项。报告始终保留 `acceptance_complete: false`，因为单次基本闭环不代替五类桌面、客户端展示、多屏混合缩放、剪贴板竞争和撤权断线的全部验收。交叉编译、模拟 HTTP 测试与真实桌面结果分别记录。


MCP 工具失败的普通日志保留 `tool/elapsed/failed`，并输出 `error_code/error_message`，程序自有错误原因及提示使用英文；GUI 失败还可包含 `input_may_have_applied/clipboard_restore`。例如剪贴板恢复失败会保留受控的恢复原因和计数。成功调用没有错误字段。日志只接收已知内置工具域的安全业务说明；SDK 参数失败按必填缺失、类型不符等类别说明，未知工具和异常使用安全分类，不打印原始参数、内容或错误载荷。错误说明最多 1024 字节，控制字符清理，非空 Token 及可识别的输入内容脱敏；未来自定义工具需单独建立安全日志契约才能输出原文原因。
