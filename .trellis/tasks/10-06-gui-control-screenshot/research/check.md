# GUI 功能质量复审

日期：2026-10-06。已读取任务 PRD、设计、执行计划、check.jsonl、后端质量/资源/错误/日志/MCP 规范及新增 GUI 规范；packages 入口与 backend Quality Check 确认本项目为单仓库。原生 KDE 新一轮重跑结果由主会话追加，不将模拟或构建记作桌面验收。

## 已修复

- `scripts/gui_fixture.py`、`scripts/gui-smoke.py`、`scripts/gui_clipboard.py`：原验证窗口未核验剪贴板恢复。新增只读 IPC，在文本输入前后、GUI 会话关闭后重新读取并比较每个 MIME 的长度和 SHA-256；没有改变原剪贴板基线，不保存内容。摘要最多 64 种格式、累计 1 MiB，IPC 默认等待 10 秒，ready 后首次聚焦按已校验授权参数等待（最多 605 秒）。
- `scripts/gui_fixture.py`、`scripts/gui-smoke.py`：Wayland 后台窗口可能无法读取剪贴板，空读取不能证明实际内容丢失。采样及每次真实输入前确认专用窗口获得焦点；可请求激活自身窗口，不自动操作系统授权 UI。失焦时等待有界，仍不能读取即失败。
- `scripts/gui-smoke.py`：finally 清理错误曾覆盖业务最初失败。报告分别记录 `failure` 与 `cleanup_failures`，失败始终使 `run_completed=false`，保留最初异常。
- `scripts/test_gui_smoke.py`：新增摘要限额/隐私、关闭后新采样与哈希变化、失焦等待、清理不覆盖原始失败回归；README 两份同步验证行为和 Qt 读取边界。
- `scripts/gui-smoke.py`：授权测试等待支持显式 `--authorize-wait-seconds`（默认 125，整数 1～605），便于配合服务的授权期限，不能自动重复弹窗。已知 GUI 错误的固定 message 仅在 512 字符内且无控制字符时保留；未知 code、其他工具和畸形结构不转发原响应。
- `scripts/gui_clipboard.py`、`scripts/gui_fixture.py`、`scripts/gui-smoke.py`：按主会话明确授权，增加显式 `--refresh-clipboard-offer` 验证准备。两次稳定的全部 MIME 读取必须与原基线一致，才在 Qt 进程内存重新发布完全相同字节；发布后再核对哈希，保持 `allow_clipboard_replace=false`。不可靠读取、版本或内容变化不发布；仅记录摘要。格式/字节上限仍为 64/1 MiB，Qt 提供者随接管或窗口退出释放。README 明确该准备动作不属于服务默认行为，也不证明初始格式已可查询。
- KDE 第五轮发现 Qt 自动新增 `text/plain;charset=utf-8`，原四格式各 14 字节及哈希不变。验证桥接现仅在显式准备阶段接受这一派生别名，须原基线缺少该别名且其长度/哈希等于原 `text/plain`；其他新增、原格式移除或变更均拒绝。发布后的完整格式集作为服务恢复基线，同时在输入前后/关闭后独立核验原基线格式并保存摘要检查证据。准备读取、变化、发布和发布后失败使用固定阶段代号；未宣称 MIME 集合完全相同。
- 第六轮真实恢复失败后，显式刷新在验证窗口独立保留全部原 raw 快照（64 MIME/1 MiB），不随 Qt ownership 丢失清除。失败清理请求有界救援；只在已确认关闭、本进程提供者所有权仍可证明、版本稳定且当前为本次测试文字或空时恢复。外部 owner 身份无法确认时不自动覆盖，保留窗口、内存备份及用户恢复按钮，报告 recovery/fixture PID；不再终止唯一备份进程。救援不会把原失败报告改为成功。

公共与 Linux 所有权文件未由审阅者并行改写。以下反馈经核心实施者修复后已按实际路径复核：

- 管理器状态在未忙时刷新实际权限与布局；共享关闭过程消除取消等待者累积，并重放关闭错误；原始 context 错误与工具边界统一为稳定业务错误。
- Portal 取消确认期间注册恢复清理，SetSelection 失败回滚现有 provider；转移 worker 饱和使用有界拒绝；缺失/非法 mime_types 或 session_is_owner 不再当成已知空剪贴板，旧 provider 同时失效。
- Wayland 逻辑配置使用实际可读 GNOME/KDE 服务的双读配置摘要与 owner 基线，授权后和操作前主动核验；像素不变的逻辑布局变化、owner 替换与断线使绝对映射失效。授权期间失效不会被 Start 重置。官方与本机证据见 `wayland-layout-check.md`。
- X11 改用可取消 DialContext、底层期限及首认证包适配；关闭先打断协议等待，再以独立有界连接释放输入。XGB NewConnNet 自身与 Setup 解析的短报文 panic 均有窄边界 guard；TARGETS/键图畸形维度与失败 provider 更新已校验和回滚。
- Wayland Close 不再忽略按键/按钮释放及 Session.Close 失败，明确报告清理失败。
- `manager.go` 采集后再次刷新时强制所选 ID 仍在当前显示器集合；已移除时返回 capture_failed，不生成新代次旧显示器坐标。回归覆盖其他显示器仍存在但所选显示器在 Capture 中移除。

## 原生平台审阅

- Windows：INPUT 联合体及 GDI 结构为 64 位布局；枚举回调单例有锁，PMv2 线程恢复，DIB/选入对象/DC 失败路径释放；负虚拟坐标与 Unicode UTF-16 代理对路径已核对。SendInput 数量失败报告部分输入。
- macOS：已核对 purego v0.9.1 的结构调用支持与 Objective-C Block 类型缓存，NewBlock 不为每次截图新增不可释放的 callback；回调 retain/释放、滤镜/配置对象与 autorelease pool 生命周期、非可变参数滚轮、修饰键与 Unicode 释放路径已核对。
- 未发现需要本审阅者修改 Windows/macOS 源码的可确认局部缺陷。macOS 系统权限 UI 与 ScreenCaptureKit 没有本实现的主动取消机制；最多一个晚到回调槽保持有界，但真实取消/永不回调行为未验证。

## 执行证据

- `go vet ./...`：通过。
- `go test ./...`：在默认沙箱因禁止 loopback socket 失败；经自动审核允许回环测试后全包通过。这是执行环境限制，未用跳过网络测试规避。
- `CGO_ENABLED=0 GOOS={windows,darwin} GOARCH={amd64,arm64} go test -c ./internal/gui`：四组合通过，含平台测试与当前共享测试；没有在对应 OS 运行。
- `python3 -m unittest discover -s scripts -p 'test_*.py'`：31 项通过，包含稳定错误、等待上限、同内容发布前不可靠/变化不发布、真实四至五格式派生别名及非法变化拒绝、原基线独立保留、准备阶段代号、不能以同文字推定外部 owner、第三方内容不覆盖与失败保留窗口回归。三份 Python 脚本语法编译通过。
- `REMOTE_MCP_GUI_PORTAL_TEST=1 GOCACHE=/tmp/remote-mcp-check-go-cache go test -race ./internal/gui -count=1`：通过（5.014 秒），运行隔离临时 D-Bus/X11 socket，不访问真实桌面；包含最后的畸形所有权、布局主动读取与关闭失败回归。
- 最后一项显示器移除修正后，`GOCACHE=/tmp/remote-mcp-check-go-cache go test -race ./internal/gui -count=1` 再次通过（1.507 秒），`go vet ./...` 与 `git diff --check` 再次通过；此次未启用私有总线测试，相关最终 Linux 隔离回归见上一条及核心实施者记录。
- `git diff --check`：通过。
- 主会话已记录全包 race、六组合构建及旧二进制 Token/匿名冒烟通过；这些不是审阅者独立执行的结果，后续核心变更须运行受影响检查。

## 尚未关闭的复审与验收边界

- 第六轮真实 Portal 恢复失败保留失败证据。控制格式默认拒绝、pending provider、目标 MIME 确认与有界恢复备份已修；最终末审新增关闭清除 raw 数据及未确认 Set 的晚到 owner 通知窗口，核心实施者已补修并冻结，审阅者已逐路径复核并独立运行隔离回归。默认中文恢复的原生成功仍未取得。
- Qt 的 QMimeData.data 会先物化单个格式，因此 1 MiB 约束摘要处理和保留，不能声称 Qt 底层读取也严格有界；脚本通过独立 fixture 进程及等待期限控制验证失败的清理。
- 原生 Windows/macOS/X11/GNOME 完整输入闭环、多屏混合缩放/旋转/热插拔、客户端图片展示、取消撤权及剪贴板竞争仍需对应环境。没有把交叉编译或模拟 D-Bus 当成原生验收。
- KDE 已有真实 2880×1800 PNG、1800×1125 逻辑大小及 160% 缩放元数据证据。第 4 轮已授权且专用窗口聚焦，原剪贴板 7 MIME/763 字节；默认 gui_text 因初始 Portal 格式未知而拒绝，关闭后原摘要仍一致，完整输入尚未通过。KDE 后端仅订阅后续 offer，Clipboard v1 不提供全部格式查询，此为接口盲区，安全拒绝不等于默认输入成功。显式同内容 offer 准备后的完整输入与关闭恢复仍等主会话验收。
- 第五轮在输入前因上述 Qt 派生别名导致验证桥接失败，未调用 text。原四格式未丢失，关闭后五格式共 70 字节；没有将此结果误报为源内容丢失。修正后的原生重跑结果由主会话追加。
- 第六轮刷新准备成功，默认 text 实际进入控件；返回 clipboard_restore_failed，关闭后 text/plain 为 9 字节测试文字，另有 1 字节 KDE onlyReplaceEmpty 标记，原 14 字节内容及其他 MIME 未恢复。这是实际恢复失败和原内容被测试覆盖，不能归因于失焦误判或仅缺初始格式。原验证窗口已退出，主会话已以原哈希及当前仍为测试文字的哈希守卫从 Klipper 历史恢复 14 字节原文本；原 MIME 集合未保证恢复，详情见 validation.md。本审阅仅确认 Klipper 方法签名，没有写入用户剪贴板。
- 系统模态授权窗口曾覆盖 fixture，不能将失焦读空误报为后端丢失内容。脚本不代点系统授权。
- Portal 恢复后提供者仅由当前会话维持；关闭后原内容持续可读不能单凭恢复返回值保证。缺少目标桌面接管时生命周期仍是实际验收边界。提供者发布前的版本核验也不等于桌面协议具备原子所有权比较。

本报告保留明确未验证状态；任务不得据此归档为五类桌面全部验收完成。


## 最终 2.2 全范围复审记录

重新执行 packages 入口：单仓库，backend/frontend 两层。backend 索引明确没有业务前端；frontend 为未采用模板，没有本任务前端改动。已按 backend Quality Check 重新读取质量规范，并核对最新 PRD/design/implement、check.jsonl、GUI、MCP、目录、错误、日志和资源规范；Windows/macOS ABI、线程及回调边界沿用先前逐路径审阅，新核心补修额外复核实际路径，不以模板索引代替规范。

跨层核对覆盖 config 默认值及 CLI/Validate、server 创建失败回滚/注册/退出顺序、工具稳定错误与 SDK 单份 ImageContent、manager 单活跃会话/去重/取消 busy/关闭共享等待/布局代次/裁剪换算/元数据限额、Linux Portal 请求与 FD/辅助进程/X11可取消认证/布局监视、全部平台输入释放以及验证脚本/README。config/server 未复制 GUI 状态机；GUI 按需探测失败不阻止旧工具。日志仅固定元数据，脚本未知错误不转发请求内容。

新增脚本局部修复：

- 同内容准备和自动救援在任何发布前拒绝 KDE onlyReplaceEmpty 控制 MIME，不自动删除标记。人工按钮含控制标记时需额外明确确认，仅恢复有效 payload 并说明原集合未完全恢复，不能报全格式恢复成功。
- fixture 保持 QMimeData wrapper 强引用，失去所有权且独立备份存在才放下；独立 raw 备份继续保留。Qt offscreen 实验确认发布后原生对象已由 C++ 所有，局部 wrapper GC 不是已证实的真实丢失原因；没有用强引用修复宣称解决 KDE 接管。
- `--fixture-inputs-only` 只允许配合 fixture，禁止刷新及允许替换，不调用 gui_text/采样；真实全选和 Backspace 清空固定字段，键鼠结果与中文/恢复待验收分别报告。仅此入口使用 Password 回显阻止 Qt 选区复制；完整中文 fixture 的 SelectAll 快捷键直接选中并接受事件，避免普通 Qt 自动发布 PRIMARY，中文仍可见。官方证据与离线验证边界见 clipboard-recovery.md。
- 首次 focus 移到 ready 后、刷新与首图前，允许 1～605 秒已校验等待；后续操作确认仍默认 10 秒。focus 超时使用聚焦错误，不能误报剪贴板读取失败。31 项离线测试覆盖首次等待参数、无 gui_text/采样、实际 Qt 字段全选清空、控制 MIME 发布拒绝、wrapper 生命周期和此前哈希/救援/清理回归。offscreen 测试不连接宿主剪贴板或 Selection。

最后独立受影响 Go 检查：`REMOTE_MCP_GUI_PORTAL_TEST=1 GOCACHE=/tmp/remote-mcp-check-go-cache go test -race ./internal/gui ./internal/server ./internal/config -count=1` 经自动审核允许隔离 socket 后通过（GUI 5.372 秒、server 2.506 秒、config 1.024 秒）。此次在最后两项核心补修之前；补修后的检查单独追加，不复用旧通过状态。

真实 KDE 更新：第七轮已 ready 且 PNG 成功，但首次 focus 等待过短，提交操作列表为空；没有文本、按键或鼠标输入，不计输入拒权或应用行为失败。第八轮 KDE Plasma 6.7.5、原生 Qt Wayland，`--fixture-inputs-only` 返回 0；实际标准 PNG、160% 映射、五次鼠标/两次按键提交、窗口点击/双击/拖拽/滚动事件及 Password 字段实际清空、关闭和重复关闭通过。证据为 `/tmp/remote-mcp-kde-evidence-08/report.json`，原内容没有入仓库。报告 `run_completed=true`、`acceptance_complete=false`：这只关闭 KDE 键鼠子集，未调用 gui_text，正常中文及默认剪贴板恢复仍 pending。

五平台验收边界：Windows/macOS/X11/GNOME 均无本任务原生完整成功证据；KDE 只有上述键鼠子集成功，以及第六轮中文已进控件但恢复失败的负面证据。实际 MCP 客户端图像展示、混合缩放多屏、布局变化原生拒绝旧截图、撤权/媒体断线和剪贴板竞争仍未验收。控制 MIME 安全拒绝不能代替普通中文恢复成功。


## 最后核心补修闭合与检查状态

- `internal/gui/clipboard_portal_linux.go`：SetSelection 成功但尚未确认时，超时后的非 owner 通知不再宣称外部最终接管或清除原备份，结果为 `clipboard_restore_failed/unknown`。单个有界 `unconfirmed` provider 保留目标字节；晚到的合法自身 owner 且格式严格匹配时重新提供该数据，之后关闭可安全重试原快照；恢复 pending 仍拒绝新默认文本替换。已核对取消后的返回、defer 顺序及原快照/待确认目标分别有界。
- `internal/gui/wayland_linux.go`：完成独立恢复尝试、连接关闭和所有 watch/FD worker 退出后，清除 data/recovery/unconfirmed/mimes 和 provider 状态。Manager 可保留终态诊断，不再把原字节随终态保留 10 分钟。
- `internal/gui/portal_protocol_linux_test.go`：真实隔离总线覆盖中间 owner false→设置超时→晚到 own true→关闭前恢复、目标 MIME 不匹配的固定最终原因、文本等待期间并发关闭主动 abort 且 watch 仍活跃确认原恢复、Session.Close 尚未返回时拒绝新 text、真实关闭失败后 raw 仍清除。不是仅断言已结束操作后 prepareClose。
- 末审确认这两项确定缺陷与缺失回归已闭合，未保留其他可确认的未修代码缺陷。Windows/macOS 的原生机制风险及五桌面实际验收缺口仍按前述边界保留，不混同于代码检查通过。
- **审阅者最终独立验证**：`REMOTE_MCP_GUI_PORTAL_TEST=1 GOCACHE=/tmp/remote-mcp-check-go-cache go test -race ./internal/gui -count=1` 通过（5.370 秒），仅隔离总线/X11；`gofmt -l internal/gui internal/config internal/server` 无输出，`git diff --check` 通过。Python 31 项离线回归及 py_compile 通过。未配置额外 Python 类型检查工具；Go 类型编译由测试/跨平台构建验证。
- 主会话最终确认该冻结版本全包 test/race/vet、六组合两个入口无 CGO 构建全部 exit 0；Python 31 项通过（1.323 秒），diff check 和上下文校验 7/7 通过。全量结果由主会话记录于 validation.md，与审阅者上述独立受影响验证分开记账。

脚本、两份 README 与本报告已冻结，可形成可审阅提交；任务仍应保持 in_progress。真实 KDE 键鼠子集通过不等于 AC-02/04 或完整首版验收完成，尤其不能把 onlyReplaceEmpty 默认拒绝记为正常中文保存恢复通过。


第八轮原生使用的二进制早于最后剪贴板未知状态/晚到通知及 Close 内存清理补修，只证明未改的截图、坐标与键鼠路径。最后新剪贴板代码只有私有总线和自动检查证据，没有原生正常保存恢复成功证据；不得把第八轮结果套用于新恢复路径。
