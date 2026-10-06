# Windows 与 macOS 原生后端实施记录

日期：2026-10-06。责任范围为 `internal/gui/platform_windows.go`、`platform_darwin.go` 及对应平台测试；没有修改共享接口、Linux 后端、配置、服务或文档。共享依赖由公共模块实施者统一写入。

## 实施行为

### Windows

- `golang.org/x/sys/windows` 动态调用 User32/GDI32，无额外系统依赖。
- 每个调用固定在当前 OS 线程，设置 PMv2 DPI 上下文，并在结束时恢复。要求 Windows 10 1703 或更新版本；未将进程全局 DPI 设置改写。
- 枚举当前交互显示器的物理桌面坐标、主屏和设备名。PMv2 的输入单位及截图像素一致，保留虚拟桌面的负坐标。
- 当前线程和输入桌面必须均为 `Default`。锁屏、安全桌面、无交互桌面和服务会话不列为可操作范围。
- 截图使用 top-down 32 位 DIB、BitBlt/CAPTUREBLT 与 GdiFlush，同步后复制 BGRA 为 Go RGBA；DC、选入对象及位图沿各条失败路径释放。原始像素上限在创建 DIB 前检查。
- SendInput 使用 64 位 INPUT 联合体的明确布局，检查每次事件提交数。坐标转换保留负虚拟桌面原点；鼠标、组合键自动释放，Unicode 文本包含 UTF-16 代理对。
- SendInput 的 UIPI 约束仍生效；不承诺控制比服务更高完整性级别的应用。事件提交成功不等同于目标控件已接收。

### macOS

- 使用锁定的 `github.com/ebitengine/purego v0.9.1` 调用系统框架，无 CGO 和额外安装依赖。
- 屏幕录制权限和辅助功能权限分别探测。Probe 不弹权限框；显式 Open 可触发屏幕录制请求，等待响应或 context 取消。系统权限 UI 自身没有 Go 可取消接口；进程期间最多一个权限请求。
- macOS 14 及以后使用 `SCShareableContent`、`SCContentFilter` 和 `SCScreenshotManager`。旧系统使用仍可用的 `CGDisplayCreateImage`；没有以新系统 API 缺失为理由将截图改成桩。当前加载接口至少需要 macOS 10.15，仍需在目标版本实际确认。
- 使用 `CGDisplayBounds` 的全局逻辑坐标以及显示模式像素尺寸；处理 90/270 度方向。截图尺寸变化返回 stale_capture，不猜测输入的 1:1 缩放。
- CoreGraphics CGPoint/CGRect 使用 Darwin 结构体 ABI；purego 官方实现已支持 amd64/arm64 的结构参数及返回。位图使用明确的 RGBA/颜色空间与垂直方向转换。
- ScreenCaptureKit 回调中 retain 图像/对象，复制或取消后释放；Objective-C Block 使用 NewBlock/Release。捕获在进程内使用一个槽，已取消但尚未返回的回调保持占用，避免反复取消积累图像、Block 或 goroutine。系统 API 若永远不回调，该唯一槽保留到进程退出，后续采集有界超时；不能称为原生取消已得到验证。
- `CapturedAt` 在原生图像返回回调时记录，早于图像转换与工具返回，`fresh` 表示本次采集请求产生的新图像；系统接口没有提供更细的硬件帧时间戳。
- 鼠标使用 CGEvent，逻辑坐标加指定显示器的桌面偏移；双击设置 click state。组合键保存本次修饰键状态，中文/emoji 使用 CGEventKeyboardSetUnicodeString。
- 滚轮调用非可变参数 `CGEventCreateScrollWheelEvent2`，避免 Darwin arm64 的 C 可变参数栈 ABI 与 purego 普通调用 ABI 不一致。
- CGEventPost 无提交数量返回值，结果仅表示事件已调用系统投递，不宣称目标应用文字已出现。辅助功能权限需按实际执行文件在系统设置授予。

### 共同边界

- 共享管理器维护输入串行和剪贴板策略。两个后端直接 Unicode 能力可用时选择 `auto/direct`；当前 `Clipboard=false`，显式 `clipboard` 返回 unsupported，不触碰旧剪贴板。
- Close 与原生事件提交互斥。清理本次已按下的键/按钮允许在 closed 标志之后发送 release，适配管理器先调用 Desktop.Close 再等待底层操作完成的顺序。
- 本模块不记录图像、文字、剪贴板或系统权限响应原文。

## 查证依据

- [purego v0.9.1 函数/结构体及内存约束源码](https://github.com/ebitengine/purego/blob/v0.9.1/func.go)、[Objective-C Block 生命周期与缓存源码](https://github.com/ebitengine/purego/blob/v0.9.1/objc/objc_block_darwin.go)。另经 Context7 `/ebitengine/purego` 查询结构体、NewCallback 和 NewBlock；实际以锁定版本源码核对。
- [SetThreadDpiAwarenessContext](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setthreaddpiawarenesscontext)、[CreateDIBSection 与 GdiFlush 要求](https://learn.microsoft.com/en-us/windows/win32/api/wingdi/nf-wingdi-createdibsection)、[INPUT 布局](https://learn.microsoft.com/en-us/windows/win32/api/winuser/ns-winuser-input)、[MOUSEINPUT 绝对坐标](https://learn.microsoft.com/en-us/windows/win32/api/winuser/ns-winuser-mouseinput)、[SendInput 与 UIPI](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-sendinput)。
- [ScreenCaptureKit](https://developer.apple.com/documentation/screencapturekit)、[SCScreenshotManager 图像采集](https://developer.apple.com/documentation/screencapturekit/scscreenshotmanager/captureimage(contentfilter:configuration:completionhandler:))、[SCShareableContent](https://developer.apple.com/documentation/screencapturekit/scshareablecontent/getexcludingdesktopwindows(_:onscreenwindowsonly:completionhandler:))、[CoreGraphics 函数](https://developer.apple.com/documentation/coregraphics/core-graphics-functions)、[CGEvent 原生滚轮二代构造](https://developer.apple.com/documentation/coregraphics/cgevent)。

## 已执行检查

使用可写缓存 `GOCACHE=/tmp/remote-mcp-native-gocache`，避免主机默认 go-build 缓存的只读限制。

| 目标 | CGO_ENABLED=0 go test -c ./internal/gui | go vet ./internal/gui | 原生测试运行 |
| --- | --- | --- | --- |
| Windows amd64 | 通过 | 通过 | 未提供环境，未运行 |
| Windows arm64 | 通过 | 通过 | 未提供环境，未运行 |
| macOS amd64 | 通过 | 通过 | 未提供环境，未运行 |
| macOS arm64 | 通过 | 通过 | 未提供环境，未运行 |

已执行 gofmt。交叉编译包含对应平台测试，不能等价于执行了这些测试或完成真实 GUI 验收。

## 原生验证入口与剩余项

- 普通 `go test ./internal/gui` 在目标系统运行平台单元测试：Windows ABI、负坐标和关闭前校验；macOS UTF-16 代理对、取消后 keyup、对象释放、关闭后的修饰键释放。此处仍未运行。
- 当前系统显式设置 `REMOTE_MCP_GUI_NATIVE_TEST=1`，执行 `go test ./internal/gui -run NativeCapture -v`，枚举全部显示器并获取实际图像；缺权限或 GUI 必须失败，普通 CI 不以跳过测试宣称通过。
- 端到端原生验收使用整项任务的 GUI smoke 与目标应用 fixture：截图→点击→中文/emoji→按键/拖拽→截图，并独立核对实际应用内容。
- 仍需分别在 Windows/macOS 的 amd64/arm64、macOS 新旧采集路径、Retina、多屏混合缩放、旋转显示器、负坐标、锁屏/权限拒绝/撤销、UIPI、反复截图、对象/句柄增长及授权取消环境验证。
- 尤其需确认 macOS SCK 回调调度、权限 UI、CGRect ABI、显示模式旋转和实际图像方向；这些不能从本次交叉编译推导为运行通过。
