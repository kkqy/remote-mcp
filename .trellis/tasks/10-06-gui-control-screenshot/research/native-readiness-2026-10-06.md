# 研究：Linux 原生 GUI 接续验收准备

- 查询：本机真实桌面与依赖、KDE 中文及全 MIME 恢复缺口、最小后续命令和协助边界。
- 范围：内部代码与历史证据、只读环境发现、官方接口/源码；不连接 Windows VM。
- 日期：2026-10-06。

## 发现

### 本机环境与证据

| 项目 | 本轮只读结果 | 可以宣称的范围 |
| --- | --- | --- |
| 当前会话 | `XDG_SESSION_TYPE=wayland`、`XDG_CURRENT_DESKTOP=KDE`；DISPLAY、WAYLAND_DISPLAY、用户总线和运行目录已设置 | KDE Wayland 候选可继续原生验收；DISPLAY 不证明独立 X11 |
| 已安装版本 | plasma-workspace/kwin/Portal-KDE 6.7.5；Portal 1.22.1；PipeWire 1.6.9；GStreamer 1.28.7；PySide6 6.11.2 | 包版本不等于本轮 GUI 已运行通过 |
| 依赖 | Python、Go、gdbus、busctl、gst-launch/inspect、kwin_wayland、dbus-run-session 存在；pipewiresrc/videoconvert/pngenc/fdsink `--exists` 均成功 | 现有 Qt 只作为验收依赖，无需增加产品运行依赖 |
| 缺失入口 | gnome-shell、gnome-session、kwin_x11、Xorg、Xephyr、Xvfb、weston、xauth、xclip/xsel、wl-copy/wl-paste 均未找到；会话文件只发现 plasma.desktop Wayland | 本机没有确认可用的 GNOME 或独立 X11 环境；不安装依赖的条件下需外部环境 |
| 第六轮 | `/tmp/remote-mcp-kde-evidence-06/report.json`：run_completed=false、clipboard_offer_refreshed=true、gui_text 报 clipboard_restore_failed | 中文曾进入目标，但全 MIME 恢复失败；后续文本救援不抵消该失败 |
| 第七轮 | 零输入，焦点/权限等待失败，run_completed=false | 不能算输入通过；旧超时文案不改变真实结果 |
| 第八轮 | report：run_completed=true、acceptance_complete=false、fixture_platform=wayland、7 次键鼠操作；未调用 gui_text | 原生截图、160% 缩放、键鼠与清空字段通过；中文及恢复仍未验 |

进程列表受执行环境可见性限制，没有取得宿主桌面进程列表；不能据此推断宿主服务不存在。本轮未创建 Portal 会话、截图、输入或采样剪贴板。

### 文件与代码模式

- `.trellis/spec/backend/gui.md:23`：默认保存所有支持格式；第三方新 owner 不覆盖；关闭后可读性另验。
- `.trellis/spec/backend/gui.md:24`：KDE 初始 offer 盲区必须默认拒绝，同内容 refresh 仅是显式测试准备。
- `.trellis/spec/backend/gui.md:65`：键鼠子集不采样剪贴板；普通文字夹具避开 SelectAll 自动 PRIMARY 复制。
- `.trellis/spec/backend/quality-guidelines.md`：当前只支持 Linux/Windows 四构建组合，平台构建与原生通过分账。
- `internal/gui/platform_linux.go:13`：存在 WAYLAND_DISPLAY 或会话类型为 Wayland 时选择 Portal 后端，不能改环境变量把宿主 XWayland 伪报为原生 X11。
- `internal/gui/x11_linux.go:447`：非空剪贴板要求 CLIPBOARD_MANAGER；独立 X11 没有管理器时，应验默认保护拒绝，不能标全 MIME 恢复成功。
- `scripts/gui_fixture.py:18`：普通可见 QLineEdit 直接处理真实 SelectAll，避免 Qt 自动 PRIMARY 复制；键鼠子集另用 Password 回显。
- `scripts/gui_fixture.py:74`：要求窗口焦点、同版本及同格式列表；只在该专用窗口采样。
- `scripts/gui_clipboard.py:45`：最多 64 格式、累计 1 MiB，原字节仅保存在内存。
- `scripts/gui_clipboard.py:60`：两次稳定读后同内容发布；只接受字节摘要完全一致的 Qt UTF-8 别名，拒绝 KDE 控制格式。
- `scripts/gui_clipboard.py:83`：救援要求关闭确认及两次正向本进程所有权证据；相同文字不能证明外部 owner 身份。
- `scripts/gui-smoke.py:425`：refresh 后独立保留原始与新 offer 基线；每次文字前后及 GUI 关闭后核验摘要。
- `scripts/gui-smoke.py:527`：refresh 失败保留窗口及有界备份；未知所有权交用户按钮恢复，不改失败结果。
- `research/validation.md`、`research/clipboard-recovery.md`：历史轮次及救援事实，保持不改写。

### KDE 中文和恢复的最小方案

专用窗口能限制输入目标，不能隔离同一图形会话的 Clipboard。普通 KDE 中文闭环优先在独立测试用户/图形会话中，以固定、非用户的多 MIME 数据作基线：text/plain、text/html、一个小 PNG 或自定义二进制格式；先取得全部格式/长度/哈希，再中文输入、每次恢复后、GUI 关闭后分别一致核验。既有 fixture 没有创建固定多 MIME 测试基线的入口，需要专用准备窗口或用户在测试会话中准备；本研究不实施该入口。

本机 `kwin_wayland` 可作为后续隔离候选，上游提供 windowed Wayland 的 `--wayland-display` 和独立 `--socket`。但本轮未启动，不能宣称嵌套 Portal/PipeWire/KScreen 已可用。只有独立总线、独立显示 socket、服务/Portal/Qt 均连接内层且没有剪贴板桥接，并确认布局监视、获授屏幕及原生窗口后，才能验该内层；私有 D-Bus 单独不能隔离宿主 selection，嵌套通过也不能覆盖普通宿主 KDE、完整 GNOME 或多屏验收。

已有保护适合作为有限备份措施，尚不足以保证用户宿主剪贴板零影响：Qt 的读取/发布之间无原子 owner 比较，格式物化发生在 Python 限额检查之前；无 refresh 路径没有 fixture 独立备份；成功路径先核验 GUI close，再终止 fixture，尚未验 fixture/provider 退出后的可读性。refresh 还会改变 owner，即使字节相同也不等于无副作用。恢复失败时保留 fixture，禁止自动重试 gui_text 或强杀唯一备份；不借 Klipper setter 恢复文本后声称完整 MIME 恢复。

含 `application/x-kde-onlyReplaceEmpty` 的原快照应继续默认拒绝。不能删除标记换取成功；用户明确选择仅恢复 payload 的救援只能记为原格式集合未完全恢复。需覆盖第三方新 owner、恢复取消/失败、关闭重试与 provider 生命周期；固定测试数据环境内执行这些破坏性边界能降低宿主影响。

### 下一轮顺序与命令

命令仅供主会话接续；本轮没有执行。每轮输出目录必须全新，服务与夹具必须处于同一实际图形会话，使用最终二进制并记录哈希。已通过的常规检查不用无依据重跑。

1. **本机可做，无桌面授权**：`python3 scripts/gui-smoke.py` 只显示帮助；需要相关回归时 `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts -p test_gui_smoke.py`。其中 offscreen 控件结果只能记自动证据。通过独立 MCP 的 `gui_status` 核对实际能力，不调用 gui_open。
2. **本机 KDE 可接续、用户协助**：由主会话在当前已授权会话启动最终测试服务，例如 `dist/linux-amd64/remote-mcp --listen 127.0.0.1:18080 --gui-authorize-timeout 10m`。Token 策略遵循现有授权，匿名例子显式 `--no-token`。
3. **本机 KDE 键鼠回归（仅新变化需要时）**：`python3 scripts/gui-smoke.py --execute --fixture --fixture-inputs-only --url http://127.0.0.1:18080/mcp --no-token --authorize-wait-seconds 605 --desktop-label 'KDE Plasma 6.7.5 Wayland' --output-dir /tmp/remote-mcp-kde-inputs-next`。必须待用户选择正确屏幕、批准 RemoteDesktop 及 KDE 特殊控制提示、夹具实际获得焦点；ready 不表示系统模态提示已消失。
4. **独立测试 KDE 会话优先**：准备固定多 MIME 基线并保持准备提供者存活，再运行 `python3 scripts/gui-smoke.py --execute --fixture --refresh-clipboard-offer --url http://127.0.0.1:18080/mcp --no-token --authorize-wait-seconds 605 --desktop-label 'KDE Plasma 6.7.5 Wayland 独立测试会话' --output-dir /tmp/remote-mcp-kde-clipboard-next`。不加 `--allow-clipboard-replace`。未知初始格式/控制格式默认拒绝另记保护成功，不能充当中文恢复通过。普通用户会话同命令须确保有独立备份及用户参与救援，仍有共享 selection 风险。
5. **外部 GNOME 原生 Wayland**：需要已有实际环境及匹配 GNOME Portal、DisplayConfig、PipeWire/GStreamer；沿用专用 fixture，先键鼠，再固定多 MIME 完整恢复。初始通知能力以真实探测为准，不套用 KDE 结论；不通过后台改权限数据库替代授权。
6. **外部独立 X11**：需要实际 X11 显示服务器、XTEST、显示布局扩展及 CLIPBOARD_MANAGER；在那个真实会话分别启动服务与 fixture，使用 `QT_QPA_PLATFORM=xcb`，并确认 fixture_platform=xcb、后端=x11。不能只在宿主 Wayland 下 unset 变量后连接其 DISPLAY。无管理器只验默认拒绝；有管理器后再多 MIME 正常恢复及关闭后可读性。
7. **用户/外部环境协助**：实际 MCP 客户端图片展示、第二屏/混合缩放/布局变化、Portal 撤销与拒绝分别记账。布局变更由用户在独立测试会话操作，再验旧 capture 被拒绝；不写宿主显示配置、不控制未知前台应用。

Portal 的 Start 屏幕/设备授权及必要的 KDE 额外提示须由用户确认；新的/失效的授权可再提示，不能沿用过去批准宣称自动获授。本轮研究没有发起这些请求。救援按钮及省略控制格式的二次确认也只能由用户决定，脚本不得代点。

### 追加：基于已安装 KWin 的有限嵌套路线

只读帮助已确认本机 `kwin_wayland` 支持 `--wayland-display/--socket/--width/--height/--scale/--output-count/--exit-with-session/--no-lockscreen/--no-global-shortcuts/--no-kactivities`。帮助用 `QT_QPA_PLATFORM=offscreen` 退出解析，没有启动 compositor；这不构成 offscreen 原生验收。

已确认入口：`/usr/lib/xdg-desktop-portal`、`/usr/lib/xdg-desktop-portal-kde`、`/usr/lib/kf6/kscreen_backend_launcher`、`/usr/bin/pipewire`、KWin screencast 插件和 KDE portal 配置。KScreen 总线名为 `org.kde.KScreen`。系统 D-Bus 激活文件还指向用户 systemd 服务，隔离试跑应显式启动私有进程，不能调用宿主 `systemctl --user` 或更新宿主激活环境。

以下是主会话可实施的启动骨架，不是已运行结果。`GUI_NATIVE_PARENT_SOCKET` 为宿主 Wayland socket 的绝对路径，`GUI_NATIVE_RUNTIME` 为主会话新建、0700 的唯一私有运行目录；`inner-runner.sh` 为后续编写的有界进程监管器：

```sh
dbus-run-session -- env \
  XDG_RUNTIME_DIR="$GUI_NATIVE_RUNTIME" \
  XDG_SESSION_TYPE=wayland XDG_CURRENT_DESKTOP=KDE \
  QT_QPA_PLATFORM=wayland WAYLAND_DISPLAY="$GUI_NATIVE_PARENT_SOCKET" \
  kwin_wayland --wayland-display "$GUI_NATIVE_PARENT_SOCKET" \
  --socket gui-native-inner --width 1280 --height 800 --scale 1 \
  --no-lockscreen --no-global-shortcuts --no-kactivities \
  --exit-with-session /绝对路径/inner-runner.sh
```

监管器开始后必须显式 `WAYLAND_DISPLAY=gui-native-inner`、unset DISPLAY/WAYLAND_SOCKET，并保留私有 DBUS_SESSION_BUS_ADDRESS/XDG_RUNTIME_DIR；为 Qt 指定 wayland。配置/缓存/数据可放唯一测试目录，避免测试 Portal 写入宿主许可记录；不要导入宿主总线或启动 Klipper、剪贴板桥接器。按顺序启动私有 `pipewire`、KScreen launcher、Portal-KDE、Portal frontend，然后只读检查总线名、接口版本及 KScreen getConfig。Portal 两进程和 KScreen 都须连接内层 socket；PID/实际进程环境由监管器自行核对，不以服务名存在当成环境正确。每阶段等待有界，失败仅清理持有的精确 PID/目录，不能用 killall/pkill 或 `--replace`。

最小停止点：

1. KWin 不能连接父 socket/渲染失败：停止，不转 offscreen、virtual 或 XWayland 算成功。
2. 私有 PipeWire 无 socket、Portal-KDE 无 screencast/remote-desktop 能力、frontend 选错 backend：停止，不连接宿主 Portal/PipeWire 来补洞。
3. KScreen `/backend` 的 `org.kde.kscreen.Backend.getConfig/configChanged` 不存在：服务现有代码会尝试根接口 `org.kde.KScreen.requestBackend("KWayland", {})`，只在私有总线上进行；必须返回真实非空布局并支持监视，再运行 gui_status。当前 launcher 存在，内层 KWin 也有实际输出协议路线，但未证实匹配本机 D-Bus ABI/插件；若缺映射，就停止输入，不能假定 1:1。见 `internal/gui/layout_portal_linux.go:21`、`:134`，以及 [libkscreen WaylandConfig](https://raw.githubusercontent.com/KDE/libkscreen/master/backends/kwayland/waylandconfig.cpp)。上游 master 已有变化，不能用它代替本机 ABI 核验。
4. 用户必须在嵌套桌面手动确认 Portal 的屏幕/设备选择和必要特殊控制提示；父桌面旧许可不覆盖私有总线新会话。若授权 UI 显示在宿主或列出宿主屏幕，立即取消并核对环境；不用自动点击授权框。KDE [RemoteDesktop 源码](https://raw.githubusercontent.com/KDE/xdg-desktop-portal-kde/master/src/remotedesktop.cpp) 保留授权对话路线。
5. Clipboard 隔离依据为内外两套 compositor selection、所有验收参与者只连内层 socket/总线、不运行同步桥。上游 [Wayland windowed backend](https://raw.githubusercontent.com/KDE/kwin/master/src/backends/wayland/wayland_backend.cpp) 未发现 selection/dataDevice 转发，但这是有限源码核对，不能替代本机完整隔离证据。本轮禁读宿主 Clipboard，故不能证明宿主前后摘要相同；如必须出具该证据，需用户自行核对或另行明确授权只读摘要。无法确认无桥接时不要发布测试 MIME，转独立测试用户/机器。

只有上述只读门槛通过后，才启动内层最终服务与原生 Qt 专用窗口，由用户确认授权，再按前述键鼠→固定多 MIME→中文恢复顺序执行。私有运行目录中的 PipeWire 可避免使用宿主媒体进程，但政策/插件是否足够仍是首轮启动的实测失败点；不安装新依赖补齐。当前状态是**可试跑的候选路线，尚未 ready**。

### 官方引用与相关规范

- [Portal Clipboard v1](https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.Clipboard.html)：扩展既有会话；RequestClipboard 在 Start 前调用，实际授权由 clipboard_enabled 报告；读写通过 MIME/owner 信号及 FD，未提供初始格式枚举方法。
- [KDE Clipboard 源码](https://raw.githubusercontent.com/KDE/xdg-desktop-portal-kde/master/src/clipboard.cpp)：RequestClipboard 订阅后续 offerChanged；会话关闭时自身 source 会被撤销。上游 master 是核对资料，不等于已安装 6.7.5 的逐行证据。
- [KWin seat 源码](https://raw.githubusercontent.com/KDE/kwin/master/src/wayland/seat.cpp)：onlyReplaceEmpty 在 currentSelection/lastSelectionOwner 存在时取消。
- [Qt 6.11 输入控件源码](https://raw.githubusercontent.com/qt/qtbase/6.11/src/widgets/widgets/qwidgetlinecontrol.cpp)：Normal 回显且有选区才 copy，常规处理按键尾部可发布 PRIMARY。
- [Qt QClipboard 文档](https://doc.qt.io/qt-6/qclipboard.html)：提供者转交对象所有权；X11 有单独 selection 和事件循环约束。在线文档现为 6.12，控件行为另用 6.11 源码核对。
- [KWin main_wayland 源码](https://raw.githubusercontent.com/KDE/kwin/master/src/main_wayland.cpp)：windowed Wayland 后端与独立 socket 选项，只支持隔离候选判断，未证明本机启动成功。
- 相关规范：backend/gui、backend/quality-guidelines；规划 PRD/design/implement 仍保留三平台/五桌面历史，当前规范已取消 macOS，不能按旧文字扩张现行支持范围。

## 注意事项与未发现项

- 按研究角色隔离规则未加载 implement.jsonl/check.jsonl；已读取任务 task.json、PRD/design/implement.md、工作流及相关规范。
- 只读 `plasmashell --version` 在执行环境异常中止，未取得可用输出；版本改用 pacman 包元数据核对，没有再启动 Qt/合成器。本轮没有证明新的原生 GUI 运行成功。
- 没有安装依赖、启动 compositor、操作宿主 clipboard、修改产品代码、提交或归档；仅写本文件。包含追加路线后的研究结束，随后冻结。
