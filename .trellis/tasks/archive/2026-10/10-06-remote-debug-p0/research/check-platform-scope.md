# 平台范围修订复审

日期：2026-10-06。范围：本任务未提交的产品平台、依赖、构建、CI、README 与前轮 P0 验收入口改动；排除用户 `.opencode/package.json`。当前产品支持 Linux/Windows × amd64/arm64。

## 发现（已修复）

- `scripts/native-smoke/main.go`：有界捕获缓冲嵌入 `bytes.Buffer`，提升的 `ReadFrom` 使真实 `io.Copy` 可以绕过 `Write` 限额。本 reviewer 改为私有缓冲，增加 `main_test.go` 的真实复制越界与预算边界回归；定向 vet/test/race 与 Windows 两架构助手无 CGO 构建通过。
- 原生验证实现者按复审提醒修正助手启动 JSON 严格 UTF-8 及传输输出捕获时的硬限额。本 reviewer 核对后才允许重建助手。
- `scripts/windows-native-smoke.py`：原先只检查摘要 `ok/platform/arch`，缺轮摘要也可通过。本 reviewer 增加四轮唯一套件/鉴权组合、每轮成功及真实清理方式、协议和 ConPTY/GUI 标记校验；仅校验通过才标 `protocol_smoke=true`。新增缺轮/重复轮回归，离线实际 discover 为 48 项通过。

## 发现（未修复）

当前没有未修复的产品发现。原 GUI 任务的原生缺口不在本轮 P0 验收中消除；本轮没有操作 GUI。Windows/arm64、Windows race 和 Windows 上的既有 Python 冒烟脚本未实际执行，这些是明确保留的验证边界，不能写为通过。

## 核对范围

- Darwin GUI 与 inspection 专用源码/测试已删除；共有 Darwin ps/lsof 解析器、错误解析及 PTY 特殊会话分支已删除，`purego` 依赖与校验项清理完整。
- Linux 适配的构建约束为 `linux`；Linux PTY 会话号仍取组长 PID，Windows Toolhelp/IP Helper、ConPTY、Job Object 代码未被本轮平台收缩改写。
- 构建仅生成四组合、两个产品入口，共八个文件；旧 Darwin 产物已清理。CI 原生 runner 仅 Linux/Windows，保留旧功能/P0 × Token/匿名四轮入口及有限 P0 artifact；只在构建步骤关闭 CGO。
- 两份 README 当前支持范围、构建目录、巡检后端、终端与 GUI 权限说明一致。当前 backend spec、PRD 和设计与该范围一致；历史三平台验证事实保留，并明确由用户后续修订取代。
- P0 报告严格 UTF-8、有界且不含凭据、内容或路径；先清理旧报告，业务、资源关闭、服务停止与日志过滤全部通过后才发布。Windows `TerminateProcess` 清理标记与提前退出拒绝回归保留。

## 验证

环境：Go `go1.27.1-X:nodwarf5`，Linux/amd64，`GOCACHE=/tmp/remote-mcp-go-cache`。

| 检查 | 结果 |
| --- | --- |
| 所有产品 Go 文件 `gofmt -l`、`git diff --check` | 通过，无输出 |
| `go vet ./...` | 通过 |
| `go test -count=1 ./...` | 所有包通过 |
| `go test -race -count=1 ./...` | 所有包通过 |
| `bash scripts/build.sh` | Linux/Windows × amd64/arm64 两入口无 CGO 构建通过 |
| Python `test*smoke.py` | 38 项通过 |
| 最终 `dist/linux-amd64` 旧功能 Token/匿名 | 两轮实际二进制冒烟通过 |
| 最终 `dist/linux-amd64` P0 Token/匿名 | 两轮实际二进制冒烟通过，巡检、等待超时/退出、日志轮转/截断、哈希补丁及日志过滤通过 |

P0 报告：`/tmp/remote-mcp-platform-token.json`、`/tmp/remote-mcp-platform-anonymous.json`。两份均报告 `platform=linux`、`arch=amd64`、`service_shutdown=graceful`。报告位于本机临时目录，仅证明本次实际运行，未触发远程 CI。

用户文件与旧 GUI task 的 SHA-256 分别保持 `03acb0f9a0c48470d1b64ff6c036c5788be9f27e186c0a556a0b1f6731548b80`、`996dc39a81a261302c8daec44db8177a21c8937b03a6ec32c45cecd6b095dd1a`。

## 原生验收辅助脚本复审

新增 Go 助手已完成定向 vet/test/race，两个有界缓冲回归通过；Windows amd64/arm64 助手构建通过。协议响应有 2 MiB 限额且严格 UTF-8，启动 JSON 有 64 KiB 限额且严格 UTF-8，传输 stdout/stderr 捕获各有 64 KiB 硬限额。受管进程/终端、日志与转发在各业务阶段返回前完成关闭，失败会阻止报告；主服务只能在本轮业务资源关闭后终止，拒绝提前退出，报告准确标注 `terminate_process`。全部四轮及临时目录删除通过后才输出最终成功。

部署脚本冻结后已复审：只连接显式授权入口，禁代理/重定向；仅精确清理持有的资源 ID 与唯一目录；进程退出后仍读完双流 end_cursor；失败解码、业务、清理、原入口复核或报告发布时不发布成功。报告原子发布及旧报告删除、非法编码、尾部读取、原服务不可用、清理失败和缺轮/重复轮都有离线回归。包测试专用运行明确 `protocol_smoke=false`；四轮成功只有严格覆盖校验通过后才标 true。

以上早期全量检查对应新增助手及后续产品修复之前的代码，最终统一检查见下节，不把早期结果当作后续修改的最终证据。

## Windows 原生证据触发的产品修复复审

原生 `fileops` 身份冲突测试及 `inspection` TCP refused 测试保持严格断言并真实失败，代码实现者修复后交给本 reviewer 独立复审：

- 原 `Patch` 主快照来自 `f.Stat`，本来就是稳定的句柄身份；原冲突测试的 `before=os.Stat` 在 Windows 使用延迟路径身份，未准确模拟该生产快照。测试改用稳定平台快照，冲突断言不放宽。
- `fileops` 初始打开/发布前最后复核与 `logstream` 初始打开/轮询的路径身份也需要及时固定。Windows `statPath` 先筛除特殊类型，再以零访问权限及 `OPEN_REPARSE_POINT/BACKUP_SEMANTICS` 打开，使用 `f.Stat` 捕获 volume/file index，关闭句柄；Linux 仍使用原 `Lstat`。返回类型再次检查，不能跟随最终重解析点或把特殊文件当普通文件读取。两模块注入同内容/mtime 但不同身份的路径替换，仍须明确拒绝；fileops 文件与目录均覆盖。
- Windows TCP 分类使用 Winsock `WSAECONNREFUSED/WSAENETUNREACH/WSAEHOSTUNREACH/WSAETIMEDOUT`，Linux 使用原 POSIX errno；包装的 `net.OpError/os.SyscallError` 回归检查分类，原真实 refused 断言保留。
- 以上代码修订后 `fileops/logstream/inspection/server` 与 Go 助手的定向 vet、test、race 通过；四组合双入口无 CGO 构建重新通过。最终 Windows/amd64 服务 SHA-256 为 `77628837b808ec02e0969899249bdfde92b53dcf45793103de8a84898ad60320`，辅助命令为 `53ca5af1f3a03295afeb5366e1e40499ade5568cc98633ae8c869bf38d7bf852`。
- 最终修订产物的 Linux 旧功能/P0 × Token/匿名四轮冒烟全部通过；P0 有限报告已覆盖为最新结果，两份均 `graceful`。
- Windows 受影响包原生重验已通过：fileops 13 项通过/1 项跳过、inspection 13/1、logstream 13/1；正式报告另见 windows-native.md。独立助手只在预期失败且 structuredContent 缺失时解析 MCP 文本 JSON；文本有 64 KiB 限额且严格 UTF-8/JSON 对象校验，成功缺失 structuredContent 仍拒绝。该兼容与非法输入回归增加后最终 Go 助手共 4 项测试通过。
- 最新源码的 `gofmt -l` 与 `git diff --check` 无输出；用户文件与旧 GUI task 哈希再次核对保持原值。规范新增的 Windows 身份、WSA errno、有界 writer 与远程部署契约与实际实现一致。

## 最终统一结果

- 最终全部产品修复及新增助手源码：`go vet ./...`、`go test -count=1 ./...`、`go test -race -count=1 ./...` 均通过；包括所有产品包及 `scripts/native-smoke`。没有再放宽失败断言或跳过原生边界。
- 最终 Python `python3 -m unittest discover -s scripts -p 'test*smoke.py'`：48 项通过。新增 Go 助手独立 vet/race 与 Windows amd64/arm64 无 CGO 构建通过。
- 当前修复版四组合双入口构建及 Linux 旧功能/P0 × Token/匿名四轮均通过。源码此后仅新增助手协议解码/回归与文档，产品产物无需重复构建或冒烟。
- `/tmp/remote-mcp-windows-native-final-smoke.json` 已独立只读核验：Windows/amd64 四个唯一旧功能/P0 × Token/匿名组合均成功，服务清理均 `terminate_process`；`protocol_smoke/deployment_cleaned/existing_service_preserved/native_conpty` 均 true，`gui_exercised/python_smoke_executed` 均 false。
- 最终报告中的服务哈希与本机最终 dist 完全相同：`77628837b808ec02e0969899249bdfde92b53dcf45793103de8a84898ad60320`。独立重建助手哈希为 `3f273cdcdad6a59d0fc36dc974af619bc4c91e810c646084e78447d5d6ea41e0`，与 Windows 报告完全相同。
- Windows 九个实际包共 111 顶层 PASS、4 SKIP；修复后的 logstream 13 PASS 替代旧 12 PASS，不重复记账。SKIP 为 Unix mode 权限、Linux 专用 PID 测试、符号链接权限及默认未启用的大文件测试，不计入 PASS；真实 Windows PID/PPID/监听 PID 在助手另验。
- 两份 README 已最小同步真实 Windows 结果、远程 Go 助手用法和分批包参数，并保留 Windows Python/race、arm64、GUI 与未触发 CI 的边界。当前 spec、PRD、设计、实施计划及最终验证记录与产品范围和上述结果一致。

最终无未修复发现。未提交、未推送、未触发 CI；停止写入代码与文档。
