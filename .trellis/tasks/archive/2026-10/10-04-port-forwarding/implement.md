# TCP 端口转发实施计划

## 实现前门槛

- [x] 用户确认转发方向、TCP 范围和管理生命周期。
- [x] 需求收敛，完成 PRD、技术设计及上下文清单。
- [x] 向用户展示正式方案，并获得其后续明确实现批准。
- [x] 执行 task.py start，加载执行阶段上下文。

## 实施顺序

1. 使用 trellis-implement 子代理实现 `internal/forwarding`：输入输出、配置、幂等创建、受限连接池、双向复制、半关闭、列表状态及完整退出清理。
2. 接入 `internal/config` 与 `internal/server`；修改初始化失败回滚、关闭路径和相关配置测试；注册四个 MCP 工具。
3. 增加真实 TCP 测试，覆盖二进制/大流量、半关闭、并发、端口占用、目标失败与恢复、超限、幂等及记录清理、停止与拨号竞争。
4. 增加独立 JSON-RPC 集成测试，覆盖匿名及 Token 模式的发现、创建、查询、列表、停止、错误标记及断连后资源保持。
5. 扩展实际二进制 smoke 验证转发请求响应与停止后的端口释放；补充中文使用说明和配置参数。
6. 使用 trellis-check 子代理完成规范检查和下列质量门禁；主会话核对结果、更新后端规范，按工作流收尾。

## 验证命令

- gofmt：仅格式化本次修改的 Go 文件。
- `go vet ./...`
- `go test ./...`
- `go test -race ./...`
- `bash scripts/build.sh`
- `python3 scripts/smoke.py --bin-dir dist/linux-amd64`
- `python3 scripts/smoke.py --bin-dir dist/linux-amd64 --no-token`

各项通过后不无依据重复运行。报告分别记录本机运行、六组合构建和其他系统原生验证状态。

## 风险与回滚点

- 重点复核连接登记与 Stop/Close 的竞争、TCP 半关闭、拨号取消、幂等创建和资源限额，防止端口/句柄/goroutine 泄漏。
- 配置结构和 App 初始化/关闭路径涉及既有功能，必须通过全量回归。
- 现有 `.opencode/package.json` 修改不属于本任务，不覆盖或提交。
- 转发规则不落盘，回滚不涉及数据迁移；退出或版本回滚会关闭所有规则。

## 执行结果

步骤 1—6 已完成，验证与检查修复记录见 validation.md。用户已确认提交计划，进入提交与归档阶段。
