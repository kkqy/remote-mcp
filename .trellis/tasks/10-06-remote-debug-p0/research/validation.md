# 验证记录

日期：2026-10-06；本机 Linux/amd64，Go 1.27.1-X:nodwarf5。构建、自动测试和对应平台原生运行分别记录。

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
