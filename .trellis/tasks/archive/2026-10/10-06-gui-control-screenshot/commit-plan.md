# 当前批次提交计划

当前代码与原生验证已完成，全部修改仍未提交。以下包含本任务此前 Windows 原生助手实现、当前 Windows-only 收敛、测试、文档和历史验收记录；不包含临时文件或构建产物。

## 1. refactor: 将 GUI 图形操作限定为 Windows

移除 Linux GUI 后端、依赖、工具注册与 CLI；保留 Windows 功能及专用原生验收助手，同步协议/脚本/文档和规范。

- `.gitignore`
- `.trellis/spec/backend/directory-structure.md`
- `.trellis/spec/backend/gui.md`
- `.trellis/spec/backend/index.md`
- `.trellis/spec/backend/logging-guidelines.md`
- `.trellis/spec/backend/mcp-contracts.md`
- `.trellis/spec/backend/quality-guidelines.md`
- `README.md`
- `README.zh-CN.md`
- `go.mod`
- `go.sum`
- `internal/config/config.go`
- `internal/config/gui_test.go`
- `internal/gui/clipboard_portal_linux.go`
- `internal/gui/layout_portal_linux.go`
- `internal/gui/linux_test.go`
- `internal/gui/platform_linux.go`
- `internal/gui/platform_other.go`
- `internal/gui/platform_support_windows.go`
- `internal/gui/portal_protocol_linux_test.go`
- `internal/gui/tools.go`
- `internal/gui/types.go`
- `internal/gui/wayland_linux.go`
- `internal/gui/x11_connection_linux.go`
- `internal/gui/x11_linux.go`
- `internal/server/gui_test.go`
- `internal/server/p0_test.go`
- `internal/server/server.go`
- `internal/server/server_test.go`
- `internal/server/tool_logging_test.go`
- `scripts/gui-smoke.py`
- `scripts/gui_clipboard.py`
- `scripts/gui_fixture.py`
- `scripts/native-smoke/gui_capture.go`
- `scripts/native-smoke/gui_capture_test.go`
- `scripts/native-smoke/gui_other.go`
- `scripts/native-smoke/gui_trace.go`
- `scripts/native-smoke/gui_trace_test.go`
- `scripts/native-smoke/gui_windows.go`
- `scripts/native-smoke/gui_windows_test.go`
- `scripts/native-smoke/main.go`
- `scripts/p0-smoke.py`
- `scripts/test_gui_smoke.py`
- `scripts/test_p0_smoke.py`
- `scripts/test_windows_gui_smoke.py`
- `scripts/test_windows_native_smoke.py`
- `scripts/windows-gui-smoke.py`
- `scripts/windows-native-smoke.py`

## 2. test: 记录 Windows GUI 验收与 Linux 范围调整

记录当前任务边界、复审与 Linux/Windows 当前产物实跑结果；保留已取消 Linux GUI 的历史研究，不将它们视为支持要求。

- `.trellis/tasks/10-06-gui-control-screenshot/check.jsonl`
- `.trellis/tasks/10-06-gui-control-screenshot/commit-plan.md`
- `.trellis/tasks/10-06-gui-control-screenshot/design.md`
- `.trellis/tasks/10-06-gui-control-screenshot/implement.jsonl`
- `.trellis/tasks/10-06-gui-control-screenshot/implement.md`
- `.trellis/tasks/10-06-gui-control-screenshot/prd.md`
- `.trellis/tasks/10-06-gui-control-screenshot/research/check-gui-native.md`
- `.trellis/tasks/10-06-gui-control-screenshot/research/check-windows-only.md`
- `.trellis/tasks/10-06-gui-control-screenshot/research/implementation-nested-wayland.md`
- `.trellis/tasks/10-06-gui-control-screenshot/research/implementation-windows-gui.md`
- `.trellis/tasks/10-06-gui-control-screenshot/research/implementation-windows-only-go.md`
- `.trellis/tasks/10-06-gui-control-screenshot/research/implementation-windows-only-scripts.md`
- `.trellis/tasks/10-06-gui-control-screenshot/research/kde-workspace-bootstrap-03.json`
- `.trellis/tasks/10-06-gui-control-screenshot/research/kde-workspace-bootstrap-04.json`
- `.trellis/tasks/10-06-gui-control-screenshot/research/kde-workspace-bootstrap-07.json`
- `.trellis/tasks/10-06-gui-control-screenshot/research/kde-workspace-bootstrap-08.json`
- `.trellis/tasks/10-06-gui-control-screenshot/research/kde-workspace-segmented-01.json`
- `.trellis/tasks/10-06-gui-control-screenshot/research/kde-workspace-segmented-02.json`
- `.trellis/tasks/10-06-gui-control-screenshot/research/kde-workspace-segmented-05.json`
- `.trellis/tasks/10-06-gui-control-screenshot/research/kde-workspace-segmented-06-cleanup.json`
- `.trellis/tasks/10-06-gui-control-screenshot/research/kde-workspace-segmented-06-hold.json`
- `.trellis/tasks/10-06-gui-control-screenshot/research/kde-workspace-segmented-06.json`
- `.trellis/tasks/10-06-gui-control-screenshot/research/kde-workspace-segmented-09-cleanup.json`
- `.trellis/tasks/10-06-gui-control-screenshot/research/kde-workspace-segmented-09.json`
- `.trellis/tasks/10-06-gui-control-screenshot/research/kde-workspace-segmented-10-cleanup.json`
- `.trellis/tasks/10-06-gui-control-screenshot/research/kde-workspace-segmented-10.json`
- `.trellis/tasks/10-06-gui-control-screenshot/research/linux-gui-removal-2026-10-07.json`
- `.trellis/tasks/10-06-gui-control-screenshot/research/native-readiness-2026-10-06.md`
- `.trellis/tasks/10-06-gui-control-screenshot/research/native-validation-2026-10-06.md`
- `.trellis/tasks/10-06-gui-control-screenshot/research/windows-gui-2026-10-06.json`
- `.trellis/tasks/10-06-gui-control-screenshot/research/windows-gui-windows-only-2026-10-07-before-child-env.json`
- `.trellis/tasks/10-06-gui-control-screenshot/research/windows-gui-windows-only-2026-10-07.json`
- `.trellis/tasks/10-06-gui-control-screenshot/research/windows-only-final-builds-2026-10-07.json`
- `.trellis/tasks/10-06-gui-control-screenshot/research/windows-only-scope-2026-10-07.md`
- `.trellis/tasks/10-06-gui-control-screenshot/task.json`

## 排除

- `.opencode/package.json`：用户已有修改，保持不动。
- `.tmp/`、`dist/`：临时证据、缓存及构建产物，不提交。

## 已完成验证

全 Go test/vet/race、四组合双入口八产物及 Windows 测试交叉编译；全范围复审后 49 项 Python 回归及相关 race/vet；最终 Linux 旧功能/P0 Token/匿名四轮及 GUI 删除边界；最终 Windows/amd64 自有窗口 Token/匿名两轮截图/真实事件/Unicode，部署及窗口清理、原主入口保持均通过。Windows/arm64 实机、多屏/混合 DPI、布局变化、撤权及实际客户端图片展示仍未验。

项目 workflow Phase 3.4 要求一次提交确认；收到确认后仅按此计划提交，不自动推送。
