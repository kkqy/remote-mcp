# Wayland 逻辑布局复审

日期：2026-10-06。此处区分官方接口证据、本机只读探测与尚未执行的真实布局变更测试。

## 已确认缺口

Portal `Start` 返回的 `size/position` 是合成器逻辑尺寸和位置，不能由媒体像素尺寸推出；[ScreenCast 官方接口](https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.ScreenCast.html) 没有供客户端刷新这些布局字段的变更信号。因此，“PNG 尺寸未变”不能证明缩放、排列或方向未变。原实现仅在媒体像素变化时关闭绝对输入，不能满足旧逻辑布局坐标必须失效的契约。

## GNOME

[Mutter 官方 DisplayConfig XML](https://raw.githubusercontent.com/GNOME/mutter/main/data/dbus-interfaces/org.gnome.Mutter.DisplayConfig.xml) 给出 `org.gnome.Mutter.DisplayConfig.MonitorsChanged`，说明每次屏幕配置变化时发出；`GetCurrentState` 包含配置 serial 和逻辑显示器布局。可先订阅再读 baseline，发生变化后关闭已有授权流的绝对坐标映射并要求重新授权。没有在本机运行 GNOME，不据此声明所有目标版本已验收。

## KDE

[KDE Plasma 6.5 BackendManager 官方源码](https://raw.githubusercontent.com/KDE/libkscreen/Plasma/6.5/src/backendmanager.cpp) 的 D-Bus 路线为 `org.kde.KScreen` 服务、`/backend` 对象、`org.kde.kscreen.Backend` 接口及 `configChanged` 信号。尚未加载后端时 `/backend` 可能不存在，不能仅 AddMatch 就声称监控可用。

本机 2026-10-06 的实际探测：

- 只读 introspect `/backend` 初始返回 `UnknownObject`。
- `/` 实际提供 `org.kde.KScreen.requestBackend(s, a{sv}) -> b`。
- 经自动审核批准，调用 `requestBackend("KWayland", {})` 初始化标准读取后端，返回 true；没有调用 setConfig 或修改显示布局。
- 随后 `/backend` 实际导出 `org.kde.kscreen.Backend.getConfig() -> a{sv}` 与 `configChanged(a{sv})`。

[upstream master BackendManager](https://raw.githubusercontent.com/KDE/libkscreen/master/src/backendmanager.cpp) 已采用进程内后端。旧 D-Bus 接口不能视为所有未来版本都有；应运行时探测读取与订阅能力，缺少可靠监控时保留截图并关闭绝对输入，明确说明原因。

## 修复建议与待验证

- 只读取显示配置并订阅信号，不调用更改显示器的方法。订阅后读 baseline，避免快速配置变更丢失。
- 在信号和每次绝对输入前的配置读取中确认布局未变；变化后更新状态，使旧 capture 失效，并提示重新授权。
- 配置提供服务重启、连接断开或读取失效也使映射失效，不能保留旧可靠标记。
- KDE 信号实际存在已验证；真实缩放或热插拔事件是否触发、配置结构稳定性、GNOME 对应运行，以及重复打开会话后的新映射仍待验收。
