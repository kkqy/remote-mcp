# 英文运行文案实施记录

## 变更范围

按用户“错误和提示要用英文”的追加要求，统一下列程序自身的静态文案：

- `internal/execution/`：配置、参数、资源、输入与原生进程错误，十个 MCP 工具描述和输入 schema 描述。
- `internal/transfer/`：配置、文件、上传和下载错误，以及八个 MCP 工具描述。
- `internal/forwarding/`：配置、地址、规则和监听错误，四个 MCP 工具描述和输入 schema 描述。
- `scripts/gui-smoke.py`、`gui_clipboard.py`、`gui_fixture.py`：CLI 描述及帮助、运行错误、报告的检查及待验收文本、验证窗口标题与恢复提示。
- `scripts/test_gui_smoke.py`：同步直接依赖静态文案的断言，更新固定 Portal 错误示例。

稳定错误码、工具名称、JSON 字段、状态枚举、函数签名及所有业务逻辑保持一致。未新增翻译表或 i18n 运行机制。原生错误路径仍使用固定安全原因，不插入原始程序参数或剪贴板数据。

源码注释、内部文档字符串、测试说明和真实中文验证数据保留中文。两份模块级文档字符串用于 argparse 的用户可见 CLI 描述，因此改为英文。`gui-smoke.py` 对 Portal 初始剪贴板格式原因的等待匹配，与 GUI 后端协调后使用英文子串 `has not provided the current clipboard formats`，等待逻辑与期限保持一致。

## 验证结果

- `GOCACHE=/tmp/remote-mcp-go-cache go test ./internal/execution ./internal/transfer ./internal/forwarding`：通过。
- 同范围 `go test -race` 与 `go vet`：通过。
- `python3 -m unittest discover -s scripts -p test_gui_smoke.py`：24 项通过。
- `python3 -m unittest discover -s scripts -p 'test_*.py'`：31 项通过。
- 四个 GUI 脚本 `py_compile`、涉及文件 `git diff --check`：通过。
- 比较 HEAD 与当前四脚本，将字符串常量归一化后的 Python AST 完全一致，确认此次只改静态文案。
- Go 生产文件剩余中文仅为注释；GUI 脚本剩余中文为注释、内部文档字符串和中文输入验证数据。

测试中的 Qt 仅连接独立 `offscreen` 平台；本轮未连接或控制宿主桌面、未读取或改写宿主剪贴板。未提交或归档，主会话负责全量检查、六组合构建、规范同步和提交处理。

## 保留边界

英文文案调整不消除现有 GUI 原生验收缺口。系统提供的权限弹窗与原生错误本地化属于操作系统边界，不能用文案修改冒充平台实测。测试故意注入的任意中文错误与数据仍用于原有脱敏、UTF-8 或失败传播回归，不属于程序自身的静态提示。
