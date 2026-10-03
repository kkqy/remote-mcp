# Windows ConPTY 修复独立审查

日期：2026-10-04。

## 结论

未发现需要进一步修改的代码缺陷。修复位于真实 Windows 进程启动路径，普通命令路径和 `CREATE_SUSPENDED → AssignProcessToJobObject → ResumeThread` 顺序保持不变。

终端分支的 `StartupInfoEx` 从零值初始化，新增 `STARTF_USESTDHANDLES` 后三个标准句柄仍为 NULL；HPCON 扩展属性保留。这与锁定依赖 conpty v0.2.0 的 Spawn 实现及 [微软维护者的说明](https://github.com/microsoft/terminal/discussions/15814)一致。用户日志中的宿主输出泄漏、终端输出仅剩 VT 序列及 shell 自然退出，与宿主重定向句柄被复制的机制吻合。修复有效性仍须 Windows 原生测试确认。

## 回归测试审查

- 独立宿主的 stdin 为立即 EOF 的管道，stdout/stderr 分别覆盖管道和文件重定向，能明确构造原故障环境。
- `-test.run` 精确选择原交互与空闲清理测试，不包含包装测试和进程辅助测试，不会递归或因辅助测试环境变量跳入其他执行路径。
- 复用原有 `STATE:kept`、`AFTER:kept`、Ctrl+C、尺寸调整及 `idle_timeout` 断言，没有放宽断言或延长原等待；包装层额外检查终端标记未泄漏到宿主输出。
- 子进程有 25 秒测试超时及 30 秒宿主 context 上限；正常及断言失败路径通过管理器清理 Job/ConPTY。宿主强制结束时 Job 的 KILL_ON_JOB_CLOSE 仍生效，避免 shell 持续持有宿主输出管道。
- 管道分支使用 CombinedOutput，文件分支在进程结束并关闭文件后读取；没有新增共享可变状态或并发写入测试缓冲区。
- 后端契约、质量规范与 README 已同步 Windows 重定向约束和原生验证边界。

## 验证

本检查代理实际执行：

- 修改的两个 Go 文件 `gofmt -l` 无输出。
- `GOCACHE=/tmp/remote-mcp-go-cache GOOS=windows GOARCH=amd64 go vet ./internal/execution` 通过。
- `GOCACHE=/tmp/remote-mcp-go-cache GOOS=windows GOARCH=amd64 go test -c -o /tmp/remote-mcp-review-windows-amd64.test.exe ./internal/execution` 通过。
- `GOCACHE=/tmp/remote-mcp-go-cache go test -race -count=1 ./internal/execution` 通过（3.207 秒）。

复用实施记录中的全项目 go vet、Windows arm64 静态检查和测试编译、六组合 12 个二进制构建结果；未重复与 Windows 改动无关的文件传输及 HTTP 运行验收。

没有 Windows 原生环境，因此不把测试编译或 Linux 通过记为 Windows CI 已通过；仍需实际运行 Windows `go test -race -count=1 ./...`。本次没有提交、建立分支或修改无关 `.opencode/package.json`。
