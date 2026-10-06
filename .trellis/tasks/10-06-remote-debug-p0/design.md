# 技术设计

## 边界与工具
一个集成任务覆盖三个模块，统一验收接入与内容过滤。分别独立派发 implementation 代理，避免并行写入公共入口或依赖文件。
- `internal/inspection`：environment_inspect、process_inspect、network_listeners、network_probe；New/DefaultConfig/Register/Close 管理器惯例。标准库及现有 x/sys 优先，无新依赖；平台特定文件隔离。系统命令仅固定命令与固定参数，禁止 shell 拼接；输出、命令时间和结果条数有界。平台能力依据研究说明收敛。
- `internal/execution`：Read 保留已有内部调用兼容，新增 ReadContext；MCP 传入请求 context。缓冲写入通知与退出通知联合等待，检查数据与订阅按锁序保证无丢失唤醒。result.reason 为 output/exit/timeout/cancelled/immediate。
- `internal/logstream`：log_open/log_read/log_close，追踪路径指向普通文件，游标为当前 generation 内的原始字节 offset。文件 identity 变化或 size 小于 cursor 重置 generation，返回 rotated/truncated，不混淆两代数据。轮询有界间隔，关闭取消等待，限制同时打开数量并回收空闲句柄；缺省立即读取。
- `internal/fileops`：file_list/file_read/file_search/file_patch。自有 typed Error 的 code/message，SDK 映射 IsError。返回 hash、行范围、truncated、预算计数。文本读取/修改最多 8 MiB，单次返回 64 KiB；目录分页单次最多 1000 项、搜索扫描最多 10000 项/64 MiB、深度最多 16、最多 1000 命中。不要整棵目录或无限长行读入内存。

## 补丁格式与发布
输入 path/expected_sha256/edits，edit 为 start_line（1 起）、delete_count、text。删除和插入以原内容行边界定义，行结束字节保留在原行内；插入 text 明确携带所需 LF/CRLF，不自动归一化；不重叠且按原坐标排序应用。校验 UTF-8/大小/范围后，同目录写临时文件并保留权限、Sync/Close，再重新核对目标 identity/hash，使用 transfer.Publish(overwrite=true) 原子替换；失败清理临时名称。
模块锁串行本模块修改，哈希复核捕捉外部已发生的修改。跨平台替换没有统一文件系统 CAS；最终复核与 Rename/MoveFileEx 之间外部非合作写入仍存在窗口，文档明示，不声称严格 CAS。上传原覆盖策略不改。

## 错误与内容
各新模块稳定小写 code/message，固定英文消息不回显路径、命令、探测 URL 或文件内容。操作系统错误映射权限/不存在/依赖/超时等类别。巡检的命令行、环境变量和网络正文不进入普通日志。新增工具域/错误类型/白名单在 server/tool_logging.go 集成，过滤候选加入新内容字段，不扩大任意响应日志投影。

## 生命周期与兼容
没有新增 GUI、没有改变鉴权、协议、transfer 工具签名或 CLI flags。日志资源与旧执行资源独立于单次 MCP 请求；关闭服务同时取消新资源，HTTP 取消只结束本次等待。预算、句柄数和超时均默认有界。所有新 schema 描述英文。旧 read 额外字段为向后兼容追加。

## 平台与回滚
Linux 本机运行，Windows/macOS 原生验收暂缺。交叉编译不替代 PID/端口、终端、权限和轮转原生验收。移除新模块注册及恢复 execution read 即可回滚，已有上传和 GUI 数据不迁移。

## 巡检收敛决策
端口巡检 P0 为 IPv4/IPv6 TCP LISTEN，UDP bound endpoint 留待后续。默认快照返回 256 项，最多 4096；内部最多扫描 16384 PID、65536 FD。默认总预算 5 秒、最多 10 秒，最多 4 个在途巡检；外部命令输出最多 1 MiB，名字/版本最多 1024 字节。运行时固定 go/node/python/java/dotnet 白名单，可选择子集，不允许自定义命令参数。网络 HTTP 使用一次 GET，不读取正文、不跟随重定向、禁用代理，TLS 使用系统 CA 验证且不提供 insecure 参数。有效 HTTP 4xx/5xx 表示 HTTP 协议探测成功，状态码由 Agent 判读。部分系统快照保留可用数据并用 partial/warnings/pid_visibility 明示可见性限制，网络失败的 structuredContent 保留此前已完成阶段。

## 等待结果补充
执行 read 在 wait_ms=0 时 reason=immediate，wait_ms>0 按 output/exit/timeout/cancelled 分类。请求取消仍返回当前 ReadResult 与游标，err=nil；传输若已断开可能无法送达响应，不意味进程停止。output 优先于 exit，避免漏读退出前尾部数据。

## 按行读取预览语义
file_read 的 next_line 为首个未完整返回行；byte_limit 时可提高 max_bytes 重读。超过 64 KiB 的单行只提供有限预览，跳过该行使用 end_line+1，不能盲拼重复前缀；需要原始全部字节时使用已有 download_read。file_search 在每行内完整匹配字面子串，不支持跨行匹配。本任务不新增 byte offset 的文本续读接口。
