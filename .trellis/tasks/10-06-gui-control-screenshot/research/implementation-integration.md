# GUI 服务集成实施记录

## 写入前边界

现有服务只创建和注册文件、执行、转发管理器；所需最小变化是在同一 MCP/HTTP 入口接入 GUI 管理器，并让配置解析及服务关闭覆盖 GUI。桌面授权、图像编码和输入状态均由 `internal/gui` 负责，服务层不复制这些逻辑。

本实施者拥有 `internal/config/config.go`、GUI 配置测试、`internal/server/server.go`、新增 GUI HTTP 集成测试、两个 README 的 GUI 新内容及 `scripts/gui-smoke.py`。不修改 `internal/gui`、共享 Go 依赖、旧 smoke 脚本、任务状态或既有用户修改；不重构现有 HTTP、鉴权与日志行为。

验证入口采用独立 JSON-RPC；真实桌面操作必须显式启用，输入计划必须注明调用方准备的目标。脚本核验 PNG 与元数据并保留操作前后图像，事件提交与目标应用中文内容验证分别记账；不把一次脚本执行当作五平台完整验收。

## 实施与验证

- 配置新增 GUI 管理器默认配置及五个参数：`--gui-idle`、`--gui-authorize-timeout`、`--gui-operation-timeout`、`--gui-max-pixels`、`--gui-max-png-bytes`。直接构造及命令行配置都由 `gui.Config.Validate` 校验。
- 服务创建、注册和关闭 GUI 管理器；初始化错误回收已经创建的转发、执行和文件管理器。继续沿用同一鉴权、Origin、协议和元数据日志。
- 新增独立 HTTP/JSON-RPC 测试，覆盖七个 GUI 与旧工具发现、区域 PNG 的实际像素及解码、坐标偏移与时间来源、匿名及 Token、连续输入和按键、稳定失效截图错误、日志不含中文输入，以及服务关闭清理。
- 主会话明确扩展所有权后，同步旧匿名工具发现测试的总数 22→29，并独立核对七个 GUI 名称与旧模块代表工具；未修改其他旧断言。
- 两个 README 增加中文 GUI 说明：部署依赖、系统授权、工具、坐标、剪贴板、限额及验证入口。明确 Wayland `captured_at_source=received_at`、`freshness=latest_available` 的时间与帧限制；Windows/macOS 使用原生 Unicode，显式剪贴板模式当前不提供。
- `scripts/gui-smoke.py` 不带 `--execute` 只显示帮助。显式运行可独立连接匿名/Token MCP、严格 PNG/元数据验证并保存证据。自定义输入计划必须声明已准备的专用目标；可选 `--fixture` 启动 PySide6 原生窗口，以四颜色标记映射截图坐标，验证目标窗口实际点击、双击、拖拽、滚动、组合键和中文文本。Wayland 强制原生 Qt Wayland 后端。
- 验证窗口不事先更改用户剪贴板；默认保持 `gui_text` 的保存恢复策略，显式 `--allow-clipboard-replace` 才允许替换。失败保留 `run_completed:false` 报告，所有出口关闭 GUI 与窗口。`acceptance_complete:false` 表示单次基本操作不覆盖全部 PRD 验收。
- 验证依赖 PySide6 与窗口事件接口按 [Qt 官方文档](https://doc.qt.io/qtforpython-6/PySide6/QtWidgets/QWidget.html) 经 Context7 核对；不属于 remote-mcp 服务运行依赖。

## 检查结果

| 检查 | 结果 |
| --- | --- |
| 所有本实施者 Go 文件 `gofmt`、`git diff --check` | 通过 |
| `GOCACHE=/tmp/remote-mcp-gui-integration-cache go vet ./internal/config ./internal/server` | 通过 |
| `go test ./internal/config ./internal/server` | 通过，回环 HTTP 检查在允许 socket 的环境运行 |
| `go test -race ./internal/config ./internal/server` | 通过 |
| `python3 -m unittest discover -s scripts -p 'test_*smoke.py'` | 12 项通过，包括原 smoke 离线回归及新增入口边界 |
| GUI 脚本与 fixture `py_compile` | 通过 |
| 不带 `--execute` 且 URL 无效的入口执行 | 通过，只显示帮助，不连接网络或桌面 |
| Windows/amd64 config/server 测试程序无 CGO 交叉编译 | 通过；仅证明构建，未原生运行 |

Go 默认缓存位于只读路径，检查使用任务专属 `/tmp` 缓存。普通沙箱禁止监听 socket，服务集成测试在自动审批通过的扩展环境运行；这不属于产品失败。

## 待验证与交接

此实施者未运行真实 GUI，主会话将使用上述入口核验本机 KDE Wayland。五类桌面、实际 MCP 客户端展示、多屏混合缩放、剪贴板竞争、撤销及断线仍需分别记账。配置/server 模拟后端和 Windows 交叉编译不能替代原生验收。当前不提交、不推送、不修改任务状态；`.opencode/package.json` 原有用户修改保持不动。
