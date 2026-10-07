# Remote MCP

[English](README.md) | **简体中文**

部署在客户机上的 Go MCP 服务。智能体通过内网或 VPN 连接客户机，传输文件、运行命令，以及操作真实交互终端。服务包含两个程序：`remote-mcp` 服务端和 `remote-mcp-transfer` 本地文件传输辅助命令。

## 下载与自动发布

可从仓库的 [GitHub 发布页](../../releases) 下载正式版本。每个压缩包只含 `remote-mcp` 服务端和 `remote-mcp-transfer` 辅助命令；Windows 文件带 `.exe` 后缀。

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

内置 `GITHUB_TOKEN` 不具备额外的工作流权限；标签目标若包含相对默认分支尚未合入的工作流修改，GitHub 可能拒绝创建正式版本。本流程不使用个人访问令牌，请先完成上述合入步骤。

普通分支推送和拉取请求继续验证，不发布版本；仅 `v*` 标签推送进入发布流程。打包脚本要求标签以 `v` 开头、后续首字符为字母或数字、其余仅含字母/数字/点/下划线/连字符，总长最多 64 字符；其他名称会明确失败。Linux、Windows 原生验证和四组合构建全部成功后，发布作业才创建草稿、上传四个包和校验文件，全部上传成功后公开为正式版本。只有该作业的 `GITHUB_TOKEN` 具有 `contents: write`，相同引用的发布串行且不取消正在执行的发布；原有二进制和 P0 验证产物保留。

已有正式版本或同标签草稿不会自动覆盖。验证阶段失败时可在 GitHub Actions 重跑；若创建草稿或上传中断，先在发布页核对本次标签与草稿附件，再手动删除**本次失败草稿**，保留标签并重跑该工作流运行。不要删除已公开版本或使用附件覆盖参数。最终公开请求若超时，结果可能已生效，必须先查看版本状态；若已公开则不重跑覆盖，后续修改使用新版本标签。

2026-10-07，[main 工作流运行](https://github.com/kkqy/remote-mcp/actions/runs/37503153099)和 [v0.1.0 标签工作流运行](https://github.com/kkqy/remote-mcp/actions/runs/37503152596)均已成功完成。[v0.1.0 正式版本](https://github.com/kkqy/remote-mcp/releases/tag/v0.1.0)已公开，四个压缩包和 `SHA256SUMS` 附件核对通过。上述 CI 与发布结果不代表 Windows/arm64、多屏/混合 DPI 或 GUI 原生缺口已验收。开发者可用 `python3 scripts/release.py package --tag v1.2.3 --dist-dir dist --output-dir .tmp/release` 本机打包，输出目录须不存在；临时文件与构建缓存均置于项目 `.tmp/`。

## 构建与启动

模块最低要求 Go 1.25，默认使用 Go 1.27 构建，无需 CGO：

```sh
go build -o bin/remote-mcp ./cmd/remote-mcp
go build -o bin/remote-mcp-transfer ./cmd/remote-mcp-transfer
```

未配置 Token 时可以直接启动。Linux：

```sh
./bin/remote-mcp
```

Windows PowerShell：

```powershell
.\remote-mcp.exe
```

Token 是可选的。未设置 `REMOTE_MCP_TOKEN` 或该变量为空、且未指定 `--token-file` 时，服务允许匿名访问：能连接服务的客户端可使用运行账号的文件和命令权限。若当前终端之前配置过 Token，可用 Bash 的 `unset REMOTE_MCP_TOKEN` 或 PowerShell 的 `$env:REMOTE_MCP_TOKEN = $null` 清除后启动。

需要鉴权时，在启动前配置非空 Token。Linux：

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

监听成功后，标准输出为每个启用网卡上的可用 IP 输出一份完整的 OpenCode 配置。同一网卡上的多个地址也分别展示，重复地址会去重并稳定排序。未配置 Token 时例如：

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

存在 VPN 地址时还会输出它的独立配置。**选择智能体可达的一份配置使用**；这些地址属于同一个服务。每段都是独立 JSON 对象，多段合在一起不是单个 JSON 文档。匿名模式完全省略 `headers`；启用鉴权后，配置会增加 `"headers": {"Authorization": "Bearer <当前 Token>"}`，其中包含真实 Token，请将其视为凭据保管。英文提示和普通日志写入标准错误，普通日志不记录 Token、环境变量值、文件内容或终端输入输出。

全接口展示会排除停用网卡、回环、未指定地址、组播和 IPv6 链路本地地址。单 IP 绑定只显示该地址；显式回环绑定说明仅限本机。网卡枚举失败时服务继续运行并提示手动配置，不输出 `0.0.0.0` 或 `::` 作为连接 URL。列出网卡地址不意味着智能体侧路由、防火墙或证书信任已就绪。

配置采用 [OpenCode 远程 MCP 格式](https://opencode.ai/docs/mcp-servers/#remote)：顶层为 `mcp`，服务类型为 `remote`，`enabled: true` 启用连接，`oauth: false` 关闭本服务不提供的 OAuth 流程。选择一段 JSON 合并到项目或用户的 `opencode.json`，已有配置只合并对应的 `mcp.remote-mcp` 条目，保留其他设置；重启 OpenCode 后使用 `opencode mcp list` 查看状态。此格式与 OpenCode 1.18.34 对应，不是 MCP 协议规定的统一配置文件；其他客户端须将相同 URL 及可选 Authorization 值转换成其配置格式。客户端须支持 Streamable HTTP 和协议版本 **2025-11-25**；启用鉴权时还需支持 Bearer Token；不提供 stdio、旧 SSE 或 OAuth 自动发现适配，也不表示已验证所有品牌智能体。

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

该选项不是免鉴权通道，也不自动配置浏览器 CORS 预检策略。防火墙应允许智能体所在内网或 VPN 访问监听端口。

## 上传和下载文件

辅助命令在智能体所在机器上运行，从本地磁盘读取真实字节，经 MCP 分块传输，文件内容不经过模型复述。

```sh
./bin/remote-mcp-transfer upload --url http://192.168.1.10:8080/mcp ./程序包.zip /客户机目录/程序包.zip
./bin/remote-mcp-transfer download --url http://192.168.1.10:8080/mcp /客户机目录/运行日志.txt ./运行日志.txt
```

默认不需要 Token，辅助命令不发送 Authorization。若服务端已启用鉴权，先设置相同的 `REMOTE_MCP_TOKEN`，或传入 `--token-file`；显式无效文件会报错。`--url` 可由 `REMOTE_MCP_URL` 提供。选项放在 `upload` 或 `download` 后、两个路径参数前。使用 `--overwrite` 才允许替换已有目标；上传和下载先写临时文件，大小和 SHA-256 校验通过后提交。失败返回非零退出码，进度写标准错误，最终 JSON 摘要写标准输出，包含 `ok`、`size`、`sha256` 或错误原因。

单文件默认 4 GiB、原始分块默认 256 KiB。目录请先用系统工具打包，压缩包按普通文件传输；服务不自动解包、不保留文件权限或原始时间戳、不支持目录同步和用户级断点续传。Unix 可执行文件上传后需用命令工具设置执行位，或用解释器运行上传的脚本。

## MCP 工具

客户端通过 `tools/list` 获得完整的类型化参数。业务失败返回 MCP `isError` 和稳定错误码；不要把协议 HTTP 200 当作工具成功。

MCP 工具失败的普通日志保留 `tool/elapsed/failed`，并输出 `error_code/error_message`，程序自有错误原因及提示使用英文；GUI 失败还可包含 `input_may_have_applied/clipboard_restore`。例如 Windows 显式请求剪贴板模式会返回 `unsupported`，并记录具体英文原因。成功调用没有错误字段。日志只接收已知内置工具域的安全业务说明；SDK 参数失败按必填缺失、类型不符等类别说明，未知工具和异常使用安全分类，不打印原始参数、内容或错误载荷。错误说明最多 1024 字节，控制字符清理，非空 Token 及可识别的输入内容脱敏；未来自定义工具需单独建立安全日志契约才能输出原文原因。


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

返回资源 ID 和状态。`wait_ms` 最多 10000 毫秒，超出等待窗口继续通过 ID 读取。普通命令默认 5 分钟超时；`background: true` 且 `timeout_ms: 0` 可运行无期限后台服务。只有显式调用命令解释器才解释管道和重定向，例如 Unix 的 `sh -c` 或 Windows 的 `powershell.exe -NoProfile -Command`。

`process_read` 参数为 `id`、`stream`（`stdout` 或 `stderr`）、`cursor`（初始为 0）和可选 `limit`。下一次用返回的 `next_cursor` 继续。`start_cursor`、`end_cursor`、`truncated` 明确说明有界缓冲是否淘汰旧内容；`data_base64` 保存原始字节，`text` 是 UTF-8 视图，`valid_utf8` 指示是否合法。Windows 非 UTF-8 程序输出应从 Base64 还原并按程序实际编码解码。

### 交互终端

```json
{"request_id":"debug-terminal-001","columns":120,"rows":30}
```

调用 `terminal_open` 后，后续 `terminal_write` 使用 `id` 和 Base64 字段 `data_base64`。例如 Unix `pwd` 加回车为 `cHdkDQ==`；Ctrl+C 为 `Aw==`。`terminal_read` 使用 `id`、`cursor` 和可选 `limit`；终端输出为合并流，保留 ANSI/VT 控制序列，没有独立标准错误。通过同一 ID 多次输入可保留目录、环境变量及前台应用状态。

`terminal_resize` 接受 `id`、`columns` 和 `rows`。Ctrl+C 输入由前台程序自行响应，强制回收使用 `terminal_close`。Windows 使用 ConPTY，Linux 使用 PTY。终端默认无调用活动 30 分钟回收，持续输出不会延长这一时间；网络短暂断开后可凭原 ID 继续。

### 一个完整调试流程

1. 在智能体机器上用辅助命令上传构建产物；Windows 使用客户机本地路径，例如 `C:\调试\app.exe`。
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

当前仅支持 Linux 和 Windows 的 amd64/arm64，macOS 支持已移除。构建脚本生成 `dist/{linux,windows}-{amd64,arm64}/`，每种组合含两个程序。GitHub Actions 在 Linux、Windows 执行原生测试及竞态检查，并运行真实二进制上传、执行、下载闭环，另运行四组合构建；CI 配置本身不能代替实际执行结果。

Linux 还需安装提供 `/bin/ps` 的 `procps`（或发行版对应包），用于识别并回收终端额外作业组。

目标系统下限为 Windows 10 1809+/Server 2019+、Linux 3.2+ 且具有 PTY。下限来自 Go 1.27 和 ConPTY 的要求，不表示每个旧系统版本均已测试。

当前开发环境为 Linux/amd64。Windows/amd64 虚拟机已通过原生包测试和真实二进制闭环，包含终端、文件替换及进程树清理；Windows/arm64 目前仅构建通过。没有前端、公网中转、多租户隔离、专用断点调试协议或自动安装系统服务功能。

历史 Windows CI 曾发现宿主标准输入输出重定向导致 ConPTY 会话误用宿主句柄的问题，已修正启动参数，并增加管道及文件重定向宿主下的交互、Ctrl+C 和空闲回收回归。对应回归已在 Windows/amd64 虚拟机实际通过；该虚拟机验收记录早于上文已成功的工作流运行，不能将此次 CI 理解为重新执行了虚拟机验收。

历史 Linux/amd64 验证已通过：竞态测试、静态检查、真实二进制上传/执行/下载闭环，以及 257 MiB 实际 MCP 双向传输。该大文件测试约 29.74 秒，采样峰值堆增量约 9.46 MiB，两端 SHA-256 一致；采样堆增量不等同于操作系统 RSS。

## TCP 端口转发

智能体可通过 MCP 在运行 remote-mcp 的机器上创建独立 TCP 监听端口，访问该机器的本机服务或它能连接的内网服务。例如目标仅监听 `127.0.0.1:3000` 时，调用 `port_forward_create`：

```json
{"request_id":"preview-3000","target_host":"127.0.0.1","target_port":3000}
```

也可指定 `listen_host`（明确的 IPv4/IPv6 地址）和 `listen_port`。省略 IP 沿用 `--listen` 的 IP；省略端口或指定 `0` 自动分配。目标可使用 IP 或主机名，不含协议、路径和端口，例如 `192.168.1.20` 或 `dev.internal`。主机名在每次接入时解析。

创建返回 `id`、`listen_address`、`target_address`、`state` 和连接计数。创建成功只代表监听成功。若返回 `0.0.0.0:45678` 或 `[::]:45678`，智能体应使用运行 remote-mcp 的机器上实际可达的 IP 和该端口访问；需能连接新增端口。IPv4 与 IPv6 按指定地址分别监听，不承诺双栈。

| 工具 | 参数与用途 |
| --- | --- |
| `port_forward_create` | 上述创建参数；相同 `request_id` 和相同参数返回同一规则，不同参数返回 `conflict` |
| `port_forward_list` | `{}`，返回 `forwards` 数组，包含活跃及保留期内终态规则 |
| `port_forward_status` | `{"id":"返回的资源 ID"}`，查询状态和连接计数 |
| `port_forward_stop` | `{"id":"返回的资源 ID"}`，关闭监听及已有连接；保留期内重复停止安全 |

状态为 `running`、`stopping`、`stopped` 或 `failed`；`active_connections` 包含拨号中的连接，`total_connections` 包含超限拒绝的接入，`failed_connections` 记录失败连接，`last_error_code` 保留最近错误。目标不可达时记录 `target_connect_failed`，仅关闭本次连接，目标恢复后规则可继续使用；连接超限记录 `connection_limit`。工具失败通过 MCP `isError` 和带 `code/message` 的错误表达。

MCP 会话断开不会停止转发；智能体用完应调用停止工具。服务正常退出关闭全部转发，重启不恢复。已停止规则的创建重试仍返回终态；要重新建立请使用新的 `request_id`。终态到期或记录容量不足被清理后，旧 ID 查询返回 `not_found`。

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

## 远程调试：巡检、等待日志与文本补丁

以下能力与已有上传、执行和终端工具共用 `/mcp`、鉴权及服务账号权限，无新增 CLI 参数。

| 工具 | 用途与主要参数 |
| --- | --- |
| `environment_inspect` | 系统、架构、内核、主机名；可选 `runtimes` 为 `go/node/python/java/dotnet` 的子集 |
| `process_inspect` | 有界 PID/PPID/name 快照；可选 `root_pid` 返回根进程及后代 |
| `network_listeners` | IPv4/IPv6 TCP LISTEN 地址、端口及可见 PID；可选 `pid/port/limit` |
| `network_probe` | `mode` 为 dns/tcp/tls/http，`target` 分别为主机名、host:port 或 HTTP(S) URL |
| `process_read`、`terminal_read` | 新增 `wait_ms`，缺省或 0 保持立即读取，最大 30000 ms |
| `log_open/read/close` | 跟踪已有普通日志文件；读取使用 `id/generation/cursor/limit/wait_ms` |
| `file_list` | `path/offset/limit` 单目录分页，返回 `next_offset` 和限制原因 |
| `file_read` | `path/start_line/line_count/max_bytes` 按行读取 UTF-8，返回全文件 `sha256` |
| `file_search` | `path/query` 区分大小写的字面子串；可配置深度、条目、字节和命中预算 |
| `file_patch` | `path/expected_sha256/edits` 校验原内容后修改已有普通 UTF-8 文件 |

巡检默认总预算 5 秒，`timeout_ms` 最多 10000；最多 4 个并发巡检。快照默认返回 256 项，单次最多 4096，内部 PID/FD 扫描也有界。返回 `partial/warnings/truncated/visibility` 表示权限、依赖、扫描上限或可见性缺口；空列表不证明整机没有对象。Linux 使用当前 `/proc` 命名空间，Windows 使用 Toolhelp 与 IP Helper；缺少依赖或权限不足提供英文代码与具体原因。运行时探测使用服务账号 PATH，不采集完整环境变量或进程命令行。

网络探测沿 DNS→TCP→TLS→HTTP 共用一个期限。失败仍保留 `structuredContent` 中的 `stages/failed_stage/code/message`；IP 字面量可跳过 DNS。TLS 使用系统 CA 校验证书，HTTP 固定一次 GET，不走代理、不跟随重定向、不读取正文；HTTP 4xx/5xx 是有效协议响应，业务健康由调用方判断。例如：

```json
{"name":"network_probe","arguments":{"mode":"http","target":"http://127.0.0.1:8080/health","timeout_ms":3000}}
```

执行读取的 `reason` 区分 `immediate/output/exit/timeout/cancelled`。原始 Base64 字节、字节游标、UTF-8 视图及截断标记继续保留；HTTP 取消只结束本次等待，不停止应用进程。先将上次 `next_cursor` 带回，再等待新输出：

```json
{"name":"process_read","arguments":{"id":"resource-id","stream":"stdout","cursor":128,"wait_ms":10000}}
```

日志资源默认最多 32 个，空闲 10 分钟回收，文件变化约每 100 ms 检查，单次最多 64 KiB。`log_open` 返回 `generation/end_cursor`：从头读取用 cursor=0，只读取以后追加内容用 end_cursor。调用 `log_read` 时同时传回上次 `generation/next_cursor`；路径替换或缩短会重置代际与偏移，返回 `rotated/truncated`。等待式结果还可用 `reason=rotated/truncated` 表示文件变化；立即读取仍是 `immediate`，变化看布尔标记。两个采样之间瞬间截断又恢复、同一文件原地重写且尺寸没有缩短，可能无法检测；它不是文件审计协议。显式 `log_close` 或服务关闭结束资源。

文件读取和修改默认最多 8 MiB，返回文本最多 64 KiB；`next_line` 是首个未完整返回的行。`truncated/reason=byte_limit` 时可在上限内提高 `max_bytes` 重读；超过 64 KiB 的长行仅提供有限 UTF-8 预览，跳过此行用 `end_line+1`，不要把重复返回的前缀盲目拼接。完整字节可使用原下载工具。搜索逐行匹配字面子串，不跨行匹配，最多 10000 条目、64 MiB 扫描、每文件 8 MiB、16 层和 1000 命中，结果也有字节预算。长行在文件扫描预算内完整匹配，返回预览可能没有展示命中位置；`issues/truncated/reason` 说明无效 UTF-8、权限失败或预算限制。目录分页采用文件系统枚举顺序，目录并发变化时 offset 会移动；不承诺稳定快照，也不跟随符号链接。

补丁行号从 1 开始，按**原文件**坐标严格递增且不重叠；`total_lines+1` 可追加。`text` 是确切替换字节，调用方自行携带 LF 或 CRLF，不自动补换行。先读取全文件哈希，再发送行补丁，例如原第二行使用 CRLF：

```json
{"name":"file_read","arguments":{"path":"/srv/app/config.txt","start_line":1,"line_count":20}}
```

```json
{"name":"file_patch","arguments":{"path":"/srv/app/config.txt","expected_sha256":"<sha256-from-file_read>","edits":[{"start_line":2,"delete_count":1,"text":"新的配置\r\n"}]}}
```

错误哈希或已观测到的外部修改返回 `conflict` 并保留现有目标。同目录临时文件校验、同步、关闭后才原子替换，失败清理临时名称；本模块补丁串行执行。请求断开或等待取消后，应重新 `file_read` 核对实际哈希，再决定下一步，不盲目重试旧哈希。最终哈希复核与发布之间仍存在外部非合作写入窗口，不提供严格文件系统 CAS。权限保持遵循平台可移植权限位，不能把它等同于 Windows 自定义 ACL 的完整复制。取消在文件系统操作之间检查，无法强制中断已阻塞的内核文件操作。

普通日志只记录工具、耗时、结果及受控英文错误码与原因，不记录文件内容、`query`、`edits`、URL、运行时版本输出或凭据。Linux 本机测试与四组无 CGO 构建分别验收，Windows/amd64 原生 PID、端口、终端和文件行为已通过虚拟机验收，Windows/arm64 仍仅构建通过；新增 P0 不消除 GUI 任务的原生验收缺口。DAP、调试会话聚合、诊断包、反向隧道和新增 GUI 留待后续。

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

现有 Linux/Windows CI 入口执行旧功能与 P0 的 Token/匿名四轮冒烟，原生构建步骤明确关闭 CGO，竞态检查保留所需配置，四组合构建继续独立执行；P0 报告保存到按运行器平台命名的产物，成功模式分别留证。历史 Linux/amd64 和 Windows/amd64 实际旧功能/P0 × Token/匿名四轮冒烟均通过，Windows 虚拟机九个包合计 111 个顶层测试通过、4 个按平台或条件跳过。这是历史虚拟机验收记录，与上文已成功的 main、v0.1.0 工作流运行分别记账。该虚拟机缺少 Python，实际验收使用下面的 Go 助手方式，未在虚拟机执行上述 Python 冒烟脚本。Windows/arm64 只有构建结果，虚拟机没有运行 Windows 竞态检测，GUI 保留独立验收边界。

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
| `gui_text` | `id/text` 与可选 `mode/paste_keys/allow_clipboard_replace`，提交 UTF-8 文本，包括中文和表情符号 |

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

脚本核对本机产物、远端文件和实际启动程序的 SHA-256。本机缓存和打包文件放项目 `.tmp/`，远端部署放 MCP 服务工作目录的 `.tmp/`；需该目录可写。用临时回环服务完成 Token/匿名两轮；仅向自己创建且确认前台和输入焦点的 Win32 窗口操作。它验证裁剪 PNG 的四色标记、真实鼠标和滚轮事件、全选清空、中文/表情符号实际进入 EDIT 控件。原生 Unicode 路线不访问剪贴板；窗口、会话、临时服务和部署目录清理后复查原 MCP 服务，再发布报告。旧功能/P0 原生入口继续默认四轮。

2026-10-07 当前构建已在 Windows/amd64 单屏 100% DPI 环境完成上述 Token/匿名原生闭环；该结果不覆盖 Windows/arm64 原生运行、多屏/混合 DPI、布局变化、撤权或实际 MCP 客户端图片展示。报告保留 `acceptance_complete: false`，构建、模拟协议及真实桌面结果分别记录。历史 Linux GUI 研究仅供追溯，不属于当前支持范围或待交付项。
