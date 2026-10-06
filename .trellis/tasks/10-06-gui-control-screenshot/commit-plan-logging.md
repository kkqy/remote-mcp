# 英文错误诊断与运行提示提交计划

状态：实施、最终复审与受影响回归均通过，用户已确认本计划，按以下范围执行提交。本轮为 `9d72316` 之后的独立修复提交，不推送远程。

## 逻辑提交

`fix: 完善英文错误诊断与运行提示`

变更内容：MCP 失败日志增加错误码和安全具体原因；全模块程序错误、提示、日志、CLI 帮助及工具/schema 描述使用英文；同步回归、规范和任务证据。中文用户数据、注释和文档保持原样，错误码、JSON 字段与业务流程不变。

文件范围：

- `.trellis/spec/backend/error-handling.md`
- `.trellis/spec/backend/index.md`
- `.trellis/spec/backend/logging-guidelines.md`
- `.trellis/spec/backend/mcp-contracts.md`
- `.trellis/spec/backend/quality-guidelines.md`
- `.trellis/tasks/10-06-gui-control-screenshot/commit-plan-logging.md`
- `.trellis/tasks/10-06-gui-control-screenshot/design.md`
- `.trellis/tasks/10-06-gui-control-screenshot/implement.md`
- `.trellis/tasks/10-06-gui-control-screenshot/prd.md`
- `.trellis/tasks/10-06-gui-control-screenshot/research/check-english-output.md`
- `.trellis/tasks/10-06-gui-control-screenshot/research/check-error-logging.md`
- `.trellis/tasks/10-06-gui-control-screenshot/research/implementation-english-cli.md`
- `.trellis/tasks/10-06-gui-control-screenshot/research/implementation-english-gui.md`
- `.trellis/tasks/10-06-gui-control-screenshot/research/implementation-english-modules.md`
- `.trellis/tasks/10-06-gui-control-screenshot/research/implementation-error-logging.md`
- `README.md`
- `README.zh-CN.md`
- `cmd/remote-mcp-transfer/main.go`
- `cmd/remote-mcp-transfer/main_test.go`
- `cmd/remote-mcp/main.go`
- `internal/config/config.go`
- `internal/config/token.go`
- `internal/execution/buffer.go`
- `internal/execution/manager.go`
- `internal/execution/platform.go`
- `internal/execution/platform_unix.go`
- `internal/execution/platform_windows.go`
- `internal/execution/tools.go`
- `internal/execution/types.go`
- `internal/forwarding/manager.go`
- `internal/forwarding/tools.go`
- `internal/forwarding/types.go`
- `internal/gui/clipboard_portal_linux.go`
- `internal/gui/helpers.go`
- `internal/gui/layout_portal_linux.go`
- `internal/gui/manager.go`
- `internal/gui/manager_test.go`
- `internal/gui/platform_darwin.go`
- `internal/gui/platform_windows.go`
- `internal/gui/tools.go`
- `internal/gui/types.go`
- `internal/gui/wayland_linux.go`
- `internal/gui/x11_connection_linux.go`
- `internal/gui/x11_linux.go`
- `internal/server/runtime_language_test.go`
- `internal/server/server.go`
- `internal/server/startup.go`
- `internal/server/startup_test.go`
- `internal/server/tool_logging.go`
- `internal/server/tool_logging_test.go`
- `internal/transfer/manager.go`
- `internal/transfer/tools.go`
- `scripts/gui-smoke.py`
- `scripts/gui_clipboard.py`
- `scripts/gui_fixture.py`
- `scripts/smoke.py`
- `scripts/test_gui_smoke.py`
- `scripts/test_smoke.py`

## 验证

- 全包 `go test ./...`、`go test -race ./...` 和 `go vet ./...` 通过；复审补强日志隐私后，受影响 server 包竞态回归及全包 vet 再次通过。HTTP 集成测试使用获准的临时回环监听环境。
- 最终源码的 Linux、Windows、macOS amd64/arm64 六组合、两个命令共十二个无 CGO 二进制构建通过。
- Python 31 项离线回归、六个脚本的 `py_compile` 通过；生产 Go 字符串语言检查通过，中文验证数据保留。
- 最终源码构建的 Linux amd64 二进制，Token 与匿名真实上传、MCP 执行、下载冒烟均通过。
- `git diff --check` 和任务 context 校验通过。

## 排除项与边界

- `.opencode/package.json` 是用户已有改动，保持原样，不暂存。
- 本地二进制、临时服务日志、截图及缓存不纳入提交。
- 本轮未重跑宿主 GUI 授权或剪贴板操作，不新增原生桌面验收结论，任务保持 `in_progress`。
- 不改依赖、GUI 后端或系统权限；仅错误说明、提示及日志诊断发生本轮约定的变化。
