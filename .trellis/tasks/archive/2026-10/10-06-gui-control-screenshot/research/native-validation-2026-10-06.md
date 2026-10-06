# GUI 接续原生验收记录（进行中）

## 当前范围与版本

用户选择继续现有 GUI 实机验收，并确认目前没有 GNOME Wayland/独立 X11 环境，先验证现有 Windows/KDE。当前 main 基线为 38a37b8；本轮修改验收助手、文档和规范，并修复实机发现的 KDE 布局误判，用户 .opencode/package.json 保持不动。Windows 8080 常驻旧服务用于部署，不替换；真正验收对象为当前源码构建并上传的临时回环实例。Linux 首个只读环境发现 dist 旧构建后已清理，当前私有环境直接运行 SHA256 dd848f74a1e86a110cb2323ce21911d5561975c992898ac772c1d4c0156b5c80 的当前构建。

## Windows 第一次真实运行：失败

- 原生交互会话 1、Windows 10.0.19044，单显示器 1920×1080，实际显示比例尚未计作混合 DPI 验收。
- 新专用窗口助手通过已有匿名入口上传，在本轮唯一目录启动临时测试服务；首次截图的精确四色标记核验失败：`Fixture markers were obscured or misplaced`。
- finally 删除临时目录时另报 PowerShell 操作失败，没有发布成功报告。后来按本轮唯一恢复记录精确清理，属于该目录的 helper/server 可执行进程数为 0，目录成功删除；原常驻入口重新初始化并发现 40 工具通过。不能将这次清理失败直接归因为进程泄漏。
- 正在补充有界 crop 像素/paint 诊断、错误与清理原因分列、精确目录释放等待，以及本机/上传/实际启动二进制 SHA256 版本链。诊断完成后复审再实跑，不能放宽四色断言或重试输入。

## Windows 第二次真实运行：版本链通过，截图仍失败

本机产物、远端展开文件及助手实际启动前哈希一致：server SHA256 e151d5889d23a304eb5c682f96c8d111e3e312265e24d1d7dd6992a69dd038fe；helper SHA256 66c312f62d0e317b7b22f2bc6e86edb3c65028aa72b98764aa57d4eb43302e4e。

失败阶段 initial_capture，区域 x48/y71/664×441；paint_count=2、paint_failure_bits=0。四色中心实际 RGB 依次为 (110,108,83)、(191,161,8)、(174,159,152)、(12,12,12)，四个7×7区域均49像素不匹配，精确目标颜色在整个裁剪图中的计数均为0。因此绘制 API 成功不能作为实际呈现证据；仍需区分 DWM 呈现时序与采集异常，不能放宽精确颜色断言。

本次清理正常，无成功报告；原常驻入口再次发现40工具通过。下一版仅允许对安全前台/几何守卫下的专用 crop 作有界只读准备等待，不重试任何输入。

## Windows 第三次真实运行：基本闭环通过

增加有界专用窗口截图准备后，Token/匿名两轮均通过；原始报告保存 windows-gui-2026-10-06.json。server SHA256 保持 e151d5889d23a304eb5c682f96c8d111e3e312265e24d1d7dd6992a69dd038fe；helper SHA256 e0ce2a1fcec1b01a93e94555fdb4057c265f00a1f239f6f9d62fd6e4882f010e，本机/上传/实际启动前哈希核验一致。

各轮独立核验客户区裁剪664×441和四色、实际点击3/双击1/拖拽1、垂直滚轮-2/水平2、事件坐标、Ctrl+A/Backspace各1及 WM_GETTEXT 完整中文/emoji。窗口前台和 EDIT 焦点守卫通过，前后 PNG 摘要不同，GUI close/reclose、窗口销毁/类注销、临时服务和目录清理、原常驻入口复查均通过。clipboard_accessed=false，使用原生 Unicode 输入。

成功与增加等待相符，但成功报告没有保存各次呈现尝试，无法据此唯一证明前两轮是 DWM 动画造成；前两次失败保留。本次只有 Windows/amd64 单屏基本闭环，Windows/arm64、多屏/混合缩放、布局变化、撤权、剪贴板保存及真实 MCP 客户端展示仍不计通过，acceptance_complete=false。

## KDE 私有嵌套第一轮：布局保护通过，输入闭环失败

私有环境为 /tmp/remote-mcp-nested-znjgovla。提供者 PID 2207753 发布自有固定 text/plain、text/html 和小二进制三格式，共 95 字节；独立 reader 在开始前读到全部固定格式及正确哈希，未读取宿主 Clipboard/PRIMARY。

用户手动授予本次嵌套 Portal 屏幕/控制权限。真实原生 Qt Wayland 截图得到 PNG 2048×1386、逻辑边界 1280×866、layout_generation=2。首次 gui_mouse 返回英文 `unsupported` 原因：`The display has no reliable absolute coordinate mapping`，0 次输入提交。closed 会话更具体的原因是布局/缩放/监视服务发生变化，要求打开新授权会话。

只读配置证据显示 WL-0 从启动的 2048×1280/modeId1 变为 2048×1386/modeId19；两次后续 KScreen 配置稳定，scale=1。当前代码通过 layoutDigest/owner 变化撤销映射；此次不能归因为静态像素/逻辑比例不等，也不能精确归因某次用户动作。原始 evidence-01/report.json 不改写。

GUI close/reclose 返回 closed。原 fixture 自读摘要与基线匹配，报告 original_preserved、backup_provider_owned=true，保留 PID 2208051；这不能证明其他进程可读。另一个 reader 在 GUI 关闭后得到空格式；进一步独立观察 15.2 秒、31 个 active=true 样本始终空，收到一次 dataChanged，未写入任何 selection。初始两次 150ms 过早读已不足以解释该结果，关闭后独立可读性为失败。

Qt 自有 selectionSource 可返回本地 QMimeData 缓存，ownsClipboard 也不足以证明 compositor 接受了本次 offer；因此必须在 refresh 之后、GUI 关闭后和提供者退出后分别独立读取。当前 0 次 gui_text，产品关闭没有恢复快照，尚不能唯一归因于产品恢复或 Portal.Close。下一轮先保持布局稳定，定位后实际点击专用控件，再刷新固定 offer 并独立核验；无法独立读取则停止中文输入。

## 尚待完成

KDE 稳定布局下鼠标/中文实际入控件、多 MIME 恢复及独立退出后可读性。多屏/混合缩放、撤权和真实 MCP 客户端图片展示继续独立待验。GNOME/X11 无现有环境，Windows/arm64 只有构建结果。任务保持 in_progress，不归档、不把旧失败改写为成功。

## KDE 私有分段第一轮：模式编号变化，0 输入

私有环境 /tmp/remote-mcp-nested-segmented-ilwa4usz，固定提供者 2275570 在用户真实点击后发布三格式测试数据。用户人工授权后，脚本在 after-open 阶段拒绝：The private layout changed: after-open；没有刷新 selection、gui_text 或其他输入。GUI 关闭已确认，历史 evidence-segmented-01/report.json 保留。

只读比较发现，授权前后的两次快照各自稳定，总线 owner 均为 :1.4；差异只有 currentModeId 与模式对象 id：70→74，稍后再次 74→84。尺寸 2048×2334、scale=1、位置和旋转均相同。生产布局摘要使用完整 KScreen 配置，因此存在模式实例重新编号误判；授权途中完整信号未留存，不能排除临时真实几何改变。最小修复范围已记录在 design/implement：严格校验当前显示器身份和几何后取摘要，读取/信号共用，保留真实变化永久禁用的保护。

本轮仅使用固定自有测试数据；主会话写入该环境 STOP，受管 supervisor 正常退出 0，提供者执行会话结束。关闭旧私有环境后才重建和开始下一轮，不接触宿主 Clipboard/PRIMARY。

## KDE 模式编号误判：修复及离线检查通过

生产 KScreen 监视现对严格校验的当前身份、几何和镜像关系取摘要，currentModeId 只用于解析实际模式尺寸；基线、信号、主动读取一致。真实几何变化、非法字段/引用、额外返回字段和 owner 变化仍禁用映射，变化后恢复原值不能复活旧会话。GNOME、Windows和剪贴板生产代码未改变。

完整 go test/race/vet、GUI Python 24 项、Windows Python 17 项、格式/diff 及 Linux/Windows × amd64/arm64 两入口 CGO0 构建通过；首次 GUI offscreen 检查在并行冷编译负载下超时，构建完成后单独原测试通过，未修改超时或断言。初次全包测试因沙箱回环 socket 受限失败，获准后全部通过。

当前 Linux binary SHA256 bb010f9e42c75748bb15479a1d7d3b5518bb9125fd3c8fcff983c5500c5c1bfe；Windows 重新构建 SHA256 仍 e151d5889d23a304eb5c682f96c8d111e3e312265e24d1d7dd6992a69dd038fe，与第三次实际通过版本一致。临时脚本提取并核验生产规则 source SHA，模式 ID 70/74/84 的六份快照几何摘要一致，实际缩放/尺寸变化摘要不同。新私有目录 /tmp/remote-mcp-nested-segmented-6q4_291z 仅准备，待最终临时脚本复审及人工新授权；未将上述离线结果记作 KDE 正常文本/恢复原生通过。

## 新临时目录规则：停止 gui_open 前的旧桌面

用户追加工作目录 `.tmp/` 规则时，6q4_291z 环境已只读复核通过，固定提供者真人点击后发布自有数据；尚未调用 gui_open，所以没有 Portal 授权弹窗。主会话仅通过持有 PID 停止监管器，受管外层正常退出 0；KWin、runner、Portal、服务及 provider 等所有本轮已知 PID 均已结束。未再读取或写入旧系统临时路径。当前 reviewed binary 已从仓库 dist 复制并核验至 `.tmp/remote-mcp-gui-native-current`；正在按新规则重建环境，KDE 原生中文/恢复仍未通过。

## 工作目录 KDE 第一次分段实跑：坐标及独立来源通过，呈现过渡阻断后续

`.tmp/kde-native-a` 当前构建 bb010... 的私有进程/环境/五个owner PID/socket及租约实时复核通过。真人点击提供者 2392962 发布固定三MIME95字节，未触宿主Clipboard/PRIMARY。Portal返回成功授权响应，用户反馈没有新弹窗；未代替用户点击任何权限框，尚未核实未弹窗的具体平台原因。

授权前后 owner 与严格几何摘要相同；真实截图2048×1315、逻辑1280×822、generation2，absolute input可用。初次真实鼠标press/release核验通过；独立原生reader在before-refresh读到三个MIME、全部哈希与95字节基线一致。

重新聚焦fixture后的native-click-before-refresh截图精确标记失败；本轮只提交一次mouse，没有refresh/gui_text/gui_key。主会话查看PNG，旧reader与新fixture可见透明过渡叠加、四色偏色，支持呈现过渡推断，不能据单帧唯一确定采集异常根因。GUI close/reclose确认closed；真人点击关闭后的全新reader，再次独立完整三格式读取与哈希通过。原始有限摘要保存research/kde-workspace-segmented-01.json。

报告finally曾记fixture_retained=true，但执行会话结束后的主机PID核验表明fixture2394308已不存在；同样核验provider2392962及supervisor2391529仍活着。本轮没有刷新/文本替换，原提供者保持，不依赖fixture作为唯一恢复来源。后续临时runner须在可能持唯一备份的refresh后失败时有界保持存活，不能用退出前poll当退出后保留证据。

最小后续只修改workspace验收shot：共享有界只读准备预算，严格标记/目标窗口身份和前后焦点守卫，通过后一次输入；不重试输入、不改生产、不弱化图片断言。正常文本、多MIME恢复、关闭后及所有提供者退出后可读性仍pending。

## 工作目录 KDE 第二次分段：首帧增代被脚本误判，0输入

新空私有环境 `.tmp/kde-native-b`，白名单脚本/helper逐文件hash与冻结版一致，未复制旧状态/权限或证据。五owner PID/环境/socket/租约/current binary bb010...实时门槛通过，provider2423496真人点击发布。gui_status实际authorizing后用户回复已授权；随后初PNG准备失败 The display layout changed during screenshot preparation。没有mouse/text/key/refresh，GUI关闭确认，旧fixture已清理。摘要在research/kde-workspace-segmented-02.json。

现存initial PNG metadata frame_sequence1/generation2、2048×1526/逻辑1280×954；后状态ready/absolute_input=true/generation2。源代码说明Wayland首次Capture从未知pixel(0,0)补齐实际值，Manager.refresh因此对完整Display差异正常增代，再发布capture；仅已知像素尺寸改变才禁映射。脚本要求before.generation==after.generation，错误拒绝了首帧初始化。未保存before状态，不能只凭现存文件唯一还原它；源代码语义与观测相符。

下一轮只修临时guard：同会话/后端/显示器集合与身份/逻辑几何且映射持续有效，capture代号与after相等；仅所截display的两pixel均未知变为与全屏capture一致的正值、代号恰+1且其余属性不变，可作为首帧初始化例外。已知尺寸变化、真正逻辑变化、缺字段仍拒绝；每次保存before/after状态。生产不变。

## 首帧脚本修复复审后：按原租约重建，未延长旧桌面

首帧初始化例外的定向回归与复审通过，最终临时 segmented/frame/test-preflight 校验和已在 check-gui-native.md 留存。当前生产源码与 Linux bb010... 二进制不变。同步最终脚本后，主会话实时检查发现私有 b 剩余时间低于20分钟启动门槛，因此没有再 gui_open、输入或刷新，也没有延长租约。通过本环境 STOP 关闭，外层监管器与固定提供者的执行会话均正常退出0。

私有 c 使用新的空目录，仅复制逐文件校验和一致的脚本、同源助手及显式配置；没有复制权限、旧运行状态或证据。新组件环境/五owner PID/socket/产物门槛通过；等待真人点击固定提供者后再启动真实验证。系统授权是否需要新弹窗仍以 Portal 实际状态为准。正常文本与独立恢复验收尚未增加通过项。

私有 c 的提供者在120秒内未检测到真实输入而退出1，等待wrapper也超时退出；未生成fixed-provider.json或分段evidence目录，没有gui_open、Clipboard读写或输入。主会话保存kde-workspace-bootstrap-03.json后写本环境STOP，outer监管器正常退出0。随后用户回复“没有白色窗口”，此时窗口已经退出；没有启动时visible/exposed/paint留证，不能据此确认此前始终可见或将问题仅归因为用户操作。后续仅改提供者启动状态留证与人工等待流程，生产和已审首帧脚本冻结。

## 私有 d：呈现已确认，用户点击与事件过滤不一致

更新后的临时提供者经定向复审后按最终SHA复制到新空私有 d。实际五owner/环境/socket/产物/原租约门槛通过；provider2468332真实原生Wayland窗口visible/exposed/active均true、paint_count3、状态新鲜，主会话据此请求真人点击。用户回复已点击，随后多次readiness仍waiting_native_click/published=false，标准错误无traceback。没有gui_open、Clipboard读写或分段evidence目录。

新增spontaneous断言存在确定可触发的标签传播缺陷：[Qt 6.11.2 官方 QApplication 源码](https://raw.githubusercontent.com/qt/qtbase/v6.11.2/src/widgets/kernel/qapplication.cpp)在鼠标父链传播中先复制spontaneous，向首个receiver通知后清除原event标记，随后父窗口收到的副本可为false。原白色说明QLabel会把忽略事件传播给Provider，因此合法原生文字点击可能被拒绝。此次没有press/release尝试记录，不能认定用户实际点击必定命中该路径。

主会话保存kde-workspace-bootstrap-04.json并按已核验held PID终止未发布的provider；提供者exit0，wrapper按signal_stopped退出1。私有桌面原租约保留、不延长；修复仅让说明标签对鼠标透明并增加有界拒绝诊断，继续保持真人输入、焦点及租约门槛。正常中文和恢复验收仍待实跑。

## 私有 e 真实分段：首帧与点击修复通过，后续截图期限仍失败

为给5分钟人工点击和后续20分钟验证留出完整余量，主会话停止私有 d（outer正常exit0），在新空 e 中复制冻结入口与最终provider；未延长旧租约、未复制权限或证据。provider2479574实际原生visible/exposed/active和paint_count3就绪。用户点击后记录2次press/2次release，最后原生左键spontaneous/active/paired均true，固定三MIME发布；通过实时组件/产物/租约门槛后启动分段脚本。

本次Portal实际authorizing，用户手动批准。首帧2048×1315、逻辑1280×822，generation1→2的未知像素初始化窄例外真实通过，exact_markers和target_signature均通过；一次实际鼠标事件核验通过。独立原生reader在before-refresh读到三MIME、95字节、全部固定哈希一致，不拥有或写入Clipboard，不接触PRIMARY。

关闭reader、重新聚焦fixture后的native-click-before-refresh准备在共享3秒内失败，英文原因Screenshot preparation deadline expired。attempt1的前后status均ready/generation2/绝对映射有效；保存PNG frame_sequence4及截图metadata，attempt2仅保存beforestatus。主会话查看PNG，此次看似为已呈现的单一专用fixture，不能直接沿用a轮透明叠加的原因；需要离线解析精确像素、目标签名与阶段耗时。没有refresh尝试、gui_text或gui_key；历史失败保留。

GUI close/reclose确认closed，用户点击关闭后的蓝色独立reader，三格式/95字节全部哈希再次通过。fixture已清理、无唯一备份hold需求，原固定provider仍属独立执行会话。完整有限报告保存research/kde-workspace-segmented-05.json，acceptance_complete/run_completed均false；正常中文、刷新后恢复及全部提供者退出可读性仍pending。

## 私有 f：截图预算修复通过，Qt内外格式视图阻断刷新门槛

最终QImage与逐次诊断版经三套离线及定向复审后按五SHA复制到新空私有 f，生产二进制不变。真人点击provider2504799并手动批准Portal。首帧初始化、before-reader和after-reader图片均精确通过；native-click-before-refresh在第二次准备成功，按attempt唯一存图/metadata/诊断，不再丢失首次证据。实际2次鼠标press/release均核验，旧Python定位导致的3秒失败没有复现。

before-refresh独立reader取到原3MIME/95字节。fixture refresh自读多出与plain完全相同的text/plain;charset=utf-8别名，变4MIME/127字节；after-refresh独立reader仍枚举3MIME/95字节、全部原哈希相同。脚本因比较期望4格式拒绝，英文Independent formats or hashes differ: after-refresh。没有gui_text/gui_key；此时不能把自读或内容相等作为新owner证明。

同版本[Qt QWaylandDataOffer源码](https://raw.githubusercontent.com/qt/qtbase/v6.11.2/src/plugins/platforms/wayland/qwaylanddataoffer.cpp)表明外部formats_sys会将UTF8别名映射成text/plain并去重，自有QMimeData则保留alias。因此3对4存在确定视图不对称，不能推断wire缺失、刷新失败或刷新成功。Qt ownsClipboard/本地source保存没有compositor接受ACK，旧f无原始协议source/offer记录，来源接管仍未证。

GUI close/reclose确认；真人蓝reader after-gui-close依旧取得完整原95字节。脚本的independent_close_failure是相对自读4格式的失败，不能写成原三格式丢失。主会话按固定provider初始3格式重新独立核对before/after-refresh/after-close全部长度/哈希，并在主机实时验证provider2504799、fixture2506001和runner2505959为私有活进程，保存research/kde-workspace-segmented-06-hold.json。

本环境只含已知固定测试数据且0text/key；完成原字节独立核验后以RUN_STOP释放hold、保存最终research/kde-workspace-segmented-06.json，再STOP私有桌面。outer/provider正常0、runner失败1，主机持有PID均退出、无需额外终止夹具，清理证据在research/kde-workspace-segmented-06-cleanup.json。没有续租、重新发布或操作宿主Clipboard/PRIMARY。

下一步只补私有协议元数据：fixture/reader及必要Portal-kde stderr实时过滤，不保存原trace或载荷，分别保留raw offer、Qt normalized视图、有限source世代/selection/serial/cancelled/send/receive与摘要。首次refresh需要新source的实际服务证据；文本恢复后的source属于Portal，不能用旧fixture证明。Qt对plain的alias映射/缓存必须明示，不能伪称所有raw MIME都有独立FD读；unknown或限额拒绝。私有完整fixture采用不预置文字的Password回显防PRIMARY复制，公共fixture和生产代码冻结。正常中文、多MIME恢复和全来源退出可读性仍待实机。


## 工作目录 g：协议日志版本拒绝，授权前退出

九文件定向复审通过后，主会话将冻结白名单同步全新空 g，未复制运行时、权限或 Clipboard 状态。实际启动在 Portal-kde trace 健康门槛失败：`unknown_protocol`，sequence=0，readiness.stage=failed。内层七个组件有界清理，outer退出0，root核验本轮9个持有PID均不存在；原始摘要保存 research/kde-workspace-bootstrap-07.json。没有提供者、gui_open、Portal授权或GUI输入，测试未触 Clipboard。

本机 pkg-config 检出 Wayland1.26.0；实施代理只读核对实际链接和[同版本日志实现](https://raw.githubusercontent.com/wayland-mirror/wayland/1.26.0/src/connection.c)，其新时钟前缀与过滤器旧毫秒前缀不兼容，是确定语法缺陷。registry消息含data_control名称时可能在sender识别失败后落unknown_protocol，与本轮sequence0相符；因未保存原日志，不能唯一还原被拒的实际消息。仅授权c私有过滤器/测试补严格新前缀及固定枚举/布尔拒绝诊断，不保存原行/参数/未知字符串、不默许未知消息；复审后再用新空环境验证。


## 工作目录 h：启动通过，点击等待及原租约超时

兼容Wayland1.26的过滤器复审通过后，h实际bootstrap达到ready_no_authorization，Portal trace ok=true/sequence4，root核验5个私有总线owner、实际环境/进程及当前服务SHA通过。白色固定提供者PID2610006实际visible/exposed/active=true、paint3，但300秒内未记录press/release，未发布数据；后续原桌面租约结束，全部组件有界退出。用户续接时root核对本轮10个持有PID均不存在，保存research/kde-workspace-bootstrap-08.json；0gui_open/授权/GUI输入，未读取或发布Clipboard。不能继续点击旧已销毁窗口。

用户明确继续后，root仅将已冻结代码、指定配置和重写私有bus路径同步全新空i，未复用运行时/权限。i实际启动及live gate通过，Portal trace健康，原租约保持1800秒；新固定提供者真实呈现且焦点确认，已再次请求真人点击。该就绪结果不等于后续来源接管、中文恢复或完整原生验收通过。


## 工作目录 i：原始wire四格式读出，退出状态核验误报

本轮provider真人press/release已确认，固定3MIME95字节发布，root实际live gate再核验5owner/私有环境/当前服务SHA/剩余原租约通过。segmented启用--exit-providers；实际gui_status=authorizing后用户手动完成WL-0授权，首帧物化、精确标记与白色签名、真实mouse一次全部通过。独立before-refresh reader PID2659587实际ok=true：wire原始4MIME127字节（原3格式95加同plain32字节UTF8别名），每个raw MIME receive一次，Qt normalized仍3MIME95；所有原长度/hash与固定数据一致。

parent随后失败 `A unique selected raw clipboard offer is unconfirmed`，0refresh/text/key，gui_close/reclose确认。用户点击关闭后蓝色独立reader PID2659668，结果同样ok=true/raw4=127/normalized3=95且全部hash一致，但parent再次报独立关闭后可读性未确认。历史report保持失败，不能据reader自报覆盖原门槛。root保存research/kde-workspace-segmented-09.json及独立cleanup摘要；精确SIGTERM私有固定提供者、STOP独立桌面，runner exit1/provider exit0/outer exit0，本轮14个持有PID全部不存在。

实施代理只读实际trace确认before-refresh selection seq7/token2、raw receive seq8..11，Qt正常退出device/offer destroy seq12/13、最终EOF；parent selected_offer读取的是已销毁最终视图，确定误用了时间点。没有新字节丢失证据。仅授权c私有helper/reader/分段机械调用及tests加入有界read_checkpoint={trace_sequence,read_completed_monotonic}，parent核验同一PID/父观察区间及有限事件prefix重建读取完成时真实状态，后缀只允许合法正常退出。完成前撤销/销毁/ID复用、提前cleanup、后续selection变化/未知/缺receive仍拒绝，producer继续最新活性门槛、EOF/cancel/destroy拒绝；旧i没有checkpoint，不能追补为新验收PASS。定向测试及复审后再实跑，正常中文/恢复及来源退出仍待证。
