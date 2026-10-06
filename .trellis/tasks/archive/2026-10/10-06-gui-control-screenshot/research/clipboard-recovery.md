# KDE 剪贴板失败后的恢复边界

日期：2026-10-06。第六轮真实输入成功提交但默认恢复失败。此记录只说明已核对接口及救援规则，不称原内容已恢复。

## 本机只读接口核对

经自动审核批准，对当前用户总线执行 `gdbus introspect --session --dest org.kde.klipper --object-path /klipper`，没有调用历史内容读取或任何写入方法。实际接口为 `org.kde.klipper.klipper`：

| 方法 | 实际签名 |
| --- | --- |
| getClipboardHistoryMenu | 无参数，返回字符串数组 |
| getClipboardHistoryItem | int32 索引，返回字符串 |
| getClipboardContents | 无参数，返回字符串 |
| setClipboardContents | 一个字符串参数，无返回值 |

[KDE 官方实现](https://github.com/KDE/plasma-workspace/blob/master/klipper/klipper.cpp) 的 setClipboardContents 同时写入 Selection 与 Clipboard，仅恢复文本，不能当成全部原 MIME 保留。getClipboardContents 读取历史顶部条目，也不能替代 Qt 对当前实际 selection 的核验。

## 由主会话执行的匹配方案

使用 QtDBus 的类型化调用，把全部条目只留在进程内存；读取菜单只用于确定有界索引数量，最多 2048 项，单项 UTF-8 最多 1 MiB。以原 text/plain 的字节长度和 SHA-256 匹配，不打印返回字符串或异常中的消息。命中后再次读取相同索引，重新验证摘要，避免历史排序变化选错。

主会话明确恢复时，可以调用上述 setter 恢复文本，但需另核验当前 Clipboard；若原多个文本 MIME 全部原哈希相同，也可以把匹配字符串的 UTF-8 字节重建为原 MIME 的 QMimeData，只写 Clipboard。历史匹配仅证明该文本字节符合基线，不能证明图像或其他非文本 MIME 也可重建。本审阅没有执行上述恢复写入。

## 验证窗口的内存备份

显式 refresh 在发布前保留全部原有格式和字节，累计 1 MiB、最多 64 种，不写盘或日志。Qt provider 接管改变不删除独立备份。

外部 Portal/Klipper provider 的身份不由 Qt Clipboard API 提供。当前值等于测试文字、为空或含 KDE 标记都不能单独证明没有用户新 owner。自动救援还要求会话已确认关闭、两次读取版本一致、Qt 两次确认本进程仍拥有提供者；没有正向证据时保留窗口及备份，由用户点击恢复按钮明确恢复。救援后仍保留提供者窗口，避免马上退出又丢内容；原验收失败状态不变。

Qt 底层单格式物化与桌面原子所有权比较仍有此前记录的接口边界。新脚本的失败保留/拒绝第三方内容覆盖已离线验证，真实救援及主会话修复后的默认恢复仍待原生重跑。


## Qt 提供者与选区补充复审

本机 PySide6 6.11.2 在独立 `QT_QPA_PLATFORM=offscreen` 进程验证：`setMimeData` 前对象归 Python 所有，发布后 `shiboken6.ownedByPython=false`，删除局部引用及执行 GC 后原生对象仍有效。因此没有证据把第六轮 KDE source 接管归因于局部 wrapper GC。验证窗口仍显式保持 `self.offered_data` 强引用，直到确认自身已失去所有权且独立原备份存在才放下引用；这明确 Python 侧生命周期，不能阻止 Qt 原生销毁或外部 owner 接管。原字节备份独立保留，不写盘或日志。

Qt 6.11 官方 `QWidgetLineControl::processKeyEvent` 在受支持选区的环境会在普通已处理按键后调用 `copy(QClipboard::Selection)`；`copy` 仅在非空选区且 `Normal` 回显时写入。来源：[Qt 官方实现](https://raw.githubusercontent.com/qt/qtbase/6.11/src/widgets/widgets/qwidgetlinecontrol.cpp)。这使普通 Ctrl+A 可能发布 PRIMARY 并被剪贴板管理器同步。完整文字验证字段现仅对 SelectAll 快捷键直接 `selectAll/accept`，避免这一自动复制尾部；仍保留正常可见中文。仅键鼠入口使用 Password 回显，另由官方复制前置条件拒绝复制。离线 Qt 回归验证真实组合键到达、全选/删除结果、回显模式和隔离 Clipboard 不变；offscreen 无宿主 PRIMARY，不能将它当作真实合成器选区监测证据。

原快照含 KDE `application/x-kde-onlyReplaceEmpty` 时，测试准备与自动救援在发布前拒绝，不自动删标记。手动恢复按钮若持有该标记，仅在额外明确确认后恢复有效 payload 并省略控制标记；保持完整原备份，报告原格式集合未完全恢复，不宣称全部 MIME 还原。
