# Remote MCP

[English](README.md) | **简体中文**

部署在客户机上的 Go MCP 服务。Agent 通过内网或 VPN 连接客户机，传输文件、运行命令，以及操作真实交互终端。服务包含两个程序：`remote-mcp` 服务端和 `remote-mcp-transfer` 本地文件传输辅助命令。

程序自有错误、提示、CLI 帮助和 MCP 工具/参数说明使用英文；文档和源码注释继续使用中文。用户文件、终端输出及 GUI 输入保持原有字节或文本，支持中文。

## 构建与启动

模块最低要求 Go 1.25，默认使用 Go 1.27 构建，无需 CGO：

```sh
go build -o bin/remote-mcp ./cmd/remote-mcp
go build -o bin/remote-mcp-transfer ./cmd/remote-mcp-transfer
```

未配置 Token 时可以直接启动。Linux/macOS：

```sh
./bin/remote-mcp
```

Windows PowerShell：

```powershell
.\remote-mcp.exe
```

Token 是可选的。未设置 `REMOTE_MCP_TOKEN` 或该变量为空、且未指定 `--token-file` 时，服务允许匿名访问：能连接服务的客户端可使用运行账号的文件和命令权限。若当前终端之前配置过 Token，可用 Bash 的 `unset REMOTE_MCP_TOKEN` 或 PowerShell 的 `$env:REMOTE_MCP_TOKEN = $null` 清除后启动。

需要鉴权时，在启动前配置非空 Token。Linux/macOS：

```sh
export REMOTE_MCP_TOKEN='replace-with-a-random-ascii-token'
./bin/remote-mcp
```

Windows PowerShell：

```powershell
$env:REMOTE_MCP_TOKEN = 'replace-with-a-random-ascii-token'
.\remote-mcp.exe
```

配置的 Token 必须是不含空白的可打印 ASCII 字符，最多 8 KiB。也可使用仅当前账号可读的凭据文件：

```sh
chmod 600 /安全目录/remote-mcp.token
./bin/remote-mcp --token-file /安全目录/remote-mcp.token --listen 192.168.1.10:8080
```

Token 文件优先于环境变量，读取时去除文件首尾空白；显式指定的文件为空、不可读、格式或权限无效时启动失败，不会退回匿名模式。非空但非法的环境 Token 也会报错。Windows 的 Token 文件请通过文件 ACL 限制为运行账号可读；程序无法用 Unix 权限位代替 Windows ACL 检查。服务不接受 Token 命令行值，避免凭据出现在进程参数中。

默认监听所有 IPv4 接口的 `0.0.0.0:8080`。限制为本机可用 `--listen 127.0.0.1:8080`；IPv6 使用 `--listen '[::]:8080'`，仅监听 IPv6，不假定系统支持双栈。端口 `0` 由系统分配，启动配置展示实际端口。停止时使用 Ctrl+C 或正常终止信号，服务清理受管理的进程、终端、传输临时文件及句柄；强杀进程或断电无法保证临时文件清理。

## 复制启动配置

监听成功后，stdout 为每个启用网卡上的可用 IP 输出一份完整的 OpenCode 配置。同一网卡上的多个地址也分别展示，重复地址会去重并稳定排序。未配置 Token 时例如：

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

存在 VPN 地址时还会输出它的独立配置。**选择 Agent 可达的一份配置使用**；这些地址属于同一个服务。每段都是独立 JSON 对象，多段合在一起不是单个 JSON 文档。匿名模式完全省略 `headers`；启用鉴权后，配置会增加 `"headers": {"Authorization": "Bearer <当前 Token>"}`，其中包含真实 Token，请将其视为凭据保管。英文提示和普通日志写入 stderr，普通日志不记录 Token、环境变量值、文件内容或终端输入输出。

全接口展示会排除停用网卡、回环、未指定地址、组播和 IPv6 链路本地地址。单 IP 绑定只显示该地址；显式回环绑定说明仅限本机。网卡枚举失败时服务继续运行并提示手动配置，不输出 `0.0.0.0` 或 `::` 作为连接 URL。列出网卡地址不意味着 Agent 侧路由、防火墙或证书信任已就绪。

配置采用 [OpenCode 远程 MCP 格式](https://opencode.ai/docs/mcp-servers/#remote)：顶层为 `mcp`，服务类型为 `remote`，`enabled: true` 启用连接，`oauth: false` 关闭本服务不提供的 OAuth 流程。选择一段 JSON 合并到项目或用户的 `opencode.json`，已有配置只合并对应的 `mcp.remote-mcp` 条目，保留其他设置；重启 OpenCode 后使用 `opencode mcp list` 查看状态。此格式与 OpenCode 1.18.34 对应，不是 MCP 协议规定的统一配置文件；其他客户端须将相同 URL 及可选 Authorization 值转换成其配置格式。客户端须支持 Streamable HTTP 和协议版本 **2025-11-25**；启用鉴权时还需支持 Bearer Token；不提供 stdio、旧 SSE 或 OAuth 自动发现适配，也不表示已验证所有品牌 Agent。

## 鉴权与 HTTPS

未配置 Token 时不校验 Authorization。配置非空 Token 后，每个 MCP 请求均校验凭据，缺失或错误返回 HTTP 401，协议会话 ID 不替代鉴权。允许访问的客户端共享运行账号的文件及命令权限；工作目录不是沙箱，程序不自动提权。进程树回收针对受管理的进程组、终端作业和 Windows Job Object；程序主动脱离 Unix 会话等行为可能无法完整回收，不构成恶意程序隔离。建议用符合调试需要的专用账号启动。

加密 VPN 可承载 HTTP；普通内网可配置 HTTPS：

```sh
./bin/remote-mcp --tls-cert /证书/server.pem --tls-key /证书/server.key
```

证书必须覆盖所选 IP 或客户端手动配置的域名。启动配置会改为 `https://`；客户端仍须验证证书。辅助命令支持 `--ca-file /证书/ca.pem` 添加自签 CA，不提供跳过证书校验选项。

不含 `Origin` 的非浏览器请求可访问。有 `Origin` 的请求默认拒绝；确有需要时设置精确允许列表：

```sh
./bin/remote-mcp --allowed-origins https://agent.example,https://console.example
```

该选项不是免鉴权通道，也不自动配置浏览器 CORS 预检策略。防火墙应允许 Agent 所在内网或 VPN 访问监听端口。

## 上传和下载文件

辅助命令在 Agent 所在机器上运行，从本地磁盘读取真实字节，经 MCP 分块传输，文件内容不经过模型复述。

```sh
./bin/remote-mcp-transfer upload --url http://192.168.1.10:8080/mcp ./程序包.zip /客户机目录/程序包.zip
./bin/remote-mcp-transfer download --url http://192.168.1.10:8080/mcp /客户机目录/运行日志.txt ./运行日志.txt
```

默认不需要 Token，辅助命令不发送 Authorization。若服务端已启用鉴权，先设置相同的 `REMOTE_MCP_TOKEN`，或传入 `--token-file`；显式无效文件会报错。`--url` 可由 `REMOTE_MCP_URL` 提供。选项放在 `upload` 或 `download` 后、两个路径参数前。使用 `--overwrite` 才允许替换已有目标；上传和下载先写临时文件，大小和 SHA-256 校验通过后提交。失败返回非零退出码，进度写 stderr，最终 JSON 摘要写 stdout，包含 `ok`、`size`、`sha256` 或错误原因。

单文件默认 4 GiB、原始分块默认 256 KiB。目录请先用系统工具打包，压缩包按普通文件传输；服务不自动解包、不保留文件权限或原始时间戳、不支持目录同步和用户级断点续传。Unix 可执行文件上传后需用命令工具设置执行位，或用解释器运行上传的脚本。

## MCP 工具

客户端通过 `tools/list` 获得完整的类型化参数。业务失败返回 MCP `isError` 和稳定错误码；不要把协议 HTTP 200 当作工具成功。

MCP 工具失败的普通日志保留 `tool/elapsed/failed`，并输出 `error_code/error_message`，程序自有错误原因及提示使用英文；GUI 失败还可包含 `input_may_have_applied/clipboard_restore`。例如剪贴板恢复失败会保留受控的恢复原因和计数。成功调用没有错误字段。日志只接收已知内置工具域的安全业务说明；SDK 参数失败按必填缺失、类型不符等类别说明，未知工具和异常使用安全分类，不打印原始参数、内容或错误载荷。错误说明最多 1024 字节，控制字符清理，非空 Token 及可识别的输入内容脱敏；未来自定义工具需单独建立安全日志契约才能输出原文原因。


| 工具 | 用途 |
| --- | --- |
| `file_stat` | 查询路径信息 |
| `upload_create` / `upload_write` / `upload_finish` / `upload_cancel` | 创建、顺序分块写入、校验提交及取消上传 |
| `download_open` / `download_read` / `download_close` | 打开、按字节偏移分块读取及关闭下载 |
| `process_start` / `process_read` / `process_status` / `process_stop` | 执行、读取输出、查询及停止进程树 |
| `terminal_open` / `terminal_write` / `terminal_read` | 创建真实终端、输入和读取合并输出 |
| `terminal_resize` / `terminal_status` / `terminal_close` | 调整尺寸、查询及关闭终端 |

创建上传、进程和终端必须传入调用方生成的 `request_id`。同一 ID 和相同参数在记录保留期内返回已有资源，参数变化则报冲突。跨 MCP 连接仍可用应用资源 ID 操作同一服务的资源；启用鉴权时仍须通过 Token 校验。网络断开不自动结束后台进程或终端，服务重启不会恢复它们。

### 普通命令和后台进程

`process_start` 示例参数：

```json
{
  "request_id": "debug-build-001",
  "command": "go",
  "args": ["test", "./..."],
  "dir": "/客户机工程",
  "env": {"CGO_ENABLED": "0"},
  "wait_ms": 10000
}
```

返回资源 ID 和状态。`wait_ms` 最多 10000 毫秒，超出等待窗口继续通过 ID 读取。普通命令默认 5 分钟超时；`background: true` 且 `timeout_ms: 0` 可运行无期限后台服务。只有显式调用 shell 才解释管道和重定向，例如 Unix 的 `sh -c` 或 Windows 的 `powershell.exe -NoProfile -Command`。

`process_read` 参数为 `id`、`stream`（`stdout` 或 `stderr`）、`cursor`（初始为 0）和可选 `limit`。下一次用返回的 `next_cursor` 继续。`start_cursor`、`end_cursor`、`truncated` 明确说明有界缓冲是否淘汰旧内容；`data_base64` 保存原始字节，`text` 是 UTF-8 视图，`valid_utf8` 指示是否合法。Windows 非 UTF-8 程序输出应从 Base64 还原并按程序实际编码解码。

### 交互终端

```json
{"request_id":"debug-terminal-001","columns":120,"rows":30}
```

调用 `terminal_open` 后，后续 `terminal_write` 使用 `id` 和 Base64 字段 `data_base64`。例如 Unix `pwd` 加回车为 `cHdkDQ==`；Ctrl+C 为 `Aw==`。`terminal_read` 使用 `id`、`cursor` 和可选 `limit`；终端输出为合并流，保留 ANSI/VT 控制序列，没有独立 stderr。通过同一 ID 多次输入可保留目录、环境变量及前台应用状态。

`terminal_resize` 接受 `id`、`columns` 和 `rows`。Ctrl+C 输入由前台程序自行响应，强制回收使用 `terminal_close`。Windows 使用 ConPTY，Linux/macOS 使用 PTY。终端默认无调用活动 30 分钟回收，持续输出不会延长这一时间；网络短暂断开后可凭原 ID 继续。

### 一个完整调试流程

1. 在 Agent 机器上用辅助命令上传构建产物；Windows 使用客户机本地路径，例如 `C:\调试\app.exe`。
2. Unix 原生二进制先调用 `process_start`，参数 `command: "chmod"`、`args: ["u+x", "/客户机目录/app"]`，等待成功退出。脚本也可直接由 `sh`、`python` 等解释器运行。
3. 调用 `process_start` 运行客户机程序，或 `terminal_open` 创建终端后发送命令；读取日志并根据输出继续输入或调整程序。
4. 用 `process_status` 确认退出结果，或显式停止后台进程/关闭终端。
5. 用辅助命令下载日志、转储或构建产物，核对最终 JSON 的大小与 SHA-256。

## 可调整的默认限制

运行 `remote-mcp --help` 查看所有参数。大小单位均为字节，时间使用 Go 时间格式（如 `30s`、`5m`）。

| 参数 | 默认值 |
| --- | --- |
| `--listen` | `0.0.0.0:8080` |
| `--max-body-bytes` | 1048576（1 MiB） |
| `--max-file-bytes` | 4294967296（4 GiB） |
| `--chunk-bytes` | 262144（256 KiB） |
| `--max-transfers` / `--transfer-idle` | 8 / 10m |
| `--transfer-retention` / `--max-transfer-records` | 10m / 1024 |
| `--command-timeout` / `--process-retention` | 5m / 10m |
| `--max-processes` / `--max-terminals` | 16 / 8 |
| `--terminal-idle` / `--sweep-interval` | 30m / 30s |
| `--output-bytes` / `--read-bytes` | 4194304 / 65536 |

增大分块后，请同步提高请求体上限，至少容纳 Base64 膨胀和 4 KiB 元数据。并发资源达到上限时返回错误，不无限增长。结束资源及去重记录会过期清理；运行中资源的创建记录仍受保护。

## 验证与平台范围

```sh
go vet ./...
go test ./...
go test -race ./...
bash scripts/build.sh
python3 scripts/smoke.py --bin-dir dist/linux-amd64
python3 scripts/smoke.py --bin-dir dist/linux-amd64 --no-token
```

构建脚本生成 `dist/{linux,darwin,windows}-{amd64,arm64}/`，每种组合含两个程序。GitHub Actions 在 Linux、Windows、macOS 执行原生测试及竞态检查，并运行真实二进制上传、执行、下载闭环，另运行六组合构建；本地没有执行过的 CI 不能算验证通过。

Linux 还需安装提供 `/bin/ps` 的 `procps`（或发行版对应包），用于识别并回收终端额外作业组。

目标系统下限为 Windows 10 1809+/Server 2019+、macOS 13+、Linux 3.2+ 且具有 PTY。下限来自 Go 1.27 和 ConPTY 的要求，不表示每个旧系统版本均已测试。

当前开发环境为 Linux/amd64。Windows/macOS 的原生终端、文件替换及进程树清理仍须在实际系统运行测试；交叉编译只证明可构建。没有前端、公网中转、多租户隔离、专用断点调试协议或自动安装系统服务功能。

Windows CI 曾发现宿主标准输入输出重定向导致 ConPTY 会话误用宿主句柄的问题，现已修正启动参数，并增加管道及文件重定向宿主下的交互、Ctrl+C 和空闲回收回归测试；修复后的 Windows 原生结果仍需 CI 确认。

本次 Linux/amd64 验证已通过：竞态测试、静态检查、真实二进制上传/执行/下载闭环，以及 257 MiB 实际 MCP 双向传输。该大文件测试约 29.74 秒，采样峰值堆增量约 9.46 MiB，两端 SHA-256 一致；采样堆增量不等同于操作系统 RSS。

## TCP 端口转发

Agent 可通过 MCP 在运行 remote-mcp 的机器上创建独立 TCP 监听端口，访问该机器的本机服务或它能连接的内网服务。例如目标仅监听 `127.0.0.1:3000` 时，调用 `port_forward_create`：

```json
{"request_id":"preview-3000","target_host":"127.0.0.1","target_port":3000}
```

也可指定 `listen_host`（明确的 IPv4/IPv6 地址）和 `listen_port`。省略 IP 沿用 `--listen` 的 IP；省略端口或指定 `0` 自动分配。目标可使用 IP 或主机名，不含协议、路径和端口，例如 `192.168.1.20` 或 `dev.internal`。主机名在每次接入时解析。

创建返回 `id`、`listen_address`、`target_address`、`state` 和连接计数。创建成功只代表监听成功。若返回 `0.0.0.0:45678` 或 `[::]:45678`，Agent 应使用运行 remote-mcp 的机器上实际可达的 IP 和该端口访问；需能连接新增端口。IPv4 与 IPv6 按指定地址分别监听，不承诺双栈。

| 工具 | 参数与用途 |
| --- | --- |
| `port_forward_create` | 上述创建参数；相同 `request_id` 和相同参数返回同一规则，不同参数返回 `conflict` |
| `port_forward_list` | `{}`，返回 `forwards` 数组，包含活跃及保留期内终态规则 |
| `port_forward_status` | `{"id":"返回的资源 ID"}`，查询状态和连接计数 |
| `port_forward_stop` | `{"id":"返回的资源 ID"}`，关闭监听及已有连接；保留期内重复停止安全 |

状态为 `running`、`stopping`、`stopped` 或 `failed`；`active_connections` 包含拨号中的连接，`total_connections` 包含超限拒绝的接入，`failed_connections` 记录失败连接，`last_error_code` 保留最近错误。目标不可达时记录 `target_connect_failed`，仅关闭本次连接，目标恢复后规则可继续使用；连接超限记录 `connection_limit`。工具失败通过 MCP `isError` 和带 `code/message` 的错误表达。

MCP 会话断开不会停止转发；Agent 用完应调用停止工具。服务正常退出关闭全部转发，重启不恢复。已停止规则的创建重试仍返回终态；要重新建立请使用新的 `request_id`。终态到期或记录容量不足被清理后，旧 ID 查询返回 `not_found`。

管理工具沿用 MCP 鉴权，**转发端口本身不使用 MCP Token 或 MCP 入口 TLS**，目标程序负责认证与 TLS。TCP 字节原样传输，支持半关闭；HTTP Host、重定向和证书名称不会被改写。此功能不提供 UDP、反向隧道或防火墙自动配置。

| 配置参数 | 默认值 |
| --- | --- |
| `--max-forwards` | 16 条活跃规则 |
| `--max-forward-connections` | 全局 256 个连接（含拨号中） |
| `--forward-dial-timeout` | `10s` |
| `--forward-retention` | 终态保留 `10m` |
| `--max-forward-records` | 1024 条记录 |
| `--forward-sweep-interval` | `30s` |

以上限额和时间必须为正值。连接无应用层空闲超时，超限连接立即关闭，不排队；记录满时优先清理最旧终态，活跃规则的幂等记录不会被驱逐。

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
