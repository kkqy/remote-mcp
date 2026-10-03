# OpenCode 启动配置输出变更

日期：2026-10-04。

## 需求与依据

用户发现启动输出的 `mcpServers/type=http` 无法直接用于 OpenCode，在确认此前仅修复本机配置后明确要求修改程序输出。主会话已核对本机 OpenCode 1.18.34 和[官方远程 MCP 文档](https://opencode.ai/docs/mcp-servers/#remote)。本次契约为 OpenCode 1.x 的顶层 `mcp`，不是其他版本的嵌套格式。

每个地址输出独立 JSON：顶层 `$schema: https://opencode.ai/config.json`，`mcp.remote-mcp` 包含 `type: remote`、`url`、`enabled: true`、`oauth: false`。后者必须真实序列化，即使值为 false 也不能省略。服务继续使用 Streamable HTTP；这里的 remote 是客户端配置类型，不修改服务协议。

保留多 IP、稳定排序、IPv6 方括号、TLS、实际监听端口和 Token 转义。Token 非空时包含 `headers.Authorization`，匿名时完全省略 headers。诊断写 stderr，独立 JSON 写 stdout。其他客户端按其格式转换同一 URL 与可选鉴权值。

## 修改范围

- `internal/server/startup.go`：输出结构及中文配置提示。
- `internal/server/startup_test.go`：用独立 map 解码覆盖外部字段，避免生产结构体掩盖序列化错误；原多 IP、TLS/IPv6、Token 和匿名断言继续保留。
- `scripts/smoke.py`：消费 `mcp`，检查 schema、remote、enabled 和显式 false。
- README、PRD、设计、实施计划：统一新格式与合并配置说明；后端 spec 由主会话同步。

不改依赖、MCP 工具、鉴权行为、用户现有 OpenCode 配置或无关 `.opencode/package.json`。不增加双格式输出或格式选项。

## 验证结果

- 回归先行：在旧实现运行两个启动输出测试，因旧 `mcpServers` 字段失败；修改实现后通过。
- `gofmt -l internal/server/startup.go internal/server/startup_test.go`：无输出。
- `GOCACHE=/tmp/remote-mcp-go-cache go vet ./...`：通过。
- `GOCACHE=/tmp/remote-mcp-go-cache go test -race -count=1 ./internal/server`：通过，1.848 秒。首次沙箱执行因禁止监听临时端口失败，经自动审批后在允许本机监听的环境重跑通过。
- `python3 -B -m unittest discover -s scripts -p test_smoke.py`：3 项通过。
- `GOCACHE=/tmp/remote-mcp-go-cache bash scripts/build.sh`：六种系统/架构组合、两个程序共 12 个产物构建成功，dist 已更新。
- `python3 -B scripts/smoke.py --bin-dir dist/linux-amd64`：带 Token 的实际二进制上传、独立 JSON-RPC 执行和下载通过。
- `python3 -B scripts/smoke.py --bin-dir dist/linux-amd64 --no-token`：匿名实际二进制闭环通过。
- 两种模式下载产物均为 24 字节，SHA-256 为 `061df3cf2b104ef68a44dacdae1f4d2cf6c50b9348a5a20da8620114eed9e856`；普通日志无 Token。
- `git diff --check`：通过。

实际运行环境为 Linux x86_64；Windows/macOS 仅交叉构建，原生验收仍待环境。本轮实施没有运行用户配置下的 `opencode mcp list`，避免连接其他 MCP；二进制冒烟仅连接临时本机服务，不能据此宣称 OpenCode 品牌客户端的端到端验证已经完成。独立检查已通过，未发现新增问题，见 opencode-output-review.md。
