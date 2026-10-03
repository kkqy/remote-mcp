# 日志与配置输出

普通日志只记录工具名、耗时、结果等元数据，不记录 Token、文件内容、环境变量值或终端输入输出。SDK 内部调试日志可能包含参数，不能直接接入普通运行日志。参考 `internal/server/server.go`。

启动配置是用户明确要求的例外：`internal/server/startup.go` 将完整客户端 JSON 写入 stdout；配置 Token 时包含当前 Token，匿名模式省略 headers。中文说明须准确描述鉴权状态，说明及运行日志写入 stderr。不得把同一份配置再次写进结构化日志。示例、测试和提交文件只能使用虚构 Token。

仅当 Token 非空时，才用其过滤日志字段；对空串调用 `strings.Contains` 总为真，会错误隐藏全部工具名。

一个有效 IP 对应一个独立完整的 JSON 对象；多个对象是备选配置，不是单个 JSON 文档，也不应全部注册为重复服务。必须用 JSON 编码器处理凭据转义，禁止字符串拼接。

启动 JSON 使用 OpenCode 1.x 的 `$schema/mcp` 结构，每个服务包含 `type: "remote"`、`enabled: true` 和显式 `oauth: false`。中文提示须说明选一份合并到 OpenCode 配置；启动 JSON 消费者与示例随格式一起更新。

单 IP 绑定也要排除不可展示的组播和 IPv6 链路本地地址，不能绕过全接口枚举的过滤规则。IPv6 链路本地作用域属于具体机器，服务端网卡名不能直接作为远端 Agent 的作用域使用。

辅助命令 stdout 用于最终结构化摘要，stderr 用于进度与诊断；不得向 stdout 打印每个 Base64 分块或复制真实文件内容。
