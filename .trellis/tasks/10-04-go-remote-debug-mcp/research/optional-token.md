# 可选 Token 变更记录

## 变更边界

用户追加要求“token不是必须的”。原行为在共享配置读取时拒绝空 Token；新行为允许未设置或空环境 Token 直接匿名启动，同时保留非空 Token 的逐请求鉴权。

- internal/config：Token 读取及配置校验；显式凭据文件为空、不可读、权限或内容无效时仍失败，文件首尾去空白行为不变。显式空文件路径也失败。
- internal/server：有 Token 才检查 Authorization；Origin、TLS、请求大小及应用资源限额不变。空 Token 不应触发所有工具名称的脱敏。启动日志说明鉴权状态，匿名 JSON 完全省略 headers。
- cmd/remote-mcp-transfer：匿名不发送 Authorization，配置凭据时照旧发送；复用服务配置的凭据读取规则。
- 回归测试：空环境、显式错误文件、真实匿名启动/独立 RPC 初始化及工具调用、普通日志工具名、多 IP JSON、真实匿名辅助命令双向分块传输且检查 HTTP 头。已有带 Token 测试继续覆盖缺失/错误返回 401、凭据发送及 TLS/重定向。
- README、prd、design、implement：说明可选鉴权，首先展示无 Token 启动。scripts/smoke.py 增加 --no-token，实际二进制验证共用原上传—执行—下载流程。

不修改文件、进程及终端生命周期，不变更依赖，不扩展前端或公网部署能力，不提交或创建分支。主会话负责更新 spec 和 task.json。

## 验证

本轮 Linux/amd64 检查全部通过：

- `gofmt -l internal/config internal/server cmd/remote-mcp-transfer` 无输出；`git diff --check` 无错误。
- `GOCACHE=/tmp/remote-mcp-go-cache go test -race ./internal/config ./internal/server ./cmd/remote-mcp-transfer` 通过，覆盖匿名及有 Token 模式。首次在沙箱内因禁止监听本机端口失败，随后获准在沙箱外执行；新增 file_stat 测试最初误用目录，修正为普通文件后通过。
- `GOCACHE=/tmp/remote-mcp-go-cache go vet ./...` 通过。
- `GOCACHE=/tmp/remote-mcp-go-cache bash scripts/build.sh` 通过；dist 下 Linux、macOS、Windows × amd64、arm64 共 12 个程序已更新。
- `python3 -B -m unittest discover -s scripts -p test_smoke.py`：3 项脚本生命周期回归通过。
- `python3 -B scripts/smoke.py --bin-dir dist/linux-amd64`：带 Token 的实际二进制上传、独立 JSON-RPC 执行、下载流程通过，确认启动配置包含正确凭据、普通日志不泄漏。
- `python3 -B scripts/smoke.py --bin-dir dist/linux-amd64 --no-token`：匿名实际二进制闭环通过，确认启动 JSON 完全省略 headers、独立 RPC 无 Authorization 且启动诊断说明匿名访问。

两次二进制闭环均下载 24 字节产物，SHA-256 为 `061df3cf2b104ef68a44dacdae1f4d2cf6c50b9348a5a20da8620114eed9e856`。运行系统为 Linux 7.2.7-zen1-1-zen，x86_64。

本轮未重复 257 MiB 测试或无关进程/终端完整测试。Windows/macOS 仅构建通过，原生验收仍待环境。独立检查已通过，见 optional-token-review.md；用户已确认本次功能提交，未推送。
