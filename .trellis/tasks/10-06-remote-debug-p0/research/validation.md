# 验证记录

日期：2026-10-06；本机 Linux/amd64，Go 1.27.1-X:nodwarf5。构建、自动测试和对应平台原生运行分别记录。

当前范围以下方“最终 Linux/Windows 验收”节为准：用户已取消 macOS 支持，并提供 Windows 虚拟机完成原生运行。前文六组合和原生缺口保留为历史记录。

## 已完成局部验证
- fileops：test/race/vet 通过，中文 CRLF/空文件/末尾无换行、流式长行尾部匹配、预算、哈希与身份冲突、并发补丁、发布与写入失败保留原目标。
- inspection：test/race/vet 通过，真实父子 PID/root 筛选与 IPv4/IPv6 监听归属；DNS/TCP/TLS/HTTP 阶段、证书验证、header 上限、no proxy/redirect/body、并发/取消/关闭；六组无 CGO 测试包编译通过。Windows ABI 依据见 platform-contracts.md。
- execution/logstream：test/race/vet 通过，真实 PTY 延迟读取、尾部退出、通知、取消/关闭，日志追加、轮转路径重建窗口、截断、代际、句柄限额及空闲回收。
- server：独立 HTTP JSON-RPC、SDK typed 错误与大 partial 结果诊断、英文运行字符串及内容/Token 过滤；服务包 test/race/vet 通过。
- 实现阶段 `/tmp/remote-mcp-p0-integration-bin/remote-mcp` 实际无 CGO 二进制 P0 Token/匿名两模式通过，均 gui_exercised=false；最终 dist 构建后的验证另记。
- Python 离线回归：`python3 -m unittest discover -s scripts -p 'test_*.py'`，31 项通过。

## 最终全范围验证
- Trellis 最终复审：gofmt 无输出、`go vet ./...`、`go test ./...`、`go test -race ./...` 全部通过，GOCACHE=/tmp/remote-mcp-go-cache；详见 check-final.md。四组复审修复均已回归，三项权限/内核专项实际 PASS 无跳过。
- `GOCACHE=/tmp/remote-mcp-go-cache bash scripts/build.sh`：退出码 0；Linux/Windows/macOS × amd64/arm64，remote-mcp 和 remote-mcp-transfer 两个入口全部构建通过，CGO_ENABLED=0。
- 最终 dist/linux-amd64 实际产物：`scripts/smoke.py` 带 Token 与 `--no-token` 两轮退出码 0、ok=true；上传/执行/终端/下载及普通日志过滤和清理闭环通过。
- 最终 dist/linux-amd64 实际产物：`scripts/p0-smoke.py` 带 Token 与 `--no-token` 两轮退出码 0、ok=true，inspection/waiting_reads/log_rotation/hash_patch 全为 true；gui_exercised=false。
- Python 31 项离线回归通过；git diff --check、任务 JSONL validate 和用户文件/GUI task 哈希核对通过。
- 无新依赖、无真实 GUI 操作、无公网业务探测；官方 API 核对来源记录于 platform-contracts.md。

## 原生缺口
Windows/macOS 原生 PID/端口/终端/日志轮转/文件发布尚未执行；不能以解析 fixture 或交叉编译替代。GUI 原任务的未验收内容与状态保持。

## 保留与范围
`.opencode/package.json` 保留会话开始内容，其 SHA-256 为 03acb0f9a0c48470d1b64ff6c036c5788be9f27e186c0a556a0b1f6731548b80。GUI task.json 的 SHA-256 为 996dc39a81a261302c8daec44db8177a21c8937b03a6ec32c45cecd6b095dd1a，仍为 in_progress。无依赖变更、无新增 GUI；用户已确认两笔计划，本次不推送。

## 提交与会话收尾
2026-10-06 用户回复“是”确认 commit-plan.md；实现提交 `0337ba2`（44 个文件），契约及验收记录按第二笔计划提交（18 个文件）。未修改产品代码，无需重复已经通过的测试。新任务和原 GUI 任务保持 in_progress，后续需 Windows/macOS 原生验收；`.opencode/package.json` 保留并排除，未推送。

## 原生验收入口续接（2026-10-06）
- 在现有 `.github/workflows/check.yml` 原生 job 中补旧功能/P0 × Token/匿名四轮冒烟；原生产物构建步骤设置 CGO_ENABLED=0，保留竞态检测配置与独立六组合交叉构建。P0 有限 JSON 报告由 artifact 按 runner 平台保存。尚未推送或触发远程 CI，因此没有新的 Windows/macOS CI 运行结果。
- P0 实际检查新增等待超时、读取退出及正常终态、同文件截断和日志等待超时；退出后逐项关闭进程/终端，日志明确返回 closed=true。Windows TerminateProcess 清理仅接受停止前仍运行且返回码为 1，报告明确标为 terminate_process；不能作为服务优雅退出验收。
- 实现代理运行 `python3 -m unittest discover -s scripts -p 'test*smoke.py'`：38 项通过（原 31 项、新增 7 项），并通过 py_compile、英文 --help、git diff --check。
- 最新 `dist/linux-amd64` P0 真实二进制 Token/匿名两轮均退出码 0；inspection/waiting_reads/waiting_timeout/waiting_exit/log_rotation/log_truncation/hash_patch 全为 true，gui_exercised=false。报告 `/tmp/remote-debug-p0-linux-token.json`（432 字节）及 `/tmp/remote-debug-p0-linux-anonymous.json`（437 字节）已核对；服务 linux/amd64、Python 宿主 linux/x86_64、service_shutdown=graceful，报告没有凭据或用户内容。
- Go 产品代码未修改，上一节已通过的全量 vet/test/race 和六组合构建不重复执行；本轮 trellis-check 全范围复审通过，无需自修，38 项 Python 回归、语法/帮助/diff 检查及已有 PyYAML 的工作流结构断言均通过，结果见 check-native-validation.md。未配置独立 Python linter，无 actionlint，不宣称已运行远程 CI。
- 用户确认存在 Windows 或 macOS 环境；具体系统、架构、连接方式尚待提供，原生运行仍未验证。用户文件及旧 GUI task 哈希与上节一致；本轮新增改动未提交，不推送，任务保持 in_progress。

## 最终 Linux/Windows 验收（2026-10-06）
- 用户明确取消全产品 macOS 支持：Darwin GUI/巡检源码、对应测试、PS/lsof 专用解析与 PTY 分支、purego 依赖已删除；Linux 平台约束、README、当前 spec、构建和 CI 收敛到 Windows/Linux。旧 Darwin 生成目录已清理，历史记录及 Trellis 工具自身跨平台实现不改写。
- Windows/amd64 原生发现并修复文件路径身份快照、Winsock errno 分类；生产 Patch 原主 before 已为 f.Stat 稳定身份，旧失败测试取样与真实打开前/最终路径窗口分别说明，冲突/refused 断言保留。logstream 同根因快照也修复并原生重验，详见 native-retrospective.md。
- 修复版 `bash scripts/build.sh`：Windows/Linux × amd64/arm64，两个入口共八个无 CGO 产物通过。最终 Windows 服务 SHA-256 为 `77628837b808ec02e0969899249bdfde92b53dcf45793103de8a84898ad60320`；最终原生助手为 `3f273cdcdad6a59d0fc36dc974af619bc4c91e810c646084e78447d5d6ea41e0`。
- 最新修复产物 `dist/linux-amd64` 旧功能/P0 × Token/匿名四轮全部通过；有限 P0 报告 `/tmp/remote-mcp-platform-token.json`、`/tmp/remote-mcp-platform-anonymous.json` 均标 linux/amd64、graceful，GUI 未操作。
- Windows 9 个原生包合计 111 项顶层 PASS、4 项 SKIP，ConPTY EOF/重定向/resize/Ctrl+C/Job 回收无跳过通过；三个修改包最终重验的结果替代其旧结果。跳过项及各包计数详见 windows-native.md，不能把 SKIP 计为 PASS。
- Windows 最终独立回环服务旧功能/P0 × Token/匿名四轮全部通过：真实 PID/PPID 与监听 PID、网络阶段、中文 CRLF 补丁及冲突、等待超时/输出/退出、日志追加/轮转/截断、普通日志过滤。`/tmp/remote-mcp-windows-native-final-smoke.json` 已经独立核验四个唯一套件/鉴权组合及实际产物哈希。
- Windows 报告明确 service_shutdown=terminate_process；业务资源先结束/关闭，每轮服务拒绝提前退出，再接受强制终止退出码 1。部署清理与原用户 MCP 入口再次初始化/工具发现均通过，未替换或停止其主服务。VM 无 Python，Python 部署脚本在 Linux 执行；不声称既有 Python 冒烟脚本已在 Windows 运行。
- 最终冻结源码（含产品修复与新 Go 助手）全量 `go vet ./...`、`go test -count=1 ./...`、`go test -race -count=1 ./...` 通过。Python 最终 48 项回归通过；Go 助手 4 项真实复制限额/错误文本兼容回归及 vet/race 通过，Windows 两架构助手构建通过。最终全范围复审的确切命令与结果见 check-platform-scope.md。
- Windows arm64 只有构建证据；GUI 原任务仍有其独立原生缺口，本轮不操作 GUI。远程 CI 未推送/触发，无 GitHub runner 实际结果。用户文件和旧 GUI task 哈希保持，本轮具体提交计划待一次确认，暂不归档。

## 已确认提交与收尾

用户回复“确认”批准 commit-platform-validation.md。前两笔工作提交 `d08dc74`、`22bd07c` 已执行，第三笔将验收脚本、契约和记录按25文件清单提交；随后仅归档当前P0、记录开发会话，不推送。提交前源码未再改动，无需重复已通过的测试；用户文件与旧GUI task.json哈希再次保持。
