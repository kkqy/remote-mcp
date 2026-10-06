# Wayland 截图与坐标式输入研究

- 研究问题：首版纳入 Wayland；研究 Portal 授权、PipeWire 静态帧、D-Bus/EIS 输入、中文输入、多屏缩放、恢复令牌与无 CGO 构建路线。
- 研究范围：内部约束与外部官方接口、源码。
- 研究日期：2026-10-06。

## 研究结果

### 结论及证据等级

Wayland 可通过 XDG RemoteDesktop 与 ScreenCast 的同一会话实现观察、输入、再次观察。GNOME/Mutter 与 KDE/KWin 当前官方源码均具有这两类后端能力。不能把“Wayland”当作全部桌面具有统一远程控制能力的承诺：`xdg-desktop-portal-wlr` 的官方接口清单只包含 Screenshot、ScreenCast；Hyprland 自己的 `xdg-desktop-portal-hyprland` 当前清单包含 Screenshot、ScreenCast、GlobalShortcuts、InputCapture，同样未列 RemoteDesktop。以上是接口及源码查证，没有原生桌面运行验收。

保留 `CGO_ENABLED=0` 六组合构建有候选路线，但“构建无需 CGO”不等于“GUI 运行无需系统组件”。建议先采用纯 Go D-Bus 加 GStreamer 采集子进程，或评估 purego 动态加载 libpipewire 的内嵌路线；实现阶段须以真实 GNOME/KDE 环境证明采集、输入、中文、多屏闭环。

### 内部文件及相关规范

| 文件 | 用途与已有模式 |
| --- | --- |
| `scripts/build.sh:5`、`:12` | Windows/Linux/macOS × amd64/arm64，两个入口均 `CGO_ENABLED=0` 构建。 |
| `go.mod:3` | Go 1.25.0；当前未引入 D-Bus、PipeWire、EIS 或 GUI 库。 |
| `internal/server/server.go:29`、`:65` | 管理器由 App 持有，在同一 MCP 服务注册工具；GUI 应沿用该边界。 |
| `internal/server/server.go:90`、`:101` | Streamable HTTP 与既有 Token 配置；Portal 本机授权与 MCP 访问凭据是两个独立层次。 |
| `.trellis/spec/backend/directory-structure.md` | 采用模块内平台文件隔离，管理器提供 `New/Register/Close`，共享依赖写入需协调。 |
| `.trellis/spec/backend/database-guidelines.md` | 活跃资源有界，ID 不绑定单次 HTTP 请求，同 Token 信任域；既有资源重启不恢复。 |
| `.trellis/spec/backend/mcp-contracts.md` | 平台实际运行与交叉编译结果分别记账；不能把未验证能力写成事实。 |
| `.trellis/spec/backend/error-handling.md` | 稳定业务错误、中文说明，真实清理失败不能伪报成功。 |
| `.trellis/spec/backend/quality-guidelines.md` | 六组合构建与各平台原生验收独立；运行环境限制明确报告。 |
| 当前 `prd.md` | 已确认 Wayland 首版必需、截图与坐标式操作；具体桌面范围未批准。 |

### Portal 同会话调用与系统授权

官方 [RemoteDesktop v2 文档](https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.RemoteDesktop.html) 与 [接口 XML](https://raw.githubusercontent.com/flatpak/xdg-desktop-portal/main/data/org.freedesktop.portal.RemoteDesktop.xml) 明确支持同会话跨 Portal 集成。候选调用顺序：

1. 查询运行时 `RemoteDesktop.version/AvailableDeviceTypes`、`ScreenCast.version/AvailableSourceTypes/AvailableCursorModes`；按实际能力提出请求。
2. `RemoteDesktop.CreateSession` 创建会话。
3. `RemoteDesktop.SelectDevices` 请求 keyboard=1、pointer=2（首版无需 touchscreen=4）。
4. `ScreenCast.SelectSources` 在该会话请求 monitor=1；如支持，`multiple=true`。首版优先显示器源，窗口源涉及移动及定位变化。
5. 如文本输入拟使用剪贴板，先调用 `Clipboard.RequestClipboard`。
6. `RemoteDesktop.Start` 统一发起本机授权，读取实际授予的 `devices/streams/clipboard_enabled`，不能把请求值当作授予值。
7. `ScreenCast.OpenPipeWireRemote` 获取受限 PipeWire FD，用授权的流进行采集；不能改连全局 PipeWire socket 并声称沿用授权。

本机用户是否共享屏幕、输入设备及剪贴板由桌面 Portal 决定；MCP 的 Bearer Token 无法代替该授权。无有效恢复授权时，应预期系统显示选择/确认对话框。已授予权限及后端策略可能减少后续提示，不能承诺“只确认一次、永远无提示”。无窗口 CLI 可向 `parent_window` 传空字符串，官方 [窗口标识约定](https://flatpak.github.io/xdg-desktop-portal/docs/window-identifiers.html) 允许这一方式，不要求创建 GUI 窗口。

请求通常异步返回 Request 路径，完成结果来自 `Request.Response`。应在调用前按随机 `handle_token` 和 D-Bus unique name 预订阅信号，核对实际返回路径，避免快速响应丢失。`Response` 的 0/1/2 分别代表成功/用户取消/其他结束。超时或取消需关闭 Request；关闭 Request 后不再等待 Response。参见 [官方 Request XML](https://raw.githubusercontent.com/flatpak/xdg-desktop-portal/main/data/org.freedesktop.portal.Request.xml)。

### 输入路线：Notify 与 EIS

| 路线 | 接口行为 | 规划含义 |
| --- | --- | --- |
| D-Bus Notify | 相对/绝对移动、Evdev 按钮、离散/连续滚动、keycode/keysym；需已授予对应设备权限。 | 可由纯 Go D-Bus 实现，不要求客户端链接 libei。低频坐标操作可作为首版候选。 |
| EIS | Start 后 `ConnectToEIS` 返回 FD，交给 libei sender；官方推荐。一次会话只能建立一次 EIS 连接。 | 需设备发现、能力绑定、resume/pause、frame、disconnect 处理。建立后整个会话必须通过 EIS 发输入，不能混用 Notify。 |

EIS 对绝对设备提供 region，需将 ScreenCast `mapping_id` 与 region 对齐。官方 [libei 客户端 API](https://libinput.pages.freedesktop.org/libei/api/group__libei.html) 说明虚拟设备使用逻辑像素；应处理设备动态移除与暂停，不向失效设备发输入。EIS 断开应终止对应桌面会话，重新授权/恢复后再建会话。

推荐把输入模式作为会话内固定策略：选择 Notify 时不调用 `ConnectToEIS`；选择 EIS 后发生能力缺失或断线，不能同会话无声回退 Notify。具体优先级由设计及验证决定，当前没有证明 Notify 已被 GNOME/KDE 移除。

### 中文文本输入

按键与任意文本是不同能力。单纯模拟物理扫描码依赖当前 keymap/输入法；不能以逐字符扫描码映射保证中文正确。`NotifyKeyboardKeysym` 虽可发送符号，但接口不承诺任意 Unicode 都会进入目标应用，需验证合成器、键图及应用链路。

可行候选：

- **Portal 剪贴板＋粘贴**：在 Start 前请求剪贴板，确认 `clipboard_enabled` 后，以 `text/plain;charset=utf-8` 等实际支持 MIME 宣告 Selection；收到 `SelectionTransfer` 后，通过 `SelectionWrite` FD 写内容，完成调用 `SelectionWriteDone`，再发目标应用支持的粘贴组合键。[Clipboard v1 官方接口](https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.Clipboard.html) 与 [XML](https://raw.githubusercontent.com/flatpak/xdg-desktop-portal/main/data/org.freedesktop.portal.Clipboard.xml) 支持这一传输模型。剪贴板与键盘授权均是必要条件，输入结果仍需截图确认。
- **新版 EIS 文本设备**：libei 1.6 起新增 `EI_DEVICE_CAP_TEXT` 与 `ei_device_text_utf8[_with_length]`，见 [sender API](https://libinput.pages.freedesktop.org/libei/api/group__libei-sender.html)。因此不能再笼统写“EIS 只能发扫描码”。必须探测实际桌面是否暴露 TEXT 设备，缺失时不能调用。KWin 当前源码在 `EIS_HAVE_16` 下创建文本设备并把 Portal keyboard 授权映射为 TEXT，见 [eisbackend.cpp:103、192](https://github.com/KDE/kwin/blob/master/src/plugins/eis/eisbackend.cpp)。所查 Mutter `meta-eis-client.c` 未找到 TEXT 能力；这不是所有 GNOME 版本不支持的证明，仍需目标版本验证。

用户已确认首版中文输入允许剪贴板兼容：直接文本能力优先，缺失时使用保存并恢复原内容的粘贴路线；无法可靠保存/恢复时默认拒绝，只有调用方明确允许替换才继续。新版 EIS UTF-8 为探测到能力后的候选优化。结果标注文本输入模式与恢复状态；存在其他应用同时修改及多 MIME 内容的竞态，不能无条件宣称恢复成功。拒绝剪贴板且无直接文本能力时，返回中文文本不可用，基础按键工具仍可工作。

剪贴板粘贴并非所有控件支持：普通编辑器 Ctrl+V 与终端 Ctrl+Shift+V 不同，安全输入控件也可能拒绝粘贴。工具应暴露输入方式或允许明确粘贴组合键，不应通过当前窗口猜测实现确定性的文本写入。

### 截图、PipeWire 帧及帧的新鲜度

ScreenCast 给出媒体流，不直接给 PNG。官方 [视频采集教程](https://docs.pipewire.org/page_tutorial5.html) 展示格式协商、process 回调、`pw_stream_dequeue_buffer` 读取及 `pw_stream_queue_buffer` 回收。官方 [stream API](https://docs.pipewire.org/group__pw__stream.html) 和 [buffer 生命周期说明](https://docs.pipewire.org/devel/page_streams.html) 是参考模式；教程默认摄像头示例需改为连接 Portal FD 与目标屏幕流。

内嵌采集需要处理像素格式、尺寸变化、stride/chunk offset、crop/transform、共享内存与 DMA-BUF 协商。建议首版优先协商可映射的 packed RGB/BGR 内存格式；若后端只能提供未实现的 GPU 格式，应明确失败，不能生成黑图或错误坐标。复制完成后立即归还原始帧，PNG 编码不占用 PipeWire 缓冲。

候选截图结果应有独立 `capture_id`、`display_id`、流/布局代次、图片宽高、采集时间与逻辑尺寸。操作者可按返回图片像素点定位，后端负责转换。输入完成后截图需避免返回输入之前排队的旧帧；可等待新的序号/时间戳，但静止桌面可能不持续产生新帧，应定义超时及允许缓存的条件，不能永远等待“下一帧”，也不能把 Notify/EIS 成功当作应用已渲染完毕。

多屏不是一次拿到原子的全桌面照片：不同屏幕流可能不同步。建议每张图片绑定一个授权显示器；若拼图，返回每块映射及时间，不能丢弃混合 DPI 布局信息。

### 坐标契约、缩放与热插拔

当前 [ScreenCast v6 XML](https://raw.githubusercontent.com/flatpak/xdg-desktop-portal/main/data/org.freedesktop.portal.ScreenCast.xml) 的 `position/size` 是合成器坐标且可选，明确不一定等于帧像素尺寸；`mapping_id` 自 v5 提供 EIS 区域匹配，v6 增加 `pipewire-serial`。优先按 serial 连接媒体流；node ID 可能复用，旧版本必须在受控会话中严密管理 ID 生命周期。

建议公共契约采用 `display_id + capture_id + 图片内 x/y`，而不是令调用者猜测全桌面坐标：

- 在已验证无裁剪/旋转的布局中，`logical_x = image_x × logical_width / image_width`，y 同理；缩略图或裁剪需要额外变换矩阵。计算用实际帧尺寸，不能把 OS 的单一缩放百分比应用到全部显示器。
- NotifyAbsolute 的 stream 参数是 PipeWire node ID，坐标为该流局部逻辑坐标。KDE 后端再加 stream geometry 偏移；调用者不能预加全桌面原点，否则二次偏移，见 [waylandintegration.cpp:271](https://github.com/KDE/xdg-desktop-portal-kde/blob/master/src/waylandintegration.cpp)。
- EIS 路线先按 `mapping_id` 选正确 region，再将局部点映射到其桌面区域；region 的 offset/size 不能当成图像像素。KWin 当前生成 region 的偏移、逻辑大小、物理 scale 与 mapping ID，见 [eisbackend.cpp:158](https://github.com/KDE/kwin/blob/master/src/plugins/eis/eisbackend.cpp)。
- 缺少可靠逻辑尺寸或映射时，可保留截图能力但禁用依赖绝对坐标的操作，返回明确能力原因；不能假定像素与逻辑 1:1。
- 热插拔、分辨率/方向/缩放变化、休眠恢复后，旧截图坐标失效，更新布局代次并拒绝旧 `capture_id`；授权显示器集合变化可能要求新会话。

官方 RemoteDesktop 当前说明引用 `logical_size`，而当前 ScreenCast XML 实际列出的是 `size`。这是文档命名不一致，不能凭不存在字段写解析；需兼容并验证目标后端的实际返回值。

### 授权及恢复令牌生命周期

RemoteDesktop v2 的 `persist_mode` 为 0（不保存）、1（应用运行期间）、2（直到撤销）；后端/用户决定是否授予，并在 Start 响应返回 `restore_token`。令牌单次使用，成功恢复后必须保存最新返回令牌；失效、设备变化、撤权等情况会退回系统提示。组合 RemoteDesktop+ScreenCast 的持久授权只通过 RemoteDesktop `SelectDevices` 管理，不能同时向 ScreenCast 写持久参数。单独 ScreenCast v4 的令牌机制只服务截图会话。[RemoteDesktop 官方说明](https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.RemoteDesktop.html)

设计建议（尚未批准的策略）：首版默认不跨进程保存令牌；运行期间复用已授权会话，MCP/HTTP 断线不自动撤销 GUI 会话。若需要重启后尽量无提示，显式配置持久恢复：按桌面用户和应用身份隔离存储、权限 0600、原子轮换、同一令牌禁止并发消费；日志与工具结果不输出令牌。持久令牌不等于持久 MCP 资源 ID，服务重启创建新资源 ID。

应监听 [`Session.Closed`](https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.Session.html)、D-Bus 断线及媒体/输入断线。关闭时释放 FD、流、辅助进程、信号订阅和按住的键/按钮。Portal 请求取消、会话失效、功能缺失与依赖缺失分别报告，不能无限循环重弹授权框。未知桌面/后端版本需能力探测，不能仅凭 `XDG_CURRENT_DESKTOP` 判定支持。

### 桌面后端支持矩阵

| 桌面及官方后端 | 官方源码查证 | 已确认的首版范围 |
| --- | --- | --- |
| GNOME/Mutter，xdg-desktop-portal-gnome | [remotedesktop.c](https://github.com/GNOME/xdg-desktop-portal-gnome/blob/main/src/remotedesktop.c) 有 Notify 输入、ConnectToEIS；[clipboard.c](https://github.com/GNOME/xdg-desktop-portal-gnome/blob/main/src/clipboard.c) 有剪贴板集成。依赖 Mutter 提供相应 D-Bus 服务。 | 完整截图/鼠标键盘/中文输入的真实桌面验收目标；明确记录测试发行版和版本。 |
| KDE Plasma/KWin，xdg-desktop-portal-kde | [kde.portal](https://raw.githubusercontent.com/KDE/xdg-desktop-portal-kde/master/data/kde.portal) 列 RemoteDesktop、ScreenCast、Clipboard；[remotedesktop.cpp](https://github.com/KDE/xdg-desktop-portal-kde/blob/master/src/remotedesktop.cpp) 有 Notify 与 ConnectToEIS。 | 同上；多屏缩放须独立测试，不能用 GNOME 结果代替。 |
| Sway 等使用 xdg-desktop-portal-wlr 的桌面 | [wlr.portal](https://raw.githubusercontent.com/emersion/xdg-desktop-portal-wlr/master/wlr.portal) 只列 Screenshot/ScreenCast。 | Portal 采集可作为截图目标；完整控制需额外合成器协议或系统输入后端，不可直接承诺。 |
| Hyprland，xdg-desktop-portal-hyprland | [hyprland.portal](https://raw.githubusercontent.com/hyprwm/xdg-desktop-portal-hyprland/master/hyprland.portal) 列截图、采集、快捷键、InputCapture，没有 RemoteDesktop。 | 必须单独探测/适配；InputCapture 捕获本机输入，不替代远端输入注入。不能将其自动归入 GNOME/KDE 控制支持。 |

这张表反映 2026-10-06 查阅的 upstream main/master，不能直接据此给出最低发布版本或发行版覆盖承诺。具体部署可由 `portals.conf` 选择不同后端，最终行为以运行时接口及授权结果为准。若用户要求 Sway/Hyprland 完整控制，应另研究各自协议、权限与依赖；本研究未验证该额外路线。

### 保留无 CGO 六组合构建的候选方案

| 方案 | 构建与运行依赖 | 风险/验证要求 |
| --- | --- | --- |
| **A：godbus + GStreamer 子进程 + Notify 输入** | Go 二进制保持 CGO=0；Wayland 运行需要桌面 Portal/PipeWire、`gst-launch-1.0` 与 pipewiresrc/videoconvert/pngenc/fdsink 插件。 | 优先候选；需 FD 传递、图片输出有界、超时杀进程和 stdout/stderr 隔离；每次新连接的 FD 生命周期需正确。依赖缺失只影响 GUI。 |
| **B：godbus + purego 加载 libpipewire + Notify 输入** | CGO=0；运行时依赖 libpipewire-0.3.so 等系统库，purego 动态 FFI。 | 省辅助命令，适合复用常驻流；需复刻 ABI/SPA POD/回调、内存所有权及格式处理。C inline/varargs 接口不能直接假定存在动态符号，必须逐一检查。 |
| **C：B 或 A 的采集 + purego 加载 libei（可选 libxkbcommon）** | CGO=0；输入还需 libei 及目标 EIS 后端。 | 官方推荐输入机制但更复杂；旧 EIS 仅键盘能力时需 keymap 处理或 Portal 剪贴板，TEXT 设备可提供 UTF-8。 |
| **D：纯 Go 原生 PipeWire/EI 协议实现** | 可保持 CGO=0，避免客户端 C 库。 | 理论路线；本次未找到足以承诺维护质量及兼容性的官方 Go 实现，不建议首版新造媒体协议栈。 |
| **E：CGO 或独立原生采集 helper** | Go 主服务可保持 CGO=0，helper 单独编译；直接 CGO 接入则破坏当前构建约束。 | 原生 C API 最直接，但带来独立产物、交叉工具链、版本及分发契约，需明确批准。 |

godbus 官方 [文档](https://pkg.go.dev/github.com/godbus/dbus/v5) 提供原生 Go D-Bus、UnixFD 与 `SupportsUnixFDs`；本次可见文档版本 v5.2.2，并不声称最新。Context7 查询 `/godbus/dbus` 也确认 FD 与信号 API，最终依据官方源码/文档。

GStreamer 官方 PipeWire 插件 [gstpipewiresrc.c](https://github.com/PipeWire/pipewire/blob/master/src/gst/gstpipewiresrc.c) 提供 `fd`、`path`（node ID，已弃用）与 `target-object`（name/serial）；[gstpipewirecore.c:64](https://github.com/PipeWire/pipewire/blob/master/src/gst/gstpipewirecore.c) 在提供 FD 时调用 `pw_context_connect_fd`。官方 [pngenc 文档](https://gstreamer.freedesktop.org/documentation/png/pngenc.html) 的 `snapshot=true` 在一帧编码后发 EOS。这为 A 提供官方接口证据，未证明完整管线已在本项目运行。

候选验证管线为传入 Portal FD，按授权节点目标连接，再 `videoconvert → pngenc snapshot=true → fdsink`；Go 使用显式 argv 和 `ExtraFiles` 传 FD，不能拼 shell 命令。取新图时不可把同一连接 socket 反复交给已退出消费者；需重新取得连接或使用常驻采集进程。插件能否映射具体桌面 DMA-BUF、静止桌面首次帧及重复连接都留待原生验证。

purego 官方 [仓库](https://github.com/ebitengine/purego) 声明 Linux/macOS amd64/arm64 为主要支持目标，支持无 CGO 动态调用；本次可见 [API 文档](https://pkg.go.dev/github.com/ebitengine/purego) 版本 v0.11.1。它不会自动验证 C struct 对齐，回调分配不可释放且数量有界，故回调应复用而非每次截图新建。只能将 B/C 标为候选，不能从“支持平台”推断 PipeWire/EIS 已成功集成。

### 已确认的桌面范围与下一步

**用户已确认：首版 Wayland 以 GNOME 与 KDE 为完整截图和输入的验收目标；其他桌面按实际能力报告，不承诺完整控制。**

系统可能要求本机首次确认是这条路线的固有限制，应在规划摘要说明；本研究不构成自动批准系统对话框的授权。中文输入兼容行为已获确认，截图与坐标契约、系统授权和剩余原生验证项已合并到最终规划，等待本次摘要的实施审阅。

## 限制与未确认项

- 没有运行真实 GNOME/KDE/Sway/Hyprland 桌面；未做 GUI 原生测试、未编写产品代码、未更改任务范围或启动实施。
- 未确定各桌面最低发布版本、各发行版默认 Portal 组合、恢复令牌在宿主无 app_id 情形的后端行为；需以目标环境验证。
- GNOME EIS TEXT 未在所查文件中找到，不能据此断言未来/所有版本不支持；KWin TEXT 的源码支持受编译选项与 libei 版本限制。
- 任意中文 Unicode、Emoji、换行、非 US 键图、终端/普通控件粘贴及剪贴板恢复竞态均待验收。
- 旋转/裁剪、多屏混合缩放、画面更新时间与屏幕热插拔后的坐标一致性待真实验证。
- upstream API 页面及源码随版本演进，实施必须固定依赖版本；当前最新文档的 v6 serial 等字段只能条件使用，不应强制老桌面存在。
- 未推荐特权 `uinput/ydotool` 作为默认绕过 Portal 的方案；如用户必须支持无 RemoteDesktop 的合成器，再单独评估必要权限及坐标来源。
