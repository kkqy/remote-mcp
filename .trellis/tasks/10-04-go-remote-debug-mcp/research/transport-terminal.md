# MCP 传输与交互式终端初步调研

调研日期：2026-10-04。以下是技术证据，最终选型见文末；产品兼容性边界随完整规划方案评审。

## MCP 服务

- 官方 Go SDK 提供服务端及 Streamable HTTP 处理器，可作为内网直连的候选实现。
- 官方鉴权示例使用 RequireBearerToken 包装 MCP HTTP 处理器，并通过 TokenVerifier 校验令牌。可以据此评估预共享 Token 的接入；静态 Token 方案不应描述为完整 OAuth 授权流程。
- 用户已确认通用 MCP 接入与本地辅助命令；客户端需支持所选 HTTP 传输及请求携带 Bearer Token。
- SDK、Go 工具链及终端库的评审选型已确定，见文末；go.mod/go.sum 在实施阶段创建。

来源：
- [官方 Go SDK](https://github.com/modelcontextprotocol/go-sdk)
- [官方鉴权中间件示例](https://github.com/modelcontextprotocol/go-sdk/blob/main/examples/server/auth-middleware/README.md)

## 终端适配

- Linux 和 macOS 可评估 creack/pty；仓库提供对应系统的 PTY 实现。
- Windows 单独评估 ConPTY 适配。微软文档确认 CreatePseudoConsole 支持输入、输出和终端尺寸，并要求调用方在结束后关闭伪控制台。
- ConPTY 输入输出为 UTF-8，并包含终端控制序列。因此 MCP 输出应保留控制序列的语义，不能将它当成普通命令的独立 stdout/stderr。
- 微软列出的 ConPTY 最低版本为 Windows 10 1809 和 Windows Server 2019。这是 API 下限；最终服务的系统下限还受所选 Go 工具链及依赖约束，不能直接据此承诺旧系统兼容。
- 创建、输入、增量读取、调整尺寸、关闭可封装为独立 MCP 工具；终端会话应由应用管理，避免与单次 HTTP 请求生命周期绑定。这属于候选设计。
- 需进一步验证 Windows 进程树回收、终端关闭时的阻塞行为、输出缓冲上限和断线清理，并在真实系统测试，不能仅凭交叉编译宣布支持。

来源：
- [creack/pty](https://github.com/creack/pty)
- [CreatePseudoConsole 文档](https://learn.microsoft.com/en-us/windows/console/createpseudoconsole)
- [创建伪控制台会话](https://learn.microsoft.com/en-us/windows/console/creating-a-pseudoconsole-session)

## 传输与客户端边界补充

- MCP 的 Streamable HTTP 使用独立运行的服务端及统一 HTTP 端点；现有官方 SDK 可承担协议实现，避免自写 JSON-RPC 生命周期。
- 规范要求校验存在的 Origin 请求头；非法 Origin 返回 403。所有连接仍须独立执行 Token 校验。
- HTTP 断开不等于取消请求。应用层进程和终端的关闭必须有显式操作及资源清理策略。
- 大文件分块是本服务的工具契约，不是 MCP 原生文件同步功能；需要客户端程序把本地字节编码后放入 MCP 参数，并把下载结果写入本地文件。
- 本轮环境检查确认 Go 工具链为 linux/amd64；未确认 Windows/macOS 运行环境。

来源：[MCP 2025-11-25 传输规范](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports)。该版本作为首版协议兼容基线。

## 评审选型与版本证据

| 组件 | 选型 | 证据及约束 |
| --- | --- | --- |
| Go SDK | github.com/modelcontextprotocol/go-sdk v1.8.0 | 官方已发布版本，go.mod 要求 Go 1.25.0；支持限制协议协商版本。 |
| Unix PTY | github.com/creack/pty v1.1.24 | 版本化包文档提供 StartWithSize、Setsize 等接口；关闭中断读取需正确设置非阻塞模式。 |
| Windows ConPTY | github.com/charmbracelet/x/conpty v0.2.0 | 版本化包文档提供创建、读写、调整尺寸、Spawn 和 Close；不是 Go v1 稳定接口，需固定版本并以内部适配层隔离。 |
| Go 工具链 | 默认使用 Go 1.27，模块最低 1.25.0 | 当前开发环境为 1.27.1；按默认构建工具链确定 macOS 13+ 下限。 |

- SDK v1.8.0 还支持更新的 2026-07-28 协议，但其请求生命周期有变化。首版明确限制到已经研究的 2025-11-25 基线，以避免意外宣称新协议完整兼容；未来扩展另做验证。
- SDK v1.8.0 的 Streamable HTTP 处理器仍有请求体上限配置；旧的 CrossOriginProtection 选项已弃用，采用外层中间件。当前请求取消传播选项只影响更新协议，应用资源清理仍由本服务管理。
- Go 官方最低要求列出 Linux 内核 3.2+、Windows 10+；结合 ConPTY API 下限，首版 Windows 目标为 Windows 10 1809+/Server 2019+。这些是技术下限推导，尚未构成本项目实际验证记录。
- 实施时须下载固定版本并核对构建；对平台清理等难点做实际实验。内部适配修正不改变产品范围，若需要削减能力则返回规划。

来源：
- [Go SDK v1.8.0 发布说明](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.8.0)
- [Go SDK v1.8.0 模块要求](https://github.com/modelcontextprotocol/go-sdk/blob/v1.8.0/go.mod)
- [Go SDK v1.8.0 HTTP 实现](https://github.com/modelcontextprotocol/go-sdk/blob/v1.8.0/mcp/streamable.go)
- [PTY v1.1.24 包文档](https://pkg.go.dev/github.com/creack/pty@v1.1.24)
- [ConPTY v0.2.0 包文档](https://pkg.go.dev/github.com/charmbracelet/x/conpty@v0.2.0)
- [Go 系统最低要求](https://go.dev/wiki/MinimumRequirements)
- [Go 1.27 链接器说明](https://go.dev/doc/go1.27#linker)

## 启动配置 JSON 格式证据

- 用户追加要求：启动输出配置 JSON，每个本机可连接 IP 分别展示一份。
- Claude Code 官方文档示例使用 mcpServers 顶层对象；HTTP 条目包含 type、url 和可选 headers，Bearer Token 可通过 Authorization 头配置。
- 官方文档明确：在该客户端中只有 url 而缺少 type 会被视为错误配置。因此输出显式加入 type=http。
- 这是已查证的一种常见客户端配置格式，不是 MCP 协议定义的统一客户端配置文件；本项目通用协议接入范围不因此改成仅支持 Claude Code，也不承诺所有客户端都能直接导入相同 JSON。

来源：[Claude Code MCP 配置文档](https://code.claude.com/docs/en/mcp)。
