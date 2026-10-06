# Windows-only GUI全范围复审

## 范围与结论

已按当前check.jsonl→PRD/design/implement加载Windows-only要求，执行get_context --mode packages（单仓库，backend/frontend两规范层），读取受影响backend index及Quality Check、GUI/MCP/生命周期/错误/日志/目录规范。复审覆盖全部未提交批次：平台删文件及依赖、config/server接入和共享假后端测试、Windows生产后端、已有全部Windows原生助手与Python部署、通用GUI/P0脚本和离线测试、两README/当前任务jsonl及.gitignore。未改用户.opencode/package.json，没有提交或归档。

**全范围代码审查通过，源码与测试冻结；无其他确认未修缺陷。** Linux默认不创建图形管理器、不探测桌面、不注册GUI或接受GUI参数；Windows七工具与原生Unicode路线保留。历史Linux研究仅供追溯，不当作活动契约或待交付。任务仍in_progress，Windows/arm64原生、多屏/混合DPI、布局变化、撤权/断线及实际MCP客户端图片展示不能凭构建消除。

## 已修发现

- `scripts/native-smoke/gui_windows.go`：消息队列清理defer原登记早于recover，清理超限panic发生于守卫已经执行之后，可从UI goroutine逃逸。现恢复守卫最后执行，覆盖窗口、类和队列清理，失败不能发布window_cleaned；没有改动原生输入或标记断言。异常队列分支依据实际defer逆序审查及构建，不声称本轮实机故障注入。
- `internal/gui/tools.go`、`internal/server/gui_test.go`：gui_text描述仍承诺Clipboard保存/恢复，当前Windows实际仅direct且clipboard unsupported。改为原生Unicode与不支持剪贴板的具体英文说明，增加假后端独立HTTP及平台发现断言；实际后端与MCP签名未变。
- `internal/gui/types.go`、`scripts/gui-smoke.py`及测试：修正残留Wayland注释和Start/End名称，删除两个仅Linux恢复路线使用的错误消息白名单；已移除错误码不能再回显任意message，Windows受支持诊断保留。
- `scripts/windows-native-smoke.py`及两个Windows脚本测试：删除两处硬编码系统临时目录的原始诊断dump。主会话明确授权临时目录规则后，本机状态、打包和报告stage固定于repository/.tmp；构建TMPDIR/TMP/TEMP/GOCACHE/GOTMPDIR显式传入项目内路径并拒绝逃逸链接。远端仅取PowerShell文件系统cwd的GetFullPath，建立该cwd/.tmp下唯一目录；相对/未确认/过长工作目录失败，没有GetTempPath或系统temp回退，不扫描其他目录。mkdir也纳入精确清理路径，失败不发布成功。
- 同一部署脚本的显式持有directory分支现设置child env TMPDIR/TMP/TEMP为该目录，使包测试t.TempDir与助手内部服务不继承系统temp。无directory的只读PowerShell探测不变；新增process_start参数断言覆盖两分支。此项为最后一次Python机械修复，Go产物未再变化。

## 数据流、资源与一致性

平台条件由gui.Supported统一进入config校验/CLI与server管理器创建/注册/关闭。非Windows桩只保障共享包构建；无管理器关闭允许nil，Token/匿名独立HTTP删除工具调用走SDK unknown_tool并有安全英文日志。注入假后端的PNG/错误/鉴权测试不等同Linux支持GUI。删除X11/Wayland/Portal/KScreen/剪贴板专属代码与Qt夹具，go.mod/go.sum不再含godbus、jezek/xgb。

Windows原生助手全部路径已审：固定OS线程与Win32 ABI、PMv2、自己HWND及EDIT焦点、裁剪区域与元数据、四个7×7精确像素、真实坐标/滚轮ticks和WM_GETTEXT完整中文emoji、启动取消与有界回收、销毁/注销后清理证据。每次截图共用3秒context、最多61次；不重试输入。标准PNG图片块、有界解码、取消后最后成功发布守卫、数值白名单16KiB诊断及原始/cleanup原因分列保持。产物本机SHA→远端GetFileHash→helper自身与目录→每轮server再核验；默认旧/P0四轮与显式--execute/--gui双轮隔离，主入口不替换。成功报告在全部资源/部署清理及主入口复查后发布。直接输入不访问Clipboard，兼容参数保留不代表支持剪贴板。

通用GUI脚本以远端backend=windows判断，不以Python宿主系统代替；无--execute不联网，删除全部Qt/Portal/clipboard fixture入口。P0发现以远端environment_inspect的os核对Linux33/Windows40及精确GUI集合、重复名称拒绝，未增加GUI调用。用户UTF-8数据不翻译，失败日志保持已知域、具体错误码、Token及输入脱敏。

## 验证

- 主会话/实施者：全包test/vet/race通过，删除平台后的四组合×两入口CGO0构建、Windows双架构gui/config/server test-c与vet通过。复审机械修改后，主会话最终重建四组合两入口及Windows双架构helper亦全部通过；构建与原生分别记账。
- 审阅者：helper `go test -race ./scripts/native-smoke -count=1`；server独立假桌面/平台/nil-close定向HTTP race；server/gui/helper vet及双Windows helper vet均通过。首轮helper HTTP测试在sandbox因socket operation not permitted失败，批准本机测试环境复跑通过；没有改测试绕过限制，没有自动审批拒绝。
- 审阅者：最终Python unittest discover **49项通过**，10个公开Python文件AST、全部Go gofmt检查及diff-check通过。覆盖远端cwd来源、绝对/有界工作目录、禁止gettempdir回退、构建child项目内缓存、原始PowerShell诊断不落盘、远端child env与无dir分支、GUI显式开关/哈希链/失败双原因及清理后成功。无额外Python静态类型工具；未重复无关生产全仓测试。
- 主会话真实Linux：最终二进制SHA `dc8d2cf2fb8d3db9dc4cea83ecb647dc43e14e0303f68cb4ff4205acb352b6a7`。旧功能/P0 × Token/匿名四轮、33工具/无GUI、七个已移除工具协议英文错误和安全unknown_tool日志、help隐藏及GUI参数英文拒绝均通过，服务已结束；P0标为graceful。
- 主会话当前Windows Go产物：server SHA `5f8ce38e48936eeaf5cf2d9e484cbc7cf8ae6286160747cf6b7546bb2d7749de`，helper SHA `13c6646090c5624b8c52fa63b5cc809da6a82ab124caaf4bf9bfb501b4094a05`。最后Python child env版已完成cwd/.tmp部署、自有窗口Token/匿名实际双轮；实际鼠标、双击、拖拽、横纵滚轮、组合键清空、中文emoji、四色裁剪、前台守卫、重复close、窗口清理及deployment_cleaned/existing_service_preserved均通过，Clipboard未访问。最终记录为windows-gui-windows-only-2026-10-07.json；较早的before-child-env记录继续保存为历史，不替代最终结果。所有测试运行已结束，无活跃测试服务。

**最终质量门槛PASS。** 本代理只在项目.tmp进行离线检查，未访问系统临时目录、GUI/Clipboard/Portal或远端服务；最终原生证据来自主会话独立实跑。Windows/arm64、多屏/混合DPI、布局变化、撤权/断线及实际MCP客户端图片展示仍为未验证边界，报告acceptance_complete=false，不能据基础双轮宣称全部覆盖。未提交、归档或修改用户.opencode。
