# 终端顺序输入误报 busy 修复

日期：2026-10-04。

## 现象与根因

用户提供的 CI 记录显示 `TestTerminalStateResizeInterrupt` 在连续输入时收到 `busy`。前一次 `Manager.Write` 已返回，但其工作 goroutine 原先先向有缓冲的 result 通道发布完成结果，再在 defer 中释放 inputBusy；接收方可能先恢复并立即发起下一次输入，看到尚未释放的占用。

这是完成通知与占用释放之间的逻辑竞态，互斥变量本身不存在未同步读写，因此竞态检测器不一定直接报告 data race。问题在平台共用的管理器中，不应通过延长真实 PTY 测试等待、重试 busy 或只修改 Unix 测试来掩盖。

## 修复与回归

- `internal/execution/manager.go`：底层 Write 返回后先释放 inputBusy，再发布结果。仍由底层写入 goroutine 持有占用；调用方取消或超时仅停止等待，不允许另一个输入与尚未结束的底层写入重叠。
- `internal/execution/terminal_write_test.go`：通过可控底层写入验证连续成功、部分写入失败后的下一次输入、真正并发时返回 busy、取消等待仍保持 busy，以及底层成功或失败结束后能继续输入。取消测试用通道控制进度，不依赖 sleep 或重试。
- 新增顺序输入回归在旧实现上真实失败：成功路径第 8732 次输入得到 busy；部分写入失败路径也得到 busy 而非 io_error。该复现来自修复前的一次 `go test -race -count=1 ./internal/execution -run '^TestTerminalWrite'`，不是人为插入延迟所得。
- 保留原始真实 Unix PTY 状态保持、尺寸调整、Ctrl+C 测试不变。

## 验证结果

本机 Linux/amd64，以下命令设置 `GOCACHE=/tmp/remote-mcp-go-cache`：

- `go test -race -count=30 ./internal/execution -run '^TestTerminal(Write|StateResizeInterrupt)'`：通过，10.549 秒。
- `go vet ./...`：通过。
- `go test -race -count=1 ./...`：通过，涵盖命令、配置、执行、端口转发、服务及传输模块。首次在受限沙箱执行时仅本机监听被拒，获自动审批后重跑全部通过；执行模块两次均通过。
- `bash scripts/build.sh`：Linux、macOS、Windows × amd64/arm64 六组合两个程序均构建成功，更新 dist 产物。
- 修改的 Go 文件已 gofmt；差异格式检查通过。

Windows/macOS 本轮未原生运行，仍需对应 CI 验证；交叉构建不代表原生终端验收完成。未提交或推送，保留无关 `.opencode/package.json` 修改及现有端口转发功能。
