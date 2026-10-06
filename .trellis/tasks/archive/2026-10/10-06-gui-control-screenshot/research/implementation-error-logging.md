# MCP 错误日志实施记录

## 写入前边界

用户追加 LOG-01：失败日志需要具体错误码与原因。变化只位于服务接收中间件和诊断辅助代码；继续保留 tool/elapsed/failed，不改变业务响应、HTTP 鉴权或其它模块。

已核实锁定 SDK v1.8.0 的 `mcp/server.go:395`：schema 及普通业务错误会调用 `CallToolResult.SetError`，返回 IsError=true、err=nil。`mcp/protocol.go:337` 的 SetError 同时保留原始 error，服务端 `GetError()` 可读取；它不序列化到 JSON，客户端 GetError 总为空。执行/转发优先读取这一类型信息，无需信任任意文本 JSON。

GUI 接收本域 gui.Error；文件模块 typed Result 经 SDK 转为 json.RawMessage，需要在精确内置文件工具边界仅投影读取 ok/code/message。诊断须同时匹配内置工具域、所属错误类型、已知错误码。未来自定义工具默认受控回退，不能因返回 code/message JSON 就获得完整原文日志权限；覆盖既有内置工具实现须遵守原本的安全消息契约。

业务中文消息已核对为固定文字、系统名、受控枚举和计数，不含调用参数。诊断有界并处理控制字符和非空 Token；附加防御只匹配 text/path/dir/command/args/env/data 等实际内容字段，不用 mode/action/keys/ID 等公开枚举或短值过滤。未知工具名也不作为可信日志元数据。SDK 错误只记录核实类别，未知原始异常不打印。

## 实施与检查

- 新增 `internal/server/tool_logging.go`，中间件失败时附加 `error_code/error_message`；GUI 另按安全状态白名单记录 `input_may_have_applied/clipboard_restore`。原 `tool/elapsed/failed` 保留，成功不附带错误字段。
- 工具名只认可精确内置域；未知/未来工具统一显示“未知工具”，不把调用者提供的名字再次拼入原因。公开 App.MCP 扩展能力保持原状。
- GUI 精确类型、本域已知码与原中文说明；文件 SDK `json.RawMessage` 只在已知 transfer 工具域投影 `ok/code/message`，必须明确 `ok=false`，上限 4096 字节；执行/转发经 SDK 转为文本 JSON 后通过服务端 `GetError()` 恢复其原始具体类型。不读取任意 Content，不盲接受通用 map 的 code/message。
- SDK 分类覆盖必填缺失、类型不符、参数对象格式、其它 schema 错误、未知工具、JSON-RPC 标准错误、取消与超时。原文仅用于固定类别识别，绝不写入日志。未知异常和未受信任工具保留“未识别错误”类别说明。
- 可信业务说明的原始上限 4096 字节，输出上限严格为 1024 字节（含省略号）；控制字符及 Unicode 格式控制字符转换为空格，非空 Token 精确替换为空间内固定标记，空 Token 不做全匹配替换。
- 内容过滤仅作固定业务消息契约的额外防御：参数 JSON 至多 1 MiB，内容字段递归最多 8 层、收集最多 128 个符合长度条件的敏感字符串候选（不是总遍历节点上限）；只处理 `text/path/dir/command/args/env/data/data_base64` 等内容字段中 4 个 Unicode 字符以上且不超过 4096 字节的字符串。mode/action/keys/ID 等枚举与短值不参与，避免把安全具体原因遮蔽。内置错误说明禁止回显原始内容的契约仍是主要安全边界。
- `toolErrorAttributes(req mcp.Request, result mcp.Result, err error, token string) []any` 是固定辅助入口；源码常量为 `maxDiagnosticMessageBytes=1024`、`maxBusinessMessageBytes=4096`、`maxDiagnosticRequestBytes=1<<20`。
- 两份 README 增加中文日志说明。业务响应和依赖没有改变。严格格式错误在 HTTP/SDK 解码阶段被拒绝、尚未进入 tools/call 中间件的请求，不伪造工具完成日志；本改动覆盖已进入该中间件的协议/业务失败。

## 检查结果

- `gofmt` 与 `git diff --check`：通过。
- `GOCACHE=/tmp/remote-mcp-gui-integration-cache go vet ./internal/server`：通过。
- `go test ./internal/server`：通过。
- `go test -race ./internal/server`：通过。
- 独立 HTTP 回归同时覆盖匿名及 Token。实际 GUI structured 错误、file typed→SDK RawMessage、execution/forwarding Error→SDK IsError 文本 JSON 均记录真实具体原因；确认 GetError 在 next 返回后仍可取到类型。
- 回归确认成功无错误字段、未知工具原名不入日志、未知 map/typed payload 不提升信任权限、未知异常/协议错误分类、虚构 Token/文本/路径未泄漏；GUI 恢复枚举与计数在 mode=clipboard 时保留，控制字符不会伪造多条日志，长说明按 UTF-8 边界截断。

## 交接

此轮仅改 `internal/server/server.go`、新增 `tool_logging.go/tool_logging_test.go`、两份 README 和本研究记录。源码已冻结，主会话/检查代理统一执行完整仓库检查及六组合构建；日志更改不需要重复授权或真实桌面输入。后续新增业务工具或错误码须显式更新日志信任边界和对应回归，不能从 JSON 形状自动授予原文日志权限。没有提交 Git，没有修改用户 `.opencode/package.json`。


## 最终复审补充

检查代理未发现需修改生产源码的确认缺陷，补充真实 HTTP 回归：4096 字节原业务说明带省略号截断、4097 字节固定回退、日志处理不修改客户端原 GUI 错误 message/IsError，以及未知工具 RawMessage 即便符合文件错误形状也不授予原文日志权限。helper 加严 UTF-8、省略号、控制/Cf 字符及无效编码断言。最终受影响 server race 通过（2.577 秒）、vet/gofmt/diff 通过；生产冻结版本未改变，主会话全量检查及实际旧二进制 smoke 继续有效。详见 check-error-logging.md。
