# Windows 原生 GUI 验收入口实施

## 写入前边界

差距为 Windows 真实 GUI 基本闭环没有验收证据，远端缺少 Python；本轮仅创建标准库 Go Win32 专用测试窗口和显式验收模式，以及复用既有部署组件的薄 Python 调度入口。所有输入限定本次窗口，前台/编辑焦点不匹配即拒绝；截图仅请求专用窗口客户区并校验图像标记与元数据。只测试 direct 中文输入，完全不调用剪贴板 API。

旧 native-smoke 的四轮 legacy/P0 入口、报告和运行默认保持；不修改生产 GUI 后端、依赖、原用户服务或 `.opencode/package.json`。Token/匿名两次分别启动独立临时服务，收回窗口、会话、临时服务和目录后才报告成功；原服务入口可用须在部署尾部复查。完整多屏/缩放/撤权/剪贴板恢复继续 pending。此实施者不连接 VM 或操作宿主桌面，真实入口由主会话运行。

## 官方 ABI 与消息依据

使用微软官方 Unicode Win32 API；amd64/arm64 均使用 64 位 uintptr 句柄及自然对齐，WNDCLASSEXW 80 字节、MSG 48 字节。UI goroutine 锁定 OS 线程并设置 PMv2，窗口过程通过标准库 syscall.NewCallback 调用。

- [WNDCLASSEXW](https://learn.microsoft.com/en-us/windows/win32/api/winuser/ns-winuser-wndclassexw)、[RegisterClassExW](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-registerclassexw)：注册 Unicode 类，CS_DBLCLKS 接收双击，退出注销类。
- [MSG](https://learn.microsoft.com/en-us/windows/win32/api/winuser/ns-winuser-msg)：原生消息布局及 message loop。
- [GetForegroundWindow](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getforegroundwindow)：每次 MCP 输入前验证本次窗口，不绕过前台权限限制。
- [EM_SETSEL](https://learn.microsoft.com/en-us/windows/win32/controls/em-setsel)：专用 EDIT 的 Ctrl+A 全选处理，Backspace 仍由真实控件删除内容。
- [WM_LBUTTONDBLCLK](https://learn.microsoft.com/en-us/windows/win32/inputdev/wm-lbuttondblclk)：双击消息序列及带符号客户区坐标；垂直滚动按 MCP 向下为正与 Win32 wheel 向上为正的方向换算核对。
- [SetWindowLongPtrW](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setwindowlongptrw)：只对子进程自身 EDIT 做 subclass，其他消息交回原过程，不访问外部窗口内容。
- [SendMessageTimeoutW](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-sendmessagetimeoutw)：读取本次 EDIT 与查询 UI 线程焦点使用 1 秒上限。
- [SetThreadDpiAwarenessContext](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setthreaddpiawarenesscontext)：UI 和几何查询线程均临时设置 PMv2，结束恢复原值。

## 确定结果

入口：`python3 scripts/windows-gui-smoke.py --execute --url <已授权匿名MCP入口> --bin-dir dist/windows-amd64 --report-file <本机报告>`。无 `--execute` 不连接远端、不部署或访问 GUI。薄入口通过 `windows-native-smoke.main(gui_validator=...)` 复用既有独立 HTTP 客户端、上传、唯一目录、PowerShell、受管进程清理及尾部原服务可用检查。旧入口默认继续执行 legacy/P0 × Token/匿名四轮，默认响应预算仍为 2 MiB；GUI 明确选择 `[临时目录, --gui, server_sha256, helper_sha256]`，响应预算为 24 MiB，PNG 本身保持 16 MiB/16777216 像素限额。GUI 没有包测试模式，不安装依赖。

`scripts/native-smoke/gui_windows.go` 创建本次 Win32 窗口及 EDIT，逐项发现七个 GUI 工具并查询 Windows 桌面可用。选择实际 primary 显示器，以 GetClientRect/ClientToScreen 得到专用客户区；区域图完整解码并验证四个颜色标记、尺寸、偏移、新鲜度/时间及代次。每次鼠标操作先重新截图，点相对该图；计数核对本次增量、真实消息坐标（允许 2 像素归一化舍入）及拖拽端点，滚动检查精确 tick 与光标位置。键与 direct 文本输入前额外核对 EDIT 焦点；Ctrl+A 兼容处理使用 EM_SETSEL，Backspace 由控件完成清空，再通过 WM_GETTEXT 比较真实中文和 emoji。输入后重新截图验证 PNG 改变。

会话关闭和重复关闭均须 closed；窗口销毁、类注销、UI 专用消息队列清理、线程解锁完成后才标 `window_cleaned`。启动失败或 10 秒超时持有夹具对象，先取消并唤醒本次窗口/线程，再有界等待退出。任何清理失败向上失败，不发布成功；服务终止保留 `terminate_process`，不能宣称优雅退出。两种鉴权各自启动临时独立服务，不停止已有服务；上传后的服务/助手 SHA-256 和部署清理状态沿用旧报告。

报告仅保存截图 SHA-256、事件计数、尺寸和检查布尔值，不输出 PNG、测试文本、凭据或用户内容。`clipboard_accessed=false` 只说明本次 direct 验收没有调用剪贴板 API，不声称宿主外部应用不改变剪贴板，也不代表保存恢复验收。`acceptance_complete=false` 始终保留，多屏混合缩放、布局变更、撤权、剪贴板保存恢复和 MCP 客户端图片展示仍 pending。

2026-10-06 已通过的本地检查：

- `go test ./scripts/native-smoke` 和 `go test -race ./scripts/native-smoke`：6 项，涵盖 PNG 像素/标记/错误元数据、显式 GUI HTTP 预算及旧有界缓冲/错误协议兼容。回环 HTTP 测试使用获准的非沙箱执行；未访问桌面或 VM。
- Linux、Windows amd64、Windows arm64 定向 `go vet ./scripts/native-smoke`。
- `GOOS=windows GOARCH=amd64/arm64 CGO_ENABLED=0 go build -trimpath ./scripts/native-smoke`，两架构助手构建通过；两架构 `go test -c` 通过。Windows 特有 ABI/预取消 UI 线程回归仅已编译，未在本机运行。
- `python3 -m unittest discover -s scripts -p 'test_windows_*smoke.py'`：14 项，新入口覆盖显式开关、两鉴权/像素/事件证据要求，以及助手/目录清理/原入口失败不报告成功；旧部署回归全部保留。
- 新旧部署与测试脚本 `py_compile`、Go `gofmt`、`git diff --check`。

本轮没有连接用户 VM、操作宿主桌面、修改生产后端或依赖、提交 Git或改任务状态。本轮构建和离线测试不代表 Windows 实机验收通过；实际运行由主会话在复审后执行。


## 首次实跑失败后的诊断与清理修复

主会话首次实跑报告 `Fixture markers were obscured or misplaced`，随后部署清理的 PowerShell 错误遮蔽了验收异常，未发布成功报告。主会话按本次唯一 state 恢复时确认本目录服务和助手进程为 0、目录能够删除、原常驻入口 40 工具仍可用；因此不能认定持续进程泄漏。标记失败的真实原因仍待数值诊断，不能将裁剪、色彩、绘图或 DWM 时序猜测写成已确认根因。

修复保持四标记各 49 像素精确断言，未增加截图或输入重试。每次截图前仅在本次窗口及子控件执行一次有界同步 Redraw；WM_PAINT 检查 BeginPaint、几何查询、brush、FillRect、对象删除、TextOutW 和 EndPaint 的结果。同步绘图成功只证明 GDI 调用完成，最终 present 仍由截图的实际像素判定。[RedrawWindow](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-redrawwindow) 说明 UPDATENOW 在返回前处理必要绘制；[BeginPaint](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-beginpaint) 仍按更新区裁剪 DC，不能据调用成功断言整个画面已经展示。

新增仅专用 crop 的数值失败证据：固定 stage、裁剪四整数、paint_count/paint_failure_bits、四中心预期与实际 RGB、各 49 像素的不匹配数及固定四种夹具颜色在 crop 内的计数。扫描限于 100 万像素，失败 JSON 最多 16 KiB；部署端按精确字段、阶段、颜色、计数和大小白名单校验。没有保存或输出 PNG、任意文字、请求载荷、窗口句柄、目录或 Token。

Go 清理器 `cleanupWithCause` 保留原始 panic 和独立 cleanup 阶段，确保原 marker 原因不会被窗口、会话、服务或目录清理异常覆盖。原服务 Cmd.Wait 完成后，部署端仅对本次精确目录最多等待 5 秒重试删除，处理 Windows 退出/句柄释放的短延迟；不扫描、杀其他进程或处理其他 state。Python finally 的清理失败独立输出 cleanup_error，并保留原验收异常链，成功报告规则不变。

版本链：prepare 安全输出当前本机 server/helper SHA-256；远端解压后逐个 Get-FileHash 比对一致才能运行。GUI 助手参数含两期望哈希，核对自身位于本次部署目录，并流式复核固定的本次 root/remote-mcp.exe 与自身文件；每轮启动前再次核对 server。成功及失败证据保留哈希而不含路径。常驻 MCP 入口不升级或替换；本机源版本与 build 信息由主会话另行记录。公共 Python CLI 不变，内部 GUI argv 更新为 `[root, --gui, server_sha256, helper_sha256]`。

本次修复检查已过：helper Go tests/race（9 项，新增真实像素数值诊断、原失败/清理失败并存、错误二进制哈希/目录拒绝）；Linux 与 Windows amd64/arm64 定向 vet；Windows 两架构 CGO=0/-trimpath 构建及 test-c；新旧 Python 部署回归 17 项（新增 hash 不符禁止启动、双重失败原因保留及诊断拒绝任意文本/超限）；py_compile/gofmt/diff-check。本实施者未连接 VM 或操作桌面，没有修改产品后端/依赖或提交。

## 第二次实跑后的三路像素定位

主会话第二次实跑已确认 server/helper 哈希链一致、唯一部署清理成功，仍在 initial_capture 失败。专用区域为 x=48/y=71/width=664/height=441，paint_count=2、paint_failure_bits=0；四标记各 49 像素全部不匹配，整个 crop 中四种预期颜色的精确计数均为 0。实际中心 RGB 与预期完全不同。该证据排除了旧测试产物与本轮清理异常，但尚不能区分窗口呈现、绘图表面与生产采集错误。只读查阅执行适配发现没有 STARTF_USESHOWWINDOW，CREATE_NO_WINDOW 仅用于不创建控制台；没有依据直接认定窗口被启动配置隐藏或修改生产后端。

本次最小差距位于验收助手：原来只检查一次 PNG，缺少同位置的原生像素证据和呈现等待。修改范围为 native-smoke 的公共 HTTP 请求 context、GUI 截图等待与 Win32 数值诊断、薄部署白名单及对应测试；不改生产、依赖、默认旧/P0 报告或输入流程。每次专用 crop 的只读等待共享 3 秒截止时间、50 毫秒间隔、61 次上限；HTTP 请求和同步绘图消息使用同一剩余期限，获取前后验证自有 HWND 的前台身份与几何。四标记各 49 个像素仍必须精确匹配，超时、守卫失败或持续错误均失败；最后实际合格的截图 capture_id 才可用于后续一次输入，不重试输入。

新增 dc 数值证据只读取本窗口 GetDC 与 DesktopDC 的四个同位置中心，后者只用客户区原点加标记偏移，不截图整桌面。两 DC 在同一已固定且启用 PMv2 的 OS 线程取得和释放。GetPixel 返回 CLR_INVALID 时记录 valid=false 和零占位 RGB，不能把失败当黑色成功；补 visible/iconic、DWMWA_CLOAKED 查询有效位及 0～7 状态值。运行时诊断仍不含 HWND、图像、路径、用户内容或凭据。微软 [GetDC](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getdc) 规定客户区/桌面 DC 及同线程释放，[GetPixel](https://learn.microsoft.com/en-us/windows/win32/api/wingdi/nf-wingdi-getpixel) 规定 COLORREF 和无效哨兵；[IsWindowVisible](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-iswindowvisible) 的可见样式并不能证明未被遮挡，[DWM 属性](https://learn.microsoft.com/en-us/windows/win32/api/dwmapi/ne-dwmapi-dwmwindowattribute) 可区分已隐藏但仍被合成的窗口。仅查询自身窗口属性，不关闭全局动画、不修改 DWM 属性。

下一次实跑的解释边界：窗口 DC 有标记而桌面 DC/MCP 无标记，倾向呈现或遮挡问题；窗口与桌面 DC 均有标记但 MCP 持续缺失，才形成服务采集或映射问题的具体证据；窗口 DC 也无标记则需继续检查绘图与窗口状态。各路采样不是同时发生，不将单次过渡差异直接当成生产缺陷。若发现生产问题，先交主会话再调整所有权。

本轮局部检查通过：helper Go race（13 项，新增延迟呈现/持续像素错误/61 次上限/超时好图拒绝/取消与守卫失败停止、真实 HTTP 截止时间回归）；Linux 与 Windows amd64/arm64 vet；Windows 两架构 CGO_ENABLED=0/-trimpath 助手构建与 test-c；新旧 Python 部署 17 项及 py_compile；gofmt/diff-check。没有连接 VM 或操作宿主桌面，真实根因和 Windows GUI 通过证据仍待主会话复审后实跑。
