# 公共 GUI 管理器与 Linux 实施记录

日期：2026-10-06。本文记录代码及验证事实，不把构建、隔离协议测试和真实桌面验收混为一项。

## 责任与变更边界

本实施者修改 `internal/gui/types.go`、`helpers.go`、`manager.go`、`tools.go`、公共测试、Linux 工厂与 X11/Wayland/Portal 剪贴板实现及其测试；唯一协调修改 `go.mod/go.sum`。Windows/macOS 平台实现、配置/server、README、脚本及任务状态由其他代理负责。

原行为没有 GUI；新增图形管理器集中承载状态转换、去重、等待/占用、坐标记录、限额及七个标准 MCP 工具。既有文件、执行、终端与转发模块不被替代。不新建网页、独立控制端口、特权输入、系统权限绕过、跨重启恢复或语义控件定位。

## 接口与资源行为

- 工厂为 `newPlatformBackend(Config) Backend`。`Backend.Probe` 不触发授权；`Backend.Open` 建立桌面。
- `Desktop` 提供能力、显示器、截图、鼠标、组合键、文本、关闭；可选 `Done/Err` 主动报告桌面断线/撤权。
- `New` 只校验配置；系统 GUI 缺失不会阻止原服务启动。`NewWithBackend` 支持可控行为与独立 HTTP 测试。
- 最多一个活跃/待授权桌面；资源和 request_id 去重记录有界。截图仅保留有界坐标元数据，不存整张 PNG。
- 开放等待最多 10 秒，总授权期限单独限制；操作占用直到实际底层完成，取消只结束调用方等待。完成先释放占用再发布结果。
- `Status(id)` 在空闲时刷新实际能力和布局；输入/采集忙时返回现有快照。显示器变化提升布局代次并使旧截图失效。
- 关闭每资源仅启动一个清理过程，重复调用共享终态；取消的关闭等待不积累后台等待者。管理器重复关闭保留清理错误。
- MCP 图片是标准 `ImageContent(image/png)`；structuredContent 仅为坐标元数据。业务失败提供稳定中文 `code/message`、MCP `isError` 和必要的部分输入/剪贴板恢复状态。

## 坐标与截图

输入点是指定截图内的有限像素坐标；排除 NaN/Inf、边界外点和过期 ID。区域偏移先加入像素点，再按实际显示器帧尺寸映射到显示器局部逻辑坐标。Windows/macOS/X11 后端按需加全局原点，Wayland 向授权 PipeWire node 提交局部逻辑坐标。

像素乘积通过除法校验防溢出，已知尺寸时在后端截图前拒绝超限。PNG 输出由专用 writer 有界存储：不能嵌入 `bytes.Buffer` 的 `ReaderFrom`，否则 `io.Copy` 可绕过 `Write` 限额。Wayland 流尺寸首次未知，使用 `png.DecodeConfig` 先检查尺寸再解码；GStreamer/PipeWire 第三方媒体内部缓冲不属于 Go 图像限额保证，不能声称整个系统峰值内存有同样上限。

Wayland 当前一次新建采集连接取第一帧，可能取得合成器最新静止画面。`freshness=latest_available`，`captured_at_source=received_at`，时间是本服务观察到第一个 PNG 字节的时间，不是源画面渲染/媒体 PTS。其他原生后端标 `acquired_at`。调用方仍须根据后续画面确认输入结果。

## Linux 后端

### Wayland

- `WAYLAND_DISPLAY` 或 `XDG_SESSION_TYPE=wayland` 优先选择 Portal，绝不把 XWayland 连接伪报为原生完整控制。
- 私有 D-Bus 连接；RemoteDesktop 与 ScreenCast 共用一个授权会话，Clipboard 在 Start 前申请。缺少 RemoteDesktop 的其他合成器可按实际 ScreenCast 能力提供截图，单独报告输入缺失。
- 订阅 Request.Response 后才发方法调用，兼容先信号后方法返回；取消/超时关闭 Request；监听 Session.Closed 和总线断线。
- 解析实际获授予的设备和流。可选 tuple 字段为空/非法不 panic；没有可靠逻辑大小则禁用绝对坐标。兼容文档中的 `logical_size` 与实际 ScreenCast `size`。
- 每次截图新取 `OpenPipeWireRemote` FD，显式 `ExtraFiles` 作为 fd=3 交给 GStreamer。使用 argv，无 shell；采集进程组、管道、FD 及超时均有清理。
- 输入使用 Notify，并跟踪本工具按下的键/按钮；关闭先取消，再串行释放 held 状态和关闭授权。未建立 EIS，不混用两套输入机制。
- Portal 没有直接 Unicode 文本能力，auto 选择已声明的剪贴板路径。先按 MIME 读取有界快照，读取或权限不可靠默认拒绝；明确允许替换才省略保存。
- 替换前检查所有权代次；实际接收方通过 SelectionTransfer/SelectionWrite 的 FD 获取中文。恢复前检查自己的所有权；新 owner 出现时跳过覆盖。恢复后的提供方数据按必要所有权生命周期有界保留，第三方 owner 出现时释放。
- 替换中途取消也走清理；缺少所有权确认则明确报告 `clipboard_restore_failed/unknown`，不伪称恢复。已有 provider 更新失败时回滚内存数据。
- 剪贴板关闭后是否由目标桌面管理器接管仍需真实桌面验证。Portal 本身不承诺关闭后的内容持久存在；不可只用 Text 返回瞬间的结果宣布该生命周期通过。

### X11

- 使用纯 Go XGB 的 X11、RandR 和 XTEST 协议；截图解码处理 TrueColor 掩码、16/24/32 位像素、字节序及行填充。
- 显示器枚举和事件提交来自同一显示服务器；剪贴板使用标准 selection 窗口，不引入 Python 或特权系统输入运行依赖。
- XGB 原工厂不支持 context，现用 `DialContext` 和底层连接期限，适配首个认证报文以使用正确 DISPLAY 的 Xauthority；不修改全局环境。停滞认证可取消并关闭连接。
- 协议调用设有期限/取消处理。关闭先打断阻塞 Check，再获取提交锁；必要时用独立的一秒限时连接确认 key/button release，失败准确报告。
- 非空剪贴板可靠恢复要求 `CLIPBOARD_MANAGER` 接管恢复数据；不存在或包含不支持/超限格式默认拒绝，显式替换另行声明。SetSelectionOwner 的所有权比较和替换在短暂 GrabServer 区间内完成；不在此区间读取应用数据或等待粘贴。
- 校验 TARGETS 字节对齐、键图维度、INCR/超限数据；拒绝不可靠格式，不通过修改全局键图伪造任意 Unicode。

## 依赖锁定及证据

- `github.com/godbus/dbus/v5 v5.2.2`：纯 Go D-Bus、UnixFD 和信号/context API；[官方源码](https://github.com/godbus/dbus)及 Context7 `/godbus/dbus`。
- `github.com/jezek/xgb v1.1.1`：纯 Go X11；原项目建议使用维护分支，当前分支包含关闭泄漏回归；[维护分支](https://github.com/jezek/xgb)。
- `github.com/ebitengine/purego v0.9.1`：由原生平台代理核对并用于 macOS，无 CGO；本实施者协调锁定。
- [RemoteDesktop](https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.RemoteDesktop.html)、[ScreenCast](https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.ScreenCast.html)、[Clipboard](https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.Clipboard.html) 的实际公共接口。
- [pipewiresrc 源码](https://github.com/PipeWire/pipewire/blob/master/src/gst/gstpipewiresrc.c)与 [pngenc snapshot](https://gstreamer.freedesktop.org/documentation/png/pngenc.html) 为显式 FD 和单帧管线依据。

下载依赖经自动审批允许后完成；编译缓存使用 `/tmp/remote-mcp-gui-go-cache`，未绕过沙箱访问桌面。

## 已执行检查

- `gofmt`：本责任文件通过。
- `GOCACHE=/tmp/remote-mcp-gui-go-cache go vet ./internal/gui`：通过。
- `GOCACHE=/tmp/remote-mcp-gui-go-cache go test ./internal/gui ./internal/config`：通过。
- `GOCACHE=/tmp/remote-mcp-gui-go-cache go test -race ./internal/gui`：通过。
- `REMOTE_MCP_GUI_PORTAL_TEST=1 GOCACHE=/tmp/remote-mcp-gui-go-cache go test -race ./internal/gui -count=1`：经审批使用隔离私有总线和临时 X11 socket，通过。该测试不访问宿主桌面或触发真实授权。
- `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c ./internal/gui`：通过，仅构建。
- 配置测试通过；server HTTP 测试在本实施者初次沙箱运行中因监听 socket 被拒而未执行成功，完整 HTTP/六组合/旧功能检查由主会话统一运行。

回归包含：200 次正常顺序输入；输入阻塞取消后的 busy；释放与结果发布顺序；关闭/重复关闭及清理错误；取消关闭不积累 worker；多屏负原点/混合缩放/裁剪像素；布局失效、NaN/Inf、尺寸溢出及采集前限额；有界坐标淘汰；权限/状态刷新；授权超时及取消；原始 context 错误的稳定映射；PNG/FD 输出有界及进程组回收；D-Bus tuple 缺失/非法；即时 Response、关闭 Request；中文 FD 字节、恢复 MIME、新 owner、恢复失败、中途取消、已有 provider 回滚和 worker 饱和。

## 原生桌面事实与待验收

主会话实际发现 KDE Wayland、Portal RemoteDesktop v2、ScreenCast v5、Clipboard v1 与 GStreamer 插件；已由用户手动批准系统授权。首次暴露缺失 tuple panic（已修，加入回归）。第二轮得到 2880×1800 PNG，逻辑大小 1800×1125，缩放为 160%；主会话正在继续实际输入及剪贴板闭环。KDE 另有远程输入许可提示，Portal Start 成功及 Notify submitted 不等于目标控件已响应。

本实施者没有操作本机授权 UI，也没有独立宣布 KDE 完整验收。最终原生结果以主会话的真实记录为准。GNOME、纯 X11、Windows/macOS、多屏拓扑变化、媒体尺寸变化、剪贴板跨 Close 和其他权限/断线场景仍需分别记录实际环境及结果；模拟与交叉编译不能替代。

## 检查阶段补充修正

- `layout_portal_linux.go` 建立实际可用的只读配置监視：GNOME DisplayConfig.GetCurrentState/MonitorsChanged；KDE KScreen `/backend` getConfig/configChanged。先订阅再读取基线，必要时只调用一次 `requestBackend("KWayland",{})` 初始化，只读获取配置，绝不调用 setConfig。实际方法不可用则禁绝对映射；服务所有者变化或授权期间/之后配置变化同样禁用映射，要求重新授权。像素帧不变的逻辑缩放/布局变化也使 Manager 的旧坐标失效。
- KDE 旧后端在未初始化时可能没有 `/backend`；只盲订阅接口名称不构成监视。依据为 [Plasma 6.5 官方后端源码](https://raw.githubusercontent.com/KDE/libkscreen/Plasma/6.5/src/backendmanager.cpp)及审阅代理本机只读探测。未来版本不导出该接口时不假定继续可靠。
- X11 认证/Setup 的依赖解析异常由窄边界隔离并关闭连接，过短/错误计数不会崩溃服务；TCP Xauthority 查找可按实际 Dial 的远端 IP 匹配认证地址。回归含畸形握手和 root 数据。
- 原始 context.Canceled/DeadlineExceeded 的结果通道路径同样转换稳定错误码；超限文本单独返回 limit_exceeded。共享关闭增加清理错误重放、200 个已取消关闭调用不积累后台 waiter 的测试。
- 新增完整私有总线 KScreen 初始化/基线/变更信号回归；畸形 signal 和监视服务消失不会 panic。上述补丁后 GUI 的全量私有总线 race、vet、Linux arm64 无 CGO test 编译再次通过。

## 最后一轮边界与真实第 4 轮事实

- 布局基线不仅检查 D-Bus 成功：GNOME 必须提供 uint32 序号，KDE 必须提供非空配置字典。递归规范化并有界编码后记录摘要及服务 unique owner，双读确认稳定；每次 `Displays` 主动重新核验，弥补信号延迟或丢失。授权期间摘要漂移、owner 替换、断线均永久禁用该会话旧映射。私有总线回归特意不运行 signal 消费者，确认主动读取能够拒绝这些情况。
- 剪贴板缺失或非法 `mime_types/session_is_owner` 不表示空内容：置未知、提升代次、释放旧 provider 快照；合法明确的空 MIME 列表才视为已知空。异常通知及明确空快照均有回归。
- Wayland 关闭检查 held release 和 Session.Close 的结果；有输入未确认释放时保留 input_failed，不能声称正常清理。私有总线覆盖两个失败路径。
- 采集前选中的显示器在采集后已从布局移除时，拒绝产生新 capture_id，防止旧 display 被误标为最新代次；回归保留其他显示器，确认仍不能产生坐标记录。
- 本轮 GUI 全量私有总线 race 通过（5.001s）；最后增加显示器移除回归后普通 GUI race 再次通过（1.521s），vet 通过。Linux arm64 无 CGO 构建通过。完整项目及六目标检查由主会话在源码 freeze 后统一执行。
- 第 4 轮真实 KDE 已完成授权、fixture 激活、截图和鼠标；默认文本立即返回 clipboard_preservation_unavailable。此时 Qt fixture 基线是 7 MIME/763 字节，关闭后哈希保持一致，但未修改剪贴板，因此该结果不是恢复成功证据。
- [KDE Clipboard 官方实现](https://raw.githubusercontent.com/KDE/xdg-desktop-portal-kde/master/src/clipboard.cpp) 的 RequestClipboard 仅连接后续 offerChanged，没有发出初始现有 offer；Portal 公共接口也不提供初始格式枚举。因此首次合法所有权通知之前保持默认拒绝，状态 reason 明确说明需要正常复制产生通知，不猜 text/plain、更不设置 allow_replace。真实后续验证由 fixture 在内存中保存有界完整 MIME、核对基线后发布完全相同的内容触发合法通知，服务自身不会执行这项准备动作。
- 同一 KDE 实现在 Session.Close 时，如果当前 source 仍属于该会话会清除 selection。恢复后跨 Close 的保持依赖实际桌面剪贴板管理器接管，需要本机完整 MIME 哈希核验；当前不能把服务瞬时恢复视为跨关闭保证。
- KDE SetSelection 的空格式列表会清空 selection 并通知非 session owner，现把合法明确空列表且所有权代次已推进视为成功恢复空快照；未知/非法通知仍不接受。真实 UnixFD 中文粘贴后恢复空内容的隔离总线回归通过。

## 第 6 轮真实恢复失败修正

- 第 6 轮实际文本已提交，但约 105ms 后报告恢复失败；关闭后原有 14 字节文本不再是当前 selection。该轮不能通过验收，不能用隔离测试代替实际结果。
- 原 offer 含 `application/x-kde-onlyReplaceEmpty`。这不是可盲目重放的普通内容格式：[KWin 官方 seat.cpp](https://raw.githubusercontent.com/KDE/kwin/master/src/wayland/seat.cpp) 对带此格式的 data-control source 在当前 selection 或最后 owner 非空时直接取消。默认快照现明确拒绝此控制格式，在任何读取、替换或按键之前返回 clipboard_preservation_unavailable；不猜测删除原 MIME，也不通过清空现有 selection 绕过规则。
- [Portal 1.20.3 前端](https://raw.githubusercontent.com/flatpak/xdg-desktop-portal/1.20.3/src/clipboard.c) 的 SetSelection 异步转发后返回；方法成功不表示媒体端已取得 selection。设置确认现要求目标 MIME 集合精确匹配且本会话 owner 为 true；目标为空时使用明确空确认。中间非 owner 通知保留 pending provider 数据，持续有界等待，不立即误判终态。
- 恢复错误返回固定阶段（method_error、owner_false、mimetype_mismatch、owner_unknown、timeout/cancelled）及代次、格式数量；只白名单返回 D-Bus 错误名称，不输出远端 error Body、原文或 MIME 内容。
- 恢复失败保存有界原快照并拒绝新文本替换；关闭先取消文本并等待其独立恢复过程完成，保留 watch/FD 服务，再仅在确认仍拥有 selection 时重试恢复。真正第三方接管时不强行覆盖；原快照保留是失败恢复能力，不等于跨关闭持久性证明。
- 隔离回归补充旧 source 非 owner→新 source owner、重复 own 通知、恢复 method error 的固定原因和原快照保留、关闭前仍有权时重试，以及 onlyReplaceEmpty 默认零修改拒绝。真实下一轮须从无此控制格式的普通 offer 验证完整内容与关闭后哈希。

## 最终复审的生命周期补充

- SetSelection 已成功转发但没有确认时，非 owner 中间通知加请求超时不表示确定的第三方接管；返回 clipboard_restore_failed/unknown，保留有界原备份，不走 skipped_new_owner。未确认 provider 同样有界暂存，晚到且格式匹配的本会话 owner 通知可以恢复其数据服务，随后仅在仍有权时安全恢复原内容。
- 连接关闭并等待 watch/所有传输 worker 结束后，清除 data/recovery/unconfirmed/mimes、owner/known/pending 与取消函数引用；管理器保留的终态会话只留诊断，不继续持有原始剪贴板内容。失败恢复关闭同样清除原始内容。
- 新增私有总线真实协议回归：非 owner→timeout→晚到 owner 的原备份保持与安全恢复；格式不匹配的固定最终原因；并发 Close 取消文本且 watch 仍活时独立恢复；授权 Close 方法还在阻塞、总线仍连接时拒绝新文本；关闭后再次拒绝文本；恢复失败关闭清除原始内容。
- 最后受影响检查：普通 GUI race 通过（1.479s），私有总线完整 GUI race 通过（5.386s，随后增强 closing 门控测试再次重跑），vet 通过，Linux arm64 无 CGO test 编译通过。未再次访问宿主剪贴板；真实第 8 轮仅键鼠结果由主会话报告，剪贴板默认保护的原生闭环仍不能宣布完成。
