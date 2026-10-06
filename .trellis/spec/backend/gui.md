# GUI 截图与输入契约

## 1. 范围与触发条件

修改 GUI 后端、配置、工具或原生验证脚本时阅读本规范。GUI 控制当前用户桌面，复用 `/mcp`、鉴权及 Origin 校验；没有独立控制网页。缺桌面、权限或采集依赖只影响 GUI，不能阻断旧工具启动。

## 2. 签名

- 管理器提供 `DefaultConfig()`、`New(Config)`、`Register(*mcp.Server)`、`Close() error`。
- `Backend.Probe(context.Context) (Status, error)` 不发起授权；`Open(context.Context) (Desktop, error)` 建立独立于创建请求的会话。
- `Desktop` 提供 `Capabilities/Displays/Capture/Mouse/Key/Text/Close`，可选 `Done/Err` 健康接口。`Close` 可与取消中的操作并发。
- 工具为 `gui_status/open/close/screenshot/mouse/key/text`，完整类型与 JSON 标签见 `internal/gui/types.go`。HTTP 层不能复制状态机。

## 3. 请求、响应和配置契约

- `gui_open(request_id, wait_ms)` 等待 0～10000 毫秒；返回 ID、状态、实际能力与显示器，`authorizing` 后查 `gui_status(id)`。请求 ID 去重，活跃记录不淘汰。
- `gui_screenshot(id, display_id?, region?)` 输出不缩放的 PNG。原始字节交给 SDK `mcp.ImageContent`，结构化内容仅含元数据，不重复图片。
- 元数据含 `capture_id/display_id/width/height/region/logical_bounds/layout_generation/captured_at/captured_at_source/frame_sequence/freshness`。序号是成功截图计数；Wayland `latest_available/received_at` 仅表示 PNG 首字节接收观察时间，不是源渲染时间。
- `gui_mouse(id, capture_id, action, x, y, ...)` 坐标相对输出图片，叠加裁剪偏移后按实际像素与逻辑尺寸换算。Wayland 向授权流提交局部坐标，原生后端按需加全局偏移。布局变化或记录过期使旧截图失效，不猜 1:1 比例。
- Wayland 必须监视逻辑布局，不能只比较帧像素。GNOME 用 DisplayConfig，KDE 按当前 KScreen D-Bus 可用性探测，订阅后读取基线；配置变化、服务替换或监视断线使映射失效。没有可靠监视器则禁用绝对输入并说明原因；只读初始化不允许调用显示配置写入接口。
- `gui_key(id, keys)` 一次提交完整组合键，最多 8 个，释放本次按键。拖拽最多 10000 毫秒，不提供跨调用长按。
- `gui_text(id, text, mode?, paste_keys?, allow_clipboard_replace?)` 支持 UTF-8；默认 `auto` 优先原生 Unicode，再剪贴板。显式 `direct/clipboard` 不静默换模式。默认粘贴键 Windows/Linux 为 Ctrl+V。`submitted` 只代表提交事件，应用结果另验。
- 剪贴板默认保存所有支持格式后替换、恢复；无法保存则拒绝。只有 `allow_clipboard_replace=true` 允许省略保存。第三方新所有者出现时不覆盖，报告 `skipped_new_owner`。恢复所需惰性提供者保留有界旧快照直到接管或关闭；关闭后可读性单独验证。
- KDE Portal 的 RequestClipboard 实现只订阅后续 offerChanged，没有初始格式读取接口。未收到可靠格式/所有权通知时必须拒绝默认粘贴，不猜测 text/plain 就宣称完整保存。缺失或非法的 MIME 字段不能被当成已知空剪贴板。测试可显式让专用窗口发布经完整读取和哈希确认的同内容 offer，不能混同为服务默认解决初始盲区。
- KDE `application/x-kde-onlyReplaceEmpty` 是所有权控制格式，非空 selection 上原样恢复会被合成器拒绝；默认保存必须拒绝，测试准备及救援也不能删除标记后宣称完整恢复。SetSelection 期间的中间非 owner 通知不能提前丢弃 provider 数据；有界等待确认自身 owner 和目标格式集合完全一致，失败后才按实际所有权处理数据。
- 恢复失败保留有界原快照并拒绝后续文本替换。关闭先取消文本操作、等待独立恢复，保持信号和 FD 传输可用；只有仍能确认自身所有权才重试，不能覆盖外部新所有者。错误原因使用固定枚举，不能拼接 D-Bus Body。
- SetSelection 已提交但 owner 确认超时属于未知状态，中间非 owner 不能直接证明第三方接管；须保留有界恢复来源，不能误报 `skipped_new_owner`。关闭等待传输 goroutine 全部退出后清除原始 provider 和 recovery 数据，终态记录不能继续持有剪贴板字节。
- 最多一个待授权或就绪会话。操作串行，真实忙时返回 `busy`，不建无界队列。取消等待不提前释放占用；底层完成、释放按键和占用后发布结果。
- 默认：空闲 30 分钟，终态保留 10 分钟/256 条；授权 2 分钟，操作 30 秒；原图 16777216 像素/PNG 16 MiB；坐标记录 64 条/5 分钟；文本 64 KiB，剪贴板 1 MiB。分配或替换前限额，像素乘法用除法避免溢出。
- CLI 为 `--gui-idle/--gui-authorize-timeout/--gui-operation-timeout/--gui-max-pixels/--gui-max-png-bytes`。Linux/Windows 四组合保持 `CGO_ENABLED=0`；Wayland 运行需用户 D-Bus、匹配 Portal、PipeWire、GStreamer 及采集插件。
- 普通日志不输出文字、图片、剪贴板、Portal 令牌或辅助进程完整 stderr。Go 输出限额不等于第三方媒体进程的总 RSS 上限。

## 4. 校验与错误矩阵

| 条件 | 结果 |
| --- | --- |
| 无桌面、缺依赖、权限不足 | `no_gui/dependency_missing/permission_denied`，旧工具仍可用 |
| 拒绝或授权超时 | `authorization_cancelled/authorization_timeout`，清理 Request 和会话 |
| 坐标 NaN/Inf、区域越界、未知或重复键 | `invalid_argument`，不输入 |
| 无有效截图或布局改变 | `stale_capture`，不猜映射 |
| 像素、PNG、文本超限 | `limit_exceeded`，不绕过限额 |
| 缺可靠映射或指定能力 | `unsupported`，说明实际原因 |
| 操作仍运行 | `busy`，不排无界队列 |
| 无可靠剪贴板保存且未允许替换 | `clipboard_preservation_unavailable`，不修改剪贴板 |
| 部分输入或恢复失败 | `input_failed/clipboard_restore_failed` 与 `input_may_have_applied`，不可盲目重试 |
| 会话撤销或连接关闭 | `session_closed`，结束等待并释放资源 |
| Portal 可选字段缺失或非法 | 业务失败或不可用映射，不对 nil Variant 执行反射转换，不 panic |

## 5. 正常、边界与失败案例

- 正常：打开 → 截图 → 以截图坐标聚焦 → 输入中文 → 再截图核对 → 关闭。
- 边界：200% 缩放的裁剪图，先叠加偏移再换算逻辑单位，允许其他屏幕负全局坐标。
- 边界：静止 Wayland 无持续新帧，等待有界且准确标注最新可用帧。
- 失败：输入取消时可能已生效，仍持有占用直到释放按键；不自动重试。
- 失败：原剪贴板含不能恢复的文件传输 MIME、超限或读取超时，默认不替换。

## 6. 必需测试与断言

- 管理器：连续成功、并发忙、取消占用、授权和关闭、坐标淘汰、裁剪缩放、多屏布局失效、NaN/Inf 与溢出。
- Linux：Portal 可选字段、失败/拒绝、响应先于方法返回、FD 读写限额、辅助进程超时/回收、剪贴板竞争与恢复失败。模拟总线不替代原生验收。
- 剪贴板恢复：覆盖控制格式拒绝、恢复中的非 owner→owner 通知、格式集合不匹配、取消后的原快照保留与关闭重试。显式刷新测试保留有界原始备份；失败不得无提示销毁唯一恢复来源，内容相等不能代替所有权证据。
- Wayland 布局：覆盖像素不变但逻辑尺寸变化、授权期间变化、监视器失败/断线和服务 owner 替换；断言旧截图拒绝、映射禁用，绝不能仅订阅未存在的接口就宣称监视成功。
- 独立 HTTP：七 GUI 与旧工具共存；PNG 只有一份正确 Base64，元数据与像素一致；匿名、Token 和 Origin 保持。
- `scripts/gui-smoke.py --execute --fixture` 只操作专用窗口，核对实际中文、组合键、点击、双击、拖拽、滚动；Wayland 窗口必须原生。剪贴板用只读哈希比较输入前、恢复后和关闭后的内容，不输出原文。
- `--fixture-inputs-only` 只与专用窗口并用，不能与刷新或允许替换并用。首次焦点等待使用有界授权期限；后续每次操作仍短期限守卫。Qt 普通 QLineEdit 全选可能复制到 PRIMARY，因此该子集使用 Password 回显模式阻止自动复制，再核验组合键清空实际字段；不调用 gui_text 或剪贴板采样，中文恢复必须保留待验收。
- 完整专用窗口也必须阻止 SelectAll 的自动 PRIMARY 复制：窗口处理真实 SelectAll 快捷键并直接选择字段，其余 Qt 输入路径保持；不能让验证准备改写 PRIMARY 后再把 Klipper 同步当成服务恢复结果。Qt offscreen 回归只证明隔离环境的控件行为，不能代替宿主剪贴板验收。
- 跑全包 test/vet、本机 race、Linux/Windows 四组合构建和旧二进制冒烟。Windows/X11/GNOME/KDE、客户端图片展示及混合缩放分别记账；构建不是原生成功。2026-10-06 用户取消 macOS 支持，Darwin 后端及相关测试不再保留。

## 7. 错误方式与正确方式

错误：调用取消就释放 busy，使新旧输入交错；或 worker 先 cancel 自己，再检查 context，把成功误报取消。

正确：底层结束并释放按键 → 解除占用 → 发布结果；调用方取消只结束等待。

错误：缺少字段时对 nil Variant 调用 `dbus.Store`，或猜测媒体像素等于逻辑坐标。

正确：先检查字段与类型，使用已查证逻辑尺寸；无法可靠映射时保留截图、禁用绝对输入。

错误：有界输出类型嵌入 `bytes.Buffer`，只覆盖 `Write`，认为 `io.Copy` 一定经过上限校验。

正确：缓冲区作为私有字段，避免提升 `ReadFrom` 方法绕过 `Write`；用真实复制路径断言超限失败。X11 连接与握手必须使用可取消传输，取消及关闭能打断等待协议回复，不只放弃等待 goroutine。
