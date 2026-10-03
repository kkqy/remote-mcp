# Windows ConPTY 标准句柄修复

日期：2026-10-04。

## 现象与根因

用户提供 Windows 10.0.26100.33438 的 GitHub Actions 竞态测试失败：`TestWindowsTerminalStateResizeInterrupt` 的 `STATE:kept` 没有进入终端输出，只有 VT 初始化/结束序列；cmd 的欢迎信息和提示符直接出现在 CI 标准输出。`TestWindowsTerminalIdleCleanup` 中 cmd 约 20 毫秒自然退出，没有等待空闲回收。

`platform_windows.go` 的终端分支设置了伪控制台属性及扩展启动结构，但没有设置 `STARTF_USESTDHANDLES`。Windows 在这种情况下仍可能把宿主被重定向的标准句柄复制给子进程，即使 `CreateProcess` 的 `bInheritHandles` 为 false。于是 cmd 使用宿主输出及已到 EOF 的输入，正好解释两个失败。现有管道传参顺序、HPCON 属性值及结构尺寸与官方用法一致，不需要更改。

证据：

- [Microsoft Terminal 官方讨论 #15814](https://github.com/microsoft/terminal/discussions/15814)：维护者说明应设置该标志并保持三个标准句柄为 NULL，防止复制宿主重定向句柄；原报告者确认这种修正有效。
- [微软伪控制台创建说明](https://learn.microsoft.com/en-us/windows/console/creating-a-pseudoconsole-session)：HPCON 通过扩展属性传入，父端负责两个通信管道和输出排空。
- 本地锁定依赖 `github.com/charmbracelet/x/conpty@v0.2.0/conpty_windows.go` 的 `Spawn` 也设置 `STARTF_USESTDHANDLES`，标准句柄维持零值，且禁止普通句柄继承。

## 修改边界

- `internal/execution/platform_windows.go`：仅在终端启动分支显式设置 `STARTF_USESTDHANDLES`，保持三个标准句柄为零。保留 `CREATE_SUSPENDED → AssignProcessToJobObject → ResumeThread` 顺序及原有输出排空/关闭机制。
- `internal/execution/terminal_windows_test.go`：新增 `TestWindowsTerminalWithRedirectedHost`，独立启动测试宿主，输入设置为空管道，输出分别重定向到管道和文件。在宿主内复用真实会话状态、尺寸、Ctrl+C、关闭以及空闲清理测试；宿主外检查测试退出结果和终端文本没有泄漏。测试有独立超时且不会递归调用自身。
- `README.md`：记录修复和原生验证边界。

本轮不修改协议、业务接口、依赖版本或普通命令启动路径；不通过延时、跳过原有用例或放宽断言掩盖问题。

## 防止重复发生

根因类型为跨层启动契约遗漏：自定义进程启动器为了保证 Job 绑定顺序，没有复用依赖的 `Spawn`，也遗漏了其中标准句柄的初始化约定；隐含假设是“禁止继承即可阻止所有父标准句柄复制”。Go 交叉编译只能检查类型、调用签名和链接，无法检查 Windows 内核对启动结构的实际语义，因此没有发现。原生 CI 的宿主重定向触发了缺陷。新增回归显式构造这种环境，不再依赖测试由终端直接运行还是被 CI 捕获；规范同步记录 HPCON、零标准句柄及 Job 恢复顺序的联合约束。

## 验证

- `gofmt` 已格式化修改的 Go 文件。
- Linux：`GOCACHE=/tmp/remote-mcp-go-cache go test -race -count=1 ./internal/execution` 通过（3.187 秒）。
- Linux：`GOCACHE=/tmp/remote-mcp-go-cache go vet ./...` 通过。
- Windows amd64/arm64：`go vet ./internal/execution` 及 `go test -c ./internal/execution` 交叉检查/测试编译均通过，测试程序位于 `/tmp/remote-mcp-execution-windows-{amd64,arm64}.test.exe`。
- `GOCACHE=/tmp/remote-mcp-go-cache bash scripts/build.sh` 通过，六种系统/架构组合共 12 个 `dist/` 二进制已更新。

本机没有 Windows 原生运行环境，因此没有声称新回归已实际经历失败/通过，也不能将交叉编译视作修复已通过 Windows 验收。用户给出的旧版 CI 是真实失败证据；修复后仍须运行 Windows `go test -race -count=1 ./...`，现有 CI 会自动包含新增回归。
