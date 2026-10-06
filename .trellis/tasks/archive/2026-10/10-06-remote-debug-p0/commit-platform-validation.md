# 平台修订与原生验收提交计划

分支：codex/remote-debug-p0；基线为已完成工作提交 01a5a54。以下是本轮新清单，与此前已确认的两笔提交分开。一次确认后依序提交，不 amend，不推送。所有路径相对于 /home/user/projects/remote-mcp。

## 1. refactor: 移除 macOS 平台支持

文件（17 项）：
- `.github/workflows/check.yml`
- `README.md`
- `README.zh-CN.md`
- `go.mod`
- `go.sum`
- `internal/execution/platform_unix.go`
- `internal/execution/terminal_unix_test.go`
- `internal/gui/platform_darwin.go`
- `internal/gui/platform_darwin_test.go`
- `internal/inspection/command.go`
- `internal/inspection/inspection_test.go`
- `internal/inspection/parsers.go`
- `internal/inspection/platform_darwin.go`
- `internal/transfer/publish_unix.go`
- `scripts/build.sh`
- `scripts/gui-smoke.py`
- `scripts/test_gui_smoke.py`

## 2. fix: 修正 Windows 文件身份与网络错误分类

文件（15 项）：
- `internal/fileops/fileops_test.go`
- `internal/fileops/manager.go`
- `internal/fileops/open_test.go`
- `internal/fileops/open_unix.go`
- `internal/fileops/open_unix_test.go`
- `internal/fileops/open_windows.go`
- `internal/fileops/patch.go`
- `internal/inspection/network.go`
- `internal/inspection/network_errors_linux.go`
- `internal/inspection/network_errors_test.go`
- `internal/inspection/network_errors_windows.go`
- `internal/logstream/manager.go`
- `internal/logstream/open_test.go`
- `internal/logstream/open_unix.go`
- `internal/logstream/open_windows.go`

## 3. test: 补齐 P0 原生验收与平台契约

文件（25 项）：
- `.trellis/spec/backend/directory-structure.md`
- `.trellis/spec/backend/gui.md`
- `.trellis/spec/backend/index.md`
- `.trellis/spec/backend/mcp-contracts.md`
- `.trellis/spec/backend/quality-guidelines.md`
- `.trellis/spec/backend/remote-debug.md`
- `.trellis/tasks/10-06-remote-debug-p0/check.jsonl`
- `.trellis/tasks/10-06-remote-debug-p0/commit-platform-validation.md`
- `.trellis/tasks/10-06-remote-debug-p0/design.md`
- `.trellis/tasks/10-06-remote-debug-p0/implement.jsonl`
- `.trellis/tasks/10-06-remote-debug-p0/implement.md`
- `.trellis/tasks/10-06-remote-debug-p0/prd.md`
- `.trellis/tasks/10-06-remote-debug-p0/research/check-native-validation.md`
- `.trellis/tasks/10-06-remote-debug-p0/research/check-platform-scope.md`
- `.trellis/tasks/10-06-remote-debug-p0/research/native-retrospective.md`
- `.trellis/tasks/10-06-remote-debug-p0/research/platform-contracts.md`
- `.trellis/tasks/10-06-remote-debug-p0/research/validation.md`
- `.trellis/tasks/10-06-remote-debug-p0/research/windows-native.md`
- `.trellis/tasks/10-06-remote-debug-p0/task.json`
- `scripts/native-smoke/main.go`
- `scripts/native-smoke/main_test.go`
- `scripts/p0-smoke.py`
- `scripts/test_p0_smoke.py`
- `scripts/test_windows_native_smoke.py`
- `scripts/windows-native-smoke.py`

## 4. chore(task): archive remote-debug-p0

前三笔完成后，当前 P0 任务验收已满足，归档到 `.trellis/tasks/archive/2026-10/10-06-remote-debug-p0/`，修正该任务内部上下文引用并清除当前任务指针。只归档当前 P0；旧 GUI、首版远程调试和规范引导任务保持原状态，不归档它们。

## 5. chore: record journal

将本轮范围、原生缺陷修复、验证结果和仍未运行的 Windows arm64/race、GUI 边界记录到 `.trellis/workspace/kkqy/journal-1.md` 及同目录索引，只包含本轮会话记录；关联前三笔工作提交，不混入用户文件。

## 排除与执行前条件

- `.opencode/package.json` 为用户会话前修改，哈希保持 03acb0f9a0c48470d1b64ff6c036c5788be9f27e186c0a556a0b1f6731548b80，始终排除。
- dist、缓存、原生部署临时目录和本机 JSON 报告不提交；旧 GUI task.json 哈希保持。
- Windows/amd64 九包 111 PASS/4 SKIP、最终旧/P0 × Token/匿名四轮及清理通过；Linux 修复版四轮、四组合无 CGO 构建、Python 48 项、Go 助手 4 项通过。最终全范围 Go/脚本复审记录为 research/check-platform-scope.md。
- 执行前核对 git dirty 与暂存集合，只提交各笔精确文件；暂存和提交失败时不扩大范围。第二笔完成后可将当前任务 commit 字段更新为该工作提交，再进入第三笔文档提交。
- 一次确认授权本清单三笔工作提交及两笔归档/日志提交；不授权推送、合并或触发远程 CI。

## 用户确认与执行记录

用户回复“确认”批准本清单全部五步；前两笔工作提交为 `d08dc74`、`22bd07c`。第三笔按上述精确清单提交后继续已授权的当前任务归档与会话记录，不推送。

## 归档完成记录

三笔工作提交为 `d08dc74`、`22bd07c`、`42aceb4`。当前 P0 状态已 completed，归档到 `.trellis/tasks/archive/2026-10/10-06-remote-debug-p0/`，内部上下文校验通过。按已确认清单提交归档并记录会话；旧 GUI、首版远程调试与规范引导任务保持原状态，用户文件保留，未推送。
