# 错误处理

程序自身的业务、配置、启动和 HTTP 错误返回英文说明，不回显 Token、原始参数或敏感请求。2026-10-06 用户要求覆盖此前中文输出规则；会话、文档和源码注释继续中文，用户实际 UTF-8 内容及系统提供的名字不翻译。参考 `internal/config/token.go` 和 `internal/server/server.go`。

辅助命令同样不得直接转发 flag 包的原始解析错误，错误参数值或参数名也可能含凭据。帮助文本不展示环境变量 URL 的默认值，`--help` 正常退出；具体回归见 `cmd/remote-mcp-transfer/main_test.go`。

HTTP 边界区分鉴权失败、Origin 拒绝、未知路径、请求限额与协议错误；不把业务失败伪装为鉴权失败。

文件工具以 `transfer.Result` 的 `ok/code/message` 表达业务结果，并设置 MCP `CallToolResult.IsError`。执行模块使用带稳定 `code/message` 的 `execution.Error`，交由 SDK 映射工具错误。两种模块保持各自已经公开的错误码大小写，不能在客户端自行改写。

SDK 映射后的 `IsError=true` 可能伴随中间件观察到的 `err=nil`，失败日志须同时检查两者，并使用 `CallToolResult.GetError()` 保留的服务端具体错误或内置结构化业务类型提取安全原因。不能通过打印完整 SDK error 或任意文本 JSON 扩大内容日志范围，具体字段与信任边界见 [日志规范](logging-guidelines.md)。

辅助命令从 `structuredContent` 解码结果，必要时兼容文本 JSON；同时检查 `IsError` 和 `ok`。成功 HTTP 状态不等于文件操作成功。辅助命令失败退出码非零，stdout 提供结构化摘要，stderr 提供英文说明、提示与进度。

文件写入失败、校验不一致或提交冲突时，保留已有目标并清理临时文件。不得先删除旧目标再尝试发布。资源关闭应允许重复调用，但真实清理失败不能伪报成功。
