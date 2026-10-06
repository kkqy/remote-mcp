# 程序英文输出实施记录

## 写入前边界

用户最新要求“错误和提示要用英文”覆盖程序自有运行输出。现有中文会话、文档与注释规则保留，但运行错误、日志文字、启动/退出说明、HTTP 错误、CLI 帮助与进度须转换为英文。

此实施者只修改 `internal/server/`、`internal/config/`、两个 `cmd/` 入口及对应断言、旧 `scripts/smoke.py` 的程序输出与回归、两个 README 的语言说明。不改变错误码、JSON 字段、状态、访问控制、隐私过滤逻辑或用户实际内容；其他代理负责 GUI、文件、执行、转发模块。原有 `.opencode/package.json` 用户修改保持不动。

采用 Go AST 提取/替换生产源码字符串字面量，避免误改中文注释。回归只同步与运行文案绑定的断言；测试中的中文用户输入继续保留。拟新增 AST 语言检查，只扫描 cmd/internal 生产源码的 Go 字符串字面量，不扫描注释/测试，待全部代理冻结后再执行跨模块检查。

## 实施与检查

- `internal/config/config.go` 和 `token.go`：监听、鉴权、资源上限错误及命令行帮助改为英文。
- `internal/server/server.go`、`startup.go`：HTTP 边界错误、启动和关闭提示、普通日志事件、未知工具标签改为英文；`tool_logging.go` 的受控分类、隐藏标记及业务原因断言同步英文。
- `cmd/remote-mcp/main.go` 和 `cmd/remote-mcp-transfer/main.go`：启动失败、服务错误、传输错误、帮助和进度改为英文；CLI 的 JSON 摘要结构及启动配置包含凭据的既有例外不变。
- `scripts/smoke.py` 和 `test_smoke.py`：断言、帮助和运行错误改为英文，仍按 UTF-8 读取实际中文数据。两份 README 记录运行语言契约，并同步专用窗口的英文恢复按钮名称。
- `runtime_language_test.go`：用 Go AST 检查 `cmd/internal` 全部非测试生产源码的 `token.STRING`，涵盖 schema 标签；失败只输出源码位置，不复制字面量。注释、测试源码和动态用户内容不受此检查限制。

父会话发现英文隐藏标记与有效 ASCII Token 或被屏蔽内容相同时可能重新输出秘密，因此本轮增加最小修复：`redactionMarker(preferred string, values []string) string` 避开这些值，冲突时使用符号；Token 为 `*` 时不会选择 `*`。`redactToken` 再用凭据自身不含的单字符阻断标记与两侧文字拼接形成的凭据。过长、无效编码和空白消息的安全回退也经过同样脱敏。输出仍以 1024 字节为限（含省略号），业务原说明 4096 字节、请求 1 MiB/8 层/128 值等边界保持不变，没有扩展未知错误的原文信任范围。

精确回归包含 Token `redacted`、`[redacted]`、`content`、`*`、`#`、`a[` 和输入 `[content redacted]`，以及空 Token、回退消息和 `owner_unknown/timeout (epoch 3->4, formats 2)` 的保留。HTTP 日志回归继续核对四工具域英文原因、错误码、SDK 转换后 `IsError=true`/`err=nil`、未知工具/schema 失败、成功无错误字段及敏感输入不泄漏。

已通过：

- `GOCACHE=/tmp/remote-mcp-gui-integration-cache go test ./internal/config ./internal/server ./cmd/...`，包含全生产源码 AST 语言检查。
- 同包 `go test -race` 和 `go vet`。
- `python3 -m unittest discover -s scripts -p test_smoke.py`（7 项）及两个脚本的 `py_compile`。
- `gofmt` 与 `git diff --check`。

普通 HTTP/CLI 测试需要本地临时监听，因此在获准的非沙箱测试执行环境完成。此次没有发起真实桌面授权或键鼠操作；全包检查、六组合构建和实际 Token/匿名旧冒烟由主会话统一执行。没有提交 Git、改变任务状态或修改 `.opencode/package.json`。
