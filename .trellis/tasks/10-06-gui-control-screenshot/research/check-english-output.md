# 英文运行输出与错误诊断最终复审

日期：2026-10-06。复审范围为当前未提交 LOG-01/02、AC-08/09，包括四模块错误诊断、服务/配置/两个命令、六个 Python 文件、README 与规范/任务文档。旧 `check-error-logging.md` 作为先前中文日志轮历史保留；本报告不改写旧 GUI 原生结果。

## 上下文与完整范围

加载 check.jsonl、当前 PRD/design/implement；packages 入口为单仓库，backend 索引说明不存在业务前端，按 Quality Check 读取质量、错误与日志规范。MCP、资源、GUI 及平台研究沿用本任务此前完整加载的契约，并核对英文追加条款。会话、文档和注释保持中文，程序自身文案英文，用户实际 UTF-8 数据不翻译。

逐路径核对 `internal/server/{server,tool_logging,runtime_language_test}.go` 与独立 HTTP 测试，追踪 SDK v1.8.0 的 SetError/GetError 和 typed 输出序列化；核对 config、execution、transfer、forwarding、gui 全部生产差异、两个 cmd 入口、六个 Python 文件、文字匹配消费者与两份 README。没有修改依赖、访问控制、GUI 状态机、用户 `.opencode/package.json`，没有访问宿主桌面或剪贴板。

## 跨层结论

- 四模块业务错误仍经过真实 SDK 链路：执行/转发的 Error 被转换为 IsError=true、err=nil，GetError 保留服务端具体类型；GUI 本域 gui.Error 保持具体结构；文件 typed Result 转为 RawMessage，只有精确 transfer 域明确 ok=false、4096 字节内的 code/message 投影可信。未知工具、未知类型、任意 Content/JSON 形状不提升原文日志权限。
- 精确域覆盖 29 个内置工具，现有四模块错误码白名单完整且大小写不变。业务原说明 4096 字节、日志 1024 字节含省略号、请求 JSON 1 MiB/内容递归 8 层/收集 128 个合格敏感候选边界保持；公开枚举和短值不误过滤。成功仍无错误字段，日志处理不修改原响应。
- 生产 Go 文字归一化比较显示，除本轮日志中间件/隐私处理外，其余生产变更只涉及字符串。错误码、字段、接口、状态及输入/恢复时序没有翻译性重构。CLI 测试额外使用 ToLower 匹配英文 token file；Python smoke 的 argparse 从内部中文 docstring 改用英文 description 常量，其余归一化 AST 未发现业务变化。
- `TestRuntimeStringsUseEnglish` 使用 Go AST 扫描 cmd/internal 的非测试字符串字面量，包括平台构建排除文件和 schema 标签；不扫描注释、测试或动态用户数据。本轮 server 测试已执行该检查。它没有把中文用户数据转换成英文；CLI 中文文件内容、GUI 中文输入/原快照样本和 UTF-8 解码仍保留。
- Python AST 检查残留中文仅为内部文档字符串、测试断言/虚构数据，以及 `旧内容`、`中文图形验证成功`、`组合键验证`等实际验证输入。CLI/help、报告提示、窗口标题与恢复提示英文。Portal reason 的等待哨兵在生产与脚本均为 `has not provided the current clipboard formats`，没有沿用旧中文导致跳过等待。
- 程序日志事件、HTTP/配置/CLI 错误及 MCP description/schema 描述英文；文档和新增注释中文。系统提供的名字、原始用户输出和操作系统界面不翻译。README 明确这一边界，原生 GUI 待验收状态保留。

## 确认发现及局部修复

### 内容标记与相邻文字重新拼出敏感候选

文件：`internal/server/tool_logging.go`、`tool_logging_test.go`。

可重现：候选 text=`foo[`，原 message=`foofoo[`；一次替换会得到 `foo[content redacted]`，重新包含完整被屏蔽候选。Token 的二次隔断不能单独修复内容候选；控制字符规范化、Token 标记和截断省略号也可能重新形成候选。

修复：完成原替换、控制字符处理、Token 脱敏及截断后，再检查已收集候选；必要时用所有候选及 Token 均不包含的单字符隔断统一替换。若末尾候选包含截断省略号，安全隔断后保留省略号：隔断不在任一候选中，不能跨边界重拼；替换腾出的字节保持总长度不超过 1024。所有隔断都与候选冲突的极端情况保守返回不与 Token 冲突的单字符，不删除标记后拼接原文字，也不扩大错误信任域。

回归：helper 覆盖内容相邻标记、换行规范化、Token 标记带入另一敏感候选、截断末尾、全部候选隔断冲突；真实 HTTP 同时验证匿名/Token 的 `foo[` 不进入日志、公开 owner_unknown/timeout 原因保留且原 GUI 错误响应不变。

### 未知工具名称后备与整枚 Token 碰撞

文件：`internal/server/server.go`、`tool_logging_test.go`。

可重现：有效 Token=`unknown_tool`，请求未注册工具；固定 tool 后备值也是 `unknown_tool`，会输出整枚凭据。这里是受控后备值的完整碰撞，不将任意公共字段中的短 ASCII 偶合扩大为新隐私规则。

修复：初始未知工具后备同样经过 redactToken。真实 HTTP 使用该虚构 Token，确认 tool 值隐藏、日志不含整枚 Token、未知工具具体原因仍在，响应保持真实协议错误。

## 未修项与界限

无其他可确认的未修代码缺陷。本轮不保证未知第三方异常原文被安全输出；只输出受控类别，内置固定安全消息仍是主要信任边界。超扫描边界及短值不被当作通用内容脱敏承诺。极端候选碰撞时错误说明保守降级，稳定错误码仍提供类别。

GUI 原生 Windows/macOS/X11/GNOME、正常中文剪贴板恢复、多屏/客户端展示/撤权等缺口仍未关闭；KDE 第八轮只有此前键鼠子集证据，本轮没有重新授权或操作真实 GUI。任务继续 in_progress，不归档。

## 验证与冻结

- 审阅者最终 `GOCACHE=/tmp/remote-mcp-check-go-cache go test ./internal/server -count=1`：通过（0.656 秒）。
- 审阅者最终同包 `go test -race -count=1`：通过（2.518 秒），包含独立 HTTP、所有英文日志隐私回归及生产 AST 语言检查；经批准允许临时回环监听，只使用虚构内容。
- 审阅者 `go vet ./internal/server`、gofmt 与 `git diff --check`：通过。Go 类型检查由包编译覆盖。
- 主会话在局部补修前已确认全包 test/race/vet、六 OS/架构×两个命令构建、31 Python、六脚本 py_compile 及 Token/匿名真实旧功能 smoke 通过。本审阅只补服务器日志，因此 Python 和未改业务模块的结果继续有效；受影响 server 普通/race/vet已以最终源码重跑。
- 最后生产冻结后，主会话再次确认十二个无 CGO 构建、全包 vet/diff 检查、最新二进制 Token 与匿名实际 smoke 全部通过。源码及本报告已冻结，可形成可审阅提交；本审阅没有提交或归档。
