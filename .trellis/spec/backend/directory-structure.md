# 目录结构

- `cmd/remote-mcp/`：服务进程入口，负责信号处理及启动退出。
- `cmd/remote-mcp-transfer/`：本地上传下载辅助命令，通过 MCP 调用服务，不使用旁路文件接口。
- `internal/config/`：命令行配置、默认值、Token 加载及配置校验。
- `internal/server/`：HTTP/MCP 接入、鉴权、Origin 校验、监听、启动配置输出和服务关闭。
- `internal/transfer/`：分块传输状态机、校验、临时文件发布、文件工具及平台提交操作。
- `internal/execution/`：普通进程、交互终端、输出缓冲、生命周期及 Unix/Windows 系统适配。
- `internal/forwarding/`：TCP 转发监听、双向流量复制、连接限额及规则生命周期。
- `scripts/build.sh`：六种系统/架构组合的两个程序构建。
- `scripts/smoke.py`：使用实际二进制完成上传、独立协议调用、执行和下载校验。
- `.github/workflows/check.yml`：跨平台检查配置；配置存在不代表远端已经执行成功。

测试与所属代码放在同一包的 `_test.go` 文件。系统差异由 `_windows.go` 及带构建约束的 Unix 文件隔离。共享 `go.mod/go.sum` 的写入必须协调，不允许并行代理覆盖依赖。

管理器统一提供 `DefaultConfig()`、`New(Config)`、`Register(*mcp.Server)` 和 `Close()`。配置、HTTP 和 CLI 层不能重复实现管理器的状态转换。接口类型由所属模块维护，辅助命令复用文件模块的输入和结果结构。
