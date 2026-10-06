# GUI 原生验收入口最终复审

日期：2026-10-06。基线 main `38a37b8`，分支 `codex/gui-native-validation`。结论：**离线审阅通过，可由主会话开始已授权 Windows 专用窗口实跑**；本报告不表示 Windows 桌面验收已经通过。

下列初版审阅记录保留；首次真实运行失败后的后续补修与最终门槛见文末，不能沿用初版检查替代新源码检查。

## 范围与规范

按 check.jsonl → PRD/design/implement 加载全部上下文，再执行 packages 查询；单仓库只有后端适用，无业务前端。读取 backend 索引及 Quality Check，核对 GUI/MCP/错误/日志/生命周期规范、历史 Windows 助手证据与 Linux 就绪研究。

审阅 `scripts/native-smoke` 新旧 Go 文件及测试、`windows-native-smoke.py`、新增薄入口与两组 Python 回归、两份 README、GUI 新七段契约及当前任务文档。生产 internal/cmd/依赖无本轮修改，未重复无关全仓检查。产品仅支持 Linux/Windows；当前按用户要求先推进现有 Windows/KDE，GNOME/X11 无环境继续待验收，不安装新桌面。

## 已核对的行为

- 默认 legacy/P0 仍运行 Token/匿名四轮，报告 GUI=false，响应预算 2 MiB；GUI 仅由显式 `--execute`/助手 `--gui` 进入，两轮独立临时回环服务。无 execute 不连接远端，包选择模式不能混用，GUI 的 24 MiB 协议预算不改变默认预算。
- 使用独立 JSON-RPC，明确发现七个 GUI 名称并查询 Windows/available；Token 轮另验无凭据 401。唯一目录、上传校验、严格 UTF-8、有界双流尾部读取和精确受管进程清理复用旧助手。
- Win32 结构按两种 64 位架构自然对齐，固定回调复用；UI 线程锁定并启用 PMv2，几何查询也临时启用 PMv2。每次输入前确认本轮 HWND 前台身份，按键/文字通过有界 UI 消息确认本轮 EDIT 焦点，不在工作线程直接 GetFocus。
- 只请求专用客户区裁剪；PNG 在解码前校验尺寸/像素预算，Base64/PNG/协议均有上限，完整解码并核验四角颜色、区域、时间、新鲜度及代次。输入使用该 capture_id 相对坐标；真实窗口消息核验点击、双击、拖拽端点（±2 像素）和精确水平/垂直滚轮 tick。
- Ctrl+A 由专用 EDIT 兼容全选，Backspace 仍由真实控件执行；WM_GETTEXT 逐字比较中文和 emoji，随后再截图。工具 submitted 或图片变化单独都不能替代实际事件/文字断言。direct 模式不调用 Clipboard API，不把它记为保存恢复通过。
- 启动失败/超时持有夹具对象，取消后唤醒本轮窗口或 UI 线程并有界等待。成功和异常均销毁窗口、注销类；清空专用消息队列、恢复线程 DPI、解锁并完成 done 后，close 再检查窗口消失及 failed，才构造 window_cleaned=true。
- gui_close/repeat-close、窗口、临时服务、目录及原入口复查均完成后才发布最终成功报告；Windows 服务终止明确标 terminate_process，不称优雅退出。报告仅含事件/尺寸/布尔值及 PNG 摘要，不输出图像、文字、Token 或原内容；普通日志另查测试内容与 Token 不泄漏。

## 发现与修正

预审反馈的两项已由实施代理在冻结前修复：启动超时不再丢弃窗口线程；GUI 模式不再只凭旧 40 工具计数，而是检查七名称和实际状态。最终审阅未发现其他确认的代码或规范缺陷，审阅者无需再次修改实施源码。旧历史失败报告没有覆盖。

## 验证

- 审阅者最终 `go test -race ./scripts/native-smoke -count=1`：6 项通过，1.640 秒。使用获准临时回环 HTTP，未连接 VM、GUI 或剪贴板。
- 审阅者新旧 Windows Python 回归：14 项通过；gofmt、diff-check 通过。首次 vet 因默认 Go cache 只读失败，改用 `/tmp/remote-mcp-gui-review-cache` 后定向 vet 通过，属于执行环境限制。
- 复用实施者冻结源码结果：普通 helper 6 测试、Linux/Windows 双架构 vet、两 Windows 架构 CGO=0 build/test-c、py_compile 均通过。Windows ABI/预取消线程测试仅已编译，尚未在 Windows 运行；不能当成原生或 Windows race 证据。

## 原生验收边界

本代理未访问宿主桌面/剪贴板或 Windows VM，没有新增原生成功证据。Windows/amd64 的实际点击、文字和清理须待主会话两轮运行；Windows/arm64 仅构建。既有 KDE 第八轮仅截图/160% 缩放/键鼠子集，第六轮完整恢复失败继续保留。

四类桌面整体、多屏混合缩放/布局变化、授权撤销、正常全 MIME 剪贴板恢复及关闭后可读性、实际 MCP 客户端图片展示继续 pending；隔离嵌套/模拟/offscreen/构建或单次工具成功不能消除这些缺口。任务保持 in_progress；未提交、归档或修改用户 `.opencode/package.json`。

## 第二版：首次实跑失败后的最终复审

再次按同一上下文与 Quality Check 核对全部新增助手/诊断/测试、薄部署、两份 README、更新 GUI 规范、任务文档及 `native-validation-2026-10-06.md`；历史研究继续保留。Linux 源码仅将 GUI 报告的五桌面待验文案改为当前 Windows/X11/GNOME/KDE 四类，复用主会话 24 项脚本回归，不访问 GUI。

确认通过的新增链路：

- WM_PAINT 的原生绘图 API 结果纳入失败位；截图前仅对本轮窗口同步 Redraw 一次，未增加截图或输入重试。仍须以实际 PNG 的四标记各 49 像素精确比对判定，绘图成功不替代最终呈现。
- 诊断仅扫描 ≤100 万像素专用 crop；Python 在输出前限制失败 JSON ≤16 KiB，按精确字段/阶段/固定四色/整数范围/产物哈希及清理标签白名单验证。只输出四中心 RGB、49 像素偏差、固定颜色计数和数值元数据，不输出图像、原文字、Token、HWND 或目录。
- Go 清理通过固定标签与原始失败并存，Python 独立输出 cleanup_error 并保留 primary_error 的异常链。既有 Cmd.Wait 结束后仅对本轮精确目录最多 5 秒等待删除，不扫描、杀其他进程或读取其他 state；任一失败均不发布成功。
- 本机 ZIP 内 server/helper SHA → 远端 Get-FileHash → 助手 `[root,--gui,server_sha256,helper_sha256]` 核对自身所在目录与实际两文件 → 每轮执行前再验固定 server 路径；成功报告还核对助手哈希证据与本轮上传一致。常驻服务不替换，默认旧四轮入口及 2 MiB 预算不变。

**发现并修复**：`gui_windows.go` 的启动失败/超时仍先调用 stopAndWait 再 panic；若回收也失败会覆盖启动原因。两个分支现接入 `gui_trace.go` 的 failFixtureStartup，以 window_cleanup defer 保留原启动失败并记录独立清理标签。新增行为回归覆盖启动失败/超时 × 清理成功/失败四组合，核验仅停止一次且原原因始终保留。本修复未修改输入、标记或资源生命周期设计，无其他确认未修代码缺陷。

最终补修后审阅者检查：helper race **10 项通过，4.370 秒**；Linux 和 Windows amd64/arm64 定向 vet、两 Windows 架构 CGO=0 build/test-c、gofmt/diff-check 均通过。新旧 Python **17 项通过**，补修没有再改 Python；复用实施者 py_compile。Windows 专属测试仍仅编译，未运行原生测试或 Windows race。源码已再次冻结，可由主会话重建/打包后实跑；旧版本运行门槛不当作新版本已运行成功。

真实证据边界：Windows 首次精确 marker 核验失败，finally 精确目录清理暂失败；主会话后来核对本轮 helper/server 进程数 0、成功删除精确目录及原入口 40 工具仍可用，不能断言持续泄漏。标记根因仍未知，不能用新增诊断宣称已经修好或验收通过。

KDE 私有嵌套已取得真实 PNG，并在布局变化后禁用绝对映射、0 次输入，属于保护证据而非输入闭环。GUI 关闭后提供者自读仍匹配，但独立观察者 15.2 秒/31 次 active 样本为空；没有 gui_text，不能归因于文本恢复路径。提供者缓存及 ownsClipboard 不证明 compositor 接受 offer，规范和 README 已要求刷新后、GUI 关闭后、提供者退出后分别独立读取；初始早期空读不作为确定丢失原因。正常完整 MIME 恢复仍 pending。具体主会话实机事实见 `native-validation-2026-10-06.md`，本代理未访问宿主或 VM。

## 第三版：三路像素定位与呈现准备

主会话第二次实跑已确认新 server/helper 哈希一致及清理正常，但 initial_capture 仍失败：paint_count=2、失败位=0，四个 7×7 均49像素不匹配、四种精确颜色计数全部为0。原常驻40工具仍可用；不能把绘图调用成功写成呈现成功，也没有确定窗口/DWM/服务采集的根因。

最终核对新增链路：每次专用 crop 共享3秒 context、间隔50毫秒、最多61次；每次实际采集与DC采样前后均核对同一前台 HWND 和客户区几何，任何守卫失败直接中止。rpcContext 使用 NewRequestWithContext，使实际 HTTP 读受该期限约束；同步 Redraw 取剩余期限且最多1秒。每次仍精确验证4×49像素，最终合格截图才用于随后一次输入，未重试输入或接受旧帧。旧/P0 rpc 保持 Background 和原客户端期限。

新增像素定位仅在同一锁定 PMv2 线程取得/释放本窗口DC和桌面DC，桌面仅采样专用客户区的四个对应点；CLR_INVALID 明确 valid=false，另查本 HWND visible/iconic/cloaked 及有效位。白名单严格限定这些数值/布尔、固定字段和attempt 1～61，16KiB总限额保留；没有未知 HWND 查询、整桌面图像、原文字、Token 或路径输出。三路不同时采样，不能用过渡差异直接认定生产缺陷。Win32同步调用和PNG校验不由Go context硬抢占；期限约束后续工作及成功发布，不声称它能强行中断任意原生API。

**发现并修复**：好帧取得后原成功条件先查ctx再遍历像素，取消若发生在校验中仍可能接受该帧。现先完成精确像素校验，再检查ctx；新增在 Image.At 校验期间触发取消的回归，证明取消的好图拒绝。主会话已将规范“初次准备”同步为“每次专用窗口截图准备”，与代码一致。无其他确认未修代码缺陷。

最终源码审阅者 helper race **13项通过，2.262秒**；Linux及Windows amd64/arm64 vet、双Windows CGO=0 build/test-c、Python17项、gofmt/diff-check全部通过。真实HTTP取消与等待/61次/守卫失败回归通过仅证明离线行为；Windows特有原生调用仍只编译。源码再次冻结，可由主会话重建打包后进行第三次实跑；本代理未连接VM或操作GUI/Clipboard，尚未增加原生成功结论，所有既有pending及失败证据保持。

## 第三次 Windows 实跑与 KDE 分段入口复审

主会话报告第三次 Windows 原生运行通过，证据为 `/tmp/remote-mcp-windows-gui-20261006-03.json`：Token/匿名两轮的实际 crop、坐标、鼠标/按键、中文和 emoji 控件内容、重复关闭、窗口与部署清理、原入口复查均通过。Clipboard 未访问，acceptance_complete 仍为 false。本代理没有再次连接 VM 或运行原生测试；此项是主会话提供的真实结果，不覆盖前两轮失败，也不证明 Windows arm64、多屏、撤权或剪贴板恢复已经通过。

按当前任务上下文和 backend Quality Check，离线审阅 `/tmp/remote-mcp-nested-segmented-ilwa4usz` 的分段入口、独立 reader、固定 MIME provider、私有启动链与离线回归。生产源码和已冻结 Windows 助手未改。确认私有环境来源和 PID 守卫、布局稳定、人工 Portal 授权、专用标记窗口定位/焦点和真实事件；刷新后的独立全 MIME 核验失败会阻止任何 text/key。关闭后观察者仅等真人点击，不借已关闭服务输入。provider 退出前核对本轮 PID、启动时间与私有环境，失败保留提供者及夹具，不扫描或杀其他进程。环境要求至少20分钟余量，清理仅覆盖本轮资源。

**发现并修复两项临时脚本缺陷**：分段入口原复用通用 1..605 秒参数，只把默认设为120秒；现私有参数实际限制为1..120秒。独立 reader 原直接创建结果 JSON 后写入，父进程可能读取半份结果；现先写临时文件，再原子替换最终结果。均有中文注释，未改变生产接口或权限流程。

定向验证通过：原离线失败流程证明独立刷新失败时没有 text/key、GUI关闭且固定源与夹具保留；新增离线边界检查证明1/120秒接受、0/121/605秒拒绝；提取实际 reader finish 函数的离线检查证明最终 JSON 只在写完后发布，临时文件消失。七个临时 Python 文件 AST 语法检查通过。没有导入或启动 Qt GUI、访问宿主 Clipboard/PRIMARY、调用新 Start 或操作 VM；无额外类型检查工具适用于这些临时 Python 文件，未重复无关全仓测试。

**执行门槛通过**：主会话执行前仍需实时复核私有组件 PID、DBus owner、socket/环境来源与租约余量，并确认显式 provider PID 已经由真人蓝区点击发布（published/native_mouse_input_verified 为 true）。本代理仅核对 readiness 的有限 PID/哈希投影，未将历史记录视为持续存活保证。读者全 MIME 基线和刷新后核验仍是运行中的硬门槛，不可用提供者自身缓存替代。正常 KDE 文本保存恢复、关闭后及提供者退出后独立可读性仍 pending；没有其他确认但未修的代码缺陷。本轮未提交、归档或修改 `.opencode/package.json`。

## KDE 模式编号漂移修复：生产与新私有入口最终复审

按更新 check.jsonl、PRD/design/implement、packages 和 backend Quality Check 复审 `layout_portal_linux.go`、新增布局回归及旧 Linux/私有总线回归、GUI 规范和私有临时入口。真实快照中70→74→84仅模式实例编号变化，不能继续对完整配置取摘要；授权全过程未保存，历史失败仍不证明期间没有短暂真实变化。

当前投影先验证完整输入的深度/节点/字典/数组/字符串/字节限额及有限数值，再严格解析显示器身份、连接/启用、位置、尺寸、缩放、旋转、当前模式尺寸和镜像关系。模式ID仅作唯一查找键，不进入摘要；输出和clones按稳定身份排序。inactive输出可缺当前模式，身份与状态仍保留；screen.currentSize与output.size分别保留，不要求二者相等。GNOME仍取serial，公开工具与剪贴板逻辑未改。

读取、configChanged和主动复查共用同一投影，真变化、owner替换、断线及畸形KDE通知仍永久禁用映射，回到旧几何不复活。克隆按官方其他输出ID与对称关系校验，复制来源0为无source；拒绝悬空、自指、非对称和复制循环，正常双输出镜像已纳入回归。

**复审反馈后已由实施者修复**：KDE读取原只拒绝空Body，会接受配置外的额外返回字段，与信号的单Body检查不一致；现仅KDE要求恰好一个返回字段，并以实际私有D-Bus多返回值方法验证拒绝。旧valid/scale简化mock更新为真实KScreen结构，畸形通知回归改为fail-closed。没有其他确认未修缺陷，本代理未并发改实施者源文件。

冻结源码全包 `go test ./...`、`go test -race ./...`、`go vet ./...` 均通过；四组合×两个命令 `CGO_ENABLED=0` 构建通过，产物位于 `dist/{linux,windows}-{amd64,arm64}`。GUI Python24项通过（单独复跑1.068秒），Windows Python17项通过；gofmt与diff-check通过。全包首次沙箱运行因socket禁止失败，获准回环环境复跑通过，无自动审批拒绝；Python首轮并行冷编译期间offscreen子进程10秒超时，未改测试或期限，编译完成后原样复跑通过。实施者最终完整私有总线GUI race5.512秒通过，独立于普通测试跳过该组的结果记账。新Linux二进制的Token/匿名既有临时服务冒烟均ok=true，不访问桌面。

`dist/linux-amd64/remote-mcp` SHA256为 `bb010f9e42c75748bb15479a1d7d3b5518bb9125fd3c8fcff983c5500c5c1bfe`；主会话已复制并核验 `/tmp/remote-mcp-gui-native-current`。新私有目录 `/tmp/remote-mcp-nested-segmented-6q4_291z` 保留此硬哈希门槛、原环境/PID/租约和人工许可边界。临时助手提取实际生产归一化与几何函数，GLib仅解析单配置tuple，不另写几何规则；结果sourceSHA与当前源核对，stable仅比较几何摘要和owner，仍保存原快照。

主会话发现的临时全局canonical函数被Clipboard摘要覆盖已由实施者修为canonical_geometry/fixed_summary；baseline构造后再次实际调用layout的完整mock回归通过。审阅者在新目录离线验证70/74/84快照摘要相同、畸形tuple及sourceSHA不匹配拒绝、新旧助手字节相同、全部Python AST通过，并复跑独立刷新失败阻断text/key及保留来源的流程。生产布局源SHA256为 `d4f8959265771b9a386fbab7bb410a9f3db0d2bc98b9d219bb5791ed63eb936f`。

**最终结论：可由主会话启动新私有bootstrap，再按实际PID/owner/socket/产物与人工provider/Portal门槛运行分段验收。** 本代理未启动新环境、发起Start、操作GUI/VM或读取宿主Clipboard。新投影只有自动和已保存快照证据，KDE正常中文恢复及关闭/来源退出后的独立可读性仍待真实验收；Windows第三次结果保持原范围，多屏、撤权、GNOME/X11等pending不增加。未提交、归档或修改 `.opencode/package.json`。

## 新 AGENTS 规则：项目 `.tmp` 重建入口复审

用户要求所有新临时文件位于工作目录 `.tmp/`、禁止访问系统临时目录后，本代理不再访问或复制旧临时环境，历史报告保留。仅复审从持有上下文与仓库重建的 `.tmp/kde-native-a` 启动、监管、私有入口、固定提供者、独立reader、分段脚本、同源助手和离线回归；核对 `.gitignore` 的 `/.tmp/`、质量规范及任务位置调整说明。生产源码保持冻结，没有重跑全仓或Windows原生验收。

**复审发现并由实施者修复的重建回退**：私有授权参数原又复用通用上限，现恢复真实1..120秒校验；fresh目录同时拒绝已有outer-pid，防止readiness前重复启动；run-private只接受精确本轮bus路径或其逗号参数形式；发布ready前再次核验四服务owner PID等于本轮受管child；固定提供者真人点击超时保留Qt退出码，不再误报exit0。失败诊断恢复受控RPC字段投影，按实际 `clipboard_restore=failed` 枚举记录，不复制message、参数或Clipboard内容。无其他确认未修代码缺陷。

启动使用固定 `/usr/bin:/bin` PATH调用现有系统二进制，不继承宿主PATH；HOME、XDG、PipeWire以及TMPDIR/TMP/TEMP均指向项目私有目录。Go助手构建关闭GOENV和模块下载，GOCACHE/GOTMPDIR和输出均位于项目 `.tmp`；标准库最小Variant载体保留生产Value解包语义，提取当前生产函数，逐次验证sourceSHA。没有手写替代几何规则或放宽限额。目录、runtime、temp、助手权限0700，固定bus和媒体socket最长75字节，小于Unix socket的108字节边界。

最终冻结版离线回归通过：独立刷新失败阻止text/key、GUI关闭及来源保留；baseline后仍可实际调用layout；failed恢复枚举保留而message不写入诊断；不足20分钟在创建证据或授权前拒绝；1/120秒接受、0/121/605秒拒绝且不执行。实际GLib wrapper解析仓库KScreen快照、模式编号等价/缩放变化/非法模式与限额/sourceSHA不匹配拒绝均通过。审阅者额外验证实际reader写入函数只在完整JSON写完后原子发布，全部新Python AST和diff-check通过。类型检查复用实施者本轮同源标准库Go助手构建，未引入Python静态类型工具。

验证命令显式设置项目TMPDIR/TMP/TEMP、GOCACHE/GOTMPDIR及PYTHONDONTWRITEBYTECODE，测试TemporaryDirectory也指定项目目录；未启动Qt GUI、Portal或桌面、未读取Clipboard/PRIMARY或旧系统临时路径。`.tmp/remote-mcp-gui-native-current` SHA256仍为 `bb010f9e42c75748bb15479a1d7d3b5518bb9125fd3c8fcff983c5500c5c1bfe`，生产布局源SHA256仍为 `d4f8959265771b9a386fbab7bb410a9f3db0d2bc98b9d219bb5791ed63eb936f`；检查时新readiness/outer-pid尚不存在。

**当前执行门槛通过，可由主会话启动 `.tmp/kde-native-a/start.py`。** 启动后必须实时核对私有组件PID/owner/env/socket和产物，再由真人发布固定来源、批准本次Portal；独立全MIME门槛失败仍禁止text/key。该结论是新入口的离线安全门槛，不是KDE中文恢复原生成功；全部既有pending保留。本轮未提交、归档或改用户 `.opencode/package.json`。

## 工作目录首轮失败后的呈现等待与备份保留复审

主会话新工作目录首轮实跑取得可靠映射、一次真实鼠标事件，以及刷新前和GUI关闭后的独立三MIME/95字节核验；后续精确标记失败，未执行refresh、text或key。截图中窗口叠加支持呈现过渡的推断，尚不能唯一确定根因。旧报告瞬时fixture_retained=true也不证明runner退出后夹具仍在；主会话后续确认夹具已经退出。本轮没有刷新，因此固定provider仍是来源，不能据此声称唯一恢复备份已经丢失。真实失败与中文恢复pending保持。

本轮仅复审项目 `.tmp/kde-native-a` 的呈现等待、独立reader就绪投影、失败保留及定向回归，生产和Windows冻结源码未改。每次截图准备共享3秒、最多61次、间隔50毫秒；初次人工焦点预等待仍最多120秒，非首次不再有额外10秒focus或重复mapping等待。专用目标的前后PID、活跃焦点、新鲜ACK及布局守卫保留，精确四色和几何容差不变，另以固定白/灰区域签名区分fixture与reader。仅呈现标记不合格可等待下一帧；取消、网络、权限、布局或焦点错误直接失败，合格截图仅触发一次输入，不重试输入。

**复审发现并由实施者修复**：

- 标准HTTPResponse正常读完后会清空fp；原循环再次取socket将正常EOF误报为无法约束传输。现缓存初始socket并先判断isclosed，Content-Length和chunked真实标准库内存链均有回归。
- refresh已发布但返回错误或格式验证失败时，原clipboard_refreshed仍为false，会提前终止可能持有唯一备份的夹具。现发请求前记录clipboard_refresh_attempted，失败先关闭GUI，再实际保持runner和夹具到RUN_STOP、环境STOP或原租约；不输入、不重新发布，也不延租。
- JSON解码和证据保存期间越过期限时，原成功路径仍可能发布可用capture。现解码后再次检查期限，截图先保存只读元数据、检查期限，最后才发布capture/point；晚解码和晚保存回归均证明0输入。
- 按主会话明确预算口径，移除非首次shot之外重复focus/mapping；新增AST防回退和所有后续ACK timeout≤3秒断言。

最终审阅者复跑 `test-preflight.py` **0.895秒**、`test-offline.py` **0.710秒**均exit0。覆盖过渡后精确帧仅一次输入、持续过渡/取消/超时/布局变化/网络/失焦/错误reader PID拒绝、真实HTTP读取期限与正常EOF、晚成功拒绝，以及独立刷新失败、refresh返回错误和格式验证失败均0text/key并真实进入有界hold循环。RUN_STOP和租约结束路径、原1..120秒授权/20分钟环境余量/同源摘要门槛保持通过。五个受影响Python文件AST与diff-check通过；这些临时Python无另设静态类型检查器，未重复生产全仓检查。

检查缓存与临时文件全部位于项目 `.tmp`，命令显式TMPDIR/TMP/TEMP和PYTHONDONTWRITEBYTECODE。最终segmented SHA256为 `a1306cdfff32627c6ae36b9ad578d98243b4bf2722bf6eaaaecaf8083e20dddd`，frame为 `82170da108c2c69419ddbecc202feaf712d30b00a4cc2eca907300cc15af7127`，reader为 `96acc7b766c88b43097cd8383b47e9fd9cdfcfa1d28bae4c161862894215b81b`。没有其他确认未修代码缺陷。

**新冻结版离线执行门槛通过。** 主会话可按校验和同步至新工作目录，重新核对私有PID/owner/环境、产物和租约后执行。3秒期限约束后续工作与成功发布，不声称能硬抢占任意同步解码或文件操作；hold仅保证runner实际存活且原租约有效时保留，不抵抗外部kill或监管回收。本代理未启动桌面、操作GUI/Clipboard、访问旧系统临时目录或改动运行证据/STOP；正常KDE中文恢复及来源退出后独立可读性仍待真实验收。未提交、归档或修改用户 `.opencode/package.json`。

## 私有b首帧代次守卫的定向复审

主会话第二轮真实运行经人工授权后，在第一张PNG的准备守卫失败，0输入，GUI关闭、刷新前夹具清理。已提供的PNG与后状态均为generation=2、ready且绝对映射有效，尺寸2048×1526、逻辑1280×954、frame_sequence=1；旧入口没有保存before状态，无法从现存文件唯一还原初始像素或排除未记录变化。本代理未访问当前b桌面或运行证据，依据主会话摘要与生产源码复核，保留本轮真实失败。

`wayland_linux.go` 的 Capture 首次把未知像素尺寸0/0更新为实际帧尺寸；仅原像素已知又改变时禁用绝对映射。`manager.go` 的 refresh 因完整Display改变递增generation，Screenshot在采集后再次refresh并发布新代次的capture。因此私有脚本一概拒绝before/after代次差异会误拒合法首帧物化；公开旧capture拒绝规则不应放宽，本轮生产未改。

实施者新增有限frame_status投影和validate_frame_transition：同代仍要求完整Display集合与顺序不变，截图session/display/逻辑几何/尺寸/全屏region及代次与after一致。唯一例外限Wayland会话第一帧frame_sequence=1、generation恰+1，仅选中display的双零像素更新为截图实际正数；名称、primary、身份、逻辑几何、absolute_input和其他显示器均不变，整个会话只能消费一次。每attempt保存有限before/after证据，不复制错误message或能力reason；前后焦点/PID与共享3秒的保存后成功发布检查继续保留。

**发现并自修测试缺陷**：新增的移除/新增显示器、错误session、缺少截图字段及巨数用例被额外错误display_id提前拒绝，未独立证明目标分支。现仅专门的same_gen_capture用例改变display_id，其余断言仍为原ID；新增真正独立的状态巨数回归。实施者的数值校验已先执行绝对值限额再调用isfinite，状态与capture的巨大整数均以RuntimeError受控拒绝，不依赖浮点转换异常。没有其他确认未修缺陷。

最终定向 `test-preflight.py`、`test-offline.py` 均exit0，四个受影响Python文件AST及diff-check通过。真实首帧shape的完整shot回归落盘before双0/generation1与after2048×1526/generation2、逻辑不变，且仅一次mouse。已知尺寸变化、部分零、逻辑/身份/顺序/其他显示器变化、缺字段/重复或增移显示器、错误session/capture/region/代次、跨两代、后续frame及巨数独立拒绝；原取消/期限/HTTP/失败hold回归保持通过。无新增Python静态类型工具，不重复冻结生产全仓检查。

最终segmented SHA256为 `f028404a25f50d66c7cbe11b41aa5aa245dedb6c7fd161756d5abeb64edd9595`，frame为 `2f03b786f459db41267658f265a5c5f72f14acf75cf28bcd104e2ed4a352fd85`，test-preflight为 `38594fcc94a4baf032e424cab9fc2dbdeb88fe1158d6ef232619e258d9ddde60`。**定向安全门槛通过，可由主会话核验同步版本、私有来源与原租约后决定下一次实跑。** 本代理只使用项目 `.tmp` 做离线检查和机械测试修复，未操作GUI/Clipboard、权限、START/STOP或运行证据。首帧例外尚未获得修后原生证据；正常KDE中文恢复及关闭/来源退出后的独立可读性仍pending，未提交、归档或修改 `.opencode/package.json`。

## 私有提供者呈现与真人点击门槛复审

主会话提供的私有c记录：120秒内未收到点击、提供者退出1，用户随后报告没有白色窗口；没有gui_open或分段输入，环境已STOP并确认受管组件退出。旧入口没有启动时visible/exposed/paint证据，不能断言此前始终可见或把超时归因于用户。本轮只复审 `.tmp/kde-native-c/fixed-mime-provider.py` 和离线测试，不改生产、segmented或frame，不访问实际桌面/剪贴板。

新增状态逐份原子发布、最多4096字节，含全局及PID记录、原租约、时间戳、窗口handle/visible/exposed/active和paint_count；show成功不等于呈现成功。只有自发左键press与release两端均active，且提交前visible/exposed/paint/active再次成立才发布固定三MIME95字节；此前没有app.clipboard调用，也不读取Clipboard/PRIMARY。点击最多300秒并受原租约约束，发布后最多1800秒且不超过原租约；STOP与信号仅结束本进程，不延租。

**发现并自修两项机械缺陷**：后续无效release会清当前clicked，原readiness因此错误撤销已经发布的历史真人输入证明；现以published或clicked记录，回归确认发布后无效事件不改变既有证明且不重复Clipboard调用。原最后一次window_status读取前才查期限，读取跨过租约后仍可能发布；现最后状态守卫后再核验期限/STOP，模拟晚状态读取证明0Clipboard调用。

最终完整Qt模块mock入口 **16个场景**及实际原子replace失败/4096字节限额回归通过，审阅者复跑exit0、0.164秒。覆盖show但未exposed、paint与exposed各自缺失、无press、两端失焦、合成/错误按钮、迟到点击、一次发布、STOP、短租约、payload及最终状态读取跨租约、发布后寿命；成功路径固定内容哈希/字节不变、报告与PID状态一致。两文件AST及diff-check通过；未初始化真实Qt或剪贴板，无另设Python静态类型工具，不重复冻结生产全仓检查。

GUI规范新增首帧例外与真人呈现两条与实现一致：状态只证明原生窗口自身可见/暴露并曾绘制，不证明宿主外层无遮挡；超时退出后不能继续请用户点击已销毁窗口。最终provider SHA256为 `5222fe3bb620bd628e03019a3e2e576728bbff6b52f66d7ca218dca8ebef3a33`，test为 `7b864e977ee5f66bcf122ac544ce066b67ada254ee43424ebabbc264c0a7c978`。**没有其他确认未修缺陷，离线执行门槛通过。** 主会话可仅同步冻结两文件至新d，实时核验原租约/组件与呈现状态后由真人点击；此结论不证明d窗口已实际呈现或KDE中文/剪贴板恢复已经通过。本代理未启动Qt/桌面、授权、访问系统临时目录或改动运行状态，未提交、归档或修改用户 `.opencode/package.json`。

## QLabel传播与有限拒绝诊断复审

主会话报告私有d提供者实际visible/exposed/active为true、paint_count=3且新鲜，用户点击后仍未发布；本次没有事件尝试记录，不能唯一确定实际拒绝路径。主会话已精确停止provider/wrapper、保留桌面原租约，本代理未访问该桌面。旧失败记录保持，不把新的离线修复写成实际发布成功。

官方同版本 [Qt6.11.2 QApplication鼠标传播源码](https://raw.githubusercontent.com/qt/qtbase/v6.11.2/src/widgets/kernel/qapplication.cpp)在首次接收者处理后清除原事件spontaneous，向父级构造副本时读取该标志；因而QLabel传播至Provider可拒绝合法原生事件。两说明标签现设WA_TransparentForMouseEvents，使它们不作为鼠标接收者；该语义与[Qt属性文档](https://doc.qt.io/qt-6/qt.html#WidgetAttribute-enum)一致。本轮仅核对c的provider/test，保留press/release两端spontaneous与active、成对左键、呈现、原租约/300秒、提交前最后期限检查及一次固定三MIME发布。

诊断仅新增饱和至2³¹−1的press/release计数和最后一个事件的固定kind/button/reason及布尔字段；没有坐标、文本、Clipboard内容、Token或累计事件列表，原逐份原子JSON≤4096字节仍生效。新测试同时断言两个标签透明，模拟标签区域点击只发布一次95字节，非spontaneous/跨焦点/非成对事件仍拒绝，计数饱和不增加Clipboard调用。没有确认新增未修缺陷，本代理无需改实现。

审阅者复跑 **17个完整mock入口**及实际原子输出/4096字节限额检查，exit0、0.264秒；两文件AST及diff-check通过，没有初始化真实Qt、桌面或Clipboard。最终provider SHA256为 `1fa87253a85ad834180a4619f45d5f79401efc9143788f179d8eaf39dce61d96`，test为 `7fec0f33ac1fa80d62079cdc4990c289fd5b60849da8c631719eb1f65dc6b5d0`。**定向离线门槛通过**，主会话实时核对租约至少1200秒及组件/版本后可同步实跑，不足则新建空环境，不延租或放宽点击判据。所有临时位置保持项目 `.tmp`，生产和分段入口冻结；正常KDE中文及完整恢复验收未增加。未提交、归档或修改用户 `.opencode/package.json`。

## 纯内存像素解析与逐attempt诊断复审

主会话私有e完整失败报告保持：首帧初始化、真实鼠标和独立before-refresh三MIME95字节通过，后续截图准备超时，0refresh/text/key；关闭后真人独立读取仍匹配。旧同名PNG被末attempt覆盖，现存合格第二帧不能唯一说明第一帧拒绝原因。旧Python像素解析耗时约1.94秒可能耗尽共享预算，迟到输入被正确拒绝。主会话已按原租约STOP e、outer/provider退出0，新空f尚未provider/gui_open；本代理只回放项目中已保存PNG，不操作运行状态。

定向复审c的fixture_frame、segmented、两既有离线测试和新增test-decoder，核对当前任务实施范围与研究。PNG签名、块CRC、IHDR尺寸/颜色类型、非交错、完整扫描行、过滤器、inflate长度与EOF及16777216像素上限保持。自动定位仅接受8位RGB/RGBA；校验后只将IHDR/IDAT/IEND交QImage纯内存解析，辅助ICC/颜色空间/文本等元数据不交native。原有Python有界inflate与Qt内部第二次解码分别记账，不继续声称一次解压。native尺寸与输入核对，RGB888行跨度只允许实际像素加0..3字节padding，总字节数必须等于stride×height；一次RGB转换结果同时供标记和签名使用，不创建QApplication。

bytes.find计数只接受RGB像素对齐且未越行的匹配，排除padding/跨像素字节；RGBA忽略alpha的原语义保持。精确四色计数、几何和固定签名未放宽。每attempt独立PNG、元数据、前后状态和诊断文件；诊断仅固定阶段/目标/拒绝枚举、计数、签名RGB与阶段耗时，没有原文字、任意错误message或图像内容。网络/权限/布局/失焦失败不重试，原3秒、61次、50毫秒、首帧唯一例外、保存后取消/期限检查及合格后一次输入保持。

审阅者依次复跑test-preflight、test-offline、test-decoder，全部exit0；五文件AST及diff-check通过。覆盖RGB/RGBA、五种行filter、全部alpha值、奇数宽padding、跨像素假匹配、辅助块剥除、畸形CRC/扫描行/尺寸/限额，以及与固定SHA旧解析和三张既存PNG的完整定位等价；三张回放native解析与定位约35.8～50.7毫秒，性能仅记录，不作脆弱通过阈值。取消、晚JSON/报告/诊断保存、首帧与真实变化、唯一输入及失败来源hold回归保持通过。没有确认新增未修缺陷，也没有本代理源码补修。

| 冻结文件 | SHA256 |
| --- | --- |
| fixture_frame.py | e806f8ddfe4eba80df550b138af779426af0ecaf3e74a51c758786a2e6052853 |
| segmented-smoke.py | ed86a30533e27edc3c9804567bf0eb474fe313a9304f3c12dc7374fb8de3debc |
| test-preflight.py | dd8af035ce8664b7c6c719f279bb0c2c1c44762da8ea1e68a9c8941120b11931 |
| test-offline.py | 7886f1ca31f6d0d577cd38e3938cb58ab222e2f6882d20b990810e279410efc6 |
| test-decoder.py | ce0fc64faeaf61a032171d3c4819e61a056d6bc8f3a1a13005d30f27cd64a105 |

**新冻结版定向门槛通过。** 主会话可核验版本、私有组件及原租约后同步新空f，再由真人发布固定来源并按实际Portal流程运行。全部命令显式使用项目TMPDIR/TMP/TEMP和PYTHONDONTWRITEBYTECODE，不访问系统临时目录、GUI、Clipboard或权限；未重复冻结生产全仓检查。同步解码/文件调用没有硬抢占承诺，预算仍约束成功发布；正常中文、完整恢复和来源退出后独立可读性继续pending。本轮未提交、归档或修改用户 `.opencode/package.json`。


## 原始MIME、来源世代与独立接收的定向复审

本轮按check.jsonl及当前PRD/design/implement续接要求复审c九文件，连同未变frame/decoder回归；生产及Windows冻结检查未重复。主会话私有f已有独立Qt视图三MIME95字节，但刷新后的原始alias/来源接管缺少协议证据；旧失败保持，不据Qt自缓存推定接管或恢复通过。新空g尚未启动，本代理没有读取运行桌面或操作Clipboard。

实际实现将raw offer、逐原始MIME读取哈希、Qt规范化视图和alias/cache解析分列。同版[Qt6.11.2 data offer源码](https://raw.githubusercontent.com/qt/qtbase/v6.11.2/src/plugins/platforms/wayland/qwaylanddataoffer.cpp)确实会在formats_sys规范化plain alias，并按请求键缓存读取结果；新门槛因此要求每个原始MIME都有同一新offer世代的真实receive，不能仅凭规范化plain视图虚构plain请求。协议过滤匹配[libwayland1.24实际日志格式](https://raw.githubusercontent.com/wayland-mirror/wayland/1.24.0/src/connection.c)，只留固定MIME、对象/世代、方向/事件、计数和父进程观测时间，不落盘原始日志、任意payload、文本或FD数字。wl/wlr/ext跨族允许合成器桥接，PRIMARY receive/publication拒绝；每次新reader精确PID、当前source token、发送/接收次数与哈希均校验。

来源send与reader生命周期的父monotonic区间只是保守观察证据；线程晚观察到的send落在区间外即拒绝，不声称逐FD配对或严格因果，报告明确fd_identity_proven=false。直接启动的Portal-KDE child PID与私有owner/env核验保持；STOP、原租约、1..120授权与20分钟环境余量没有延长。trace失败、独立刷新校验失败阻止后续text/key；attempted-refresh失败仍先关闭GUI并持有runner/固定备份到原租约或明确STOP，不救援覆盖不可证内容。Qt底层同步读取没有1MiB硬分配承诺；此测试只对固定有界来源的读取结果执行限额和哈希核验。

**发现并自修**：TracedProcess在Popen后首次原子发布或线程启动异常，调用方尚未登记PID，原构造路径可能遗留child；现构造边界只回收本次child或显式新建的受管组，TERM后wait最多3秒，必要KILL后再wait最多3秒，关闭stderr。不能确认回收时固定清理错误保留原初始化异常cause，不返回可用实例。另source_service原可接受已EOF producer的缓存current；现直接拒绝断开的生产者，正常完成的reader EOF仍可保留其已完成接收证据。

定向回归增加首次发布/线程失败×普通child/独立组×正常退出/KILL共8场景及无法确认回收的cause场景；三协议族producer EOF独立拒绝。正常8MiB累计输出预算边界测试确认预留failure空间首次明确落盘ok=false，后续停止发布不会让旧ok快照冒充健康；该测试以可达计数边界注入验证，没有实际生成8MiB日志。现有64MiB输入、4096字节行、4096事件、累计256对象创建、1MiB状态及有限输出守卫保留。

审阅者依次复跑test-client-trace、test-trace-reader、test-preflight、test-offline及test-decoder，**五套均exit0**。覆盖原始/规范化/cache区别、跨族桥接与FD非证明、source/offer新旧世代、PID/PRIMARY/限额、取消/租约/失焦、刷新失败hold，以及共享3秒/61次、首帧物化唯一例外、晚JSON与证据保存0输入和成功后一次输入。纯内存QImage和三张已有PNG等价回放保持通过，不创建QApplication；性能仅记录，不作为通过阈值。九文件及frame/decoder共11个Python AST/尾空白检查与git diff --check通过，无另设静态类型工具；未修改公共脚本、生产、文档规范或用户.opencode文件。

| 冻结文件 | SHA256 |
| --- | --- |
| client_trace.py | 1170c79a34662feef43f5ab98ca2d2ec0c8e4459b32e1d6c847939fd22db215c |
| inner.py | 4fb4b13a3b4411496c02f8a6de78b3ebaebec534f71ea166f1f29e143ae61597 |
| private-fixture.py | 003263d3b9525248fd5386f969c745114db234daba7272b07168032947986400 |
| transport-reader.py | b8968a60a5f9f4ce59380a171cb698cec5321c99218bcb98d6b873b04c8f80da |
| segmented-smoke.py | aa83de37699f5abd207efdedae909c57e4735bd5163694df13fd670eddd95385 |
| test-client-trace.py | 3d41cd2464facf7ad0be3df96fb468c2eb79ac43f05441b6e714ff7443a69929 |
| test-trace-reader.py | 044e017d4482b44c4f735a28d19d6e2c3038058fed9ec46149692db5c0687015 |
| test-preflight.py | 656fcaaf6c5edb80984cb4f7a6c28860440116e54e9dac920f693340f582f16b |
| test-offline.py | 259700949b440c84e2e39d7eba558cac6e48d82bd870f89b6f6e35c73c788860 |

**定向离线门槛通过，源码冻结；无其他确认未修缺陷。** 主会话可以按SHA同步空g，重新核验私有来源、owner/PID、产物与原租约后进行真人授权和实跑。KDE正常中文/恢复及关闭、fixture退出、全部来源退出后独立可读性仍pending，不以mock、缓存或tool成功代替；不可证空读保留失败。所有临时/缓存命令只使用项目.tmp及显式TMPDIR/TMP/TEMP、PYTHONDONTWRITEBYTECODE；本代理未访问系统临时目录、GUI、Clipboard、Portal或运行状态，没有提交或归档。


## Wayland1.26时间前缀与首拒绝诊断复审

主会话私有g在授权前出现unknown_protocol/sequence0，未启动provider、gui_open或输入；9个持有PID已退出，原bootstrap-07记录保持。原首行未保存，不能唯一确定本次真实路径。已确认本机版本与旧parser存在可重现兼容缺口；[Wayland1.26官方connection.c](https://raw.githubusercontent.com/wayland-mirror/wayland/1.26.0/src/connection.c)1475–1481明确输出HH:MM:SS及六位微秒。旧数值时间前缀不能识别该格式，registry的data_control参数可能被误判未知协议。此结论区分确定代码缺陷与旧g实际归因，不改失败记录。

本轮只审c/client_trace.py及test-client-trace.py：CLOCK26严格限小时00..23、分秒00..59、微秒恰六位，旧前缀保持。SENDER与完整LINE共用同一精确前缀；分片registry识别sender后丢弃，不把global参数当协议来源。新增宽松头部只生成拒绝分类，不能创建事件或证明；未知相关接口、ANSI/discarded、PRIMARY、畸形相关事件继续拒绝。

首rejection_diagnostic仅固定category/timestamp_syntax/interface/method枚举与布尔；未知名字映射other，不存原行、参数、实际时间、队列名称、FD或payload。首次失败字段固定，后续错误不覆写；失败原子文件沿现有输出预算只保留有界摘要，资源错误没有输入上下文。source世代/EOF、接收发送证明、精确启动回收、租约和取消边界保持，未改其他冻结文件。**没有新增确认缺陷，本代理无需修源码。**

审阅者复跑test-client-trace、test-trace-reader、test-preflight、test-offline，四套均exit0；两文件AST/尾空白及git diff --check通过。新增范围/位数、分片丢弃、三协议族完整正向、未知/PRIMARY拒绝、有限诊断与原子落盘回归通过，旧启动失败有界清理及cause、预算/晚输入/失败hold回归保持。此轮未重复生产全仓及未变decoder检查，没有新增静态类型工具。

最终client_trace.py SHA256：`81a5a68878525d50c1e27f2276dbe8ef3b0b86589b93f321f291175c66cae2ba`；test-client-trace.py：`21bdd176a16647383e58780d9337ce0d33e2f33a06c2e373b1c95b3359ca9003`。**定向离线门槛通过、源码冻结**，主会话可按SHA同步空h并重新核验私有来源/原租约后实跑。g归因、正常KDE中文及完整恢复/来源退出后独立读取仍待真实证据。所有命令显式使用项目.tmp；未访问系统临时目录、GUI、Clipboard、Portal、运行状态，未提交、归档或修改.opencode。


## 独立读取完成检查点与退出历史复审（2026-10-07）

主会话私有i真实reader已读出wire四MIME127字节、Qt规范化三MIME95字节且每MIME receive一次；parent用Qt正常退出销毁后的当前selected状态判断，因而误报。该运行仍0refresh/text/key，全部14持有PID已清理，原segmented-09及cleanup记录保持failure。本轮只审c的六个checkpoint相关文件和当前implement/gui规范；保存的i事件只用于纯离线回放，不操作i状态，也不追补旧实机PASS。

成功reader在最后raw receive、焦点、租约及offer守卫后、成功JSON与退出前记录read_checkpoint；parent先snapshot再记录finished。重建函数强制PID/sender、连续真整数序号、4096事件及256次创建、0..1e12有限时刻和started≤completed≤snapshot≤finished。完整历史重建目标世代、MIME与selected状态，prefix内目标必须活着且每raw MIME receive恰一次；完整长度/hash及Qt规范化视图由原校验继续核验。后缀仅允许completed之后已知reader对象的正常destroy，所有最终graph必须与全history一致；不能简单丢弃后缀。reader正常EOF保留已完成历史证明，producer仍取最新状态，EOF/cancelled/destroyed/current变化继续拒绝。

**发现并自修一项prefix缺口**：正常Filter可产生目标receive完成→selection(nil)或另一offer→重新selection原offer的完整一致历史，原检查点函数会接受已撤销的读取证明。按主会话确认的精确边界，现记录已receive的实例及后续失去选择的历史；最终目标只要曾读后撤销即永久拒绝此prefix，重新选择不能抹去撤销。初始nil、未读取其他offer的初始化，以及同一目标未失去选择的重复通知仍接受。新增三协议族×5条完整Filter历史回归，负例明确命中撤销错误，不由乱序、时刻或graph不一致提前遮蔽。没有其他确认未修缺陷。

审阅者最终复跑test-client-trace、test-trace-reader、test-offline、test-preflight，**四套exit0**；六文件AST/尾空白及git diff --check通过，无新增静态类型工具。实际i退出事件的旧结果没有checkpoint仍拒，仅测试合成的receive之后/destroy之前时点可通过；提前destroy、selection变化、ID复用、坏世代、乱序、非法序号/时间、最终graph不符及完整一致缺receive均拒绝。完整分段mock使用真实严格递增monotonic确认snapshot先于finished，原四类刷新/来源失败0后续text/key、关闭GUI及有界hold保持。原3秒/单次输入/首帧、取消、晚解码/保存及清理回归保持；未重复生产、Windows或未变decoder检查。

| 冻结文件 | SHA256 |
| --- | --- |
| client_trace.py | f0dd2fdd7be132fc6d23c3c7afca84641afbb838789337ccfb12c0d7f077c46a |
| transport-reader.py | b897a67c847106ddccca4394bf5251831b4f0620b5dc5b20697546009912f2dd |
| segmented-smoke.py | bf2902a24d1b21059334ab1a4114c288b8582623ea6b6c6da80b3f75f086b2d3 |
| test-client-trace.py | 3d38f9209b303472c643fc861621a19772b70cf91f83741cbf0bf7c204ea5ab6 |
| test-trace-reader.py | bc2f13c4fde08c654b76cc2f2f18a08572d30ab18eae8afec6ddad4a0d203921 |
| test-offline.py | 8e38b0acffec6a2e5dd966c710fdb4eacedd5393cc251f38d84d95ff1407c2fc |

**定向离线门槛通过，源码冻结。** 主会话可按SHA同步新空j，重新核验私有来源/owner/PID和原租约后进行真人授权与实跑。检查点证明历史reader读取，不证明当前producer、逐FD身份或中文恢复；正常KDE恢复、关闭及来源退出后独立读取继续pending。所有命令明确TMPDIR/TMP/TEMP为项目.tmp和PYTHONDONTWRITEBYTECODE，本代理未操作GUI/Clipboard/Portal、live状态、系统临时目录或生产，未提交、归档或修改.opencode。
