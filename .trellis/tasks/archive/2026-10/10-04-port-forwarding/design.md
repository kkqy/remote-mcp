# TCP 端口转发技术设计

## 架构与边界

新增 `internal/forwarding`，提供 `DefaultConfig`、`New`、`Register`、`Close`，遵循既有管理器模式。`internal/config` 负责命令行与默认监听 IP 注入，`internal/server` 负责工具注册、初始化失败回滚和服务退出清理。使用 Go 标准库网络能力，不引入依赖或新程序。

数据流：Agent 的普通 TCP 客户端 → remote-mcp 机器上的独立 TCP 监听器 → 指定目标。MCP 仅控制规则生命周期，数据不经过模型或 MCP 编解码。

## 工具契约

- `port_forward_create`：必填 `request_id`、`target_host`、`target_port`；可选 `listen_host`、`listen_port`。监听 host 为明确 IPv4/IPv6 地址，默认继承服务监听 IP；端口范围 0—65535，0 为自动分配。目标 host 为 IP 或不含协议、路径、端口的主机名；目标端口范围 1—65535。
- `port_forward_list`：无参数，返回有界的活跃及保留期内终态记录，按创建时间与 ID 稳定排序。
- `port_forward_status`：必填 `id`，返回状态快照。
- `port_forward_stop`：必填 `id`，完成监听和所有连接清理后返回终态；保留期内重复停止安全，记录已清理则返回 `not_found`。
- 状态至少含 `id`、`state`（running/stopping/stopped/failed）、`listen_address`、`target_address`、`active_connections`、`total_connections`、`failed_connections`、`created_at`、可选 `ended_at` 和稳定 `last_error_code`。创建成功只代表监听成功，不承诺目标已连通。
- 返回地址是实际绑定地址。工具描述与说明明确：全接口地址须替换为 Agent 可达的该机器 IP；不自动猜测路由或承诺可达。
- 模块定义独立的结构化 `code/message` 错误，沿用执行模块的 SDK 工具错误方式；验证独立 JSON-RPC 响应的 `isError`。稳定错误码包括 invalid_argument、conflict、not_found、resource_limit、listen_failed、closed、internal。

## 转发与失败行为

每个入站连接单独拨号到目标，使用管理器生命周期上下文和拨号超时，不继承单次创建工具请求的上下文。目标主机名每次拨号解析；目标失败只关闭本次入站连接，增加失败计数并记录固定错误码 target_connect_failed，规则保持可用。

两向复制使用有界缓冲，EOF 时对另一端 CloseWrite，继续读取剩余响应；非 EOF 错误、主动停止或管理器关闭时关闭两端，等待工作 goroutine 回收。协议字节不修改，不记录数据内容，不附加 MCP 凭据，不主动给普通 TCP 套入口 TLS。

IPv4/IPv6 监听分别选择 tcp4/tcp6，不假定双栈。目标拨号与监听地址族独立。停止与 accept/dial 的竞争必须保证迟到连接无法逃逸：先标记 stopping，关闭 listener、取消拨号，再关闭已登记连接并等待回收。

## 生命周期、幂等及限额

- 按规范将资源与 request_id 存在内存；规范化省略默认值后的输入用于幂等比对，自动端口按请求中的 0 比对，不按分配结果比对。相同 request_id 的并发创建不能生成多个监听器。
- 规则不自动空闲回收，连接不设置应用层空闲时限；显式停止和正常退出负责释放。
- 建议默认配置：最多 16 条活跃规则、全局 256 个连接槽位（包括拨号中连接）、拨号超时 10 秒、终态保留 10 分钟、最多 1024 条记录、清理间隔 30 秒。提供对应 forwarding 专属命令行配置并校验正值。
- 超过连接上限立即关闭新接入连接，记录 connection_limit；不创建无界等待队列。每条规则仅保存计数与最后错误码，不保存逐连接历史。
- 记录满时优先清理最旧终态；活跃规则和创建中的幂等占位不得驱逐。已停止规则在保留期内的创建重试返回其终态，重新创建需要新 request_id。
- 管理器锁不跨拨号、复制和等待 goroutine 使用；统一锁顺序和连接登记规则，Close 可并发重复调用，实际清理完成后才返回。

## 接入、兼容与回滚

维持现有 MCP 入口及已有工具契约；转发管理自动继承 Token、Origin 和请求限制。App.New 后续失败须关闭已构造的管理器；App.Close 需清理转发管理器。检查仓库里 Config 复合字面量，避免新增字段造成编译或默认值回归。

无持久化迁移。回滚为移除新增模块、配置和注册即可；升级或回滚重启都会终止内存转发规则。中文说明补充到 README.zh-CN.md，新文档和注释保持中文；README.md 现有英文不整体改写，必要时补充指向中文功能说明的入口。

## 验证与剩余限制

通过真实 socket、独立 JSON-RPC 及实际二进制测试覆盖 PRD 验收。非回环测试优先使用可用网卡地址；无可用环境时显式跳过并报告，不能将回环测试称为真实内网验证。竞态检查与六组合构建沿用项目规范；Windows/macOS 原生结果单独记录。

本设计未留下阻塞技术未知项；上述限额为实现默认值，在最终评审中一并展示其有界资源原则。
