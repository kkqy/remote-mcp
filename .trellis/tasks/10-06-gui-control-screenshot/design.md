# GUI 图形操作与截图技术设计

状态：最终规划已获用户批准，进入实施阶段；平台能力须以实际测试结果为依据。

## 架构与模块边界

- 新增 `internal/gui/`，维护图形会话、能力探测、截图、输入、参数校验、平台适配和资源清理。
- 遵循既有管理器接口：`DefaultConfig()`、`New(Config)`、`Register(*mcp.Server)`、`Close()`。
- `internal/config` 负责配置加载；`internal/server` 负责创建、注册与关闭，不复制图形状态转换。
- Windows、macOS、Linux X11/Wayland 后端使用平台构建约束隔离；共享层不暴露平台专属句柄。
- 沿用 `/mcp`、Streamable HTTP 和协议版本 `2025-11-25`。GUI 不建立独立网页、控制端口或认证体系。
- `New` 仅校验配置；桌面和可选依赖按需探测，缺失不导致整个服务启动失败。

## MCP 工具契约

| 工具 | 核心输入 | 输出与行为 |
| --- | --- | --- |
| `gui_status` | 可选 `id` | 无 ID 查询后端可用性；有 ID 查询会话状态、实际权限及显示器，不触发授权。 |
| `gui_open` | `request_id`、可选 `wait_ms` | 创建当前用户图形会话，返回 `id/state/capabilities/displays`；授权等待超出窗口返回 `authorizing`，继续查状态。 |
| `gui_close` | `id` | 清理会话；保留期内重复关闭返回同一终态。 |
| `gui_screenshot` | `id`、可选 `display_id/region` | 原始尺寸 PNG 图片块和结构化截图元数据。省略显示器选默认显示器或第一个获授权显示器，并在结果明确标识。 |
| `gui_mouse` | `id/capture_id/action`、动作所需 `x/y` 或拖拽起终点、按钮/滚动步数/持续时间 | 移动、点击、双击、拖拽和滚动；所有点相对指定截图，输入串行。 |
| `gui_key` | `id/keys` | 一次完整单键或组合键，自动释放本次按下的键。统一常用键名和修饰键，平台不支持的键明确失败。 |
| `gui_text` | `id/text`、可选 `mode/paste_keys/allow_clipboard_replace` | UTF-8 文本；模式为 `auto/direct/clipboard`，显式替换默认关闭。结果报告文本提交模式与剪贴板恢复状态。 |

- 工具结构通过 Go 类型定义 schema，使用英文描述；后端动作枚举、键名与边界由共享层验证。设计文档和源码注释继续中文。
- `gui_key` 不提供跨调用长按；拖拽需要的鼠标按住状态仅存在于一次有界调用内。
- `gui_text` 的 `auto` 优先已探测并验证的直接文本能力，否则采用用户已认可的剪贴板兼容策略。未可靠保存旧内容前不修改剪贴板。
- 普通应用默认粘贴组合键为 Windows/Linux 的 Ctrl+V、macOS 的 Meta+V；终端等不同目标由调用方指定 `paste_keys`，不猜测前台窗口或控件。
- 截图使用 SDK `mcp.ImageContent`，传原始 PNG 字节，由 SDK 序列化为 Base64。结构化结果仅含元数据，不重复整张图片。
- 输入结果的 `submitted` 只表示后端提交了事件，不能等同于文字已进入控件或应用已渲染。部分失败返回 `input_may_have_applied`，避免盲目重试。
- 业务失败使用稳定 `code/message` 及 MCP `isError`；错误说明不回显输入文本、图片或剪贴板内容。

## 截图与坐标契约

截图元数据包含 `capture_id/display_id/width/height/region/logical_bounds/layout_generation/captured_at/captured_at_source/frame_sequence/freshness`；裁剪 `region` 包含相对整显示器的偏移。`freshness` 区分新采集与后端最新可用帧。`captured_at_source=acquired_at` 表示原生同步采集观察时间，`received_at` 表示 Wayland PNG 首帧接收观察时间，不是媒体源渲染时间。`frame_sequence` 是本会话成功截图序号，不是 PipeWire 媒体帧序号。

- 默认截图不缩放；不提供跨显示器拼图或按窗口选择捕获。
- 输入点是返回图像内部像素坐标，原点为左上角，右和下为正；以 `capture_id` 查询所属显示器及变换。
- 裁剪输入先加区域偏移，再使用实际帧尺寸、逻辑尺寸与方向变换映射。Windows 处理虚拟桌面负坐标与 DPI；macOS 和 Wayland 不能假定像素等于输入逻辑单位。
- Wayland NotifyAbsolute 使用授权流局部逻辑坐标和相应 PipeWire node，不重复添加全局偏移；后端以实际返回的逻辑尺寸为依据，兼容已查证的 `size/logical_size` 文档差异。
- EIS 若启用，必须用 `mapping_id` 匹配正确 region；首版基础路径使用 Notify，EIS 仅在原型证明需要且可用时选为整会话输入机制。
- 显示器授权、布局、方向、分辨率或缩放变化后更新 `layout_generation`，使旧截图坐标失败；截图 ID 到期或被有界淘汰也返回 `stale_capture`。
- Portal 本身没有通用布局变化信号，仅比较媒体像素不足以检测纯逻辑缩放变化。GNOME 使用当前桌面 DisplayConfig 的 `MonitorsChanged/GetCurrentState`，KDE 按实际可用 KScreen D-Bus 的 `configChanged/getConfig` 建立只读监视。先订阅并读取基线，再授权；期间及之后配置变化、服务替换或监视断线均禁用当前绝对输入映射，要求重新打开授权会话。监视接口不可用则明确缺失原因，不猜测拓扑，也不新增永久权限或修改显示配置。
- 只列出可访问或获授权的显示器，不将不可访问屏幕标为可操作；缺少可靠坐标变换时禁用绝对输入并说明原因，不做 1:1 猜测。
- 操作后截图不得无标记返回操作前的缓存。静止桌面不一定持续产帧，允许准确标注最新可用帧，等待有上限；调用方仍须核对画面结果。

## 会话、并发与限制

状态为 `authorizing/ready/closing/closed/failed`。能力分别列 `screenshot/mouse/keyboard/text` 与缺失原因；只具有截图的后端不报告完整操作可用。

- `gui_open` 按 `request_id` 去重，同 ID 同参数返回已创建资源，不同参数返回 `conflict`。活跃资源去重记录不能被淘汰。
- 一次服务最多有一个待授权或可操作的桌面会话；同一桌面不能由多个输入调用交错控制。真实输入忙时返回 `busy`，不建立无界队列。
- 资源 ID 不绑定单次 HTTP 或 MCP 连接；原访问控制继续生效，ID 不能替代认证。
- 运行期间复用已授权会话；首版不把恢复令牌写盘，服务重启创建新资源并按系统规则重新授权。
- 调用取消不证明输入未提交；底层结束并完成按钮释放后才解除输入占用。关闭应中断等待、结束底层资源，不能让输入在关闭后继续运行。

默认限制作为实现基线，配置统一由管理器校验：

| 项目 | 默认值 |
| --- | --- |
| 活跃会话 | 1（单当前用户桌面，不增加多租户语义） |
| GUI 无调用空闲时间 | 30 分钟 |
| 终态保留 / 最大资源记录 | 10 分钟 / 256 |
| 授权等待总期限 / `gui_open.wait_ms` 上限 | 2 分钟 / 10 秒 |
| 普通截图与输入调用等待上限 | 30 秒 |
| 原始截图像素 / PNG 字节上限 | 16,777,216 像素 / 16 MiB |
| 保留的坐标记录 / 记录有效期 | 每会话 64 条 / 5 分钟（仅元数据，不长期保存 PNG） |
| 文本大小 / 剪贴板快照大小 | UTF-8 64 KiB / 1 MiB |
| 组合键数 / 拖拽时长上限 | 8 个 / 10 秒 |

首版开放必要的 GUI 空闲、等待及截图大小配置；其他上限可保持有测试覆盖的常量。超限在执行前拒绝，不能先截图分配巨大缓冲再检查编码大小。

## 平台后端与依赖路线

### Windows

在当前用户交互桌面使用 Win32 截图和 SendInput，统一 DPI 模式，检查事件提交数量。中文优先 Unicode 输入；尊重 UIPI 与交互桌面限制，不控制安全桌面或保证更高权限应用可用。系统句柄与位图必须按失败路径释放。

### macOS

截图与输入分别检查系统权限。采用 purego 调用原生系统框架，先验证目标系统可用的截图 API、Unicode 输入、线程约束、ABI 和对象释放。若旧截图 API 在目标系统不可用，选择系统支持的采集接口；不能为了通过构建把 macOS 截图静默降级为不可用。

### Linux X11

使用当前显示服务器连接，探测截屏、显示器布局和 XTEST 输入能力。截图与输入属于同一显示服务器。中文直接输入能力不足时遵守公共剪贴板策略。原生 Wayland 会话不能以 XWayland 后端伪报完整支持。

### Linux Wayland

首版完整验收覆盖 GNOME/KDE；wlr 与 Hyprland 等其他桌面按实际能力探测，可提供截图时仍明确输入不可用。具体接口证据见 `research/wayland.md`。

1. 以当前用户会话 D-Bus 探测 RemoteDesktop/ScreenCast 的接口版本和实际设备能力。
2. 异步创建同一授权会话，选择键盘、指针及显示器；文本兼容所需 Clipboard 在 Start 前申请。
3. `RemoteDesktop.Start` 触发必要的本机系统授权。调用方可取消，期限到达关闭 Request，不循环弹窗。
4. 读取实际获授予的设备、显示器与剪贴板权限；这些可以少于申请项。缺少权限分别报告。
5. 从 `OpenPipeWireRemote` 获取受限 FD，连接授权流；不改连全局 socket 假装沿用授权。
6. 首选原型路线为纯 Go D-Bus 加 GStreamer `pipewiresrc → videoconvert → pngenc snapshot=true → fdsink` 采集。系统运行需 PipeWire、匹配的 Portal 后端、GStreamer 命令及对应插件；Go 主程序保持无 CGO。
7. 辅助进程由显式 argv 和 `ExtraFiles` 传 FD，不拼接 shell。截图输出有界，进程退出、超时及服务关闭均回收进程组、FD 和管道。每次新采集正确取得连接，不反复复用已被消费者接管的 socket。
8. 备选为 purego 动态加载 libpipewire；需要验证动态符号、SPA 数据结构、回调、像素格式及缓冲所有权。不假定所有 C inline 接口都有可调用符号。
9. 基础输入使用 D-Bus Notify；若原型决定改用 EIS，整个会话固定使用 EIS，不混用两种机制，并按实际设备探测 UTF-8 TEXT 能力。
10. 监听授权撤销、Session.Closed、D-Bus/媒体/输入断线，停止输入并释放会话、流、信号订阅及句柄。

首次或授权失效时可能需要目标机用户确认；不能承诺永久免提示。最低桌面/依赖版本在原型后按实际测试锁定，不将上游源码可用误报为旧发行版全部兼容。

## 中文文本与剪贴板

用户已同意兼容路线：直接输入优先，没有直接文本能力时采用公开声明的剪贴板粘贴模式。

- 默认必须先可靠保存原内容及恢复所需的 MIME 信息；空剪贴板也需正确处理。内容超出支持类型、快照大小或读取期限则不替换。
- KDE 当前 Portal RequestClipboard 只订阅格式变化，不发送初始 offer；公共接口没有初始格式列表查询。未收到可靠通知时按无法保存处理，默认拒绝。测试原生恢复路径可以显式发布已完整读取并确认相同的原 MIME 内容以触发通知，不把该验证准备包装成服务的通用自动恢复方案。
- `allow_clipboard_replace` 默认 false；无法可靠保留时返回 `clipboard_preservation_unavailable`。只有显式设为 true 才允许省略保存恢复，结果必须标明替换行为。
- 粘贴前确认权限和目标组合键可用；粘贴完成后确认仍持有本次内容，再恢复旧内容。若检测到其他应用的新内容，不覆盖并返回 `skipped_new_owner`。
- 并发剪贴板所有权变化不能被包装为恢复成功；恢复失败、取消后部分输入等情况明确返回状态，必要时标明输入可能已发生。
- 输入结果是事件提交结果；控件拒绝粘贴、输入法行为或界面渲染只能通过实际结果确认。原生测试必须检查文字而不只检查事件调用成功。
- 不向日志或响应输出原剪贴板内容。每份剪贴板快照受大小和格式数量限制。Portal/X11 的恢复需要按需提供内容，恢复后可保留旧快照直到其他应用接管或会话关闭；不能在调用返回时释放仍作为恢复来源的数据。会话关闭后的原内容可读性须单独验收，不能只检查文本调用返回时的恢复状态。
- KDE `onlyReplaceEmpty` 控制格式不能在非空 selection 上原样恢复，默认拒绝包含它的快照。恢复期间保留 provider 数据，确认所有权及完整目标格式后才能报成功；失败保留有界原备份，禁止再次替换，关闭时仅在确认仍拥有 selection 时重试。验证窗口的独立备份保留于内存，不借删除控制格式绕过完整恢复要求。

## 错误与日志

错误码至少包含 `invalid_argument/unsupported/no_gui/permission_denied/dependency_missing/authorization_cancelled/authorization_timeout/session_closed/not_found/conflict/busy/stale_capture/capture_failed/timeout/limit_exceeded/input_failed/clipboard_preservation_unavailable/clipboard_restore_failed`，后续增加需同步契约与测试。部分能力失效与整体会话关闭应区分。

沿用既有普通日志规则，只记录工具、耗时和结果。截图、文字、剪贴板、Portal 恢复令牌、D-Bus 原始敏感响应和辅助进程完整 stderr 不进入普通日志。

2026-10-06 用户追加失败原因日志：在服务端统一 MCP receiving 中间件为失败调用增加安全的错误码和具体原因，兼容 `CallToolResult.IsError` 与返回 error 两条路径。业务错误保留具体英文原因；SDK 参数/协议或未知异常使用能定位类别的受控诊断，不能转储原始 error、请求参数或完整响应内容。日志长度与控制字符有边界，Token 非空时过滤凭据；日志功能自身不改变调用响应。用户随后要求所有程序错误和提示改为英文，该文案调整可以改变 message/description/help 文本，但保持错误码、JSON 字段和业务行为。

## 兼容、实施验证与回退

维持现有工具与访问控制行为以及六组合无 CGO 构建。GUI 依赖缺失、授权拒绝或无桌面时只影响 GUI 调用。新模块停止或移除可在 server 注册与关闭边界回退，不变更现有资源契约。

关键技术未知项已研究，并安排第一阶段原型验证：Wayland FD 和静态帧、中文输入及恢复、混合 DPI 映射、macOS 原生 ABI/API、各后端最低可用版本。原型不改变 MVP；若结果要求变更已审阅范围、构建或安装条件，返回规划审阅。真实平台验收缺失时保持未验证状态，不能以交叉编译替代。
