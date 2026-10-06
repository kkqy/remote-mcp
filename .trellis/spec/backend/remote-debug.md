# 远程调试 P0 契约

## 1. 范围与触发条件
修改 `internal/fileops`、`internal/logstream`、`internal/inspection` 或 `execution.ReadContext`，以及新增工具注册/诊断日志时，必须核对本契约。各模块复用管理器惯例 `DefaultConfig/New/Register/Close`；新工具不改变鉴权、上传默认覆盖策略或 GUI 权限契约。

## 2. 签名与入口
- 文本文件：`file_list/file_read/file_search/file_patch`；所属方法 `(*fileops.Manager).List/Read/Search/Patch(context.Context, <Input>) (<Result>, error)`。
- 执行输出：原 `Read(ReadInput)` 内部兼容接口保留，MCP 使用接收请求 context 的 `ReadContext`。
- 日志跟踪：`log_open/log_read/log_close`；独立资源管理器，不绑协议会话。
- 系统巡检：`environment_inspect/process_inspect/network_listeners/network_probe`。
- 新工具同时添加到 `server.toolDomain` 及可信错误白名单；禁止自动信任任意第三方 code/message JSON。巡检失败 response 可能包含较大 partial 结果，注册器使用 SetError 保留服务端 typed Error 指针，并同时返回 typed out/nil error，以保留 structuredContent 和具体日志原因；不放大原始 JSON 投影上限。

## 3. 数据契约
### 文件
- `file_list`：path/offset/limit，有界单目录分页；next_offset 为本次枚举坐标，目录变化时不能声称稳定快照。符号链接显示类型但不跟随。
- `file_read`：path/start_line/line_count/max_bytes，行号从 1 开始；返回完整原始文件 SHA-256、total_lines、text、start_line/end_line/next_line、truncated/reason。中文 UTF-8 不变，LF/CRLF 原样保留，超过返回预算的长行可返回有限 UTF-8 预览并指明未完整返回。next_line 为首个未完整返回行，可能仍为当前行：byte_limit 可提高 max_bytes 重读，超过 64 KiB 的长行仅预览，跳过用 end_line+1；调用者不能盲拼重复前缀。
- `file_search`：path/query（区分大小写的 UTF-8 字面子串，逐行匹配而不跨行）与 max_depth/max_entries/max_scan_bytes/max_file_bytes/max_matches；目录深度、条目、扫描字节及响应字节均有上限。每个命中提供 path/line/text/truncated；部分扫描失败列入 issues，不能把未扫描当成不存在。超长行在文件扫描预算内仍须完整匹配，预览限额不作为匹配限额。
- `file_patch`：path/expected_sha256/edits，其中 edit 是 start_line/delete_count/text。按原内容坐标严格递增、不重叠；total_lines+1 追加；text 为确切 UTF-8 替换字节，调用者显式携带所需换行。只修改已有普通文件，拒绝符号链接目标；没有文件创建或删除协议。
- 默认文件上限 8 MiB、响应文本/搜索预算 64 KiB，列表最多 1000 项，搜索最多 10000 条目/64 MiB 扫描/16 层/1000 命中。不得用无界 ReadDir(-1)、无限行缓冲或整棵目录内存快照绕过预算。
- 补丁串行本模块修改；同目录临时文件保持权限、Sync/Close、发布前核对 identity/hash，复用 transfer.Publish(overwrite=true)。发现冲突必须保留原文件。Go Chmod 在 Unix 保持权限位，Windows 同目录临时文件继承目录 ACL，不能声称复制原目标的自定义 ACL、属主或扩展属性。跨非合作外部编辑器不提供严格文件系统 CAS；最终复核与平台替换之间的竞争窗口必须明确说明。
- 路径在普通类型检查后、实际打开前仍可能被替换；Unix 使用 O_NONBLOCK/O_NOFOLLOW，Windows 使用 OPEN_REPARSE_POINT（目录同时使用 BACKUP_SEMANTICS），打开后再核对类型和 identity，防止被替换的管道阻塞或链接被跟随。此边界与不能强制取消已阻塞的正常内核 I/O 分别处理。

### 输出等待与日志
- 执行 read 新增 wait_ms，范围 0..30000，省略/0 立即读取。字节游标、Base64、UTF-8 视图及 buffer truncated 仍是旧契约；不把字符数当字节偏移。
- 等待新输出、退出、超时或取消后返回明确结果；订阅通知与检查数据必须保证无丢失唤醒，退出前已收集的尾部输出不能跳过。
- 文件跟踪轮转/截断必须区分 generation 与 generation 内字节 cursor。轮转后不能把旧文件偏移直接应用到新文件；两个采样间发生截断又恢复到原尺寸可能无法观测，应如实记录。
- 日志资源默认最多 32 个、每读 64 KiB、100 ms 轮询、10 分钟空闲回收；OpenResult 返回 id/generation/end_cursor，ReadInput 为 id/generation/cursor/limit/wait_ms。ReadResult 保留 byte cursor/Base64/text/valid_utf8，新增 generation/rotated/truncated/reason。Close 取消等待并关闭句柄，HTTP 取消仅结束本次调用；CloseLog 可重复调用。Windows 句柄使用 SHARE_DELETE 允许路径轮转。
- 已打开路径轮转时暂时缺失，带等待读取在预算内等候重建；关闭与轮询同时发生也返回最后快照及 cancelled。普通文件系统阻塞 I/O 不能由标准库强制取消，取消在 I/O 块边界检查。

### 巡检
- 系统响应说明当前服务账号/namespace 可见性，不把空列表解释为整机不存在对象。进程返回 PID/PPID/name，不额外采集 argv/environ。
- P0 监听为 IPv4/IPv6 TCP LISTEN，UDP bound endpoint 留待后续。PID 不可见使用空集合及 visibility，不伪造 PID 0；权限、依赖和预算导致的部分结果明确 partial/warnings/truncated。
- 运行时 go/node/python/java/dotnet 使用固定命令白名单与当前 PATH；缺失、版本查询失败或超时分别返回英文代码与原因。版本输出和系统名字是响应数据，不进入普通日志。
- 内核版本读取失败时保留基本 os/arch，返回 partial 和带稳定错误码的受控英文 warning；Linux 可选 IPv6 表或进程目录访问失败同样按具体权限/缺失类别说明，不能统一伪报依赖缺失或普通 I/O 错误。
- DNS→TCP→TLS→HTTP 共享同一个总 deadline，保留成功阶段及 failed_stage。IP 字面量可跳过 DNS；TLS 默认系统 CA，禁止跳过验证；HTTP 一次 GET、不走代理、不跟随重定向、不读正文、header 有界。HTTP 4xx/5xx 是协议响应成功，status_code 由调用者判断业务健康。
- 默认预算 5 秒、上限 10 秒，最多 4 个并发巡检；默认返回 256 项、最多 4096，内部最多 16384 PID/65536 FD。外部固定命令输出最多 1 MiB，单名字/版本最多 1024 字节。固定命令取消后的遗留管道清理可额外等待最多 100 ms，WaitDelay 不保证任意 PATH 脚本的后代进程树回收；Windows 原生查询不可中途取消时，实际返回前保持并发占用。

## 4. 校验与错误矩阵

| 条件 | 必须行为 |
| --- | --- |
| 参数范围、哈希或补丁格式非法 | 稳定 invalid_argument/invalid_patch，不写目标 |
| 哈希不符、读中变化或发布前身份变化 | conflict，保留原目标 |
| UTF-8 非法或文件超过上限 | invalid_utf8/file_too_large，不能默默替换编码 |
| 文件/依赖不存在或权限受限 | 明确 not_found/dependency_missing/permission_denied；部分搜索或巡检说明不完整 |
| 写入、同步或发布失败 | 具体受控英文原因、保留目标、清理临时文件 |
| 输出/扫描预算达到上限 | truncated 及受限原因，不能宣称扫描完整 |
| DNS/TCP/TLS/HTTP 失败 | 稳定阶段码、failed_stage 和已完成阶段，不丢失 structuredContent |
| 等待超时/取消、管理器关闭 | 有界结束和明确结果，不自动停止应用进程 |

## 5. 正常、边界与失败示例
- 正常：file_read 返回全文件 hash，用相同 expected_sha256 将第 2 行替换为 `新内容\r\n`；未修改的 CRLF 与中文字节保持。
- 边界：长行返回预览被截断，但 query 位于长行末尾时 file_search 仍发现命中；扫描文件字节预算耗尽则标明不完整。
- 失败：读出 hash 后另一个调用改动文件，旧 hash 的补丁必须冲突，不能覆盖新内容。
- 边界：terminal_read 的输出为空时等待；程序退出或调用取消后释放本次等待，不破坏终端输入生命周期。
- 失败：TLS 自签 CA 不受信时，TCP 阶段保持成功、TLS 明确失败、HTTP 跳过。

## 6. 必需验证
文件模块验证中文、CRLF、末尾无换行、空文件、无效 UTF-8、长行跨块匹配、预算、错误哈希、并发补丁及注入发布失败，断言原文件与临时文件状态。等待验证延迟输出、空等待、退出尾部数据、取消、关闭及真实 PTY；日志验证追加、轮转、截断、代际、句柄上限与空闲清理。巡检验证本机真实父子 PID/监听 PID、平台解析 fixture 和网络各阶段失败/取消/超限。
独立 HTTP JSON-RPC 验证工具发现、参数 schema、成功及 IsError/code/message，普通日志没有路径、URL、query、edits.text、运行时输出或 Token。全量 test/vet/race、六组合无 CGO 构建及真实二进制冒烟遵循质量规范；Windows/macOS 原生未运行必须标为未验证。

## 7. 错误方式与正确方式
错误：在只返回部分行后对预览计算 hash，拿此 hash 作为全文件修改前提；或先删除旧文件再写新内容。
正确：完整有界扫描原字节生成 SHA-256；校验原内容后写同目录临时文件，发布前复核并原子替换，失败保留原目标。

错误：先检查缓冲为空，再不加同步地订阅下一次通知；或日志轮转后延用旧 cursor。
正确：检查与订阅遵循同一锁序，写入唤醒所有读取者；轮转明确切换 generation 和偏移。
