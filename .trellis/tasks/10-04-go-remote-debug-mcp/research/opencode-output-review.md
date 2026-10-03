# OpenCode 启动配置独立检查

日期：2026-10-04。

## 检查结果

本轮未发现需修复的代码缺陷。检查范围为启动 JSON 结构、对应测试、实际二进制验证脚本及使用说明；没有修改产品代码。

- `ClientConfig` 序列化为顶层 `$schema` 与 `mcp`；服务固定为 `remote-mcp`，`type` 为 `remote`，`enabled` 为 true，`oauth` 为 false。布尔字段未使用 omitempty，因此 false 不会被省略。
- 非空 Token 才生成 `headers.Authorization`，匿名模式省略整个 headers；JSON 编码器继续正确处理 Token 中的引号和反斜线。
- 逐 IP 完整对象、地址过滤与排序、真实端口、IPv6 URL 和 HTTPS 生成路径未改变。诊断仍写入 stderr，JSON 写入 stdout。
- 启动契约测试改为独立 map 解码，明确检查新字段和值、旧 mcpServers 缺失、匿名 headers 缺失，并保留多 IP、TLS/IPv6 和凭据转义覆盖。
- 已检查所有 ClientConfig 使用点及启动输出消费者；动态端口和 TLS 集成测试仍可使用更新后的结构，scripts/smoke.py 已使用新字段并检查显式 oauth=false。README 与后端 MCP 契约已同步。

## 验证

- 独立执行 `gofmt -l internal/server/startup.go internal/server/startup_test.go`：通过，无输出。
- 独立执行 `git diff --check`：通过。
- 独立执行 `GOCACHE=/tmp/remote-mcp-go-cache go vet ./internal/server`：通过，包含本包类型检查。
- 复用本轮实施记录：全项目 go vet、server 包 race 测试、Python 3 项测试、六组合 12 个程序构建，以及带 Token/匿名两个实际二进制闭环均通过。依据见 `opencode-output.md`。本轮无代码修复及新增覆盖缺口，因此未重复整套检查。

## 保留边界

本次检查没有运行用户全局 `opencode mcp list` 或连接其他 MCP；不据此声称 OpenCode 客户端端到端验收完成。Windows/macOS 原生运行验收仍待环境，这属于原有验收边界，不是本次输出格式变更新增缺陷。

无关 `.opencode/package.json` 改动保持原样；未提交或创建分支。
