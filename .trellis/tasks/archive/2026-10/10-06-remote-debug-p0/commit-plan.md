# 提交计划

分支：codex/remote-debug-p0；基线：main（9a6336b）。用户确认后按顺序执行，不 amend，不推送。所有路径相对于 `/home/user/projects/remote-mcp`。

## 1. feat: 增加远程调试三项 P0 能力

文件：
- `README.md`
- `README.zh-CN.md`
- `internal/execution/buffer.go`
- `internal/execution/manager.go`
- `internal/execution/manager_test.go`
- `internal/execution/read_wait_test.go`
- `internal/execution/terminal_unix_test.go`
- `internal/execution/tools.go`
- `internal/execution/types.go`
- `internal/fileops/fileops_test.go`
- `internal/fileops/manager.go`
- `internal/fileops/open_unix.go`
- `internal/fileops/open_unix_test.go`
- `internal/fileops/open_windows.go`
- `internal/fileops/patch.go`
- `internal/fileops/search.go`
- `internal/fileops/text.go`
- `internal/fileops/tools.go`
- `internal/fileops/tools_test.go`
- `internal/fileops/types.go`
- `internal/inspection/command.go`
- `internal/inspection/inspection_test.go`
- `internal/inspection/manager.go`
- `internal/inspection/network.go`
- `internal/inspection/parsers.go`
- `internal/inspection/platform_darwin.go`
- `internal/inspection/platform_linux.go`
- `internal/inspection/platform_linux_test.go`
- `internal/inspection/platform_windows.go`
- `internal/inspection/tools.go`
- `internal/inspection/tools_test.go`
- `internal/inspection/types.go`
- `internal/logstream/manager.go`
- `internal/logstream/manager_test.go`
- `internal/logstream/open_unix.go`
- `internal/logstream/open_windows.go`
- `internal/logstream/tools.go`
- `internal/logstream/types.go`
- `internal/server/p0_test.go`
- `internal/server/server.go`
- `internal/server/server_test.go`
- `internal/server/tool_logging.go`
- `internal/server/tool_logging_test.go`
- `scripts/p0-smoke.py`

## 2. docs: 固化远程调试 P0 契约与验收

文件：
- `.trellis/spec/backend/directory-structure.md`
- `.trellis/spec/backend/index.md`
- `.trellis/spec/backend/logging-guidelines.md`
- `.trellis/spec/backend/mcp-contracts.md`
- `.trellis/spec/backend/quality-guidelines.md`
- `.trellis/spec/backend/remote-debug.md`
- `.trellis/tasks/10-06-remote-debug-p0/check.jsonl`
- `.trellis/tasks/10-06-remote-debug-p0/commit-plan.md`
- `.trellis/tasks/10-06-remote-debug-p0/design.md`
- `.trellis/tasks/10-06-remote-debug-p0/implement.jsonl`
- `.trellis/tasks/10-06-remote-debug-p0/implement.md`
- `.trellis/tasks/10-06-remote-debug-p0/prd.md`
- `.trellis/tasks/10-06-remote-debug-p0/research/check-final.md`
- `.trellis/tasks/10-06-remote-debug-p0/research/check-modules.md`
- `.trellis/tasks/10-06-remote-debug-p0/research/inspection.md`
- `.trellis/tasks/10-06-remote-debug-p0/research/platform-contracts.md`
- `.trellis/tasks/10-06-remote-debug-p0/research/validation.md`
- `.trellis/tasks/10-06-remote-debug-p0/task.json`

## 排除

- `.opencode/package.json`：用户会话前未提交修改，始终排除且保留。
- 旧 GUI 和远程调试任务目录、go.mod/go.sum、构建产物 dist 与缓存：不属于本次提交。
- 当前任务保留 in_progress，Windows/macOS 原生验收尚未完成，不执行归档。

## 验证与执行前条件

Trellis 全范围复审、全量 test/vet/race、六组两个入口无 CGO 构建、dist 旧/P0 Token/匿名四轮冒烟与 Python 31 项回归全部通过，详见 research/validation.md。执行前再次核对 dirty 状态，只暂存以上文件；确认此计划仅授权以上提交，不包含推送。

## 用户确认与执行记录
2026-10-06 用户回复“是”确认以上两笔计划。实现提交 `0337ba2` 已生成；文档按第二笔清单提交。保持任务进行中，不归档、不推送。
