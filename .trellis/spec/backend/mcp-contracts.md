# MCP 与运行资源契约

## 1. 适用范围

涉及 HTTP 接入、工具签名、文件辅助命令、输出游标或资源生命周期的修改必须核对本契约。实现来源为 `internal/server/server.go`、`internal/transfer/{types,tools,manager}.go` 和 `internal/execution/{types,tools,manager}.go`。

## 2. 签名与入口

- MCP 入口为 `/mcp`，当前协议协商基线为 `2025-11-25`。
- 管理器接口：`New(Config) (*Manager, error)`、`Register(*mcp.Server)`、`Close() error`。
- 文件工具：`file_stat`、`upload_create/write/finish/cancel`、`download_open/read/close`。
- 进程工具：`process_start/read/status/stop`。
- 终端工具：`terminal_open/write/read/resize/status/close`。
- 本地辅助命令入口：`remote-mcp-transfer upload|download`，CLI 完整参数以程序 `--help` 和 README 为准。

## 3. 数据与环境契约

- `REMOTE_MCP_TOKEN` 提供凭据，`--token-file` 显式文件优先。Token 为非空可打印 ASCII，不允许空白；Unix 凭据文件不得向组或其他账号开放权限。
- `Authorization: Bearer <Token>` 对每个请求校验；请求带 Origin 时必须匹配允许源。
- 上传创建参数为 `request_id/path/size/sha256/overwrite`；分块为 `id/offset/data`，其中 data 为 Base64，offset 和 size 为原始字节数。
- 下载读取参数为 `id/offset/length`，结果复用 `transfer.Result`；调用者验证哈希、长度和状态后才宣称成功。
- 普通命令参数为 `request_id/command/args/dir/env/background/timeout_ms/wait_ms`；shell 解释只在显式执行 shell 时发生。
- 终端输入为 `id/data_base64`，尺寸为 `id/columns/rows`。Ctrl+C 的原始字节 0x03 编码为 `Aw==`，程序可以自行处理或忽略该输入。
- 输出包含 `start_cursor/next_cursor/end_cursor/truncated/data_base64/text/valid_utf8`。原始字节是权威数据，文本视图不能代替二进制还原。
- 普通进程分别读取 stdout、stderr；终端只提供合并 terminal 流，保留控制序列。
- 启动输出格式为 `mcpServers.remote-mcp` 下的 `type/url/headers`，每个 IP 独立 JSON，实际 URL 来源于已绑定监听器。

## 4. 校验与错误矩阵

| 条件 | 预期行为 |
| --- | --- |
| Token 缺失或错误 | HTTP 401，不执行工具 |
| 不允许的 Origin | HTTP 403 |
| 无效工具参数、偏移或尺寸 | 稳定业务错误，不 panic |
| 默认上传到已有文件 | 冲突，原文件保持 |
| 长度或 SHA-256 不符 | 上传失败，清理临时文件 |
| 相同 request_id、不同创建参数 | 冲突，不创建第二份资源 |
| 读取游标早于缓冲起点 | 返回截断标记及实际起点 |
| 非 UTF-8 输出 | 保留 Base64 原文，文本注明解码状态 |
| HTTP 断开 | 不自动停止应用进程或终端 |
| 监听失败 | 不输出成功连接配置 |

## 5. 正常、边界与失败示例

- 正常：分块上传程序，校验后通过进程工具运行，再下载产物；Unix 可通过命令工具设置执行权限。
- 边界：空文件仍走创建、完成和空内容 SHA-256 校验；无需伪造空写块。
- 失败：后台日志超过输出缓冲后，从旧游标读取必须看到 truncated，不能返回看似完整的日志。
- 失败：启动时有两个网卡地址但仅绑定其中一个，只能输出该地址，不能输出未监听地址或 0.0.0.0。

## 6. 必需验证

对应测试入口为 `internal/server/*_test.go`、`internal/config/*_test.go`、文件与执行模块测试以及辅助命令测试。断言要覆盖原目标未损坏、ID 没有重复创建、内存/记录有界、配置可解析、普通日志无 Token、进程树及文件句柄清理。

Linux 本机运行、Windows/macOS 原生运行和六组合构建分别记账。没有对应环境时保留未验证状态，不能用模拟终端消除真实平台验收要求。

## 7. 错误方式与正确方式

错误：直接把二进制 Base64 交给模型逐块复述，并用整文件 `ReadFile` 构造请求。

正确：辅助命令用有界缓冲逐块调用 MCP，模型只接收摘要与校验结果。

错误：先删除已有目标，再把临时文件移动过去；或者硬链接发布成功后忘记删除临时名称。

正确：校验完毕后执行平台支持的原子提交；默认覆盖策略在提交时仍成立，成功和失败都处理临时名称清理。

错误：用普通 stdin/stdout 管道宣称支持交互式终端。

正确：Unix 使用 PTY，Windows 使用 ConPTY；尺寸、控制字符和关闭行为按原生平台验证。
