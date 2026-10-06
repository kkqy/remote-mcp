# GUI 英文运行文案实施记录

## 要求与变更边界

用户追加要求程序错误和提示使用英文，优先于此前中文运行文案约定。会话、文档、源码注释与测试断言说明仍使用中文。

本轮仅修改 `internal/gui/` 的静态输出文字：公共管理器和参数校验、工具 description/schema 描述、Linux X11/Wayland/Portal、Windows、macOS 的错误 message、capabilities reasons、默认显示器标签和内部固定诊断。没有修改错误 code、JSON 字段、接口、枚举、超时、资源生命周期或输入行为；没有改写用户输入、剪贴板字节或系统返回的显示器名称。

涉及生产文件为 `clipboard_portal_linux.go`、`helpers.go`、`layout_portal_linux.go`、`manager.go`、`platform_darwin.go`、`platform_windows.go`、`tools.go`、`types.go`、`wayland_linux.go`、`x11_connection_linux.go`、`x11_linux.go`。`manager_test.go` 中模拟业务失败与能力原因同步英文；中文测试输入、恢复原文和敏感原始异常样本保留。

## 消费者协调

- GUI 会话不存在固定 message 为 `GUI session not found`，已告知服务/HTTP 日志测试负责者。
- Portal 初始未知 status reason 为 `The Portal has not provided the current clipboard formats; default preservation requires an ownership notification from a normal copy operation`。GUI 验证脚本可用 `has not provided the current clipboard formats` 匹配既有等待哨兵，已告知脚本负责者；只替换静态匹配，不改变等待或剪贴板准备行为。
- 默认快照未知 message 为 `The Portal has not provided a reliable clipboard ownership and format snapshot`。
- KDE 控制格式拒绝 message 为 `The clipboard contains the KDE onlyReplaceEmpty ownership control format, which cannot be restored unchanged over a nonempty selection`。
- 动态恢复模板仍保留 method_error/owner_false/mimetype_mismatch/owner_unknown/timeout/cancelled、epoch 与 format count；input_may_have_applied/clipboard_restore 字段和值保持。

## 检查

- 用静态词法比较 HEAD 和工作树：生产修改仅涉及 254 个字符串字面量，注释及字符串之外的源码完全一致；未改变非中文的错误枚举或 JSON 键。
- 使用 Go parser/AST 扫描全部 GUI 生产文件，包含各平台构建排除文件及 schema 标签：1250 个字符串字面量中剩余中文为 0。注释与测试内容不参与该检查。
- `gofmt` 与 `git diff --check -- internal/gui`：通过。
- `GOCACHE=/tmp/remote-mcp-gui-go-cache go test -race ./internal/gui`：通过（1.476s）。
- `REMOTE_MCP_GUI_PORTAL_TEST=1 GOCACHE=/tmp/remote-mcp-gui-go-cache go test -race ./internal/gui -count=1`：通过（5.379s），只连接临时私有总线。
- `GOCACHE=/tmp/remote-mcp-gui-go-cache go vet ./internal/gui`：通过。

未再次访问宿主桌面或剪贴板，没有新增运行依赖，没有提交或推送。全项目检查与六目标构建由主会话统一执行；本轮文案改变不增加任何平台原生验收结论。
