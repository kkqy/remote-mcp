# 日志与配置输出

普通日志记录工具名、耗时、结果，以及经过筛选的失败错误码和具体原因，不记录 Token、文件内容、环境变量值或终端输入输出。SDK 内部调试日志可能包含参数，不能直接接入普通运行日志。参考 `internal/server/server.go`。

启动配置是用户明确要求的例外：`internal/server/startup.go` 将完整客户端 JSON 写入 stdout；配置 Token 时包含当前 Token，匿名模式省略 headers。英文说明须准确描述鉴权状态，说明及运行日志写入 stderr。不得把同一份配置再次写进结构化日志。示例、测试和提交文件只能使用虚构 Token。

仅当 Token 非空时，才用其过滤日志字段；对空串调用 `strings.Contains` 总为真，会错误隐藏全部工具名。未知工具的英文回退标签同样须经过过滤，避免合法 Token 恰为 `unknown_tool` 时在回退字段输出完整凭据。

一个有效 IP 对应一个独立完整的 JSON 对象；多个对象是备选配置，不是单个 JSON 文档，也不应全部注册为重复服务。必须用 JSON 编码器处理凭据转义，禁止字符串拼接。

启动 JSON 使用 OpenCode 1.x 的 `$schema/mcp` 结构，每个服务包含 `type: "remote"`、`enabled: true` 和显式 `oauth: false`。英文提示须说明选一份合并到 OpenCode 配置；启动 JSON 消费者与示例随格式一起更新。

单 IP 绑定也要排除不可展示的组播和 IPv6 链路本地地址，不能绕过全接口枚举的过滤规则。IPv6 链路本地作用域属于具体机器，服务端网卡名不能直接作为远端 Agent 的作用域使用。

辅助命令 stdout 用于最终结构化摘要，stderr 用于进度与诊断；不得向 stdout 打印每个 Base64 分块或复制真实文件内容。

Go 服务的启动 JSON、普通日志，以及传输辅助命令的摘要和诊断均按 UTF-8 输出。Python 验证脚本读取这些文件或 subprocess 文本流时显式指定 `encoding="utf-8"`，不能依赖 Windows 的本地代码页；保留严格解码，不能用忽略或替换错误来掩盖编码问题。成功摘要须在日志校验和服务清理完成后才输出，避免先报成功再抛出异常。

## MCP 失败诊断契约

### 1. 范围与触发条件

修改 MCP 接入、中间件、业务错误类型或普通日志时核对本契约。用户要求失败时提供具体原因；只记录 `failed=true` 无法判断是参数、资源、权限还是恢复失败。诊断由服务端统一处理，各模块不重复打印工具响应。

### 2. 签名与错误来源

- `mcp.MethodHandler` 返回 `(mcp.Result, error)`，通过 `AddReceivingMiddleware` 观察 `tools/call`。
- 同时检查 Go error 和 `*mcp.CallToolResult.IsError`。已锁 SDK 的工具适配会把业务或 schema 错误转换成 `IsError=true, err=nil`。
- `CallToolResult.GetError() error` 可提供 SDK `SetError` 保留的服务端原始错误。执行/转发优先通过它提取具体错误类型，不能假设客户端可取得此指针，也不能随意解析并信任任意文本 JSON。
- `toolErrorAttributes(req mcp.Request, result mcp.Result, err error, token string) []any` 统一生成安全失败字段。文件工具的 SDK typed 输出会变为 `json.RawMessage`：只在精确内置 transfer 工具域投影 `ok/code/message`，原始结构最多 4096 字节，不能延伸到任意 Content 或自定义工具。

### 3. 日志字段与内容边界

- 所有工具调用保留 `tool/elapsed/failed`；失败增加 `error_code/error_message`，成功不附错误字段。
- 内置 GUI、传输、文件操作、日志跟踪、巡检、执行和转发按精确工具域、具体业务类型及已知错误码提取英文原因，保持原错误码大小写。新增工具或错误码时同步诊断支持与测试。程序自身日志事件、受控回退与隐藏标记也使用英文，文档和注释继续中文。
- 未知工具、SDK 参数/协议错误、取消、超时和未识别异常采用受控的原因分类，不能直接打印 `err.Error()`、请求参数、完整 StructuredContent 或 Content。
- 文本长度有上限，控制字符不能注入多行日志；非空 Token 与敏感输入值不得出现在诊断中。空 Token 不能被当作任意字符串的匹配项；公开枚举不能被误当成内容而遮掉整个错误原因。
- 英文隐藏标记也属于日志内容：不能包含当前 Token 或被屏蔽值，标记与相邻文字拼接后也不能重新形成 Token 或已收集的敏感内容；固定回退说明同样经过脱敏。保留标记冲突与拼接的回归，不能直接用固定 `[redacted]` 替换所有凭据。
- 原始业务 message 最多 4096 字节，日志 message 最多 1024 字节（包含截断省略号），无效编码或超限原文采用固定说明。额外防御仅处理最多 1 MiB JSON，在内容字段内递归最多 8 层，收集最多 128 个符合长度条件的敏感字符串候选；这不是严格的 128 个遍历节点上限。仅匹配 `text/path/dir/command/args/env/data/data_base64/query/edits/target/expected_sha256` 等字段中至少 4 个字符、最多 4096 字节的值。短值或超过扫描边界的请求依赖已核对的内置固定消息契约，不把额外防御当成通用任意错误脱敏器。
- GUI 失败可附布尔 `input_may_have_applied` 和白名单 `clipboard_restore` 状态，不能记录原剪贴板数据或任意附加响应字段。
- 日志诊断不改变 MCP 响应、输入是否已提交或剪贴板恢复状态，不用日志字段替代调用方的业务结果。

### 4. 校验与错误矩阵

| 来源 | 日志行为 |
| --- | --- |
| 内置工具具体业务错误 | 保留已知错误码和安全英文原因 |
| `err=nil` 但 `IsError=true` | 仍为失败，提取受支持的业务类型或 SDK 分类 |
| SDK 必填/类型/JSON 校验失败 | 输出相应受控原因，不转发可能含参数值的原文 |
| 未知工具或任意第三方错误内容 | 输出受控诊断，不将任意 code/message 对象当可信业务类型 |
| 取消或超时 | 明确区分取消/超时，不输出内容 |
| 成功 | `failed=false`，无 `error_code/error_message` |

### 5. 正常、边界与失败示例

- 失败示例：`tool=gui_text failed=true error_code=unsupported error_message="Clipboard text input is not supported on Windows"`，另有既有耗时字段。
- 边界：匿名配置仍能看到工具名及原因；Windows 的 `mode=clipboard` 返回具体英文不支持原因。
- 失败：工具文本块或 SDK 错误夹带虚构凭据、路径、输入文本时，日志不能包含它们；不能为了提供原因开启 SDK 全量调试日志。

### 6. 必需测试

- 独立 HTTP JSON-RPC 覆盖 GUI、文件、执行与转发业务失败，核对具体 code/message 且响应契约不变。
- 覆盖 SDK 转换后 `err=nil/IsError=true`、缺失参数、类型/JSON 错误、未知工具与未识别异常。
- 覆盖成功不附错误字段、匿名和 Token 配置、虚构敏感输入、控制字符与文本上限。
- 覆盖 Token 与英文隐藏标记冲突、相邻文字拼接形成 Token 或敏感内容、固定回退说明含 Token，以及用户内容恰好等于内容隐藏标记的情况。
- 新增已知业务码时，断言有具体原因；不能只检查日志包含 `failed=true` 或宽泛的“失败”。

### 7. 错误方式与正确方式

错误：只检查 Go error；SDK 已转换的业务失败没有原因。或直接把完整 error/Content 打进日志，连带输出请求内容。

正确：同时检查结果的 `IsError`，使用服务端保留的具体错误及受支持的业务类型；只记录错误码和经过筛选的英文诊断。

新增远程调试工具的诊断与预算见 [远程调试 P0 契约](remote-debug.md)。文件操作和日志跟踪使用具体 typed Error；巡检 wrapper 保留服务端 GetError 指针及完整阶段响应，超过 4096 字节的失败 partial 结果仍可提取固定业务原因，不能为了日志记录整个快照。
