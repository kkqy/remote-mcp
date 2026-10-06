# 图形桌面平台边界研究

研究日期：2026-10-06。本文区分已查证的约束与待验证的技术方案，不表示 GUI 功能已实现或已通过平台验收。

## 仓库证据

- `scripts/build.sh:12` 固定以 `CGO_ENABLED=0` 构建；现有依赖没有图形桌面后端。
- `internal/server/server.go:65` 起集中注册各模块工具；新模块可以沿用相同 MCP 入口与访问控制。
- 已安装的 `github.com/modelcontextprotocol/go-sdk@v1.8.0/mcp/content.go:57` 提供 `ImageContent`，包含 `Data []byte` 与 `MIMEType`。截图可以返回标准 MCP 图片块，同时提供结构化坐标元数据；客户端实际展示能力仍须单独验证。
- 初始探测未找到 `Xvfb`；后续已发现当前 KDE Wayland 桌面及用户会话 D-Bus，具备本机 KDE 原生验证条件。Xvfb 缺失不能推导为无图形环境，X11 独立会话仍待确认。

## 系统约束

### Windows

微软的 [SendInput 文档](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-sendinput) 说明该接口用于合成鼠标和键盘输入，受到 UIPI 限制，不能据此承诺控制更高完整性级别的应用；失败返回值也不足以单独确定 UIPI 是根因。

首版建议面向运行服务的当前交互桌面，不承诺 Windows 服务会话、锁屏或安全桌面控制；这属于待审阅的兼容边界。截图、DPI 与多屏坐标还需专项验证。

### macOS

Apple 提供 [CGPreflightScreenCaptureAccess](https://developer.apple.com/documentation/coregraphics/cgpreflightscreencaptureaccess()) 检查屏幕采集访问权限，提供 [AXIsProcessTrustedWithOptions](https://developer.apple.com/documentation/applicationservices/1459186-axisprocesstrustedwithoptions) 检查辅助功能客户端信任状态。截图和输入要分别处理权限状态，不能由其中一项推断另一项可用。

为保留现有无 CGO 构建，可以研究 [purego](https://github.com/ebitengine/purego) 调用系统框架；这里只确认该项目提供无需 CGO 调用 C 函数的机制，不代表选定方案已适配本项目或六种构建组合。

### Linux X11

[X.Org XTEST 协议](https://www.x.org/docs/Xext/xtest.pdf) 定义合成按键、按钮和指针事件。可通过 X11 协议访问屏幕并注入输入，但必须探测显示服务器连接及所需扩展，不能仅因存在 DISPLAY 就报告完整能力。

### Linux Wayland

[XDG RemoteDesktop Portal 文档](https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.RemoteDesktop.html) 规定创建会话、选择设备及启动授权流程，并通过 ScreenCast/PipeWire 联合采集屏幕。首次启动通常由桌面显示授权对话框；持久授权和恢复令牌也受具体 Portal 支持及用户许可约束。

由这些接口差异可推断，Wayland 需要额外的授权会话与图像采集实现，不能直接把 X11 后端当作完整 Wayland 桌面支持。XWayland 中可访问部分 X11 窗口不等于整个 Wayland 桌面可被操作。

## 依赖方案的限制

[kbinani/screenshot](https://github.com/kbinani/screenshot) 提供跨系统截图及多显示器支持，但其文档注明 macOS 需要 CGO；不能直接作为保留当前六组合无 CGO 构建的完整方案。若采用该库，应拆分平台后端并单独研究 macOS 实现，依赖版本需在技术设计阶段锁定。

## 已确认决策与待收敛边界

- 用户已要求首版包含 Wayland；Windows、macOS、Linux X11 与 Wayland 均纳入规划。
- Portal 授权、会话生命周期及 PipeWire 采集必须包含在 Wayland 的首版交付中，不以 XWayland 假报完整支持。
- 用户已确认 Wayland 首版以 GNOME/KDE 为完整操作验收目标；其他后端按实际能力报告，专项研究见 `wayland.md`。
- 截图粒度和坐标契约将在平台边界确认后进入最终规划审阅。
