# GUI 功能验证记录

日期：2026-10-06。本文持续更新；工具存在、接口存在、构建成功和 GUI 原生验收是不同证据。

## 环境与只读探测

- Linux/amd64，Go 1.27.1。
- 当前会话为 KDE Wayland；DISPLAY、WAYLAND_DISPLAY、会话 D-Bus 与 XDG_RUNTIME_DIR 均已设置，本文不记录原始地址值。
- GStreamer 命令存在，pipewiresrc、videoconvert、pngenc、fdsink 插件可用。
- 原生可控测试窗口可使用已有 PySide6；它仅为验证依赖，不是 remote-mcp 服务运行依赖。
- sandbox 内访问会话 D-Bus 返回权限错误。只读 gdbus 探测经自动审核批准后执行成功：RemoteDesktop v2、设备掩码 7；ScreenCast v5、源/光标模式掩码 7；Clipboard v1。
- 用户已完成桌面授权，第二轮通过标准 MCP 捕获真实 PNG：2880×1800 像素、1800×1125 逻辑尺寸，160% 缩放；源码字段缺失导致的首次解析崩溃已修复并回归。
- KDE 额外远程控制特殊权限框曾覆盖专用窗口，用户随后明确已同意；第二轮只有一次鼠标提交，未执行文本粘贴。该轮 Qt 失焦时读取各格式为零，不能据此断言原剪贴板实际丢失。后续须确认窗口获得输入与剪贴板读取权限再核验。

## 验收进度

| 项目 | 状态 |
| --- | --- |
| GUI 公共契约、错误与生命周期测试 | 第六轮恢复修复后全包 test、vet、race 通过；私有 D-Bus race 通过 |
| 旧文件/进程/终端/转发回归 | 全包 test/race 与实际二进制 Token/匿名冒烟通过 |
| 六组合无 CGO 构建 | 第六轮恢复修复后两个入口全部通过 |
| KDE Wayland 截图与原生输入闭环 | 第八轮真实 PNG、160% 缩放坐标、点击/双击/拖拽/滚动及组合键通过；中文完整恢复待验收 |
| GNOME Wayland 原生验收 | 未验证，缺少已确认环境 |
| Windows/macOS 原生 GUI 验收 | 未验证，缺少本机对应操作系统 |
| X11 原生 GUI 验收 | 待确认是否有可用独立会话，不能以 XWayland 全桌面替代 Wayland 验收 |

系统授权必须由用户确认，不能通过改权限存储或控制其他会话绕过。原生输入仅作用于为验证创建的专用窗口，不盲目操作未知前台应用。

## 已执行的常规检查

- `GOCACHE=/tmp/remote-mcp-go-cache go test ./...`、`go vet ./...`、`go test -race ./...`：首轮通过。回环监听测试经自动审核允许后执行，没有跳过失败测试。
- `GOCACHE=/tmp/remote-mcp-go-cache bash scripts/build.sh`：Windows/Linux/macOS × amd64/arm64 的两个命令均构建通过，CGO 关闭。
- `python3 scripts/smoke.py --bin-dir dist/linux-amd64` 与 `--no-token`：实际二进制 Token/匿名闭环均通过。
- Python 离线检查随剪贴板摘要、焦点保护和清理错误回归扩充，最终数量及复审结果见 `research/check.md`。
- 第三轮 KDE 授权等待到期，未执行截图或输入；会话关闭及重复关闭通过，未改剪贴板情况下关闭后的哈希基线一致。该证据不能算作中文粘贴后的恢复验收。

真实桌面截图证据保存于本机 `/tmp/remote-mcp-kde-evidence-02/`，未提交含桌面内容的 PNG、原始 D-Bus 地址或剪贴板内容。失败轮次独立存储，不能混作成功闭环。

- 第四轮最终布局监视版已获授权并确认原生窗口焦点，PNG 与第一步坐标点击通过。默认中文粘贴立即返回 `clipboard_preservation_unavailable`；关闭后原七个格式、累计 763 字节的哈希均保持。拒绝是保护行为，不能计作中文输入成功。
- KDE [Clipboard 官方实现](https://raw.githubusercontent.com/KDE/xdg-desktop-portal-kde/master/src/clipboard.cpp) 的 RequestClipboard 只订阅后续 offerChanged，没有发送初始格式。公共 Portal 没有初始格式查询接口，代码保持未知快照时拒绝；下一步验证准备将显式发布经完整读取和哈希确认的同内容 offer，仍使用默认保存恢复模式。
- 第五轮同内容准备被 Qt 自动增加 `text/plain;charset=utf-8` 派生别名的行为拦截，未调用文本工具。原四个格式各 14 字节仍一致；验证已补仅允许这个逐字节摘要一致的已知别名，保留原始与新 offer 两份基线，其他变化仍拒绝。
- 第六轮同内容准备与原格式独立核验通过，默认文本实际进入原生 Wayland 控件（第一步测试文字），但恢复返回 `clipboard_restore_failed`。关闭后 Clipboard 留下测试文字，真实恢复缺陷正在处理，不能计为闭环成功。
- 本轮清理后确认当前仍为测试文字，再在 Klipper 历史中查找原 SHA-256 匹配项，恢复并重新核对成功，全程没有输出原内容。Klipper 接口只能恢复文本，原 MIME 集合仍未得到恢复证据；该 setter 的源码同时操作 PRIMARY，不能把它当成通用的多格式 Clipboard 原子恢复机制。后续 fixture 必须独立保留有界原备份，避免失败清理销毁唯一恢复来源。
- 第六轮之后修复了 SetSelection 中间非 owner 通知提前丢弃 provider 数据的问题，等待实际所有权及目标格式集合确认。恢复失败保留有界原快照、拒绝再次替换，并在关闭时仅仍拥有 selection 才重试。新增回归通过，但尚不能据此宣称真实 KDE 正常恢复通过。
- KDE `application/x-kde-onlyReplaceEmpty` 会让非空 selection 上的原样恢复被拒绝；默认快照及验证准备均拒绝该控制格式，不删除标记来伪造完整恢复。当前 Klipper 恢复来源可能携带它，后续将先验证不接触剪贴板的输入子集。
- 第七轮采用显式键鼠子集，没有任何 gui_text 或剪贴板读写。Portal 已 ready、PNG 成功，但 KDE 特殊权限确认框仍在；操作前焦点保护在 10 秒超时，未提交任何输入，关闭与重复关闭成功。用户随后回复已授权；脚本正在延长首次焦点等待并纠正误导性的剪贴板超时文案，不能把本轮算作键鼠验收成功。
- 第八轮使用修正后的 `--fixture --fixture-inputs-only`，退出码 0、`run_completed=true`。Qt 平台确认为 `wayland`；标准 PNG、2880×1800 像素到 1800×1125 逻辑坐标、实际点击/双击/拖拽/滚动事件均通过。Ctrl+A 后 Backspace 使固定字段实际为空；遮罩模式阻止 Qt 自动复制选区。共提交五次鼠标和两次按键，关闭及重复关闭通过，没有 gui_text 或剪贴板内容读写；外部应用行为不能由这个子集保证。证据仅留 `/tmp/remote-mcp-kde-evidence-08/`，`acceptance_complete=false`，不能代替中文恢复、多屏及五类桌面验收。
- 最终复审补充了未确认 owner 超时后的 `unknown` 与有界恢复来源、晚到通知、并发关闭/关闭中拒绝文本、MIME 不匹配及关闭后清除全部 raw 数据。私有总线全量 race 通过（5.365 秒），随后最终全包 test/race/vet、六组合两个入口构建、31 项 Python 回归、diff 与任务上下文检查全部通过。第八轮二进制早于这两项最终剪贴板修复，只证明保持不变的截图/键鼠路径；新恢复路径仍只有自动证据，不代表原生恢复成功。
