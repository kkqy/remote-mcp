# Windows GUI 截图与输入契约

## 1. 范围与触发条件

修改 GUI 后端、配置、工具或原生验证脚本时阅读本规范。用户于 2026-10-07 取消 Linux 全部 GUI；仅 Windows 提供图形操作。Linux 保留命令行远程调试工具，不创建图形管理器、不探测桌面、不注册 `gui_*`、不显示或接受 `gui-*` 参数。macOS 不在支持范围。

Windows GUI 控制服务运行账号的当前交互桌面，复用 `/mcp`、鉴权及 Origin 校验；没有独立控制网页。缺少桌面或权限只影响 GUI，不能阻断文件、进程、终端和转发。

## 2. 签名与平台边界

- 管理器提供 `DefaultConfig()`、`New(Config)`、`Register(*mcp.Server)`、`Close() error`。
- `Backend.Probe(context.Context) (Status, error)` 只读查询；`Open(context.Context) (Desktop, error)` 建立独立于创建请求的会话。
- `Desktop` 提供 `Capabilities/Displays/Capture/Mouse/Key/Text/Close`，可选 `Done/Err` 健康接口。`Close` 可与取消中的操作并发。
- Windows 工具为 `gui_status/open/close/screenshot/mouse/key/text`，类型与 JSON 标签见 `internal/gui/types.go`。HTTP 层不能复制状态机。
- 配置和服务接入共用平台支持条件；非 Windows 构建桩只用于共享包可编译，不能注册到产品。Linux 模拟 HTTP 测试可显式注入假后端；产品默认必须无 GUI 管理器，关闭须支持该状态。

## 3. 请求、响应和配置契约

- `gui_open(request_id, wait_ms)` 等待 0～10000 毫秒；返回 ID、状态、实际能力与显示器；请求 ID 去重，活跃记录不淘汰。
- `gui_screenshot(id, display_id?, region?)` 输出不缩放的 PNG。原始字节交给 SDK `mcp.ImageContent`，结构化内容仅含元数据，不重复图片。
- 元数据含 `capture_id/display_id/width/height/region/logical_bounds/layout_generation/captured_at/captured_at_source/frame_sequence/freshness`；序号是成功截图计数，Windows 原生采集时间来源为 `acquired_at`。
- `gui_mouse(id, capture_id, action, x, y, ...)` 坐标相对输出图片，叠加裁剪偏移后按实际像素与逻辑尺寸换算，再加显示器全局偏移。布局变化或记录过期使旧截图失效，不猜 1:1 比例。
- `gui_key(id, keys)` 一次提交完整组合键，最多 8 个，释放本次按键。拖拽最多 10000 毫秒，不提供跨调用长按。
- `gui_text(id, text, mode?, paste_keys?, allow_clipboard_replace?)` 支持 UTF-8；Windows `auto/direct` 使用原生 Unicode，不访问 Clipboard。显式 `clipboard` 返回 `unsupported`，不静默换模式；兼容参数保留不代表支持剪贴板粘贴。`submitted` 只代表提交事件，应用结果另验。
- 最多一个待授权或就绪会话。操作串行，真实忙时返回 `busy`，不建无界队列。取消等待不提前释放占用；底层完成、释放按键和占用后发布结果。
- 默认：空闲 30 分钟，终态保留 10 分钟/256 条；授权 2 分钟，操作 30 秒；原图 16777216 像素/PNG 16 MiB；坐标记录 64 条/5 分钟；文本 64 KiB。分配前校验限额，像素乘法用除法避免溢出。
- Windows CLI 为 `--gui-idle/--gui-authorize-timeout/--gui-operation-timeout/--gui-max-pixels/--gui-max-png-bytes`；全部须为正值。Linux 不校验未使用的 GUI 配置。Linux/Windows 四组合保持 `CGO_ENABLED=0`。
- 普通日志不输出文字、图片、剪贴板或凭据。业务错误向 Agent 返回具体英文原因、稳定错误码和必要的 `input_may_have_applied`；错误日志遵循已有安全过滤契约。

## 4. 校验与错误矩阵

| 条件 | 结果 |
| --- | --- |
| Linux 调用 GUI 工具 | 协议未知工具；不进入 GUI 处理器 |
| Linux 使用 GUI CLI 参数 | 英文参数错误 |
| Windows 无桌面或权限不足 | `no_gui/permission_denied`，旧工具仍可用 |
| 坐标 NaN/Inf、区域越界、未知或重复键 | `invalid_argument`，不输入 |
| 无有效截图或布局改变 | `stale_capture`，不猜映射 |
| 像素、PNG、文本超限 | `limit_exceeded`，不绕过限额 |
| 指定能力或文本模式不可用 | `unsupported`，说明实际原因 |
| 操作仍运行 | `busy`，不排无界队列 |
| 部分输入失败或取消 | 明确英文原因与 `input_may_have_applied`，不可盲目重试 |
| 会话关闭 | `session_closed`，结束等待并释放资源 |

## 5. 正常、边界与失败案例

正常：打开 → 截图 → 以截图坐标聚焦 → 输入中文 → 再截图核对 → 关闭。边界：裁剪、高 DPI、其他屏幕负全局坐标须使用实际映射。失败：输入取消时可能已生效，仍持有占用直到释放按键；不自动重试。系统 UIPI 可能拒绝向更高权限应用输入，不控制安全桌面或服务会话。

## 6. 必需测试与断言

- 管理器：连续成功、并发忙、取消占用、打开和关闭、坐标淘汰、裁剪缩放、多屏布局失效、NaN/Inf 与溢出。
- Linux 默认独立 HTTP 工具发现无七 GUI，移除工具调用被拒绝；CLI 帮助及参数拒绝覆盖；无 GUI 管理器时完整关闭和旧工具正常。
- 显式假后端 HTTP：七 GUI、PNG 只有一份正确 Base64、元数据与像素一致、英文错误、匿名/Token/Origin 保持。假后端不代表 Linux 产品支持 GUI。
- `scripts/gui-smoke.py --execute` 可从任意 Python 宿主验证远端 Windows HTTP/PNG；按服务后端判断，不能按宿主系统误拒远端 Windows。默认只读截图；`--input-plan` 仅控制调用方已准备的专用目标，返回事件提交不能代替应用实际结果。
- 删除 Linux X11/Wayland/Portal/KScreen 后端、专属测试、Qt 夹具及专用依赖；不能把旧研究继续注入当前实现契约。
- 跑全包 test/vet、本机 race、Linux/Windows 四组合构建和旧功能/P0 Token/匿名实际冒烟。Windows 原生验收、构建及未验证边界分别记账。历史 Linux GUI 研究保留供追溯，不计为当前要求。

## 7. 错误方式与正确方式

错误：调用取消就释放 busy，使新旧输入交错；或仅在处理器内拒绝 Linux GUI，却仍公开工具和 CLI。

正确：底层结束并释放按键 → 解除占用 → 发布结果；调用方取消只结束等待。平台边界在配置和服务注册处生效。

错误：有界输出类型嵌入 `bytes.Buffer`，只覆盖 `Write`，认为 `io.Copy` 一定经过上限校验。

正确：缓冲区作为私有字段，避免提升 `ReadFrom` 方法绕过 `Write`；用真实复制路径断言超限失败。

## Windows 专用原生窗口验收

### 1. 范围与触发
远端 Windows 没有 Python、已有用户授权的匿名 MCP 入口时，本机薄部署脚本上传标准库 Go 原生助手；只操作本轮创建的窗口。默认旧功能/P0 四轮不得因此访问 GUI。

### 2. 命令签名
`python3 scripts/windows-gui-smoke.py --execute --url <已授权MCP入口> --bin-dir dist/windows-amd64 --report-file <本机报告>`。底层助手为 `native-helper.exe <唯一部署目录> --gui <server_sha256> <helper_sha256>`；不带 `--execute` 只显示帮助，不连接远端。不能混用包测试选择参数。

### 3. 输入与输出契约
复用 Windows 原生部署的严格 UTF-8、有界输出、唯一目录及精确资源清理。本机缓存、状态和打包目录固定于项目 `.tmp/`；远端只在服务工作目录的 `.tmp/` 下建立本轮唯一目录，不回退系统临时目录。prepare 输出本机 server/helper SHA256；远端展开后分别核对 GetFileHash，助手验证自身位于本轮目录及两份哈希，每轮启动固定路径的服务前再次核验。GUI 模式运行 Token/匿名两轮，只监听目标机回环，不替换主服务。Win32 消息循环固定在独立 OS 线程，启用每显示器 DPI 感知；每次输入前核对自己 HWND 的前台身份，键盘及文本另核对自己的 EDIT 焦点。裁剪区域必须完全位于窗口客户区和选中显示器内，通过实际 PNG 像素验证四色标记；使用该截图的 capture_id 进行输入。原生 Unicode 路线不读取或写入 Clipboard。

最终报告包含 `gui_exercised=true`、`clipboard_accessed=false`、`acceptance_complete=false`，两轮实际事件、区域/标记核验、UTF-8 控件文字核验、前台守卫、重复关闭及窗口清理证据。PNG 只保留摘要；GUI 失败可输出最多 16 KiB 的白名单数值诊断（阶段、区域、绘制次数/失败位、四色中心 RGB/不匹配数/颜色计数、本窗口与桌面 DC 四点 RGB/有效性、可见/最小化/遮蔽状态、截图尝试数、哈希、清理阶段），不输出图像、文字、Token 或完整目录。每次专用窗口截图准备允许共用最多 3 秒 context、间隔 50 毫秒且最多 61 次只读截图，每次前后核对同一前台 HWND 和客户区几何，仍必须满足四个 7×7 精确颜色断言；不重试任何输入。绘制/截图断言失败的原始英文原因与清理失败分列，已有进程 Wait 结束后仅对本轮目录有界等待最多 5 秒释放，不扫描或删除其他资源。主服务仍可发现工具且临时部署清理完成后才发布成功报告。多屏/混合缩放、布局变化、撤权、实际 MCP 客户端展示及 Windows/arm64 原生运行继续 pending。

### 4. 校验与失败矩阵
| 条件 | 结果 |
| --- | --- |
| 未指定 --execute | 显示帮助，不部署、不打开窗口 |
| 窗口启动超时、关闭或焦点不属于本轮窗口 | 有界失败并清理，不向未知窗口输入 |
| 上传、实际启动的二进制哈希不符 | 失败，不启动测试服务 |
| PNG 区域、标记或元数据不符 | 失败，不凭截图调用成功宣称坐标正确 |
| EDIT 文字、真实鼠标/组合键事件不符 | 失败，submitted 不算验收证据 |
| 窗口、测试服务或部署目录清理失败 | 不发布成功报告 |

### 5. 正常、边界与失败案例
正常：七个 GUI 工具发现与 Windows 能力探测 → 打开专用窗口 → 裁剪及四色核验 → 实际鼠标/滚轮/全选清空 → 中文及 emoji 入控件 → 再截图 → 重复关闭 → 销毁窗口并注销类 → 清理部署 → 复查主入口。边界：单屏 100% 环境通过仍不能算混合 DPI 或 Windows/arm64 原生成功。失败：窗口失去前台身份时中止输入，不去控制当前其他应用。

### 6. 必需测试与断言
PNG 完整解码、区域/标记错误、图像限额、有界协议响应、摘要真实性、--execute 与旧模式分离须有离线回归；新增 Win32 ABI/启动清理代码须做对应 Windows 构建。原生运行必须断言真实点击、双击、拖拽、水平/垂直滚轮、Ctrl+A/Backspace 清空和 WM_GETTEXT 的完整 Unicode 文字，且成功摘要发布前确认窗口已销毁、类已注销。构建结果与实际桌面证据分别记录。

### 7. 错误方式与正确方式
错误：在工作线程调用 GetFocus 代替窗口 UI 线程的焦点，或 startup 超时后丢弃尚未退出的窗口线程，再提前报告 window_cleaned。

正确：通过有界 UI 线程消息核验 EDIT 焦点；启动、异常和成功路径都持有窗口生命周期，销毁窗口并注销类后再确认清理，无法确认则失败。
