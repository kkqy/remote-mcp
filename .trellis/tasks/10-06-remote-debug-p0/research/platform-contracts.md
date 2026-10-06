# 巡检平台契约核对

范围修订（2026-10-06）：用户已取消 macOS 支持，以下 macOS 内容保留为早期设计与验证历史，不作为当前实现或验收要求。当前仅支持 Linux/Windows，Windows 虚拟机的实际结果另见 windows-native.md。

实施代理已核对 Windows IP Helper 的一手 API/结构说明：

- [GetExtendedTcpTable](https://learn.microsoft.com/en-us/windows/win32/api/iphlpapi/nf-iphlpapi-getextendedtcptable)：查询大小后有界分配；owner PID LISTENER 类别为 3；IPv4/IPv6 地址族分别为 2/23。
- [MIB_TCPROW_OWNER_PID](https://learn.microsoft.com/en-us/windows/win32/api/tcpmib/ns-tcpmib-mib_tcprow_owner_pid)：IPv4 行 24 字节，DWORD 四字节对齐，端口网络字节序。
- [MIB_TCP6ROW_OWNER_PID](https://learn.microsoft.com/en-us/windows/win32/api/tcpmib/ns-tcpmib-mib_tcp6row_owner_pid)：IPv6 行 56 字节，端口及 scope ID 按官方网络字节序处理。

内部解析 fixture 在 Linux 运行；系统 API 以模拟返回覆盖最多 1 MiB 分配、最多三次不足缓冲重试、权限错误和畸形长度。上述证据不代表 Windows 原生运行通过。

macOS 使用固定 `/bin/ps` 与 `/usr/sbin/lsof`；命令 stdout/stderr 分别捕获并共享有界预算，固定英文权限诊断映射 permission_denied，原诊断不回显。lsof 退出 1 只有两条流均为空才当无匹配；有诊断明确不完整。机器字段增加地址族标记，避免 `*:port` 将 IPv6 误判 IPv4。解析 fixture 已在 Linux 测试；macOS 原生命令/权限尚未验证。

## 剩余原生验收
Windows/macOS 的当前运行时、真实父子进程、IPv4/IPv6 监听 PID、账户权限不足、文件轮转和补丁发布均需在对应平台实际执行。六组合无 CGO 构建只证明编译链路。GUI 原任务仍有其独立验收缺口，本任务不改变其状态。

## 已发现的诊断边界
巡检 partial 失败可能超过日志的 4096 字节原始响应投影限制。inspection 注册器通过 CallToolResult.SetError 保存服务端可信 *inspection.Error，仍返回 typed out/nil error，客户端阶段与部分结果不丢失。真实 SDK 中间件/客户端测试覆盖超过 4096 字节的 100 条中文进程数据；服务端 GetError 可取得具体错误，客户端不持有服务端错误指针。
