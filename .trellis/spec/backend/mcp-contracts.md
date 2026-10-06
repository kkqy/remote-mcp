# MCP 与运行资源契约

## 1. 适用范围

涉及 HTTP 接入、工具签名、文件辅助命令、输出游标或资源生命周期的修改必须核对本契约。实现来源为 `internal/server/server.go`、`internal/transfer/{types,tools,manager}.go` 和 `internal/execution/{types,tools,manager}.go`。

## 2. 签名与入口

- MCP 入口为 `/mcp`，当前协议协商基线为 `2025-11-25`。
- 管理器接口：`New(Config) (*Manager, error)`、`Register(*mcp.Server)`、`Close() error`。
- 文件工具：`file_stat`、`upload_create/write/finish/cancel`、`download_open/read/close`。
- 进程工具：`process_start/read/status/stop`。
- 终端工具：`terminal_open/write/read/resize/status/close`。
- 远程调试 P0 工具：`environment_inspect/process_inspect/network_listeners/network_probe`、`file_list/file_read/file_search/file_patch`、`log_open/log_read/log_close`；执行 read 新增 `wait_ms`，详细签名、预算和代际见 [远程调试 P0 契约](remote-debug.md)。
- GUI 工具：`gui_status/open/close/screenshot/mouse/key/text`；图片、坐标、授权和剪贴板规则见 [GUI 契约](gui.md)。
- 本地辅助命令入口：`remote-mcp-transfer upload|download`，CLI 完整参数以程序 `--help` 和 README 为准。
- Windows 执行适配入口：`(*windowsProcess).spawn(*exec.Cmd, []windows.Handle) error`；终端通过扩展属性指定 HPCON，普通进程通过句柄列表重定向标准输入输出。

## 3. 数据与环境契约

- Token 可选：未指定 `--token-file` 且 `REMOTE_MCP_TOKEN` 未设置或为空时启用匿名接入。非空 Token 必须为不含空白的可打印 ASCII，最多 8 KiB。
- `--token-file` 显式文件优先；文件为空、不可读、内容非法或 Unix 权限向组/其他账号开放时必须报错，不能退回匿名接入或环境变量。
- 服务配置非空 Token 时，每个请求校验 `Authorization: Bearer <Token>`；匿名模式不要求该请求头。两种模式均保留 Origin 校验、TLS 和资源限额。
- 辅助命令复用相同凭据加载规则；无 Token 时不发送 Authorization，有 Token 时发送 Bearer 凭据。
- 上传创建参数为 `request_id/path/size/sha256/overwrite`；分块为 `id/offset/data`，其中 data 为 Base64，offset 和 size 为原始字节数。
- 下载读取参数为 `id/offset/length`，结果复用 `transfer.Result`；调用者验证哈希、长度和状态后才宣称成功。
- 普通命令参数为 `request_id/command/args/dir/env/background/timeout_ms/wait_ms`；shell 解释只在显式执行 shell 时发生。
- 终端输入为 `id/data_base64`，尺寸为 `id/columns/rows`。Ctrl+C 的原始字节 0x03 编码为 `Aw==`，程序可以自行处理或忽略该输入。
- `Manager.Write(context.Context, WriteInput)` 的完成契约：底层 Write 返回后先释放该终端的 inputBusy，再向调用方发布成功或 I/O 错误结果；顺序调用不能因上一调用的延迟清理得到 busy。等待取消或超时时，底层可能仍在写入，必须保留占用直到真正结束，不能在请求返回时提前释放。
- 输出包含 `start_cursor/next_cursor/end_cursor/truncated/data_base64/text/valid_utf8`。原始字节是权威数据，文本视图不能代替二进制还原。
- 普通进程分别读取 stdout、stderr；终端只提供合并 terminal 流，保留控制序列。
- Windows ConPTY 子进程必须显式设置 `STARTF_USESTDHANDLES`，并将 StdInput/StdOutput/StdErr 保持为 NULL（Go 零值）。仅传 `bInheritHandles=false` 不能阻止父进程重定向的标准句柄被复制；普通进程分支仍使用各自的真实管道句柄。参见 [微软维护者说明](https://github.com/microsoft/terminal/discussions/15814)。
- Windows 进程始终先 `CREATE_SUSPENDED` 创建，成功绑定 Job Object 后再 `ResumeThread`；不能为修复终端而取消进程树回收约束。
- 启动输出采用 OpenCode 1.x 配置：顶层 `$schema` 为 `https://opencode.ai/config.json`，`mcp.remote-mcp` 下固定 `type: "remote"`、`enabled: true`、`oauth: false`，并包含实际监听器生成的 `url`。不输出旧的 `mcpServers` 或 `type: "http"`，也不生成本地服务的 command。
- 配置非空 Token 时才包含 `headers.Authorization`；匿名模式完全省略 `headers`。每个 IP 独立完整 JSON，可选一份合并入 OpenCode 配置，其他客户端需转换格式；不把客户端配置格式与 MCP HTTP 传输协议混为一谈。
- 服务启动 JSON、普通日志及传输辅助命令文本均为 UTF-8；`scripts/smoke.py` 的文件打开及 subprocess 文本读取必须显式指定 UTF-8。脚本最终成功摘要只在业务、日志检查和清理全部成功后发布。

## 4. 校验与错误矩阵

| 条件 | 预期行为 |
| --- | --- |
| 服务未配置 Token，请求不含 Authorization | 可初始化并调用工具 |
| 服务已配置 Token，请求凭据缺失或错误 | HTTP 401，不执行工具 |
| 显式 Token 文件为空、不可读或非法 | 配置失败，不切换匿名模式 |
| 环境变量 Token 非空但格式非法 | 配置失败 |
| 不允许的 Origin | HTTP 403 |
| 无效工具参数、偏移或尺寸 | 稳定业务错误，不 panic |
| 默认上传到已有文件 | 冲突，原文件保持 |
| 长度或 SHA-256 不符 | 上传失败，清理临时文件 |
| 相同 request_id、不同创建参数 | 冲突，不创建第二份资源 |
| 读取游标早于缓冲起点 | 返回截断标记及实际起点 |
| 非 UTF-8 输出 | 保留 Base64 原文，文本注明解码状态 |
| HTTP 断开 | 不自动停止应用进程或终端 |
| 监听失败 | 不输出成功连接配置 |
| 无论有无 Token | 启动 JSON 明确包含 oauth=false，避免客户端尝试 OAuth |
| Windows 终端的宿主 stdin/stdout/stderr 被重定向 | 输入输出仍经 ConPTY，shell 不因宿主输入 EOF 提前退出 |
| 上一终端写入已返回成功或 I/O 错误后再次顺序调用 | 不因上一调用遗留的占用返回 busy |
| 底层终端写入仍未完成，包括调用方等待已取消 | 新写入返回 busy，不启动重叠写入 |
| 验证脚本解码、日志校验或清理失败 | 非零退出，不提前输出 ok=true，不忽略无效编码 |

## 5. 正常、边界与失败示例

- 正常：分块上传程序，校验后通过进程工具运行，再下载产物；Unix 可通过命令工具设置执行权限。
- 正常：不设置 Token，直接启动服务与辅助命令，完成匿名 MCP 上传和下载；设置非空 Token 后使用相同凭据连接。
- 边界：空文件仍走创建、完成和空内容 SHA-256 校验；无需伪造空写块。
- 边界：CI 捕获宿主标准输出、输入为 EOF 时，Windows 终端仍可持续输入命令；真正达到空闲时限后状态原因为 idle_timeout。
- 边界：输入等待取消后，后续输入仍须等底层写入实际结束；取消不代表字节未送达，不自动重试。
- 失败：后台日志超过输出缓冲后，从旧游标读取必须看到 truncated，不能返回看似完整的日志。
- 失败：启动时有两个网卡地址但仅绑定其中一个，只能输出该地址，不能输出未监听地址或 0.0.0.0。

## 6. 必需验证

对应测试入口为 `internal/server/*_test.go`、`internal/config/*_test.go`、文件与执行模块测试以及辅助命令测试。断言要覆盖原目标未损坏、ID 没有重复创建、内存/记录有界、配置可解析、普通日志无 Token、进程树及文件句柄清理。

鉴权变更须覆盖匿名初始化及真实工具调用、匿名上传下载、带 Token 模式下缺失/错误凭据的 401、多 IP 匿名配置没有 headers、匿名客户端不发送 Authorization、显式文件错误不降级。检查空 Token 不会导致日志工具名全部被脱敏过滤。

启动格式验证须用独立 map 解析，检查 mcp/remote/enabled/oauth 的真实字段和值，以及旧字段缺失；不能只复用生产结构体导致错误 JSON 标签同时被写入和读取。同步修改实际二进制验证脚本等启动输出消费者。

Windows 终端须在宿主标准输入为 EOF、输出被重定向的独立子进程中回归，核对状态保持、调整尺寸、Ctrl+C 和 idle_timeout，并检查交互输出没有泄漏到宿主 stdout/stderr。不能只断言创建成功，不能延长等待或放宽退出原因掩盖标准句柄接错。

终端输入需要平台无关的可控写入回归：连续顺序调用、真实并发 busy、请求取消后仍持有占用、底层错误后的释放，以及真实 PTY 连续输入。Go race 检测不保证发现所有逻辑竞态；结果发布与资源释放的先后关系须单独断言。

Linux 本机运行、Windows/macOS 原生运行和六组合构建分别记账。没有对应环境时保留未验证状态，不能用模拟终端消除真实平台验收要求。

## 7. 错误方式与正确方式

错误：Python 以系统默认代码页读取 Go 的 UTF-8 用户输出，在 Windows 上解码失败；或日志检查前就打印成功。程序自身提示改为英文不意味着用户数据可以按本地代码页解码。

正确：所有 Go 文本输出边界显式使用严格 UTF-8 解码，全部验证与清理通过后才打印成功。

错误：goroutine 用 defer 释放输入占用，在 defer 执行前向结果通道发送完成通知。

正确：底层 Write 结束 → 释放输入占用 → 发布结果；请求取消只结束等待，不提前解除仍在运行的写入占用。

错误：认为 CreateProcess 的 bInheritHandles=false 足以防止 ConPTY 使用父进程的重定向标准句柄。

正确：终端设置 STARTF_USESTDHANDLES 且 Std* 为 NULL，保留 HPCON 扩展属性及 Job 绑定顺序，用宿主重定向场景做原生回归。

错误：把 `mcpServers` 和 `type: "http"` 的配置称作可直接用于 OpenCode，或把远程地址配置为 `type: "local", command: [""]`。

正确：OpenCode 1.x 使用 `mcp` 和 `type: "remote"`，本服务明确设置 `oauth: false`。协议可互通不代表客户端配置文件格式相同。

错误：空 Token 仍生成 `Authorization: Bearer `，或捕获 Token 文件错误后以匿名模式继续运行。

正确：空配置明确对应匿名模式，配置输出及客户端均省略鉴权头；显式凭据配置错误仍失败。

错误：直接把二进制 Base64 交给模型逐块复述，并用整文件 `ReadFile` 构造请求。

正确：辅助命令用有界缓冲逐块调用 MCP，模型只接收摘要与校验结果。

错误：先删除已有目标，再把临时文件移动过去；或者硬链接发布成功后忘记删除临时名称。

正确：校验完毕后执行平台支持的原子提交；默认覆盖策略在提交时仍成立，成功和失败都处理临时名称清理。

错误：用普通 stdin/stdout 管道宣称支持交互式终端。

正确：Unix 使用 PTY，Windows 使用 ConPTY；尺寸、控制字符和关闭行为按原生平台验证。
