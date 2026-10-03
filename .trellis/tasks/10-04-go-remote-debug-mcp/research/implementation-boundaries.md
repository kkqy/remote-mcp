# 实施边界与协作分工

本项目从空业务仓库实现已批准的 Go 服务，行为缺口位于服务入口、MCP 工具、文件与进程生命周期及系统适配层。

- 服务基础代理：拥有 go.mod、go.sum、cmd/remote-mcp/、internal/server/、internal/config/、启动 IP 配置输出、README、构建与 CI 配置及根目录忽略文件；负责集成另外两个模块。
- 文件代理：拥有 internal/transfer/ 与 cmd/remote-mcp-transfer/，实现文件工具、管理器、辅助命令和模块测试。
- 执行代理：拥有 internal/execution/，实现普通进程、后台进程、Unix PTY、Windows ConPTY 及模块测试。
- 主会话：协调接口，维护任务及真实后端规范，审查验证结果并执行最终集成检查；检查代理在实施完成后审核并修正质量问题。

模块路径使用 remote-mcp。共享 go.mod/go.sum 仅由服务基础代理修改。其他代理需要依赖时发送具体版本给服务基础代理；所有代理共享工作区，不回退他人改动。

模块集成约定：transfer 与 execution 各自暴露 Config、New(Config) (*Manager, error)、(*Manager).Register(*mcp.Server)、(*Manager).Close() error。Config 默认值由模块提供 DefaultConfig() Config；服务端配置层覆盖后传入。具体字段由模块负责人尽早发给服务基础代理，公共 JSON 参数与结果使用类型化结构。

同一资源生命周期的状态、锁、清理和工具注册由模块单一负责人拥有；不可在 HTTP 层假装完成资源关闭。跨模块不共享可变全局变量。业务错误使用稳定错误码并由 MCP 工具错误返回，普通输出与终端输出的差异按 design.md。

本次不实现前端、目录同步、断点续传、反向中转或多租户。平台差异必须真实记录，未获得 Windows/macOS 环境不伪造运行验证；提供可执行三平台测试和 CI 配置。
