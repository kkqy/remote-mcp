# 提交计划

状态：最终复审及常规检查已通过，用户于 2026-10-06 回复“确认提交”。只提交本任务改动，不推送远程。

## 逻辑提交

`feat: 增加跨平台 GUI 截图与输入工具`

文件范围：

- `internal/gui/`：公共契约、生命周期、五类桌面后端及回归。
- `internal/config/config.go`、`internal/config/gui_test.go`：GUI 配置及校验。
- `internal/server/server.go`、`internal/server/server_test.go`、`internal/server/gui_test.go`：工具注册、关闭和 HTTP 回归。
- `go.mod`、`go.sum`：锁定后端依赖。
- `scripts/gui-smoke.py`、`scripts/gui_clipboard.py`、`scripts/gui_fixture.py`、`scripts/test_gui_smoke.py`：显式原生验证入口及离线回归。
- `README.md`、`README.zh-CN.md`：中文 GUI 使用说明、依赖和限制。
- `.trellis/spec/backend/gui.md`、`index.md`、`directory-structure.md`、`mcp-contracts.md`：新增契约及规范入口。
- `.trellis/tasks/10-06-gui-control-screenshot/`：批准的规划、实施/检查上下文、研究、真实验收记录及本计划。

## 排除项

- `.opencode/package.json` 是用户已有改动，保持原样，不暂存、不纳入提交。
- 本机 `/tmp` 中的截图、运行日志和剪贴板证据不纳入仓库。

## 完成边界

提交代码不代表全部原生验收通过。任务保持 `in_progress`，不归档；KDE Wayland 截图与键鼠子集已通过，中文完整剪贴板恢复、其他原生桌面、多屏与授权边界仍待验收。第六轮真实恢复失败和原文本恢复边界保留记录。
