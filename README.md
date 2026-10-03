# Remote MCP

部署在客户机上的 Go MCP 服务。Agent 通过内网或 VPN 连接客户机，传输文件、运行命令，以及操作真实交互终端。服务包含两个程序：`remote-mcp` 服务端和 `remote-mcp-transfer` 本地文件传输辅助命令。

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

存在 VPN 地址时还会输出它的独立配置。**选择 Agent 可达的一份配置使用**；这些地址属于同一个服务。每段都是独立 JSON 对象，多段合在一起不是单个 JSON 文档。匿名模式完全省略 `headers`；启用鉴权后，配置会增加 `"headers": {"Authorization": "Bearer <当前 Token>"}`，其中包含真实 Token，请将其视为凭据保管。中文说明和普通日志写入 stderr，普通日志不记录 Token、环境变量值、文件内容或终端输入输出。

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
