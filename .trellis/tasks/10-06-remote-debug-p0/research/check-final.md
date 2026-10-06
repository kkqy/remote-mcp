# 三项 P0 最终全范围复审

复审范围为本任务的 `internal/fileops`、`inspection`、`logstream`、execution 等待修改、server 接入及日志诊断、两份 README、`scripts/p0-smoke.py` 和相关后端规范。未修改 `.opencode/package.json`、旧 GUI 任务、transfer 接口或公共依赖，未提交和推送。

## 已修复发现

| 文件 | 问题 | 修复与实际回归 |
| --- | --- | --- |
| `internal/inspection/platform_linux.go` | IPv6 表访问失败固定报告依赖缺失，进程目录访问失败固定报告普通 I/O，丢失权限原因 | 使用实际系统错误分类；两个真实模式权限测试通过并保留部分地址/PID 可见性 |
| `internal/fileops/types.go` | 未充分说明部分行的 next_line 及逐行搜索语义 | 按主代理决策补充英文输入/输出 schema，保留既定游标行为；spec/design/README 同步明确有限预览及不跨行匹配 |
| `internal/fileops/manager.go`、`open_unix.go`、`open_windows.go` | Lstat 后直接普通打开：换成 FIFO 会先阻塞，换成指向原 inode 的链接会被跟随 | Unix NONBLOCK/NOFOLLOW；Windows OPEN_REPARSE_POINT/BACKUP_SEMANTICS；打开后继续核对类型和 identity。注入检查后替换的 FIFO/同原文件符号链接测试通过，原内容保留 |
| `internal/inspection/manager.go` 和三平台适配 | Linux Uname、macOS Sysctl 失败只返回空 kernel，环境结果遗漏原因 | 私有查询返回错误；保留 os/arch/运行时数据并附 partial 和受控 warning。注入权限失败验证稳定码、具体英文原因且无底层原始错误内容 |

## 全范围核对
- execution 读取与订阅使用同一缓冲锁，写入唤醒全部订阅者；退出在输出排空后发布，等待退出再次读取尾部。取消和服务关闭返回读取结果，缺省 read 保持立即行为，真实进程和 PTY 延迟输出及尾部回归通过。
- logstream 使用 generation 和原始字节游标，轮转/缩短重置坐标；路径轮转短暂缺失在等待预算内保留旧快照并等待重建。取消、poll/Close 竞态、重复关闭、活跃等待不空闲回收、旧句柄关闭失败和 sweeper 清理错误均有测试。Windows 打开包含 SHARE_DELETE，原生行为仍未验证。
- server 新管理器创建失败回滚及 Close 接入齐全。精确工具域、已知错误码和具体 typed Error 保留安全诊断；巡检使用 SetError 可信指针，超过 4096 字节的 partial structuredContent 保留，未扩大普通日志 JSON 投影边界。
- 独立 HTTP JSON-RPC 发现 40 个工具，核对新增工具及 wait_ms schema，实际调用新增工具、进程/终端等待和日志轮转，错误响应含稳定 code/message，网络失败保留成功阶段。日志回归核对内容与凭据过滤、SDK schema 失败、成功不附错误字段。
- `p0-smoke.py` 源码显式严格 UTF-8，协议读取有上限，资源清理与普通日志校验完成后才输出成功；脚本的实际二进制运行由主代理验证，本复审未重复运行。
- `.trellis/spec/backend/remote-debug.md` 同步路径打开竞争防护、巡检失败原因和已确认行读取语义；文档未将交叉编译写成原生验收通过。

## 最终代码状态验证
- 格式：`gofmt -l internal/fileops internal/inspection internal/logstream internal/execution internal/server` 无输出。
- 静态检查：`GOCACHE=/tmp/remote-mcp-go-cache go vet ./...` 通过。
- 编译与测试：`GOCACHE=/tmp/remote-mcp-go-cache go test ./...` 通过。
- 竞态检测：`GOCACHE=/tmp/remote-mcp-go-cache go test -race ./...` 通过。
- 本机权限分类与内核失败专项 verbose 回归：三个测试均实际 PASS，没有跳过。
- 六组合无 CGO 构建、旧冒烟、P0 Token/匿名冒烟及 Python 回归由主代理继续验证；本记录不预先声明其结果。

## 未修改的真实限制
- Windows/macOS 原生 PID、端口、终端、轮转及权限验收没有对应环境，本任务与旧 GUI 任务保留该缺口。
- 对非合作外部修改不提供严格文件系统 CAS；正常内核文件 I/O 不能由标准库强制取消，检查后换成 FIFO 的可避免问题已修复。
- 日志两个采样间的瞬间截断恢复或同尺寸原地重写可能无法观测；按行读取超过 64 KiB 的长行仅预览，跨行文本搜索不在当前接口范围内。这些均已明确记录。

除上述已确认限制，没有尚未修复的本任务代码问题。复审结束，停止写入，交回主代理进行构建与真实二进制验收。
