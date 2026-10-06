# Windows-only GUI 验证脚本实施

## 当前范围
用户于2026-10-07取消Linux GUI。本批只调整公共Python验证脚本及其离线测试；不修改Go后端、Windows原生助手、Windows部署脚本、README或当前规格。历史KDE研究与本轮原生结果由主会话保留，不转成Windows成功。

## 文件变化
- `scripts/gui-smoke.py`：保留独立HTTP/JSON-RPC初始化、七GUI工具发现、有界英文业务失败、严格PNG/坐标元数据核验、匿名/Token/CA、显式`--execute`、调用方自有目标`--input-plan`、中文输入原文、关闭与重复关闭、失败报告。核对服务`gui_status.backend == windows`，不检查或限制Python宿主系统；报告分列`host_platform`和`target_platform`。默认截图证据目录改为工作目录`.tmp/gui-evidence`。
- 删除Qt夹具、标记定位/默认输入计划、剪贴板IPC/采样/备份/刷新/救援/保留提供者以及Wayland原生窗口判断；移除`--fixture`、`--fixture-inputs-only`、`--refresh-clipboard-offer`和`--allow-clipboard-replace`参数。自有Windows输入计划仍可显式调用当前公开参数，服务保持最终参数校验权。
- 删除`scripts/gui_fixture.py`和`scripts/gui_clipboard.py`：搜索当前scripts、internal、cmd及CI后没有保留消费者。公共验证不再依赖PySide6或Qt；历史私有KDE证据不继续作为当前验证入口。
- `scripts/test_gui_smoke.py`：保留通用边界回归并增加Linux宿主到远端Windows的模拟流程、移除参数的无连接拒绝、Windows帮助范围、缺工具/错误后端在open前中止、中文emoji输入及本轮session/capture绑定、失败关闭不发布成功、实际PNG文件和元数据不一致时拒绝。临时目录显式使用项目`.tmp`。

## 已执行检查
所有命令显式设置`TMPDIR/TMP/TEMP=/home/user/projects/remote-mcp/.tmp`及`PYTHONDONTWRITEBYTECODE=1`。

- `python3 -m unittest discover -s scripts -p test_gui_smoke.py -v`：12项通过。
- `python3 -m unittest discover -s scripts -p test_windows_gui_smoke.py -v`：7项通过，Windows专用部署行为未修改。
- `python3 -m unittest discover -s scripts -p test_windows_native_smoke.py -v`：10项通过，旧功能/P0部署行为未修改。
- 四个GUI入口/测试文件`ast.parse`通过；`git diff --check`通过。
- 删除模块在当前生产/脚本/CI无剩余消费者；旧平台及已移除选项只出现在应拒绝这些入口的离线断言中。

## 真实边界
本代理没有连接远端、启动GUI、读取或修改Clipboard/PRIMARY、调用Portal或访问系统临时目录。模拟HTTP和PNG验证不代表新构建在Windows原生运行通过。Windows多屏、混合DPI、布局变化、断线、arm64及实际MCP客户端图片显示继续据主会话实际证据记账。未提交或推送。

## Windows 原生报告范围同步
当前Windows的显式`clipboard`模式返回`unsupported`，剪贴板保留已不属活动交付范围。因此仅从`windows-gui-smoke.py`的必需pending集合、对应离线成功样例和`native-smoke/main.go`的GUI摘要pending列表移除`clipboard_preservation`；其余多屏/缩放、布局、撤权和客户端图片显示边界及`clipboard_accessed=false`保持。原生鼠标、键盘、Unicode输入和清理实现不变。此三文件变更已冻结，由主会话在最终构建时重建助手。
追加变更定向验证：Windows GUI Python 7项与两Python文件AST通过，Go入口gofmt完成；Go全量检查与最终助手重建由主会话统筹，不重复执行其正在运行的质量门槛。全局diff-check曾发现其他所有者当前编辑的规格/README末尾空行，已通知主会话，不改其文件；本批三个入口定向diff-check通过。
