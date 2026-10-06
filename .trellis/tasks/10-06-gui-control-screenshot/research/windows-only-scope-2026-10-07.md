# GUI 平台范围调整

用户于 2026-10-07 明确取消 Linux 的全部 GUI，仅保留 Windows；此决定取代旧 Wayland 首版要求。当前任务的 PRD、设计、执行计划和 backend 规范已同步；旧 Linux 研究仅用于追溯，从当前实现/检查注入中移除。

## 实际边界
Linux 服务不创建图形管理器、不注册 gui_*、不显示或接受 GUI CLI 参数；删除 X11/Wayland/Portal/KScreen/Clipboard 专属实现、回归与 dbus/xgb 依赖。共享管理器及显式假后端协议测试保留供 Windows 验证。Qt 测试窗口和剪贴板辅助脚本删除；通用 HTTP/PNG 验证仍可从 Linux 宿主操作远端 Windows。

Windows 原生后端的截图、坐标、输入和 Unicode 实现保持。当前 Windows 仅支持 auto/direct，Clipboard 模式明确 unsupported；旧报告里的 clipboard_preservation 不再列为待交付。多屏/混合 DPI、布局变化、撤权、Windows/arm64 实机及 MCP 客户端图片展示据环境分别记账。

## KDE 历史测试清理
当前 j 测试在范围变更后停止，未发生验证器输入或 Clipboard 刷新。最终 runner 退出码 1（授权未完成），provider/outer 退出码 0；持有的 12 个 PID 均确认不存在。原始结果见 kde-workspace-segmented-10.json 和对应 cleanup.json。没有继续发起 Linux 权限或点击请求。

## 验证记录
Go 实施与脚本离线检查分别由 implementation-windows-only-go.md、implementation-windows-only-scripts.md 记录。最终 Linux 33工具/CLI/七未知工具调用及旧功能/P0两鉴权四轮见 linux-gui-removal-2026-10-07.json。复审后重建的四组合八产物及助手两架构见 windows-only-final-builds-2026-10-07.json。最终 Windows/amd64 当前二进制及部署子进程 .tmp 环境的双模式真实专用窗口验收通过，报告 windows-gui-windows-only-2026-10-07.json 包含裁剪/标记/真实鼠标组合键/中文emoji/前台守卫/清理及旧入口保持。全范围复审另记 check-windows-only.md；交叉编译不算 Windows/arm64 原生成功。

未修改用户 .opencode/package.json，未提交或推送当前批次。
