# Windows 原生验收

日期：2026-10-06。用户提供 Windows 虚拟机的匿名 MCP 入口，并明确撤销 macOS 支持。本记录只描述实际执行证据；Windows arm64 交叉构建不算原生通过。

## 环境与隔离方式

- 独立 JSON-RPC initialize/tools-list 成功，已有入口注册 40 个工具；environment_inspect 确认 Windows/amd64。
- PowerShell 和 Go 可用，Python 与 py launcher 缺失。因此没有在 Windows 上运行现有 Python 冒烟脚本；新增 `scripts/windows-native-smoke.py` 在 Linux 侧只负责 MCP 部署与清理，`scripts/native-smoke/main.go` 在 VM 本机执行独立协议验收。
- 仅连接用户提供的 MCP 入口；禁止 HTTP 代理和重定向。每轮部署使用随机独立临时目录，上传哈希校验后的验收 ZIP；本机 Windows Go 测试二进制以及两个产品程序在该目录中运行。
- 服务冒烟使用 VM 本机独立回环监听和临时虚构 Token。主入口、主服务进程和持久配置不替换、不停止。验收只调用本任务创建的 process、terminal、log、transfer 和 forwarding 资源，不调用 GUI 工具、不运行 GUI 测试包。
- 部署脚本严格 UTF-8、有界双流读取，退出后继续读取到各自 end_cursor；finally 停止本任务进程并核对 exited，仅删除本次持有的精确临时目录。成功报告在清理和原入口再次 initialize/tools-list 确认后发布。

## 已执行的包测试

以下计数是顶层测试，不重复计算子测试。未完成或失败的包不计入通过总数。

| 包 | PASS | SKIP | 状态 |
| --- | ---: | ---: | --- |
| internal/config | 6 | 0 | 原生通过 |
| internal/execution | 20 | 0 | 原生通过；含 ConPTY 宿主 stdin EOF、管道/文件重定向、状态、resize、Ctrl+C、idle_timeout 和 Job 后代回收 |
| internal/forwarding | 5 | 0 | 原生通过 |
| internal/logstream | 13 | 1 | 修复后原生重验通过；新同 metadata 替换回归 PASS，符号链接创建权限不足场景跳过 |
| internal/server | 25 | 0 | 原生通过；生产源码一并打包供语言检查，不用空目录绕过源码检查 |
| internal/transfer | 7 | 0 | 原生通过 |
| cmd/remote-mcp-transfer | 9 | 1 | 原生通过；默认未启用 257 MiB 双向大文件 TestLargeStreaming |
| internal/fileops | 13 | 1 | 身份快照修复后重验通过，新文件/目录同 metadata 替换回归 PASS；Unix mode 权限场景 SKIP |
| internal/inspection | 13 | 1 | Winsock 分类修复后重验通过；TestRealProcessAndListener 专为 Linux，Windows PID/PPID/监听 PID 由独立助手实际验证 |

最终 9 个包共 111 顶层 PASS、4 SKIP。logstream 修复后 13 PASS 替代修复前的 12 PASS，不重复计数。config/execution/forwarding 在同次部署后续发现失败前已有原生 PASS 输出，但当次整体没有发布成功报告；本表据实际测试输出记录单包结果。后四个独立包先用 `--package-only` 执行，报告明确 `protocol_smoke=false`；修复后的 fileops/inspection/logstream 原生重跑全部通过。

## 原生发现与修复要求

1. `fileops.TestRevalidationDetectsIdentityAndHashChanges` 在同内容、同大小、恢复 mtime 的文件替换后未返回 conflict。Windows `os.Stat/Lstat` 的 FileInfo 身份可能延迟到 SameFile 时按路径读取；在句柄身份冻结前检查目标会遗漏替换。原生产 Patch 主 before 已来自 f.Stat，身份本来稳定；旧测试的 os.Stat 取样未准确模拟该快照，实际修复的是打开前、最终路径复核及日志路径取样窗口。已补立即捕获的零访问、无跟随句柄身份和 Windows 文件/目录替换回归，原始严格 conflict 断言保留并原生通过；日志打开和轮询亦同步冻结身份。
2. `inspection.TestProbeTCPRefusalAndOverallTimeout` 对真实已关闭回环端口返回 `tcp_failed`，但 reason 为 `connection_error`。Windows WSAECONNREFUSED 与 Unix errno 不同，已按 Windows WSA 常量修复 refused/unreachable/timeout 分类，保留真实拒绝连接的 refused 断言，原生通过。
3. 首次部署的 PowerShell 诊断采用系统代码页，严格 UTF-8 解码拒绝该诊断；尚未执行包测试。部署端统一显式 Console/OutputEncoding UTF-8 后重试成功，Go 产品的严格 UTF-8 断言保持。首次创建的目录清理已确认完成，未发布成功报告。

## 最终真实服务验收

最终产品 SHA-256：`77628837b808ec02e0969899249bdfde92b53dcf45793103de8a84898ad60320`；独立助手 SHA-256：`3f273cdcdad6a59d0fc36dc974af619bc4c91e810c646084e78447d5d6ea41e0`。均对应本轮最终上传字节，部署脚本对 ZIP 内二进制计算哈希，避免事后读取已变化的本机产物。

| 套件 | Token | 匿名 | 原生结果 |
| --- | --- | --- | --- |
| 旧功能 | PASS | PASS | 辅助命令上传/哈希、PowerShell 执行、下载/原字节校验、TCP 转发二进制往返 |
| P0 | PASS | PASS | UTF-8/CRLF/末尾无换行、读搜与哈希补丁/冲突保护；process 与真实 ConPTY 默认立即/超时/输出/退出；日志追加/轮转/同文件截断/超时；真实父子 PID 和监听 PID；DNS、HTTP 204、TLS 失败与 TCP 失败阶段保留 |

- Token 服务拒绝无凭据请求；匿名启动 JSON 完全省略 headers，四轮均独立初始化、发现 40 个工具。
- ConPTY 由本机 Windows Go 程序启动子服务，宿主 stdin 为 EOF、stdout/stderr 捕获；业务终端输出只通过 MCP 读取。额外 execution 原生包验证保持 shell 状态、resize、Ctrl+C 和 Job 后代回收。
- 每轮先逐项结束/关闭受管进程、终端、日志、转发，再终止临时 Windows 服务。Windows Kill 为 TerminateProcess，退出码为 1，报告为 `service_shutdown=terminate_process`，不宣称服务优雅关闭。提前退出依旧失败。
- 日志严格 UTF-8，确认没有临时路径、Token、中文文件/日志内容、查询、探测 URL 和等待输出；P0 失败日志包含 conflict/tls_failed/tcp_failed 及具体原因。
- 最终部署目录已精确删除；原主入口再次 initialize/tools-list 成功且工具集合不变。仅此时发布四轮有限 UTF-8 JSON 成功报告，`protocol_smoke=true`、`deployment_cleaned=true`、`existing_service_preserved=true`、`gui_exercised=false`。

有限最终报告：`/tmp/remote-mcp-windows-native-final-smoke.json`。可复用执行方式：

```bash
python3 scripts/windows-native-smoke.py --url '<用户已授权的 Windows MCP 入口>' \
  --bin-dir dist/windows-amd64 --report-file /tmp/windows-native-smoke.json
```

同一脚本默认同时执行固定白名单的 9 个原生 Go 测试包。`--package <白名单包>` 支持受影响包重验；`--package-only` 报告没有协议冒烟，`--skip-package-tests` 只运行四轮真实服务。报告验证恰好 legacy/P0 × Token/匿名四个唯一组合，全部成功且原生 ConPTY=true、GUI=false；缺轮、重复轮、清理/报告失败均不得发布成功。

首次 Go 助手未兼容预期 file_patch conflict 的 SDK 文本 JSON 错误，抛出 Missing object result；这属于验收助手问题，不是产品失败。最终仅在 expected failure 时严格解析有界 UTF-8 错误文本 JSON，对成功仍要求 structuredContent，并补对应错误/非法 UTF-8/超限/错误状态回归。最终助手本地 4 项 Go 回归及 race 通过，薄部署脚本 10 项 Python 离线回归通过。

## 保留的验证边界

- Windows/amd64 原生包测试及四轮实际服务通过；Windows race 未在 VM 执行，不能把预编译 CGO=0 测试程序称为原生 race。
- Windows arm64 只有交叉构建；没有 arm64 原生环境。
- 现有 Python 冒烟脚本没有在 Windows 执行，VM 无 Python；独立 stdlib Go 助手提供上述实际协议闭环。Linux 现有四轮脚本验证由主代理单独记账。
- GUI 未操作，原 GUI 任务的验收缺口继续保留。macOS 支持由用户明确撤销；移除支持不等于原生验收通过。
