# Windows 冒烟脚本 UTF-8 修复独立审查

日期：2026-10-04。

## 审查范围与结论

检查 `scripts/smoke.py`、`scripts/test_smoke.py` 的本轮差异以及实施记录 `smoke-utf8-fix.md`。未修改 Go、协议、端口转发或 README，未提交代码。

- 日志文件、服务启动 JSON 的 `Popen` 文本流，以及辅助命令 `run` 的 stdout/stderr 均显式使用 UTF-8，默认严格解码。不存在通过忽略或替换非法字节掩盖错误的处理。
- 最终成功摘要位于服务停止、日志校验、日志文件关闭和临时目录清理之后；任何该阶段异常都会阻止成功输出。
- 回归以真实临时日志文件和文本解码执行 `main()`，仅替换子进程和网络边界；cp1252 模拟能够检验遗漏编码的调用，未绕过实际日志检查。正常路径同时覆盖 Token 与匿名模式。
- 已同步的后端 MCP 契约和质量规范与当前实现一致。CI 已运行脚本单元测试，无需新增步骤。

## 已修复的检查缺口

原有新增测试覆盖日志校验和非法编码失败，缺少显式清理失败的成功输出断言。在 `scripts/test_smoke.py` 增加 `test_cleanup_failure_does_not_report_success`：关闭模拟服务的真实文本流后抛出清理异常，验证异常传播且 stdout 为空。没有调整生产行为。

## 独立验证

- `python3 -B -m unittest discover -s scripts -p test_smoke.py -v`：7 项通过，正常编码测试含两种鉴权模式子用例。
- `git diff --check`：通过。
- 两个 Python 文件以 UTF-8 读取并执行 `compile(..., 'exec')`：语法检查通过。项目未配置 Python 静态类型检查器，本次无 Go 变更，不额外运行无关 Go 检查。
- 实施者已完成 Linux 实际二进制两种鉴权模式的闭环，本轮未重复该验证。

没有遗留已确认代码缺陷。Windows 原生执行仍待 CI，Linux 上的 cp1252 模拟不等同于 Windows 原生验收。
