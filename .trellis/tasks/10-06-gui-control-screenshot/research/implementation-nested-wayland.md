# 独立 KDE 嵌套桌面试跑记录

当前入口已按用户最新AGENTS重建到项目 `.tmp/kde-native-a`，见末尾追加。此前系统临时目录路径仅保留为历史记录，禁止再访问或执行；旧监管器、组件与固定提供者已由主会话结束。

日期：2026-10-06。初始范围为本机 Linux 私有嵌套会话的启动与只读验收准备，不修改生产代码、不安装依赖。下表记录首次只读 ready 快照；主会话后续授权与 evidence-01 失败分析见末尾追加，**完整 GUI 和独立剪贴板恢复尚未通过**。

## 历史保留环境（已退出）

目录 `/tmp/remote-mcp-nested-znjgovla`，目录及 runtime/config/cache/data/home 均为 0700。监管器最多保留 30 分钟，创建该目录的 `STOP` 文件可提前精确清理。日志、启动来源和诊断均在此目录，未含图像、输入原文、用户剪贴板或 Token。

| 项目 | 实际证据 |
| --- | --- |
| 当前二进制 | `/tmp/remote-mcp-gui-native-current`，主会话当前源码构建；SHA256 `dd848f74a1e86a110cb2323ce21911d5561975c992898ac772c1d4c0156b5c80` |
| MCP | `http://127.0.0.1:57281/mcp`，匿名、仅回环，PID 2204769 |
| 私有总线 | `dbus-run-session` PID 2204585，其 `dbus-daemon` PID 2204586；socket 位于本次 runtime |
| KWin/监管器 | KWin PID 2204587，inner.py PID 2204620；windowed 父连接仅指定 `/run/user/1000/wayland-0` |
| 内层 Wayland | 本次 runtime 下 `gui-native-inner`，socket UID=1000 |
| 私有媒体 | PipeWire PID 2204628、WirePlumber PID 2204631，`pipewire-0`/manager socket 位于本次 runtime |
| KScreen | PID 2204633、owner 与启动 PID 相同；根接口只读 requestBackend("KWayland", {}) 返回 true；/backend 的 getConfig 和 configChanged 均存在 |
| 实际布局 | 唯一已连接/启用输出 WL-0，2048×1280、scale=1、位置0,0；不能把父窗口1280×800当成实际媒体或输入尺寸 |
| Portal | KDE backend PID 2204683、frontend PID 2204701、PermissionStore PID 2204669，D-Bus owner PID 均与本次启动进程一致 |
| 接口 | RemoteDesktop v2 devices=7；ScreenCast v5 sources=7/cursors=7；Screenshot v2 targets=0；Clipboard v1 |
| 产品 Probe | 独立 MCP initialize/initialized/gui_status：backend=wayland-portal、state=available，截图/鼠标/键盘/文本/剪贴板为可申请能力，direct_text=false；尚无授权显示器 |

`readiness.json` 保存上述结果及实际服务环境，`launch-environment.json` 是明确白名单启动来源，`environment.json` 是监管器内层环境，`isolation-check.json` 是持有 PID 的父子链及匹配私有 socket 路径。所有当前组件均属于 UID1000，Portal/KScreen/媒体/MCP 的实际 `/proc/<pid>/environ` 确认了私有 runtime/config/data/DBus 与内层 WAYLAND_DISPLAY；KWin 的 environ 受内核 Permission denied，**该项不能声称独立读取成功**，只以白名单 exec 来源、父子 PID 和实际 socket 证明启动关系。默认 sandbox 的进程命名空间看不到受审批启动的进程；这些有限的本次 PID 检查在 require_escalated 上下文执行，没有升级到 root 或绕过内核限制。

## 隔离与启动边界

私有 bus.conf 不包含自动激活服务目录或 systemd 激活；所需进程均显式启动。HOME、XDG_CONFIG_HOME、XDG_DATA_HOME、XDG_CACHE_HOME、PIPEWIRE_RUNTIME_DIR 均指向本次目录；内层 DISPLAY/WAYLAND_SOCKET/XAUTHORITY/REMOTE_MCP_TOKEN 未继承。系统总线地址指向不存在的本次私有路径，因而 RTKit/蓝牙/文档 Portal 等可选组件有明确警告，未借宿主服务补齐。没有 systemctl/import-environment、Klipper、selection 同步桥、X11 或 GNOME 进程。

只读核对了上游 [KWin WaylandDisplay](https://raw.githubusercontent.com/KDE/kwin/master/src/backends/wayland/wayland_display.cpp) 和 [windowed backend](https://raw.githubusercontent.com/KDE/kwin/master/src/backends/wayland/wayland_backend.cpp) 的父协议初始化/输入路径，未发现 selection/data-device 转发。这是有限源码证据，**不是已安装二进制完整隔离证明**。本轮禁止读取宿主 Clipboard/PRIMARY，因此没有宿主前后哈希；不能把没有桥接进程和私有环境记录写成已实测宿主剪贴板零变化。

## 前置试跑及清理

- 首轮在读取 KWin non-dumpable environ 时立即停止，尚未启动 Portal；随后按主会话同意改为明确记录不可读限制。
- 两轮 KScreen 检查错误地假定固定1280×800，实际 WL-0 为2048×1280；停止后按真实有界布局核验，不推导缩放坐标。
- 一轮私有 Portal 各接口已就绪，但临时 MCP 客户端导入脚本时缺少 scripts 搜索路径，停止并修正临时导入入口。
- 一轮只读 ready 使用 dist 旧构建，主会话识别后提供当前构建。临时进程监管接力在主会话制止前已启动，随后立即 STOP、恢复旧监管器精确清理；该接力方案废弃，当前环境从全新目录直接启动当前 binary，无暂停或接力进程。
- 有限 PID 清理复查确认所有上述旧试跑的 KWin、D-Bus、媒体、Portal、MCP 及临时替换 MCP 均不再存活。未使用 pkill/killall/--replace；保留诊断目录便于追溯。

## 下一步入口（尚未运行）

仅在主会话决定后，于 require_escalated 上下文调用下列临时入口。`run-private.py` 从本次白名单和内层环境构建完整 exec 环境，拒绝已退出的监管器；不从调用者继承宿主显示、总线或凭据。

固定数据提供者已准备，未启动：

```sh
python3 /tmp/remote-mcp-nested-znjgovla/run-private.py \
  python3 /tmp/remote-mcp-nested-znjgovla/fixed-mime-provider.py
```

只发布脚本内固定的 text/plain、text/html、自定义小二进制 MIME，不读取用户内容或 PRIMARY。其窗口/程序提示英文，中文仅为固定 payload。格式、长度与 SHA256 写入 fixed-provider.json，原字节仅在进程内存。

主会话先通知用户手动确认内层权限，再发起完整专用窗口验证：

```sh
python3 /tmp/remote-mcp-nested-znjgovla/run-private.py \
  python3 /home/user/projects/remote-mcp/scripts/gui-smoke.py \
  --execute --fixture --refresh-clipboard-offer \
  --url http://127.0.0.1:57281/mcp --no-token \
  --authorize-wait-seconds 605 \
  --desktop-label 'KDE nested native Wayland' \
  --output-dir /tmp/remote-mcp-nested-znjgovla/evidence-01
```

不添加 allow_clipboard_replace。Portal 屏幕/设备选择及必要 KWin 控制提示须用户在本次嵌套桌面中确认；若列出宿主屏幕或授权 UI 出现在宿主而非内层，立即取消并核对。gui_status available 不等于授权和输入已获准。

补充只读阶段摘要入口已准备，尚未运行：

```sh
python3 /tmp/remote-mcp-nested-znjgovla/run-private.py \
  python3 /tmp/remote-mcp-nested-znjgovla/read-fixed-mime.py --phase before
```

同一入口可选 after-gui-close/after-fixture-exit/after-provider-exit，须人工聚焦本次只读窗口，30秒内核对两次稳定摘要；输出仅 MIME/长度/哈希，不发布数据或访问 PRIMARY。all_fixed_payloads_readable 只核对固定 payload，完整格式集合必须与 before 逐项比较，不能忽略额外/丢失格式。提供者显式退出入口为创建 PROVIDER_STOP；整个环境的 STOP 同时结束提供者。独立 reader 自己持有格式对象只在一次读取期间，不充当 ClipboardManager。

目前没有内层 ClipboardManager。安装清单仅发现 Plasma clipboard 插件及 libklipperplugin.so，没有独立 /usr/bin/klipper；本轮未启动 plasmashell 或自行编写管理器。GUI close、fixture 退出、原 provider 退出后的完整 MIME 可读性必须实际分别记录；现有状态不能宣称恢复成功。若需要管理器，须主会话先审现有入口和内层隔离方案，不能安装依赖、借宿主管理器或把重新发布固定字节当作产品恢复。

## 验证结果与剩余工作

五个临时脚本通过 Python AST 解析；实际私有组件启动、环境来源/owner PID/父子链/socket、Portal 属性、KScreen 布局和最终二进制 gui_status 均已执行。生产文件未修改，未无依据重跑 Go 全量检查。授权、获授流的 GStreamer 截图、实际输入、中文完整恢复、退出后 MIME、多屏/混合缩放/撤权仍未验收；本报告不能覆盖普通宿主 KDE、GNOME 或 X11 原生结果。

## 追加：evidence-01 实际布局漂移拒绝

本轮由主会话启动私有固定提供者 PID2207753，独立 before reader 核对3个固定 MIME、95字节；随后主会话发起 Portal.Start，用户手动授权。首张真实 PNG 为2048×1386、Portal logical_bounds=1280×866、generation=2。第一次 gui_mouse 返回 unsupported，submitted_operations=[]；本轮未执行键鼠或 gui_text。GUI close/重复close已确认。

本代理只读查询关闭会话 e425f3e83941eee338437f8daa6ebc5b，并两次读取当前私有 KScreen。`mapping-failure-readonly.json` 包含完整状态、捕获元数据、两次现配置和初始配置，结果为：

- closed 会话 display.absolute_input=false；reason.absolute_input 为 `The display layout, scaling, or monitoring service has changed; open a new authorized session`。
- 当前 WL-0 的 modeId=19、2048×1386、scale=1；两次现配置相同，但与首次启动 modeId=1、2048×1280、scale=1 不同。
- 生产 monitorLayout/verifyLayout/layoutSignal 比较配置摘要和服务 owner，变化即 markLayoutInvalid；displayFromPortal 对正的 logical_size/size 建立映射，没有因 KScreen物理尺寸与Portal逻辑尺寸的静态比例不同而直接禁用。

因此本轮拒绝是**实际布局漂移触发可靠性保护**。没有逐事件历史，不能断言具体用户动作或授权对话导致了哪次 resize，也不能仅凭启动与现配置推断变化恰好发生在 Start 哪一时刻。上游 [KWin WaylandOutput](https://raw.githubusercontent.com/KDE/kwin/master/src/backends/wayland/wayland_output.cpp) 的 configure/fractional-scale 路线会更新输出 mode/scale，说明 windowed 输出可随父窗口实际配置变化；上游与安装版本不同，不能以该源码替代真实布局。主会话决定保持当前窗口与scale、不重建、不放宽产品校验，新Start仅由主会话发起。

## 追加：夹具自读与独立传输不一致

evidence-01 的夹具 finally 自读报告全原格式匹配、clipboard_recovery=original_preserved。其 clipboard-response.json 同时报告 backup_provider_owned=true，PID2208051继续持有有界固定备份。这只证明夹具自身还能读取同内容缓存，**不能证明独立客户端可读或已交由管理器接管**。历史 report 保持原样，此处补记更强的独立证据。

主会话运行独立 `read-fixed-mime.py --phase after-gui-close` 后，mime-after-gui-close.json 显示 formats=[]、total_bytes=0、all_fixed_payloads_readable=false，夹具仍存活。为排除临时读取器仅两次150ms快照过早，本代理另启只读 observe-clipboard.py：clipboard-observer.json 记录31个连续active样本、共约15.2秒，ownsClipboard始终false、收到一次dataChanged，所有样本仍为formats=[]/total0。它未发布、清空或救援selection，未申请授权、未输入、未关闭fixture/provider，也未访问PRIMARY。

这使“仅150ms异步早读”不足以解释本轮结果；**独立可读性仍为失败**，但不能唯一归因PortalClose或断言合成器清空的准确时点。只读源码核对给出以下有限解释：

- [Qt6.11 QWaylandClipboard](https://raw.githubusercontent.com/qt/qtbase/6.11/src/plugins/platforms/wayland/qwaylandclipboard.cpp) 在本客户端持有 selectionSource 时直接返回 m_clientClipboard；ownsMode 也检查该本地source。[QWaylandDataDevice](https://raw.githubusercontent.com/qt/qtbase/6.11/src/plugins/platforms/wayland/qwaylanddatadevice.cpp) 提交 selection 后立即保存本地source，取消通知才移除。故自读/ownsClipboard不等价于另一客户端成功获得offer和全部字节。
- 本轮0 gui_text，产品 prepareClose 的 recovery=nil 分支直接返回，不调用 SetSelection。[KDE ClipboardPortal](https://raw.githubusercontent.com/KDE/xdg-desktop-portal-kde/master/src/clipboard.cpp) 的上游关闭回调只在source属于该Portal session时撤销；未证明本轮关闭撤销了fixture source，也未证明它已独立可读。

后续验证必须单列“提供者自读摘要”与“独立客户端传输摘要”，并在刷新offer之后、gui_close之后、fixture/provider退出之后核对全部原格式；固定payload匹配不能替代完整格式集合比较。需要改可复用验证脚本的相关流程时先由主会话明确授权；本轮未改这些流程。

## 主会话下一轮最小顺序

1. 保留上述失败证据；确认旧GUI为closed、原格式仅为本轮固定数据、fixture恢复结果为original_preserved。夹具仍是当前offer提供者，不能把该状态描述为外部接管成功。
2. 主会话核对精确PID2208051属于本次fixture后有界退出它，原固定提供者PID2207753保留。运行独立reader的 after-fixture-exit 阶段，真实记录空/丢格式或全部匹配，不能先reseed遮掉该结果。
3. 仅重新发布脚本内固定测试payload，保留新提供者并记录PID；现有原provider仅发布一次，没有运行时reseed命令，因此必要时可启动第二个固定提供者，不能冒称管理器接管。先独立读取全部格式核对新基线，并在fixture同内容refresh后再次独立核对；任一步不可独立读就暂停中文恢复验收。
4. 不调整嵌套窗口、scale或宿主布局。新gui_open之前两次KScreen布局与owner一致，保存快照；主会话发起Start且用户手动授权，ready之后再两次读取，确保与授权前基线一致。若变化则保留失败并重新授权，不能复活旧坐标或猜映射。确认absolute_input=true后才进行专用窗口输入。
5. 如实际输入发生，gui_close、fixture退出、固定provider退出后的独立全MIME摘要分别记账；无ClipboardManager仍可能不能跨provider生命周期恢复，不能用自读或reseed宣称AC-04通过。

另按主会话窄范围分配，仅把 scripts/gui-smoke.py 的 pending 静态文案由“五类桌面”改成当前 Windows/X11/GNOME Wayland/KDE Wayland，未改行为；现有24项离线GUI脚本回归通过（0.892秒），没有重跑宿主桌面操作。内部生产GUI代码未修改。


## 追加：仅临时分段验收入口（尚未原生执行）

旧环境的独立读取失败保持为FAIL；真实输入serial缺失只是候选原因。按主会话明确分配，本轮没有修改GUI生产或扩大可复用脚本，在临时目录准备分段入口复用现有Client、PNG定位、专用夹具及既有中文输入计划。所有新的桌面流程仍等待主会话执行和用户手动授权。

- `segmented-smoke.py` 在新gui_open前、ready后各读取两次KScreen配置和unique owner，并要求四份基线一致、absolute_input可靠。权限等待与初次夹具焦点默认120秒；没有自动批准、猜映射或恢复旧capture。
- 在截图中定位专用夹具，只点击本夹具空蓝板，并核对新产生的press/release及坐标。独立reader夺取焦点后、同内容refresh之前，再核对一次夹具自己的真实点击，避免把ActiveWindow或另一窗口输入serial当作本提供者的成功。
- 新 `transport-reader.py` 每阶段启动独立Wayland客户端、不发布selection、不访问PRIMARY。读取必须先收到自己蓝板真实press/release，ownsClipboard=false；全部MIME/长度/哈希有界，非空offer观察两秒稳定，空offer观察15秒。GUI仍存活时只点该reader专有蓝板，关闭后明确输出人工点击阶段；不能用Qt提供者自缓存代替传输。
- 基线/refresh后独立全格式必须匹配，否则立即阻断gui_text/gui_key。只有通过该门槛才执行既有计划；每次输入前确认夹具焦点和有效映射，每次gui_text后核对真实字段及独立原MIME。随后分别核对gui_close、夹具退出、全部本次固定提供者退出后的独立摘要。
- 只有前一独立恢复检查通过才退出夹具或显式 `--exit-providers` 指定的固定来源。提供者每次退出前核对私有环境、精确脚本argv和PID启动标识；失败关闭GUI但保留未确认的夹具/provider，不自动救援或reseed。
- 入口启动要求原30分钟租期至少还剩20分钟，不延长监管器、不SIGSTOP接管。当前旧环境不足该门槛，入口会在gui_open之前拒绝；主会话应先保存旧fixture退出后独立结果，再精确STOP旧环境并启动全新同结构目录。

已准备新目录 `/tmp/remote-mcp-nested-segmented-ilwa4usz`，本报告冻结时**没有启动该目录**：无outer-pid/readiness、无新Portal.Start、无新MIME发布。监管白名单、0700目录、私有总线/Wayland/PipeWire/Portal/KScreen结构不变；current二进制SHA256仍硬校验为dd848f74a1e86a110cb2323ce21911d5561975c992898ac772c1d4c0156b5c80。实际新输出尺寸以只读readiness为准，未因过测修改scale或放宽生产保护。

主会话精确启动顺序如下（桌面socket/进程访问由正常审批执行）：

```sh
python3 /tmp/remote-mcp-nested-segmented-ilwa4usz/start.py
```

监管器只启动私有组件与只读gui_status；主会话重新核对readiness中的组件来源、owner和接口。随后启动仅固定数据的提供者，由用户点击其内层窗口后发布，记录fixed-provider.json内PID：

```sh
python3 /tmp/remote-mcp-nested-segmented-ilwa4usz/run-private.py \
  python3 /tmp/remote-mcp-nested-segmented-ilwa4usz/fixed-mime-provider.py \
  --wait-for-native-click
```

主会话通知用户本轮Portal授权及关闭后独立reader点击的位置，再以记录的实际提供者PID执行（示例PID不能照搬）：

```sh
python3 /tmp/remote-mcp-nested-segmented-ilwa4usz/run-private.py \
  python3 /tmp/remote-mcp-nested-segmented-ilwa4usz/segmented-smoke.py \
  --execute --authorize-wait-seconds 120 \
  --output-dir /tmp/remote-mcp-nested-segmented-ilwa4usz/evidence-segmented-01 \
  --provider-pid <本次固定提供者PID> --exit-providers
```

若有多个本次来源，逐个重复provider-pid，不能遗漏后宣称跨提供者退出通过。关闭后的 `manual_reader_click` 阶段须用户只点击内层独立reader的空蓝板，期间不发布数据。无ClipboardManager仍是环境限制，任何阶段空或格式丢失都真实记失败；run_completed只能在全部退出后的独立校验完成后为true，acceptance_complete仍false（不能以单KDE代替其余平台/场景）。

离线检查已完成：新目录全部Python文件AST解析、help/no-execute行为（未创建证据/桌面进程），以及 `test-segmented-offline.py` 完整模拟流程。该回归让夹具自读成功但refresh之后独立reader为空，断言gui_text/gui_key未被调用、GUI已close、夹具与固定provider保留，并另记录关闭后独立失败。它只验证门槛逻辑，不能替代原生传输、权限、实际输入或恢复验收。未执行新的宿主/内层桌面操作，也没有退出旧fixture/provider。


## 追加：新分段环境 evidence-segmented-01 模式实例ID漂移

新环境由主会话实际bootstrap并核对PID/owner/env/socket/hash；固定provider PID2275570经用户真实点击发布，native_mouse_input_verified=true。主会话执行分段入口，用户手动授权且保持外层尺寸，运行仍在after-open布局门槛失败：无submitted_operations，gui_close确认，夹具PID2284179保留。本代理未新Start、发布、输入或退出来源。

本代理只读精确比较保存的layout-before-open.json和layout-after-open.json，随后经私有run-private入口读取关闭会话及两份现配置，落盘 `evidence-segmented-01/layout-failure-readonly.json`。结果如下：

| 字段 | before（两份稳定） | after（两份稳定） | 后续只读current（两份稳定） |
| --- | --- | --- | --- |
| KScreen unique owner | :1.4 | :1.4 | :1.4（补充只读查询） |
| currentModeId及modes[0].id | 70 | 74 | 84 |
| WL-0尺寸 | 2048×2334 | 2048×2334 | 2048×2334 |
| scale / pos / rotation | 1 / (0,0) / 1 | 1 / (0,0) / 1 | 1 / (0,0) / 1 |
| mode名称 / refreshRate | 2048x2334@90 / 90 | 相同 | 相同 |
| connected / enabled | true / true | 相同 | 相同 |

精确字符串差分表明before→after只差上述两个模式实例ID；after→current也仅该两个ID由74变84。其余配置（包括screen.currentSize=2048×1280这个与output不同的字段）逐字一致，故这次保存的门槛失败不是字段顺序改变或已观测尺寸/scale/位置/owner变化。

closed会话ef0e480e3609ae82918e5894704dfd7a实际返回state=closed、absolute_input=false、generation1、logical_bounds=1280×1459、pixel_width/height=0（未走截图）；reason.absolute_input为 `Reliable logical layout monitoring is unavailable or the layout changed during authorization; absolute coordinate input is disabled`。能力仍保留text初始Portal格式盲区原因；本代理没有读取Clipboard。

生产 `layout_portal_linux.go` 的readLayout(KDE)、layoutSignal和主动verify均对完整config调用layoutDigest，包含currentModeId和mode.id；它并非已实现几何投影。因而**不能将本轮归为只有/tmp门槛误报**：这些模式实例ID漂移同样会触发产品保守失效，仅放宽临时检查无法复活生产绝对输入。没有授权全程信号/配置历史，不能排除快照之间曾出现真实几何变化，也不能声称模式ID重编号是此次产品失效的唯一因果。

已向主会话报告最小后续方案，尚未实施、仍需其授权范围：KDE基线读取、信号与主动校验共用严格类型检查的canonical当前几何及owner摘要；通过currentModeId查找实际当前mode尺寸，不把易变模式实例ID作为几何本身，同时保留output身份、connected/enabled、位置、有效尺寸、scale、rotation及服务owner等映射必要信息。必须覆盖ID换而几何相同、ID不换但尺寸/scale/位置改变、授权中间真实几何改变后返回原状仍失效、无效/缺失mode映射、owner变更；临时门槛随后使用同一标准。当前生产代码和临时门槛均未修改，固定来源及夹具仍由主会话掌握生命周期。


## 追加：生产KDE当前几何摘要修复及冻结

主会话把最小生产修复追加design/implement并明确授权后，本代理修改 `internal/gui/layout_portal_linux.go`，新增 `layout_portal_linux_test.go`，调整linux_test.go与portal_protocol_linux_test.go的旧KDE模拟配置。Windows、GNOME serial路线、公开GUI接口和剪贴板生产逻辑均未改。本节是实现与离线/模拟测试记录，不能替代新二进制的原生授权验证。

依据 [KDE Plasma/6.7 ConfigSerializer](https://raw.githubusercontent.com/KDE/libkscreen/Plasma/6.7/src/configserializer.cpp) 的serializeOutput/Mode/Screen与本机a{sv}快照，模式编号用来定位当前模式；输出身份和各几何字段独立保留。readLayout、configChanged与主动verify现在使用同一KScreen投影：输出id/name、connected/enabled、pos、output.size、scale、rotation、当前实际mode.size、clones/replicationSource，另保留screen.currentSize。模式临时ID、非当前模式枚举顺序/额外合法模式、图标与其他不影响当前坐标的字段不进入摘要；数组按稳定身份排序。screen.currentSize与output.size分别保存，不添加二者相等的猜测约束。

完整源配置在投影前仍受既有递归深度24、节点8192、字典256、数组4096、字符串4096字节与总JSON256KiB限制，忽略字段也不能绕过限额或NaN/Inf拒绝。输出额外限64、每输出mode限256；必需字段严格检查类型、有限数值和32位几何整数范围；scale>0且<=64、rotation限定KScreen四种值，输出身份/模式ID唯一，currentModeId必须唯一解析当前实际尺寸。正常未连接且禁用的输出支持空currentModeId、空modes及默认size(-1,-1)，其身份/连接/启用仍在摘要；启用却未连接或缺有效模式失败。KDE getConfig要求正好一个返回字典，匹配监视路径的畸形配置通知使映射失效，不再静默忽略。owner替换、读取失败以及中途真实几何变化后回原值的永久失效语义保持。

[Output头文件](https://raw.githubusercontent.com/KDE/libkscreen/Plasma/6.7/src/output.h) 说明clones引用其他output并对称，replicationSource=0表示无来源；据此完成所有输出后检查悬空、自指、非对称clones及复制循环，拒绝无法解释的镜像配置，不新增镜像输入能力。真实变化测试使用双输出及双向clone，不把单输出悬空引用当正常几何。

新增回归使用本轮真实只读快照结构（含不相等的screen/output尺寸），覆盖模式重编号、输出/mode枚举顺序、身份/位置/尺寸/当前mode尺寸/scale/rotation/连接/启用/镜像几何变动，无效类型/模式查找/重复身份、全部有界数据约束，inactive无mode，匹配信号畸形、无关信号隔离，中途几何变化后原值不可复活，以及私有总线读/信号/主动核验标准一致与额外返回字段拒绝。旧授权漂移、owner替换和断线回归改用真实shape，原有失效断言保留。

最终检查：

- `GOCACHE=/tmp/remote-mcp-gui-layout-go-cache go test ./internal/gui -count=1`：通过0.210秒。
- `GOCACHE=/tmp/remote-mcp-gui-layout-go-cache go vet ./internal/gui`：通过。
- `GOCACHE=/tmp/remote-mcp-gui-layout-go-cache REMOTE_MCP_GUI_PORTAL_TEST=1 go test -race ./internal/gui -count=1`：审批下完整临时私有D-Bus/X11模拟回归通过5.512秒；不访问宿主桌面/剪贴板，不发起Portal授权。默认沙箱的私有总线启动EOF如实标为环境限制，正常审批重跑通过。
- gofmt已完成。全仓gate与产品二进制重建由主会话/检查代理统一执行，本代理未额外扩大构建或修改依赖。

临时分段入口也已同步，避免手写另一套生产规则：`build-canonical-helper.py`仅提取上述生产normalizeLayout/kscreenGeometry/kscreenDigest函数到/tmp小Go助手，并用本项目已有godbus依赖编译。`kscreen_canonical.py`通过本机已有PyGObject GLib读取gdbus文本，再传有界JSON；助手在同份规则下恢复Variant结构校验，输出摘要及几何，且逐次核对生产layout文件的sourceSHA。该临时依赖是当前机器已有能力，不是新增产品运行时要求。stable门槛只比较几何摘要和unique owner，raw配置仍保存在新证据中，不改写历史report。clipboard固定摘要变量与canonical_geometry名称分开，离线流程在baseline之后再次调用实际layout函数覆盖名字遮蔽回归。

保存的六份原生70/74/84快照全部同几何摘要；scale与当前mode尺寸改变摘要不同，未知mode、悬空复制和超字符串被拒绝。完整离线失败门槛回归在旧/新目录均通过：夹具自读成功而refresh后独立空读时，text/key保持阻断，GUI关闭，来源保留。这些都不证明新的实际输入或剪贴板跨提供者退出恢复。

生产与/tmp已分别通知主会话和检查代理冻结；旧私有桌面由主会话精确STOP。本代理仅准备新的 `/tmp/remote-mcp-nested-segmented-6q4_291z`，无Start/授权/发布。新目录包含canonical小助手和同结构监管入口；inner.py当前二进制白名单仍为旧dd848...，必须由主会话新build后同步准确SHA，再只读bootstrap，不能取消版本门槛。若生产layout文件之后再修，须重建canonical-helper以通过sourceSHA门槛。此前原生失败证据、无ClipboardManager限制以及关闭/夹具退出/全部提供者退出后的独立读取要求继续有效，剪贴板生产逻辑不在本轮修复范围。


## 追加：按新AGENTS重建到项目.tmp（未启动）

用户即时更新规则：临时文件只能在工作目录 `.tmp`，不得访问系统临时目录。收到该规则后，本代理没有读取、复制、扫描或执行任何旧系统临时文件；仅从自身会话上下文、现仓库源码和本报告重建必要入口。主会话已结束全部旧监管器/组件/provider，并将审过的Linux产品二进制放 `.tmp/remote-mcp-gui-native-current`，SHA256为bb010f9e42c75748bb15479a1d7d3b5518bb9125fd3c8fcff983c5500c5c1bfe。

新目录是 `/home/user/projects/remote-mcp/.tmp/kde-native-a`，0700；runtime/config/cache/data/home/logs/temp/go-cache/go-tmp全部位于本目录并0700。最长Unix socket路径为75字节（pipewire-0-manager），bus、内层Wayland、PipeWire主/manager路径均小于108字节。此时没有启动：不存在outer-pid/readiness/私有socket；本代理没有调用Portal、Clip API或实际桌面输入。以下入口只在主会话审批与用户手动授权后执行。

| 文件 | 边界 |
| --- | --- |
| start.py / inner.py | 私有无自动激活总线；明确现成KWin、PipeWire、WirePlumber、KScreen、PermissionStore、KDE Portal、frontend和当前MCP PID；只读Probe，不Start |
| run-private.py | 只使用监管器白名单；总线地址必须精确本次runtime/bus或其逗号参数，内层socket/PID/TMP变量核对后exec |
| fixed-mime-provider.py | 默认必须本白色专用窗口真人press/release后才发布3MIME95字节固定数据；不读取Clip/PRIMARY；超时退出码准确透传 |
| transport-reader.py | 每阶段独立原生全屏窗口，实际蓝板press/release且ownsClipboard=false；全格式摘要，非空稳定2秒、空稳定15秒；结果/ready原子pending+replace |
| segmented-smoke.py | 授权前/后几何+owner双读；余租约>=20min；私有参数1..120秒硬上限；独立基线/刷新门槛先于中文；每次焦点/真实事件守卫；关闭后蓝板人工读取 |
| build-canonical-helper.py / kscreen_canonical.py | 当前生产Go投影同源；临时标准库Value载体替代D-Bus类型，不连接总线；GLib文本解包与有界JSON、sourceSHA核验 |
| test-offline.py | 完整mock阻断路径、实际同源摘要和GLib wrapper、sourceSHA过期拒绝、租约/参数边界；不启动桌面 |

PATH明确为 `/usr/bin:/bin`，只调用已安装系统二进制，不继承宿主PATH；HOME/XDG/媒体/总线全部私有。每个子组件显式TMPDIR/TMP/TEMP为本目录temp，Go助手GOCACHE=go-cache、GOTMPDIR=go-tmp、GOENV=off、GO111MODULE=off、CGO_ENABLED=0，仅标准库，无依赖下载或全局模块缓存写入。KWin父连接仍只读使用已授权宿主绝对socket `/run/user/1000/wayland-0`，内层程序不继承宿主display/bus/凭据，不读取宿主Clipboard/PRIMARY。KScreen初始化只有requestBackend只读路线，无setConfig。ready前四个服务owner PID再次与本次Popen PID逐项核对，并记录unique owner；组件实际env也核对私有TMP变量。内核阻止独立KWin环境读取时仍只记录明确启动来源/PID链，不绕过权限。

bootstrap拒绝已存在STOP/readiness/outer-pid的目录，不能启动两套监管器；监管器仍有30分钟界限，创建STOP按持有的精确组件进程组结束，不做SIGSTOP接力或unsafe延期。provider/fixture的未确认恢复来源不会被分段脚本自动退出；全部提供者退出后独立格式仍可能因没有ClipboardManager而失败，必须保留该真实限制，不能借自缓存或重新发布固定内容宣称接管。错误诊断仅固定码、input_may及恢复枚举（实际failed），不复制Body/输入内容。

实际离线检查均使用显式项目临时环境：

```sh
TMPDIR=/home/user/projects/remote-mcp/.tmp/kde-native-a/temp \
TMP=/home/user/projects/remote-mcp/.tmp/kde-native-a/temp \
TEMP=/home/user/projects/remote-mcp/.tmp/kde-native-a/temp \
PYTHONDONTWRITEBYTECODE=1 \
/usr/bin/python3 .tmp/kde-native-a/build-canonical-helper.py
```

构建通过；同环境执行 `.tmp/kde-native-a/test-offline.py` 通过：夹具自读成功但refresh后独立为空时，gui_text/gui_key未调用、GUI确认关闭、fixture/provider保留；baseline之后实际layout函数仍可调用；failed枚举记录正确且没有message泄漏；当前仓库真实快照重编号同摘要、scale改变不同、无效mode/超字符串拒绝；实际GLib文本wrapper正确，伪造过期sourceSHA拒绝；租约不足20min在创建证据/GUI授权前失败；参数1/120接受noexecute、0/121/605拒绝且不创建证据。TemporaryDirectory均明确dir=项目temp，未使用系统临时目录。全部Python AST、help/no-execute和socket路径长度检查通过。

生产layout保持冻结，本轮没有再改生产代码、依赖、公共脚本或任务状态。主会话/检查代理已收到新路径和冻结通知；新环境仍须实际bootstrap重新核验，不能把这次重建离线通过等同于原生就绪或验收。

主会话下一步精确入口（本代理未运行）：

```sh
/usr/bin/python3 /home/user/projects/remote-mcp/.tmp/kde-native-a/start.py
```

只读门槛通过后，主会话启动私有固定provider，用户点击白色专用窗口一次：

```sh
/usr/bin/python3 /home/user/projects/remote-mcp/.tmp/kde-native-a/run-private.py \
  /usr/bin/python3 /home/user/projects/remote-mcp/.tmp/kde-native-a/fixed-mime-provider.py
```

记录实际本次提供者PID，再由主会话通知用户授权并执行分段（所有权限弹窗必须手动，关闭后各阶段只点独立reader蓝板）：

```sh
/usr/bin/python3 /home/user/projects/remote-mcp/.tmp/kde-native-a/run-private.py \
  /usr/bin/python3 /home/user/projects/remote-mcp/.tmp/kde-native-a/segmented-smoke.py \
  --execute --authorize-wait-seconds 120 \
  --output-dir /home/user/projects/remote-mcp/.tmp/kde-native-a/evidence-segmented-01 \
  --provider-pid <实际本次PID> --exit-providers
```

补记并发状态：上述“未启动”是本代理离线准备/冻结时的状态。最后仅做存在性检查时发现readiness/outer-pid已出现，主会话确认这是检查代理最终PASS后由root发起的真实bootstrap，已ready_no_authorization。该目录生命周期此后由主会话持有；本代理未再执行任何启动、停止、授权或剪贴板操作，脚本继续冻结。

## 追加：工作目录首轮实跑边界及有界截图准备

本节依据主会话实际运行的 `.tmp/kde-native-a/evidence-01` 与其独立观察反馈，不改写历史报告。本轮 Portal 成功响应、稳定当前几何/绝对坐标映射、专用夹具首次实际鼠标事件均通过；独立 before-refresh 与 after-gui-close 读取均为固定三格式95字节且摘要相同。其后 native-click-before-refresh 的精确四 marker 门槛失败，尚未发出 refresh_offer，中文/键盘输入均为0。主会话看到截图中前一个 reader 与新夹具有透明淡入淡出叠加，四个精确颜色都偏离；这是呈现过渡的候选原因，不能据此单独归因生产截图错误，也不能弱化精确颜色断言。

原 finally 瞬间 poll 存活即记录 fixture_retained，并不能证明 runner 退出后仍保存备份；主会话实际核验该夹具在执行会话结束后已消失。因为此轮没有刷新或输入，原固定 provider 仍存，不发生唯一原来源丢失。本代理按批准的最小范围修正临时 runner 生命周期：刷新请求发出前就记录 clipboard_refresh_attempted；刷新返回错误、格式验证失败、独立刷新后读取失败都可能已发布原快照，不能按“未成功”自动结束夹具。GUI 关闭和独立关闭后读取完成后，活夹具由仍存活 runner 有限持有，报告 runner_holding/PID/原租约；只等待 evidence/RUN_STOP、整体STOP、夹具退出或原租约，无任何额外输入/读写/救援。hold 结束明确 retained=false 和固定原因，实际保留状态仍由主会话核验宿主 PID。刷新前失败则清理自己的夹具并报告 cleaned，不宣称 runner 退出后保留。

截图准备只改工作目录临时 `segmented-smoke.py`、新增 `fixture_frame.py` 和 reader 的 ready 时间戳，生产及仓库 GUI 脚本不变。每次准备共享3秒等待/成功发布截止、最多61次、帧不合格后最多等待50毫秒；每帧前后核对本目标进程、焦点/paint与映射generation。fixture 使用新请求 ACK；reader 使用本轮持有 PID、active/painted及250毫秒刷新 ready 的新鲜时间戳，不能借别的相同 marker 窗口当目标。原四颜色、计数、矩形/尺度断言全部保留；额外在固定控件内部 (570,140) 取签名，fixture 显式白色字段为RGB(255,255,255)，reader 相同位置背景为RGB(229,232,235)，远离字段文本和边框。PNG 校验与定位复用同次有界解压扫描行，整次采集只一次解压。

只重试格式/CRC/元数据完整校验通过而 marker/目标签名暂不合格的图片；权限、网络、协议、布局、目标退出或失焦立即失败。最终合格 capture 才允许一次输入，输入本身不重试。临时 PreflightClient 将实际 urlopen/socket 每次响应读取 timeout 限为同一剩余截止；HTTPResponse 最后 Content-Length 块自动关闭 fp 后正确结束，chunked 同样支持。过截止才返回的好响应/好图也拒绝。同步 Python PNG 校验、JSON/磁盘及系统调用不能被强抢占，故3秒是共享等待与成功发布预算，不声称操作系统所有同步工作硬上限；late completion 不会触发输入。

冻结前全部验证仅离线、无桌面/Portal/Clipboard访问，临时目录显式为项目 `.tmp/kde-native-a/temp`：

```sh
TMPDIR=/home/user/projects/remote-mcp/.tmp/kde-native-a/temp \
TMP=/home/user/projects/remote-mcp/.tmp/kde-native-a/temp \
TEMP=/home/user/projects/remote-mcp/.tmp/kde-native-a/temp \
PYTHONDONTWRITEBYTECODE=1 /usr/bin/python3 .tmp/kde-native-a/test-preflight.py
```

同环境 `test-preflight.py` 通过：过渡后精确帧只一次输入；持续过渡、晚到好帧、取消、布局变化、网络错误、失焦、reader错误PID均不输入；两窗口类型交叉签名拒绝及一次PNG解压；实际RPC timeout/迟到拒绝；标准库HTTPResponse真实Content-Length与chunked内存链完成不会因fp关闭误失败；失败hold的RUN_STOP及原租约结束无新输入。`test-offline.py` 通过三个完整mock分支：独立刷新后读取失败、refresh请求返回错误、refresh结果格式验证失败，都阻断text/key、确认GUI关闭、进入仍存活夹具hold并等待RUN_STOP；同源摘要/GLib/sourceSHA、20分钟租约、120秒参数门槛旧回归继续通过。

主会话在剩余租约不足20分钟门槛后已精确STOP旧a环境，监管器和固定provider退出均为0；本代理没有启动a或b。后续由主会话白名单复制脚本/helper及仅portals.conf/bus.conf到新的工作目录私有b环境，不能复制证据、状态、令牌或延长旧租约；检查代理定向通过后再启动。关闭后、夹具退出后、全部提供者退出后的独立可读性仍分别必需，本轮没有text/refresh成功证据，完整剪贴板恢复和接管继续待验收。

补充最终预算口径：仅首次授权后的focus预等待保留120秒；非首次safe click、plan-before、每次operation及authorized reader均去除shot外重复focus/mapping，改由同一shot内部fresh ACK、前后mapping和3秒截止完成。JSON解析后及报告save后也再次检查截止/取消，检查成功后才发布capture/point；迟到JSON与迟到save回归均0input且无可用capture。两套离线检查重跑全通过，12个Python AST通过，尚未启动新环境。


## 追加：私有b首帧像素物化与临时generation门槛

主会话实际运行 `.tmp/kde-native-b/evidence-01` 已人工授权，首shot被“layout changed during screenshot preparation”拒绝，0input；GUI已关闭且刷新前夹具清理。当前实际PNG元数据为2048×1526、逻辑1280×954、display_id=60、frame_sequence=1、generation=2；后状态仍ready、absolute_input=true、同尺寸且generation=2。本代理仅只读这些已落盘文件和生产源码，未操作仍活的b桌面、Portal或Clipboard。

生产Wayland Capture会首次把display.PixelWidth/Height从0设为PNG实际尺寸；仅old.PixelWidth已知且尺寸变化才禁绝对映射。Manager.refresh对完整Display差异递增generation并清旧capture，Screenshot随后以新generation发布cap。故首PNG像素物化允许before1→after2，不能在临时验收里把一切代号增加当布局失效。此次旧脚本覆盖latest-status且未保留before，现存frame_sequence1/after映射有效与该合法路径一致，但无法从已存文件唯一还原before，不能断言授权间没有任何未保存变化。

最小修复仅在a的 `fixture_frame.py` 与 `segmented-smoke.py` 临时入口：before/after状态投影严格校验session/backend/ready、mouse/screenshot能力、最多64个完整Display身份及类型、有限逻辑几何、双0或双正pixel，输出去掉message和capability reasons。相同generation也必须整Display集合/顺序一致，cap的session/display/尺寸/逻辑/全屏region及generation必须等于after。唯一额外允许的是Wayland首frame_sequence=1、generation恰before+1、选中display两pixel都0→cap对应实际正数；name/primary/身份/逻辑/absolute与所有其它display字段及顺序不变。这个例外在整session只能使用一次；已知pixel改变、部分0、跨两代、已失效映射、未知模式/错误cap身份或缺字段均拒绝。没有放宽生产generation、映射或旧capture规则。

每attempt在实际PNG前和后分别保存有限before-status/after-status证据；PNG当前cap元数据仍沿用原证据文件。写入、校验、匹配完成后仍检查同一3秒截止，迟到不发布可输入capture；没有重复input或额外授权。离线新增真实首帧shape(2048×1526/1280×954)全shot回归：双0/generation1→已知/generation2通过，一次mouse，保存的前后证据尺寸与逻辑相符。纯校验覆盖同代变化、已知pixel、部分0、逻辑/name/primary/absolute/身份、其它display像素/集合顺序、增移display、缺字段/重复ID、错误session/captureID/capgen/逻辑/region、后续frame、跨两代、非法巨数等失败。原过渡/取消/期限/RPC/hold回归继续通过。

明确项目TMPDIR/TMP/TEMP与PYTHONDONTWRITEBYTECODE=1下，`test-preflight.py`、`test-offline.py`均通过；全部12个Python AST通过。本轮生产及仓库GUI脚本不改，b实际执行入口已结束，后续由主会话核对定向review与hash后同步并重新决定实跑，原20min启动门槛不延长。

最终定向复审发现新增测试case被机械列表替换误带入same_gen分支，造成错误display_id提前拒绝并掩盖各自真正边界。检查代理独占修正该测试分支；runtime无需新增改动。复审增加独立巨数status/capture RuntimeError断言与非wrong-display用例仍保持display_id=60的防掩盖检查，两套离线测试再次exit0、4个变化文件AST及diff-check通过。最终test-preflight SHA为38594fcc94a4baf032e424cab9fc2dbdeb88fe1158d6ef232619e258d9ddde60；segmented f028404a25f50d66c7cbe11b41aa5aa245dedb6c7fd161756d5abeb64edd9595与fixture_frame 2f03b786f459db41267658f265a5c5f72f14acf75cf28bcd104e2ed4a352fd85保持。检查代理和主会话已收到最终冻结门槛，仍没有本代理实机操作。


## 追加：c真人点击超时及临时provider呈现状态

用户反馈未看到白色窗口；主会话记录 `research/kde-workspace-bootstrap-03.json` 为manual_provider_click_not_observed：provider在120秒未收到点击后exit1，wrapper同样超时，主会话STOP c、outer退出0。此轮没有Portal.Start、Clipboard访问或GUI输入，不构成原生验收成功；旧来源、组件均由主会话精确结束。

本次最小范围仅 `.tmp/kde-native-c/fixed-mime-provider.py` 与新增 `test-provider-offline.py`，不修改segmented/frame/生产或其它UI。提供者现在在show前发布created状态，随后每250毫秒采样真实 `windowHandle().isExposed()`、QWidget visible/active与实际paintEvent计数；show或visible不等于exposed，paint计数也不能代替compositor暴露状态。参照[Qt QWindow::isExposed](https://doc.qt.io/qt-6/qwindow.html#isExposed) 与 [QEvent::spontaneous](https://doc.qt.io/qt-6/qevent.html#spontaneous)：窗口被应用show后仍可能未暴露；spontaneous仅说明由窗口系统产生，不能证明输入者的人类身份。未自动批准/自动点击，主会话仍要求真人对专用窗口操作。显式白底黑字样式固定该测试窗口的“白色”描述，两个已有标签不扩展UI结构。

状态阶段区分created、waiting_presentation、waiting_native_click、fixed_mime_published及明确退出/超时；包括本PID/title/platform、visible、native_handle_present、exposed、active、paint_count、更新时间、点击截止与原租约，只有固定标志，完全不读取Clipboard。global `fixed-provider-readiness.json` 与 `fixed-provider-<PID>-readiness.json` 均4096字节限额、pending写入后原子replace；固定测试摘要报告也改原子发布。状态不是Clipboard数据，不含用户内容。实际QWindow exposed/paint仍不能证明宿主外层嵌套窗口未被其它窗口遮挡或目标用户看见；主会话须同时核验存活PID/新鲜状态，并只在实际visible+exposed+paint>0时请求真人点击。

人工点击总等待为300秒且受原session lease同时约束，不因窗口晚呈现延长。native左键press和release双方必须active并为spontaneous事件，未配对、非左键、inactive或合成Qt事件均不发布；提交前还要visible/exposed/paint以及剩余期限确认，构造固定payload跨期也拒绝。点击之前0次app.clipboard调用，PRIMARY从未触碰。发布后最多1800秒或原租约，以更早者为准，STOP/原租约或精确PID信号结束，不延长原监管器。超时英文为 `Timed out after 300 seconds waiting for a native left-button press and release in the active, exposed private provider window`，原租约提前到期有单独英文原因。

项目c/temp显式TMPDIR/TMP/TEMP和PYTHONDONTWRITEBYTECODE=1下，完整mock Qt入口测试通过15个场景：show-only在121秒仍等待但300秒准确失败；paint未exposed/exposed未paint保持0Clipboard；未按即释放、inactive按下/释放、合成事件、非左键、迟到点击拒绝；有效原生配对只一次写固定3MIME95字节；STOP、短原租约、构造payload跨租约、发布后1800秒与原租约的先到界限均正确退出。实际原子replace失败与超过4096字节的状态不会替换旧文件；没有真实Qt QApplication、窗口或Clipboard初始化。两文件AST检查通过，已交检查代理定向复审/冻结，本代理没有启动c或d。主会话下一轮wrapper等待必须匹配300秒并受原租约，而不能仍沿用120秒。

定向复审最终补齐两项机械边界：发布后历史native_mouse_input_verified使用published or clicked，后续无效release不能擦掉已验证证明；最后window状态读取后再检查deadline/STOP，慢状态读取跨期保持0Clipboard。检查代理实际运行16场景与原子/限额回归exit0(0.164秒)，AST/diff通过。最终provider SHA5222fe3bb620bd628e03019a3e2e576728bbff6b52f66d7ca218dca8ebef3a33，test SHA7b864e977ee5f66bcf122ac544ce066b67ada254ee43424ebabbc264c0a7c978。仅c临时提供者/测试与本报告，未启动d；冻结交主会话。


## 追加：原生QLabel事件向parent传播丢失spontaneous标志

私有d实际provider PID2468332的状态visible/exposed/active=true、paint_count=3；用户明确已点击，后续状态却仍waiting_native_click、published=false、native_mouse_input_verified=false，stderr无traceback。旧状态未记录press/release尝试，故不能断言未收到事件，也不能唯一确认本轮点击命中哪个widget。主会话已精确停止该provider/wrapper，保留私有desktop原租约；此轮0Portal、0Clipboard、0GUI工具input。本代理未修改d文件或live状态。

只读查询本机PySide6.QtCore.qVersion为6.11.2，没有初始化QApplication。官方[同版QApplication::notify源码](https://raw.githubusercontent.com/qt/qtbase/v6.11.2/src/widgets/kernel/qapplication.cpp#L2534)的鼠标传播循环在首次notify(child)后将原event.m_spont清false（L2552），parent副本从已清的mouse.spontaneous读取（L2540）。[同版QLabel源码](https://raw.githubusercontent.com/qt/qtbase/v6.11.2/src/widgets/widgets/qlabel.cpp#L1490)在无text control时ignore事件，触发向parent传播。因而把parent Provider回调的spontaneous当原生鼠标硬条件，是一个确定可触发的合法点击拒绝缺陷；它能解释用户点说明文字后未发布，但缺少本轮事件留证，仍只能推断实际原因，不能倒推已确诊。

主会话授权的最小修复只编辑c临时provider/test：两个已有纯说明QLabel设为WA_TransparentForMouseEvents，让文字/空白都由同一原生Provider直接接收，保留spontaneous、active、成对左键与所有exposed/paint/期限/租约/提交前条件。[Qt属性文档](https://doc.qt.io/qt-6/qt.html#WidgetAttribute-enum)说明该标志让鼠标命中跳过widget及子树；不增application eventfilter，不新增UI控件，不改变固定数据或旧Portal/生产实现。

诊断只包含最多2^31-1饱和press/release尝试次数和单个最后事件：kind、active、spontaneous、固定button枚举left/right/middle/other、paired_press、accepted、固定reason枚举accepted_press/accepted_release/rejected_button/rejected_inactive/rejected_non_spontaneous/rejected_unpaired_release。没有坐标、键、窗口文字、host数据或事件数组；状态原4096字节/原子替换门槛保持，press/release后立即刷新纯状态，不访问Clipboard。

17个完整Qt-mock场景加原子/限额检查均通过；明确检查两label透明属性，模拟目标命中说明文字时原始receiver为Provider且有效原生点击只发布一次固定3MIME95字节。非spontaneous、inactive按压或跨focus释放仍拒绝；诊断原因/布尔/枚举、计数饱和及发布后历史证明不重复Clip调用均有回归。两文件AST通过；mock并非实际Qt hit-test或实机点击成功证据，后续仍由主会话人工流程验证。本代理未启动GUI、访问Clipboard或延长租约，c最小修复冻结交检查代理。

检查代理本轮定向PASS：17个全入口mock与原子/4096字节限额回归实际exit0(0.264秒)，AST/diff绿，同版Qt传播源码与属性文档复核。最终provider SHA1fa87253a85ad834180a4619f45d5f79401efc9143788f179d8eaf39dce61d96，test SHA7fec0f33ac1fa80d62079cdc4990c289fd5b60849da8c631719eb1f65dc6b5d0；无复审追加代码变化。主会话已收到门槛，原租约与未实机成功边界保持。

## 追加：e截图准备超时的离线定位及私有像素解析替换

主会话实际e/evidence-01已人工授权，首帧pixel物化、初次fixture及reader精确定位、一次实际mouse、独立before-refresh全部3MIME95字节均通过。native-click-before-refresh的3秒共享准备超时，0refresh/text/key；gui_close/reclose确认，真人afterClose独立reader仍读出全部95字节，刷新前fixture清理。提供者仍由主会话持有原租约，本代理只读已落盘PNG/有限状态与日志，没有新截图、GUI、Clipboard或Portal操作。

必须区分当前PNG与首次拒绝帧：attempt1 before/after状态时间为14:07:56.396/58.976 UTC；attempt2 before时间59.080，当前PNG写入时间59.262且frame_sequence=4。旧capture_frame每次用相同label覆盖PNG/metadata，因此当前图是第二次尝试，不是第一次。首次进入下一循环只能证明其精确marker或目标签名未合格，原图已被覆盖，不能再唯一归因；不可用当前看似稳定图倒推首次成功。两次已保存状态均generation2/ready/absolute_input=true。

旧算法的纯离线回放显示，当前第二次图四marker各1024、同1.6缩放点位、fixture签名RGB(255,255,255)全部合格；Python逐字节PNG unfilter和逐像素tuple扫描耗约1.94秒，严格zlib校验约11毫秒。其它两张真实PNG也完全合格，旧定位约1.52–1.95秒。真实首尝试GUI截图RPC约182毫秒，到after-status约2.4秒；第二次PNG约166毫秒返回后旧解析耗尽剩余预算，原门槛正确拒绝迟到input。该证据解释解析成本，但不证明被覆盖的第一张为何未合格。

本次只编辑c私有fixture_frame.py/segmented-smoke.py/test-preflight.py及必要test-offline.py适配、新test-decoder.py，先按SHA核对并同步冻结e源。生产、公共客户端、live e、provider、期限和颜色断言均不动。PNG原签名/块类型与尺寸/CRC/完整扫描行/rowfilter/解压长度/EOF/像素上限校验保持；校验后仅IHDR/IDAT/IEND像素块给已存在PySide6 QImage纯内存解析，不传ICC/gAMA/sRGB/文本/EXIF等辅助数据，不调用颜色空间转换，不初始化QApplication，不创建窗口。严格校验一次有界inflate，QImage另一次有界C inflate；此处明确为两次，不再宣称一次解压。像素解析结果仅转换RGB888一次，复用行bytes做C bytes.find与按像素/行边界计数，排除padding/跨像素偶然匹配。原RGBA规则忽略alpha，0/1/127/255及全alpha范围实测保留原RGB，没有premultiply或透明色丢失。

逐attempt使用唯一PNG/metadata、before/after-status、frame-diagnostic和preparation-diagnostic文件。诊断只包含固定目标/阶段/拒绝枚举、四色计数、单点签名RGB及各阶段毫秒数，不记录任意message、图片或用户文本；首张不再被末张覆盖。原3秒/最多61次/50毫秒、前后本PID/focus/paint/layout、首帧唯一例外、最终保存后的截止/STOP检查保持；权限/网络/映射失败不重试，只有已校验图像不合格才重试只读capture，最终合格后仅一次input。同步PNG解析和系统文件写入不能强抢占，完成后已过预算仍不发布可输入capture，这不是每条系统调用都能在3秒内终止的承诺。

三套离线验证通过：test-preflight保留所有取消/期限/网络/focus/HTTP Content-Length与chunked/late JSON/late保存/首帧及真实变化/hold回归，增加诊断保存跨期与保存期间STOP保持0input及首末尝试不覆盖；test-offline保留刷新返回错误/格式无效/独立不可读时0text/key、GUI关闭及保留runner至RUN_STOP的全入口断言。test-decoder固定SHA读取旧e算法，RGB/RGBA五种过滤器、641宽行padding、所有alpha、辅助ICC剥除、无marker/错误签名/畸形CRC/扫描行/像素限额及3张实际已保存PNG的点位/签名等价均通过。性能仅记录，不用脆弱阈值判定成功。

| 已有PNG | 新严格校验毫秒 | 新native解析与定位毫秒 | 旧算法加等价比较毫秒 |
| --- | ---: | ---: | ---: |
| initial-native-click | 11.774 | 34.938 | 2216.315 |
| reader-before-refresh | 13.420 | 36.381 | 1811.437 |
| native-click-before-refresh（旧末次） | 15.386 | 37.213 | 2287.760 |

命令均显式TMPDIR/TMP/TEMP=$PWD/.tmp/kde-native-c/temp、PYTHONDONTWRITEBYTECODE=1，以/usr/bin/python3依次运行c/test-preflight.py、test-offline.py、test-decoder.py；全部exit0，五文件AST与git diff --check绿。冻结后交检查代理定向复核，主会话再决定是否在原租约内同步新脚本实跑；这轮仅离线，不算中文输入、刷新恢复或provider退出后可读性已验收。

检查代理最终定向复审PASS：三套测试/五文件AST/diff绿，冻结SHA一致，无追加代码修复。独立复跑3张已有PNG及RGB/RGBA各filter/alpha/padding/辅助元数据剥离等价通过，native定位约35.8–50.7毫秒；没有QApplication/GUI/Clipboard操作。逐attempt命名与3秒保存后拒绝迟到门槛复核通过。主会话已收到可同步下一私有环境的门槛。

| c冻结文件 | SHA256 |
| --- | --- |
| fixture_frame.py | e806f8ddfe4eba80df550b138af779426af0ecaf3e74a51c758786a2e6052853 |
| segmented-smoke.py | ed86a30533e27edc3c9804567bf0eb474fe313a9304f3c12dc7374fb8de3debc |
| test-preflight.py | dd8af035ce8664b7c6c719f279bb0c2c1c44762da8ea1e68a9c8941120b11931 |
| test-offline.py | 7886f1ca31f6d0d577cd38e3938cb58ab222e2f6882d20b990810e279410efc6 |
| test-decoder.py | ce0fc64faeaf61a032171d3c4819e61a056d6bc8f3a1a13005d30f27cd64a105 |

## 追加：f 原始 offer 与 Qt 视图差异的私有协议证据门槛

主会话的 f/evidence-01 实际截图、两次专用 mouse、刷新前独立三原格式95字节通过。夹具同内容发布后的自读增加 UTF-8 alias32字节，总127；独立 Qt 视图仍列三格式95字节，全部原hash一致，原门槛拒绝继续，0text/key。GUI close/reclose确认，关闭后独立三原格式也仍全部可读；失败runner按原租约保留夹具，随后主会话负责精确清理f。没有新trace可以倒推旧f来源，不宣称旧源仍在或新源已成功发布，也不把缺alias的Qt列表等同原字节丢失。

同版本[Qt6.11.2 QWaylandMimeData](https://raw.githubusercontent.com/qt/qtbase/v6.11.2/src/plugins/platforms/wayland/qwaylanddataoffer.cpp) L164–225确认：formats_sys将UTF-8 alias映射为plain并去重；retrieveData_sys先查请求key的cache，只有请求MIME不在实际m_types时才把plain映射到alias。实际raw offer同时包含plain与alias时，逐raw key首先请求会各自发receive；之后normalized视图复用cache。这只是源码允许的路径，不是本轮实际FD读取证据。[Qt Clipboard setter](https://raw.githubusercontent.com/qt/qtbase/v6.11.2/src/plugins/platforms/wayland/qwaylandclipboard.cpp)会添加同内容alias，自读及ownsClipboard可来自本地source指针，不作为compositor确认。普通[wl设备setter](https://raw.githubusercontent.com/qt/qtbase/v6.11.2/src/plugins/platforms/wayland/qwaylanddatadevice.cpp)使用input serial；[data-control setter](https://raw.githubusercontent.com/qt/qtbase/v6.11.2/src/plugins/platforms/wayland/qwaylanddatacontrolv1.cpp)不使用serial，不能一概要求wl路径。[Wayland1.24 logger](https://raw.githubusercontent.com/wayland-mirror/wayland/1.24.0/src/connection.c)使用接口/object/方法、可选队列名及FD编号；本轮忽略wire时钟，统一记录父监管器的monotonic观察时间。

本次仅在c工作目录新增client_trace.py、private-fixture.py、test-client-trace.py、test-trace-reader.py，修改inner.py、transport-reader.py、segmented-smoke.py及其两个既有离线测试。所有原始trace仅经过child stderr管道；fixture/reader/portal-kde三个sender kind分别直接Popen实际进程，filter PID就是该child.pid，没有shell launcher PID冒充Qt或Portal owner。Portal-kde仍直接执行原/usr/lib路径，原四D-Bus owner==held child.pid、环境、socket与STOP清理门槛保留。唯私有Portal后端增加WAYLAND_DEBUG=client，stdout丢弃、stderr立即过滤，普通已识别sender行不保留任意payload，只有有界分行头部缓冲；不写原始trace/键/窗口文字/剪贴板载荷。内层租约从inner开始1800秒，较ready后开始更严格，只缩短不延长。

过滤器的硬限额为每行4096字节、总输入64MiB、4096事件、256次对象创建（含销毁后复用）、单次JSON1MiB、累计发布8MiB。已知事件只记录固定枚举、uint32对象ID/serial、创建序号token、固定MIME白名单、父monotonic时间；不保存FD编号或FD内容。超过限额、未知相关协议/未知MIME/畸形必要字段、断线、STOP或原租约结束都禁止证明成功。元数据0600、pending→replace原子发布。wl_data_device、zwlr_data_control、ext_data_control三族均有有界解析；ID重用必须先destroy并分配新token。被动PRIMARY设备初始化/offer只记录类别，PRIMARY source创建、发布或receive一律拒绝；不会访问PRIMARY。私有fixture包装仅设置现成字段Password回显且不预置文字，以避免实际Ctrl+A把selection发布到PRIMARY；公共夹具及生产接口不变。

reader先取当前实际selected offer的原始MIME，再对每个raw key读取并摘要；Qt normalized格式列表另列，不冒充wire列表。成功必须该offer世代的每个raw MIME恰有一次真实协议receive，原三格式hash/长度保持且alias单独32字节摘要一致。normalized二次读取明确标为缓存视图，记录canonical plain source/resolution和raw receive计数。只有alias的offer且Qt plain解析经alias时如实记录resolved_via_alias，并不能通过要求raw plain的门槛；没有receive、重复/旧offer receive或错误raw摘要均停止。Qt内部会先把FD数据读入QByteArray，Python再检查1MiB；本方案用于已有严格固定测试源，不把Python摘要限额夸大为Qt底层缓冲上限。

刷新后还必须观察新fixture source的set_selection、当前source创建token及原raw MIME offer，并在本次新reader的父monotonic区间内，每个MIME恰有一次该新source.send，对应reader.receive与完整摘要。旧source、已取消/销毁source、区间外send不能证明。compositor可桥接wl与data-control，两端族分别记录source_protocol/reader_protocol，不要求相同；wl source要求非零serial，control source不造serial。两进程的FD编号不相等也不能比较，证据明确是fresh_reader_interval_correspondence，fd_identity_proven=false；不宣称通过日志证明两个FD对象身份或载荷逐字节沿同一FD配对。原提供者无需额外trace：刷新前只核验固定原字节及raw集合，不声称当前owner身份；刷新后以确切新source服务证明为门槛。

每次中文前用新的独立reader核验当前已证明producer仍实际服务；gui_text后必须Portal-kde当前新恢复source的set_selection/offer/send加新reader raw receive/全部摘要，不能拿fixture旧source或内容相同代替。Portal临时文本source可能随后恢复，只选本次tool前序号之后最终当前source，所有中间源及取消记录保留，最多四次set_selection；不能跨ID世代混入。任何trace/proof失败仍0后续text/key，GUI关闭及独立close读取结束后按原RUN_STOP/lease保留可能唯一夹具，不救援或覆盖。原截图3秒/61次/50ms、精确四色/目标签名、首帧物化唯一例外与单次输入保持，trace健康门槛纳入原共享预算。

五套离线验证通过：client-trace覆盖三族/跨族、new source/token、旧send/窗口外send/cancel/current/缺少与重复receive、格式/hash/PID/kind、cache/alias模型、unknown/PRIMARY、对象/事件/输入/输出限额与原子权限；trace-reader通过实际read1假管道线程（未起真实进程）、raw first读取/cache视图、alias-only拒完整raw门槛、STOP/lease/revision/focus/trace failure及缺receive，私有包装仅Password与租约退出mock。原preflight所有3秒/late JSON/late保存/首帧/网络/focus/hold回归保持，新增trace失败0input。完整segmented入口mock覆盖独立raw不符、refresh响应错误/自读无效及source未服务时0text/key、GUI关闭及RUN_STOP保留。decoder旧真实PNG/五filter/alpha/辅助元数据/畸形限额回归仍通过，native离线定位28.8–34.0毫秒；不启动QApplication、GUI或新采集。九文件AST、git diff --check绿；未重跑生产包或声明原生恢复验收。

所有命令显式TMPDIR/TMP/TEMP=$PWD/.tmp/kde-native-c/temp、PYTHONDONTWRITEBYTECODE=1，以/usr/bin/python3运行test-client-trace.py、test-trace-reader.py、test-preflight.py、test-offline.py、test-decoder.py。没有Go改动，不触发构建/cache。下一轮由主会话将白名单新模块及包装器复制到全新空私有目录，原bootstrap入口不变；segmented仍通过run-private.py启动`segmented-smoke.py --execute --output-dir <新私有目录>/evidence-01 --provider-pid <held fixed provider> --authorize-wait-seconds 120`，最终退出源验收另显式--exit-providers。reader现在内部必传--trace、--evidence，由TracedProcess监管；不直接启动无过滤reader。readiness新增portal_trace(pid/kind/path/raw_trace_saved=false)，结果分列raw_offer/raw_summary/qt_normalized_summary/raw_read_resolution，report新增source_service_proofs及self_refresh_returned；clipboard_refreshed只在正向外部服务门槛通过后为true。代码冻结交检查代理；下一轮真正协议路径与读完FD证据仍待原生运行，任何不可证路径停在安全失败而非弱化断言。

## 追加：g 启动期未知协议拒绝与本机 Wayland1.26日志前缀

主会话已按复审SHA启动私有g，readiness.stage=failed，error=Private clipboard trace proof is unavailable；Portal-kde实际PID2604055的过滤元数据reason=unknown_protocol、sequence=0。没有provider、GUIOpen、Clipboard或input，内层组件全部清理，outer16249正常0。原始trace按设计未保存，因此不能重建具体拒绝行。本次不读写g、不启动环境、不操作GUI/Clipboard，只有c过滤器与对应离线测试机械补丁。

本机只读`pkg-config --modversion wayland-client`和包数据库均为1.26.0；Portal-kde `ldd`确认链接/usr/lib/libwayland-client.so.0。此前查阅1.24.0 logger并只支持旧数值时间前缀，未核对实际本机1.26，属于确定的版本适配缺陷。[官方同版Wayland1.26 connection.c](https://raw.githubusercontent.com/wayland-mirror/wayland/1.26.0/src/connection.c#L1475)改用HH:MM:SS及6位微秒；queue、方向、sender/interface方法结构仍存在。旧SENDER/LINE匹配失败后，registry.global参数中的data_control名称触发unknown_protocol。纯离线按官方格式构造registry行，旧冻结代码确实产生unknown_protocol/sequence0，与g一致；由于没有保存g原始行，仍不能唯一断言实际首个消息就是该registry行。[同版client源码](https://raw.githubusercontent.com/wayland-mirror/wayland/1.26.0/src/wayland-client.c#L1170)说明stderr TTY或FORCE_COLOR可启ANSI，NO_COLOR可禁色；现监管stderr为pipe、私有环境不继承ForceColor，本补丁不解析ANSI或discarded相关消息为有效证据。

主会话授权的最小改动只在c/client_trace.py/test-client-trace.py：共用严格新时间前缀，小时00..23、分秒00..59、微秒恰6位；旧数值格式保留，不改变协议事件、输入次数、raw读取/source证明或限额。actual sender为wl_registry仍丢弃参数；未知相关接口、无法识别sender、畸形消息、PRIMARY操作仍明确拒绝，不把unknown静默放过。

新增单个首个rejection_diagnostic，正常为null；失败JSON和snapshot均保留它。内容仅固定category、timestamp_syntax、已知interface/method枚举或other，以及sender_recognized/known_interface/direction_marker/queue_marker/discarded_marker/ansi_marker/strict_message_syntax布尔；宽松头部探测只用于失败分类，绝不参与成功接收或source证明。没有时间值、队列名、未知接口/方法原名、对象参数、原行、MIME载荷或事件数组；后续错误不覆盖首诊断，失败文件仍0600、原子与原输出限额。限额/IO/租约等没有当前行的错误只用固定默认类别。诊断区分sender_unrecognized与unknown_related_interface，能避免下轮再次只知道unknown而不知道语法门槛。

四套受影响离线验证exit0：test-client-trace新增00:00:00及23:59:59边界、错范围/5或7位/非法时间拒绝、分片registry和data-control、同新版头部完整wl/wlr/ext create/set_selection/offer/receive/send正向证明、unknown接口与方法/ANSI/discarded/PRIMARY固定拒绝、首诊断不覆盖及任意字符串不落盘。test-trace-reader保留构造失败精确child/组TERM→KILL与cleanup失败原因回归；test-preflight原3秒/首帧/取消/late-save/0输入保持；test-offline四种刷新/来源失败hold回归保持。两个改动文件AST与git diff --check绿；命令仍每条显式TMPDIR/TMP/TEMP=$PWD/.tmp/kde-native-c/temp及PYTHONDONTWRITEBYTECODE=1，用/usr/bin/python3，未重跑未变化的PNG decoder或生产包。

冻结SHA：client_trace.py=81a5a68878525d50c1e27f2276dbe8ef3b0b86589b93f321f291175c66cae2ba；test-client-trace.py=21bdd176a16647383e58780d9337ce0d33e2f33a06c2e373b1c95b3359ca9003。其余脚本、g和生产继续冻结。需检查代理定向复审；任何新环境/实际原生验证仍由主会话管理，不因本机语法复现即宣称Portal trace或中文恢复已验收。

## 追加：i 独立 reader 正常退出与读取完成时点分离

主会话私有i已真人点击固定provider及Portal授权，首帧/专用mouse通过，独立before-refresh reader PID2659587实际报告ok=true。原始offer四格式127字节、每MIME receive一次；Qt normalized三原格式95字节的全部hash一致。父runner却报A unique selected raw clipboard offer is unconfirmed，未refresh/text/key；GUIclose/reclose确认，主会话随后负责独立close读取和全部精确清理，14持有PID均不存在，本轮历史报告保持不改。

只读i/evidence-01已保存元数据确认失败路径：reader trace seq7为selected offer token2，seq8..11各原MIME receive；seq12/13分别是device及offer destroy，EOF=true，最终objects都destroyed。reader已在finalguard确认选中offer有效后写成功JSON、退出Qt；父随后validate_reader_result按最终snapshot调用selected_offer，错误地把正常退出销毁当读取时失效。不能仅去掉destroy/EOF校验，因为读之前撤销和旧source仍应拒绝。

主会话授权仅c client_trace.py/transport-reader.py及父segmented三行必要调用、对应既有测试：成功reader新增read_checkpoint={trace_sequence,read_completed_monotonic}，在raw/hash/receive/focus/revision/lease/STOP最终guard之后、成功原子JSON及app.exit之前记录。父先取得child.snapshot再记录finished（旧相反顺序会把snapshot观察时刻排除在父区间外），用既有started/finished构建严格历史prefix，再给原raw/hash校验与source_service。后者producer继续最新实际snapshot，EOF、cancel、destroy仍拒，不把reader历史证明挪用于当前producer。

prefix最多4096事件、256对象创建，checkpoint序号必须非bool正整数且在最终序号内，事件必须连续并与最终sequence/list长度一致；所有monotonic非bool数值、有限且0..1e12，父start≤completed≤最终snapshot观察时刻≤finished，事件时刻非下降并在父区间内。全history重建device/offer身份、create token、MIME、selection和receive状态，最终graph须与最终snapshot完整一致；未知字段/事件、缺receive、重复create或ID复用、读之前withdraw/cleanup及时点越界均拒。checkpoint处offer必须仍活且被选中、原MIME每项恰一次receive，raw四格式/hash和normalized视图门槛保持。checkpoint后的相关事件仅允许已知本reader对象且严格在completed之后的退出destroy，任何迟到selection改变、额外receive或未知事件都拒绝。reader最终EOF可在上述完整证明后保留final_reader_eof标记，不代表producer可用EOF旧记录。

定向离线回放读取i已有完整trace/result（有界1MiB、不修改i）：原结果缺checkpoint继续明确拒绝；仅测试中构造位于最后receive与首次destroy之间的synthetic时间点，证实prefix重建为seq11、两对象未销毁、四次receive且原摘要相等，合法后续Qt device→offer destroy通过。该synthetic不能追补i原生验收PASS。变体覆盖提前destroy、晚selection、读前withdraw、事件乱序、最终graph不一致、token错误、ID复用、checkpoint早于receive/等于destroy、未来或bool序号、bool/NaN/巨数时刻、父区间及snapshot越界；另生成完整一致的缺plain receive历史，实际在receive-proof边界拒绝，不能用格式乱序先失败遮掉缺证据分支。

四套受影响测试exit0、六文件AST及git diff --check绿。test-trace-reader明确成功时checkpoint字段，保留Qt raw/cache/alias、取消及构造失败cleanup。test-offline四种完整失败流仍0text/key、GUI关闭并保留到RUN_STOP，使用真实严格递增monotonic断言start<completion<snapshot≤finished，不能以相等mock遮住父调用先后。test-preflight原3秒/61次/精确marker/首帧/late-save/一次input与hold全部保留。未重跑未变化decoder或生产包，也没有GUI/Clipboard/Portal操作；源码冻结交检查代理后下一轮原生由主会话管理。

| 本次c冻结文件 | SHA256 |
| --- | --- |
| client_trace.py | 40871b054e5fe4adceb323d9174a7e7d1068c6d290ca88e99a70d0a580ec68c9 |
| transport-reader.py | b897a67c847106ddccca4394bf5251831b4f0620b5dc5b20697546009912f2dd |
| segmented-smoke.py | bf2902a24d1b21059334ab1a4114c288b8582623ea6b6c6da80b3f75f086b2d3 |
| test-client-trace.py | 156e30666f953fea73bd7f1122ca6cb235f5162163badad781a598b59ae9583b |
| test-trace-reader.py | bc2f13c4fde08c654b76cc2f2f18a08572d30ab18eae8afec6ddad4a0d203921 |
| test-offline.py | 8e38b0acffec6a2e5dd966c710fdb4eacedd5393cc251f38d84d95ff1407c2fc |

每条检查显式TMPDIR/TMP/TEMP=$PWD/.tmp/kde-native-c/temp、PYTHONDONTWRITEBYTECODE=1，/usr/bin/python3运行test-client-trace.py、test-trace-reader.py、test-offline.py、test-preflight.py。新checkpoint只适用于新成功结果，历史缺失不能回填；过滤器未知协议与有限拒绝诊断、首帧例外和当前source正向服务门槛均未放宽。
