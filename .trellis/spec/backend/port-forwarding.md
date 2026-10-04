# TCP 端口转发契约

## 1. 适用范围

涉及 `internal/forwarding`、转发配置、MCP 注册或服务关闭时阅读本规范。转发监听位于运行 remote-mcp 的机器，目标可为回环或其可达内网服务；首版仅 TCP。

## 2. 接口签名

管理器提供 `DefaultConfig()`、`New(Config)`、`Register(*mcp.Server)`、`Close()`；模块维护 CreateInput、IDInput、Status、ListResult。

| 工具 | 输入 | 输出 |
| --- | --- | --- |
| port_forward_create | request_id、target_host、target_port，可选 listen_host、listen_port | Status |
| port_forward_list | 空对象 | forwards 状态数组 |
| port_forward_status | id | Status |
| port_forward_stop | id | 资源清理后的 Status |

## 3. 数据与生命周期契约

- 省略监听 IP 时沿用服务监听 IP，端口省略或为 0 时自动分配，返回真实绑定地址。全接口地址不是 Agent 的连接地址，调用方须使用机器可达 IP。
- 目标主机支持 IP 或主机名；监听 IP 必须明确，IPv4/IPv6 不假定双栈。目标按每个入站连接独立拨号。
- 相同 request_id 和规范化输入复用同一规则，自动端口的幂等比较使用请求值 0；保留期内已停止规则也返回原记录。
- 状态含 id、state、listen_address、target_address、active_connections、total_connections、failed_connections、created_at，可选 ended_at 和 last_error_code。创建成功只证明监听成功。
- MCP 断连后继续运行；停止或服务退出关闭监听、取消拨号、关闭连接并等待工作协程结束。重启不恢复，无自动空闲回收。
- 并发连接上限包括拨号中的连接；终态记录有时间和数量限制，活跃规则及其幂等记录不可驱逐。
- MCP Token 和入口 TLS 仅保护管理入口；TCP 字节原样转发，目标自行鉴权和加密，不自动插入凭据或改写 HTTP。

## 4. 校验与错误矩阵

| 条件 | 结果 |
| --- | --- |
| 非法主机、监听 IP、端口或创建键 | invalid_argument |
| 同一创建键对应不同参数 | conflict |
| 监听失败或端口被占用 | listen_failed，不留下活跃规则 |
| 活跃规则或记录超限 | resource_limit |
| ID 不存在或已清理 | not_found |
| 管理器关闭后创建 | closed |
| 目标拨号失败 | 关闭该连接，记录 target_connect_failed，规则仍运行 |
| 连接超限 | 关闭新入站连接，记录 connection_limit，不排无界队列 |

业务错误经 SDK 标记 MCP isError，不因 HTTP 200 声称成功。普通日志不记录流量或凭据。

## 5. 正常、边界与失败示例

- 正常：创建 `target_host=127.0.0.1,target_port=3000`，Agent 访问 remote-mcp 机器的实际 IP 和返回端口。
- 边界：客户端结束发送但继续读，目标仍可返回完整响应；两向复制必须保留 TCP 半关闭语义。
- 失败：目标暂未启动时，连接失败但规则继续存在，目标启动后新的连接可成功。

## 6. 必需测试

管理器测试覆盖真实二进制流、半关闭、并发、停止与拨号竞争、幂等、端口占用、规则和连接限额、记录过期；独立 JSON-RPC 测试覆盖发现、四工具、匿名/Token、isError 和断连保持；实际二进制 smoke 检查请求响应及停止后端口释放。非回环和各系统原生运行必须按实际环境记录，不以交叉编译替代。

## 7. 错误方式与正确方式

错误：一个复制方向遇到 EOF 后立即关闭两端，导致目标响应被截断。

正确：正常 EOF 只对目标写端执行 CloseWrite，继续等待反向复制；错误或主动停止才关闭整个连接。

错误：使用创建工具请求的上下文控制规则寿命，或仅关闭监听器就返回停止成功。

正确：使用管理器/规则生命周期上下文，停止时取消拨号并回收已接受的连接，等待清理完成。
