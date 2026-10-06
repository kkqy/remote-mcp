# 研究： 环境、进程与网络巡检

- 问题： 依据现有代码与规范，确定环境运行时、系统进程树、监听端口及 PID、DNS/TCP/TLS/HTTP 有界探测的最小跨平台工具契约。
- 范围： internal；同时检查本机已下载依赖和 Go 标准库源码，不联网、不新增依赖。
- 日期： 2026-10-06

## 研究结果

### 已找到的文件与规范

| 文件 | 用途 |
| --- | --- |
| `AGENTS.md` | 项目 Trellis 入口；会话中的补充要求指定中文会话、文档、注释 |
| `.trellis/workflow.md` | 任务规划、子代理执行、复审和提交流程 |
| `.trellis/spec/backend/index.md` | 后端规范入口；运行字符串英文、用户 UTF-8 数据保留 |
| `.trellis/spec/backend/directory-structure.md` | 管理器模块边界和统一构造/注册/关闭接口 |
| `.trellis/spec/backend/mcp-contracts.md` | MCP 工具、请求上下文、错误和平台验收契约 |
| `.trellis/spec/backend/error-handling.md` | 稳定错误码、具体英文原因、原始输入和凭据不回显 |
| `.trellis/spec/backend/logging-guidelines.md` | 精确内置工具域、错误白名单、内容和 Token 过滤 |
| `.trellis/spec/backend/database-guidelines.md` | 资源计数、生命周期、请求取消和关闭边界 |
| `.trellis/spec/backend/quality-guidelines.md` | 测试、race、六组构建和原生验证分别记账 |
| `.trellis/spec/guides/cross-layer-thinking-guide.md` | 新工具结果、注册、日志消费者同步 |
| `.trellis/spec/guides/code-reuse-thinking-guide.md` | 搜索和复用已有实现；避免复制错误解码 |
| `go.mod:3` | Go 1.25；MCP SDK v1.8.0、x/sys v0.41.0、purego v0.9.1 |
| `internal/server/server.go:42` | 管理器创建、注册、失败回滚和应用关闭 |
| `internal/server/tool_logging.go:27` | 精确工具域及安全业务错误解析 |
| `internal/config/config.go:18` | 配置组合、默认值及校验 |
| `internal/execution/tools.go:8` | typed MCP 注册、请求 context 传递 |
| `internal/execution/types.go:104` | 执行模块稳定小写错误码与 JSON 错误类型 |
| `internal/execution/platform_unix.go:219` | 已有 `/bin/ps` 调用；仅进程树回收使用 |
| `internal/execution/platform_windows.go:37` | 现有 x/sys Windows 原生调用和句柄关闭模式 |
| `internal/gui/tools.go:10` | 显式 structuredContent 与 IsError 包装 |
| `internal/transfer/tools.go:9` | 失败仍保留 typed 结构化结果的注册模式 |
| `internal/gui/wayland_linux.go:61` | LookPath 和 dependency_missing 的既有能力探测方式 |
| `internal/gui/platform_darwin.go:65` | purego 动态加载库、依赖缺失和 API 不支持分类 |
| `internal/server/runtime_language_test.go:16` | 生产字符串及 jsonschema 标签的英文检查 |
| `scripts/build.sh:5` | Linux/macOS/Windows × amd64/arm64，两个程序均 CGO_ENABLED=0 |

### 推荐模块与注册方式

新增独立 `internal/inspection/` 管理器，避免把系统快照混入仅登记本服务子进程的 execution.Manager。遵循 `DefaultConfig/New/Register/Close`，共享部分放 `types.go/manager.go/tools.go/network.go`，平台差异放 `platform_linux.go/platform_windows.go/platform_darwin.go`。主代理统一修改 `config`、`server`、日志白名单和 README；巡检实施代理只拥有新模块，避免与日志/文件代理争用文件。

工具命名按现有 `<对象>_<操作>` 风格建议：

| 名称 | 输入建议 | 输出关键字段 |
| --- | --- | --- |
| `environment_inspect` | 可选 `runtimes` 固定枚举子集、`timeout_ms` | `os/arch/kernel/hostname/runtimes/capabilities` |
| `process_inspect` | 可选 `root_pid`、`limit`、`timeout_ms` | `processes:[{pid,ppid,name}]`、`root_pid`、`truncated`、`partial`、`warnings` |
| `network_listeners` | 可选 `pid/port` 过滤、`limit/timeout_ms` | `listeners:[{protocol,address,port,pids,pid_visibility}]`、`truncated/partial/warnings` |
| `network_probe` | `target`、`mode`（dns/tcp/tls/http）、`timeout_ms` | `ok/code/message/failed_stage/stages`，各阶段状态、耗时及已获得的元数据 |

主会话已确定上述四个工具名；不可沿用 `process_status` 表示全系统巡检，因为它已有明确资源 ID 契约。进程树采用扁平 PID/PPID 列表即可重建，不需要新增不可控递归嵌套输出。`root_pid` 为 0/缺省代表当前可见系统快照，指定根时只返回其自身和后代。父节点可能已退出或不可见，明确是瞬时快照，不伪造父节点。

新模块错误采用稳定小写码：`invalid_argument/closed/resource_limit/dependency_missing/permission_denied/unsupported/io_error/timeout/cancelled`；网络阶段增加 `dns_failed/tcp_failed/tls_failed/http_failed`。阶段细分类可用 `reason` 固定枚举，如 `not_found/refused/unreachable/certificate_untrusted/certificate_name_mismatch/protocol_error`，不要以自由系统 err.Error() 作为普通日志原因。

网络失败必须保留已成功阶段；若直接 `(out, error)` 返回给 SDK，错误路径可能丢掉 typed 输出。推荐复用 transfer 的显式 `CallToolResult.IsError` + typed 结果模式，失败响应仍含完整 `ProbeResult` 的有限字段。同步日志域对 inspection 的安全 `ok/code/message` 投影，不打印 stages、地址、证书、URL 或外部命令输出。GUI 现有 wrapper 在失败时只输出 Error，不适合直接复制为保留网络阶段的实现。

### 有界规则建议

以下数值是建议值，须在设计中固定：单次默认 2 秒、最大 10 秒；环境默认总预算可取 5 秒；最多 4 个并发巡检，达到上限立即 `resource_limit`，关闭管理器取消所有在途探测。快照默认返回 256 项、最大 4096 项；内部扫描设置独立总条目/文件描述符上限，例如 16384 个 PID、65536 个 FD；单次结构化输出最大 1 MiB；单个名字/版本串最大 1024 字节，外部命令累计捕获最大 1 MiB。输出达到上限时置 `truncated=true`，扫描未完整时置 `partial=true`，不要只截断 JSON 后交给 SDK。

每个循环检查 ctx，并受同一个总 deadline 限制；根过滤、FD 扫描、解析和序列化同样属于预算。不能为了固定行数先无界整机读取后再切片。`ReadDir(-1)`、`CombinedOutput`、`Output` 和无上限 Scanner 均需谨慎。默认 Scanner 上限会造成超过 64 KiB 行的隐式解析失败；明确设置最大行长并处理 Err。

已有 `execution.processTable` 有 2 秒限制，但使用无界 `.Output()` 且 ctx 来自 Background（`platform_unix.go:219`），不能直接作为新巡检的有界实现；需要单独有界命令执行器，或经协调后抽取公共实现。`exec.Cmd.WaitDelay` 可限制取消后及子进程遗留 I/O 管道的等待；Go 本机源码 `os/exec/exec.go:289` 说明了这一点。固定命令一般不产生后代，但测试仍应覆盖脚本替身遗留管道，避免声称 CommandContext 自身保证所有后代和管道结束。

### 环境与运行时

基础 `os/arch` 来自 `runtime.GOOS/GOARCH`， hostname 用 `os.Hostname`。Linux 内核可用 x/sys unix.Uname，发行版可有界解析 `/etc/os-release`；Windows 用已有 x/sys `RtlGetVersion`；macOS 用 x/sys Sysctl 查询 `kern.osrelease/kern.osversion`。其中 `/etc/os-release` 可缺失或字段异常，不能使基本系统信息整体失败。

常用运行时用固定白名单 `go/node/python/java/dotnet` 即可覆盖 MVP；可后续扩展 ruby/php/rustc。固定映射到 `go version`、`node --version`、`python3 --version`（找不到时试 python）、`java -version`、`dotnet --version`。缺省检查这 5 类，入参只允许选择子集，不允许远端构造任意可执行文件或参数。`java -version` 通常走 stderr，因此需捕获有限 stdout/stderr。Windows Python launcher 的 `py -3 --version` 可以作为额外固定 fallback，但不可因为 launcher 存在便宣称 python3 可执行文件存在。

对每个 runtime 独立返回 `available/path/version/code/message`，PATH 找不到是 `dependency_missing`（例如 `Node.js executable was not found in PATH`）；找到但不可执行是 `permission_denied`；版本查询超时是 `timeout`；非零退出是受控 `io_error` 和具体固定原因。个别 runtime 失败不让整个环境工具丢掉系统信息。PATH 和版本是 Agent 需要的数据，只进入响应，不进普通日志；不返回环境变量全集。命令按服务当前用户及环境执行，不能宣称已探测其他用户交互 shell 的 PATH。

捕获外部版本输出也必须限字节和时间；固定 whitelist 不是有界保证，因为 PATH 中程序可为脚本或异常程序。应显式标注版本截断，用户/系统实际中文名称不翻译；生产定义的原因和能力说明均英文。

### 平台能力矩阵

| 能力 | Linux | Windows | macOS |
| --- | --- | --- | --- |
| 系统/架构 | Go + unix.Uname；发行版文件可选 | Go + RtlGetVersion | Go + unix.Sysctl |
| 运行时 | PATH 固定命令有界探测 | 同左；.exe 自动 LookPath | 同左 |
| 进程 PID/PPID/name | `/proc/<pid>/stat` 或 status/comm 有界读取 | Toolhelp32 进程快照 | `/bin/ps -axo pid=,ppid=,comm=` 有界执行，或有界 sysctl 封装 |
| TCP LISTEN 地址/端口 | `/proc/net/tcp`、`tcp6`，state 0A | IP Helper GetExtendedTcpTable，IPv4/IPv6 owner PID LISTENER 表 | 固定 lsof 参数输出机器字段 |
| 监听 PID | socket inode → 可见 `/proc/<pid>/fd` symlink，可多 PID | owner PID 行，自带关联 | lsof PID 字段，可见范围尽力而为 |
| DNS/TCP/TLS/HTTP | Go 标准库、无 CGO | Go 标准库、无 CGO | Go 标准库、无 CGO |

P0 将端口巡检明确收敛为 TCP LISTEN（IPv4/IPv6），满足调试服务端口定位；UDP 是 bound endpoint，没有 LISTEN 状态，若纳入则必须分别命名 `bound`，不能把 UDP 伪装 TCP listener。范围选择须在 PRD/设计中明示。

Linux 细节：

- `/proc` 反映服务可见 PID/network namespace，不能称作物理宿主机全系统。监听表与 FD 映射必须使用兼容 namespace 范围。
- 进程 stat 的 comm 可含空格/括号，不能直接 strings.Fields 后固定列取 PPID；定位 comm 的括号边界后解析。退出竞争造成 ENOENT 正常跳过，权限拒绝单独记录有限聚合 warning。读取的 name 不是完整 argv；不顺带返回 cmdline 或 environ。
- TCP 地址字段需正确处理 IPv4 和 IPv6 的字节序；在 Linux amd64/arm64 上仍需要 fixture 回归。解析时过滤状态 0A，列号和十六进制必须严格验证。
- inode 的持有者可能多个 PID，使用去重有界数组。PID 不可见或没能完成 FD 扫描时返回空 `pids` + `pid_visibility:"unknown"/"partial"`，不能写 PID 0 伪造关联。
- hidepid/权限不足时部分可用数据保留，`partial` 和具体英文原因如 `Some process file descriptors are not readable; listener PID ownership is incomplete`。没有 `/proc` 时明确 `dependency_missing` 或 `unsupported`，不是成功空列表。

Windows 细节：

- 已下载 `x/sys/windows` 提供 `CreateToolhelp32Snapshot/Process32First/Process32Next`（`syscall_windows.go:327`），`ProcessEntry32` 包含 `ProcessID/ParentProcessID/ExeFile`（`types_windows.go:974`）；初始化 Size 并在所有路径关闭快照 handle，UTF-16 名字转 UTF-8。
- 在本机 x/sys v0.41.0 搜索未找到 GetExtendedTcpTable 封装，需要用 `windows.NewLazySystemDLL("iphlpapi.dll").NewProc(...)` 调用。查找失败为 `unsupported/dependency_missing`，不让服务启动失败。
- 常见两次调用模式先取得所需 buffer，再分配；必须先校验 buffer 字节上限，最多固定次数处理 ERROR_INSUFFICIENT_BUFFER 重试，验证条目数与 buffer 长度，不可无界重试。IPv4/IPv6 ROW 结构、port 网络字节序、IPv6 scope_id 分别处理，禁止随意假定两种结构相同。
- API 返回 Windows 状态码应使用返回值分类，不依赖 LazyProc.Call 的 last-error 判断成功。ERROR_ACCESS_DENIED 归为 permission_denied。原生 API 通常不支持 ctx 中途取消，不可每次 goroutine 竞速返回后无限累积未结束调用；并发名额由真实调用生命周期持有。
- 以上 API 构造建议源于本地已锁依赖和 Windows 既有调用模式，IP Helper ROW 的精确定义仍须实施时核对 SDK/官方契约并在原生环境验收；当前没有检验新的 Windows 实现。

macOS 细节：

- 优先固定 `/bin/ps -axo pid=,ppid=,comm=`，只取 PID/PPID/可执行名，固定 LC_ALL=C；前两个整数后保留剩余名字，避免空格名称截断。也可以 x/sys `SysctlKinfoProcSlice("kern.proc.all")` 取 `Proc.P_pid/Eproc.Ppid/Proc.P_comm`，本地 x/sys `syscall_darwin.go:519` 已提供；但是 helper 先按系统结果整体分配，且 kern.proc.all 为内核瞬时快照，不能直接宣称内存有严格上界。若用此方案需写有上限原生 sysctl 读取封装，或明确该边界。
- 监听 PID 最小依赖方案使用 `/usr/sbin/lsof`，可固定 `-nP -a -iTCP -sTCP:LISTEN -F0pcn` 输出机器字段。无 DNS/服务名转换，NUL 字段和进程/文件组解析，测试多 FD 同一 listener、IPv6 和名字含空格。依赖缺失明确 `dependency_missing`，权限拒绝 `permission_denied`。标准文本 netstat 无法可靠给 PID，不应降级为伪完整数据。
- lsof 的进程可见性受当前权限限制；没有警告不代表所有用户 PID 均可见。返回 `pid_visibility:"best_effort"` 或能力说明，不能把未返回端口宣称不存在。退出码 1 有时表示没有匹配；只有确认输出/受控 stderr 没有权限或解析问题，才能当空结果；不能将所有非零退出视为无端口。
- 不建议 P0 直接扩展 purego 绑定 libproc 及大量 socket ABI：已有 GUI native binding 可参照，但容易引入额外结构维护和原生验收负担。lsof 明确依赖比笼统 unsupported 更符合真实可用能力。

### 网络探测的阶段契约

建议一个工具按 `mode` 指定最远阶段。`dns` 输入域名，`tcp/tls` 输入 host:port，`http` 输入 http(s) URL。拒绝 URL userinfo、fragment、非 http(s) scheme、空 host、非法端口；URL/path 长度有界。不要把 query、host 或 URL 拼进普通日志英文 message。仅探测单个目标，不接受无界目标列表。

`stages` 固定 DNS→TCP→TLS→HTTP，每项 `stage/state/elapsed_ms/code/message`，state 使用 `ok/failed/skipped`。未要求阶段和先前失败后的阶段为 skipped，失败阶段不丢已获元数据。

1. DNS：`net.Resolver.LookupIPAddr(ctx, host)`；IP literal 使用 skipped + 有限目标地址。最多使用有限个解析 IP（建议 16），截断明示；DNS 失败保留 dns_failed，细分无记录、超时和取消。无 CGO 不等于所有平台 DNS 配置行为完全相同；不要宣称解析了每个 NSS 插件。
2. TCP：对已解析 IP 调用 `Dialer.DialContext`，避免二次 DNS 探测不一致；最多固定 IP 次数、共享总预算。返回本地/远端 endpoint。多地址时某一 IP 失败而另一个成功应保留有限尝试摘要，整体 tcp 成功。不能让每个 IP 独占完整 timeout 导致线性超时放大。
3. TLS：同一连接 `tls.Client` + `HandshakeContext`，ServerName 用原目标 host，默认系统 CA 验证，最低 TLS1.2。不在 P0 开放跳过证书验证；自签名失败应报告 tls_failed 和 certificate_untrusted。返回协商版本、cipher suite 和有限证书摘要即可，不返回完整证书链。证书未知根、名称不匹配、过期、TLS 协议错误分为受控原因。
4. HTTP：在同一 TCP/TLS 连接上执行一次固定 GET（或 HEAD；需设计固定默认），通过受限 http.Transport 实现 header 大小与协议解析限制，默认不跟随重定向，Proxy=nil，不读 body。HTTP 4xx/5xx 表示已收到有效 HTTP 响应，http 阶段可为 ok 并给 status_code；是否业务健康由调用者判断。302 同样返回原状态，不扩展为新的隐式目标。不给任意 request body 或 Authorization/header 自定义；不要输出所有响应 header，避免 Set-Cookie 内容进入结果。

必须共享一个 overall deadline，TCP conn 加 deadline，TLS/HTTP 同受 ctx；HTTP 的读取、header 解析、body close 均需有界。不要手写无限制 ReadResponse 解析器；现有标准库 Transport 有 MaxResponseHeaderBytes（Go 本机 `net/http/transport.go:275`）。每次连接和 transport 在结束时关闭，不保留跨调用资源或 cookie jar。

DNS/TCP/TLS/HTTP 失败是被测目标结果，可将 `ok=false` 与 IsError=true 一起返回，message 例如 `DNS name resolution failed: no address was found`、`TCP connection was refused`、`TLS certificate is not trusted`、`HTTP response headers exceeded the configured limit`。取消为 `cancelled`；总预算到期为 `timeout`，同时保留 `failed_stage`。不得原样转发 net.OpError/url.Error，里面常带完整目标 URL。

### 最低验收案例

| 范围 | 必须验证的真实断言 |
| --- | --- |
| 环境 | 当前平台 os/arch 正确；PATH 中找到固定运行时；缺失、不可执行、非零退出、超时、超量输出分别有稳定码；java stderr 版本可读取；中文名字保留 |
| 进程 | 测试启动父/子进程，快照包含真实 PID/PPID；root_pid 筛选；中文/空格/括号名字 fixture；退出竞争；达到条目上限置 truncated；扫描取消后退出 |
| TCP listener | 本机起 IPv4/IPv6 TCP listener，发现实际端口及当前 PID；多个持有者去重 fixture；权限不足仍保留地址、明示 PID 不完整；空表与依赖缺失区分 |
| Linux parser | stat comm 特殊字符、TCP/IPv6 字节序、畸形行、同 inode 多 PID、hidepid/ENOENT、FD 扫描上限；fixture 不代替真实 /proc 回归 |
| Windows API | 两类 owner PID ROW 长度/字节序 fixture、所需 buffer 超限、不足缓冲重试上限、AccessDenied、snapshot handle 关闭；原生程序监听 PID 验收单列 |
| macOS commands | ps/lsof 固定参数及 NUL 字段解析、命令缺失/权限/空结果、stdout 截断、遗留 pipe 取消；原生当前 PID listener 验收单列 |
| 网络成功 | 本机 DNS fixture 或可注入 resolver + TCP + 测试 CA TLS + HTTP 200/404/503；每个最远阶段返回正确 skipped/ok |
| 网络失败 | 可控无 DNS 记录、拒绝连接、总 deadline、TLS 未知 CA/主机名/过期/协议错误、HTTP 超大/慢 header；failed_stage 与前序成功结果保留 |
| 隐式行为 | 302 不请求第二个目标；设置 HTTP_PROXY 不走代理；不读大/无限 body；不重复 DNS 到不同 IP；同一连接完成 TCP/TLS/HTTP |
| 取消/关闭 | 已取消 ctx 不发请求；请求中途取消关闭 conn；管理器关闭终止在途；并发超限立即稳定失败；Windows 不可取消原生调用仍持有真实占用直到结束 |
| MCP/日志 | 独立 JSON-RPC 发现新 schema 并调用；失败 structuredContent 含稳定码与阶段；普通日志有安全原因且无 URL、Token、PATH、命令输出；成功无错误字段 |
| 综合 | gofmt、go vet、go test、race（可运行平台）、六组无 CGO 构建；Linux 原生结果与 Windows/macOS 未验证明确分开 |

## 一手资料

未联网；以下均为本机已下载的一手源码/文档，不代表查证最新版本：

- Go 1.25 项目标准库：`/usr/lib/go/src/net/lookup.go:220`（LookupIPAddr）、`net/dial.go:529`（DialContext）、`crypto/tls/conn.go:1512`（HandshakeContext）、`crypto/tls/common.go:694`（默认宿主 CA）、`net/http/transport.go:275`（响应 header 上限）、`os/exec/exec.go:289`（WaitDelay）。实际本机 GOROOT 版本由实施检查另行记账，项目最低版本由 go.mod 确定。
- x/sys v0.41.0：`/home/user/go/pkg/mod/golang.org/x/sys@v0.41.0/windows/syscall_windows.go:327`（Toolhelp）、`windows/types_windows.go:974`（ProcessEntry32）、`windows/syscall_windows.go:1602`（RtlGetVersion）。
- x/sys v0.41.0：`/home/user/go/pkg/mod/golang.org/x/sys@v0.41.0/unix/syscall_darwin.go:519`（SysctlKinfoProcSlice）、`unix/ztypes_darwin_arm64.go:814`（KinfoProc）；amd64 同类结构也存在。

## 限制与未发现项

- 研究开始时 PRD 为占位稿；主会话随后确定上述四个工具名并完成规划激活。本文件的数字限额及 HTTP GET/HEAD 仍为研究建议，最终以任务 design.md 为准。
- 仓库未发现通用巡检管理器、gopsutil、系统 listener PID 查询实现；不能把 execution 登记资源当作系统进程树。
- x/sys 当前未直接封装 IP Helper owner-PID TCP 表；本次没有核对外部 SDK 精确 ROW 定义，实施时需要补上核对和 fixture，不能凭建议直接写 unsafe 结构。
- macOS lsof/ps 命令及 Windows 原生 API 尚未运行验证；没有进行任何产品代码修改或构建，也不能把上述矩阵写成已经支持的最终事实。
- 纯软件有界取消无法保证平台内核 API 在所有异常环境中及时返回；不要用无限 goroutine 包装模拟保证。Go DNS 解析同样需记录平台行为，以实际并发和 deadline 约束为准。
- 快照受当前账号和 namespace 可见性影响。缺少结果不等于系统不存在该进程/端口；partial、warnings 和 visibility 必须保留。
- 稳定错误安全消息须同步 server/tool_logging.go 精确白名单；新增任意动态 message 字段却只扩充内容敏感键列表并不能保证日志安全。
- 六组无 CGO 构建是要求，原生 Windows/macOS 验收应保留未验证条目；不得据此关闭仍有原生验收缺口的 gui-control-screenshot 任务。
