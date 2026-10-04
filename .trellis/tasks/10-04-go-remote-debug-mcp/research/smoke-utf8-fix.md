# Windows 冒烟脚本 UTF-8 日志读取修复

日期：2026-10-04。

## 原因与修复

用户提供的 Windows CI 日志显示上传、执行、下载完成后，`logs.read()` 按 Windows 默认 cp1252 解码 Go 输出的 UTF-8 中文日志，在字节 `0x90` 处失败。文件传输成功不代表脚本已完成全部检查。

- `scripts/smoke.py` 对日志文件、服务启动 stdout、传输辅助命令 stdout/stderr 显式使用 `encoding="utf-8"`，保留默认严格解码，非法 UTF-8 仍然失败。
- 成功 JSON 从传输完成处移至服务退出、日志检查、文件关闭和临时目录清理全部成功之后，避免先报告 `ok: true` 再抛异常。
- 保留现有端口转发、鉴权和数据完整性检查，不修改 Go 代码或 CI 平台配置。

## 回归证据

`scripts/test_smoke.py` 增加执行真实 `main()` 的边界替身：真实临时文件接收 UTF-8 中文日志；启动 JSON 按显式编码读取；辅助命令输出按显式编码解码；未提供编码时模拟 cp1252。网络边界以最小成功响应替代，避免回归依赖监听权限或已构建程序。

1. 旧脚本在 Token、匿名两个子用例均出现 cp1252 `UnicodeDecodeError`，包含与 CI 相同的日志读取位置及 `0x90` 字节。
2. 仅修复编码时，日志泄漏检查失败及非法 UTF-8 用例均发现 stdout 已提前输出成功 JSON，两个断言失败。
3. 移动成功输出后，六项 Python 测试通过；无效 UTF-8 未被替换或忽略。

验证命令与结果：

- `python3 -B -m unittest discover -s scripts -p test_smoke.py`：6 项通过，包含原有生命周期检查。
- `python3 -B scripts/smoke.py --bin-dir dist/linux-amd64`：Token 模式通过。
- `python3 -B scripts/smoke.py --bin-dir dist/linux-amd64 --no-token`：匿名模式通过，中文匿名日志断言通过。
- 两次实际二进制闭环均下载 24 字节，SHA-256 为 `061df3cf2b104ef68a44dacdae1f4d2cf6c50b9348a5a20da8620114eed9e856`；使用现有 Linux 二进制，未重建程序。

本机冒烟首次因沙箱限制无法启动监听，获准本机监听执行后通过。当前环境为 Linux，仅模拟 cp1252 默认编码；Windows 原生结果仍须重新运行 CI，不能声称已通过。
