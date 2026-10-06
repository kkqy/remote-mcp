# MCP 失败原因日志质量复审

日期：2026-10-06。本轮范围为用户追加 LOG-01/AC-08；先前 GUI 已提交 `9d72316`，不重新执行原生授权或输入，也不把日志检查算作 GUI 验收。

## 加载与完整范围

按 check.jsonl、PRD、design、implement 读取新增日志需求；执行 packages 入口确认单仓库，按 backend 索引 Quality Check 读取质量、日志与错误规范。MCP、资源及 GUI 规范沿用同任务此前已加载的完整契约，并核对新增日志条款。不存在本轮业务前端改动，frontend 模板不适用。

逐路径核对 `server.go` receiving 中间件、`tool_logging.go`、独立 HTTP 测试、两份 README 和实施研究；同时只读追踪 GUI、transfer、execution、forwarding 的实际注册器和全部错误来源，以及锁定 SDK v1.8.0 的 tool/server/protocol 源码。没有改其他模块、依赖或用户 `.opencode/package.json`。

## 已验证的实现行为

- 中间件同时观察 Go error 与 CallToolResult.IsError。SDK 普通业务错误及 schema 错误转成 IsError=true、err=nil；GetError 保留服务端原类型，执行/转发通过具体类型取原因。GUI 原注册器保留本域 gui.Error 指针；文件 typed Result 经 SDK 变为 RawMessage，仅在精确 transfer 域投影明确 ok=false 的 code/message，原投影不超过 4096 字节，不读取任意 Content。
- 工具域精确覆盖全部 29 个内置工具（GUI 7、文件 8、执行 10、转发 4）。核对各实际源码中 failure/Code 字面量：GUI 18、文件 12、执行 12、转发 7 个当前业务码均在对应白名单，保持大小写。不存在仅因名称前缀或未知工具借用错误类型/JSON 形状就输出原文的路径。覆盖现有工具注册必须继续遵守该域的安全消息契约，不把辅助函数当成通用第三方错误脱敏器。
- 四模块现有消息是固定说明、受控状态/原因与计数，不回显命令、路径、输入、图片、剪贴板或 D-Bus Body。SDK 参数分类前缀来自锁定源码；HTTP 回归实证必填缺失、类型不符、参数对象错误及未知工具类别，未知普通异常仅记录固定类别，不转储 Error()/Content。
- 已知原业务 message 最多 4096 字节，日志输出最多 1024 字节，含省略号且按 UTF-8 边界截断。控制字符及 Unicode Cf 变空格；非空 Token 做精确替换，空 Token 不全匹配。请求内容仅是额外防御：JSON 至多 1 MiB、收集深度 8、最多 128 个符合长度条件的敏感候选字符串，公开枚举/键/ID及短值不参与。这里的 128 是收集数量，短值/容器的遍历仍由字节与深度限制；主会话已同步规范措辞。
- 失败保留 tool/elapsed/failed 并增加 error_code/error_message；GUI 只额外输出布尔 input_may_have_applied 与已知 clipboard_restore 状态。成功不附错误字段。代码没有修改原 CallToolResult/error、业务状态机或 HTTP 鉴权。

## 发现与已修复

未发现需修改生产实现的可确认缺陷。本审阅者补齐 `internal/server/tool_logging_test.go` 的验证缺口：

- 长日志原测试只断言上限/编码，未断言省略号和原始 4096 边界；新增真实 SDK/HTTP 的恰好 4096 字节说明截断、4097 字节固定回退，同时断言客户端仍收到完整原 GUI message、IsError 不变。
- 新增未知工具 RawMessage（明确 ok=false、合法文件错误码、虚构敏感 message）真实 HTTP 回归，保证文件投影权限不能从 JSON 形状自动扩展到未知工具。
- helper 回归明确要求截断带省略号、控制字符与 Cf 替换、无效 UTF-8 与超限原文固定回退。现有 HTTP 回归继续覆盖四模块实际失败、匿名/Token、SDK 转换、敏感内容过滤、成功无错误字段及原 GUI 恢复原因/状态。

## 未修项及边界

无确认未修生产代码缺陷。没有修改主会话负责的规范/规划；候选字符串数量措辞已由主会话同步规范，本审阅同步了实施研究。

严格 HTTP/SDK 解码阶段即拒绝、尚未进入 tools/call receiving 中间件的请求，不会生成本轮的“工具调用完成”日志；研究已说明此边界。未知工具或没有安全消息契约的异常只有受控类别，不能承诺日志总包含任意原始异常详情。短字符串和超扫描边界的内容依赖已核对的内置固定错误消息契约。

先前五平台原生、多屏、客户端图片展示、撤权及正常剪贴板恢复待验收状态全部保留；本轮无桌面/剪贴板操作。

## 验证

- 审阅者最终 `GOCACHE=/tmp/remote-mcp-check-go-cache go test -race ./internal/server -count=1`：通过（2.577 秒），含全部新增 HTTP 回归。经自动审核允许本机临时回环监听，使用虚构内容，不连接真实 GUI。
- 审阅者 `go vet ./internal/server`：通过；本轮三个 Go 文件 gofmt 无未格式化文件；`git diff --check` 通过。Go 类型检查由包编译与 race 测试覆盖。
- 主会话已确认生产冻结版本全包 test/race/vet、六组合两个入口无 CGO 构建、实际旧功能二进制 Token/匿名 smoke 均通过；上下文 7/7 与 diff 检查通过。随后本审阅者仅增测试，生产源码未改，因此全量构建与二进制证据不被作废；新增测试以本报告的最终受影响 server race/vet 为准。

本轮实现及补充回归已可审阅，未提交或归档任务。
