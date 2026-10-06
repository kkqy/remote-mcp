# 原生验收差异复盘

日期：2026-10-06。本轮 Windows/amd64 实际执行找到了跨平台取样与错误分类差异；最终通过结果另见 windows-native.md，不把早期失败或交叉构建算成功。

## 1. 根因分类
- 隐含假设（E）与原生覆盖缺口（D）：假定 Windows os.Stat/Lstat 的文件身份已随属性查询固定。Go 的路径身份可能延迟到首次 SameFile 才查询，新文件可被误取作前置身份。失败测试本身也是按路径懒取样；生产 Patch 主 before 原本由 f.Stat 捕获且稳定，真正需要加强的是打开前、最终路径复核及日志路径快照。
- 隐含假设（E）：假定 syscall.ECONNREFUSED 等 POSIX 名称在 Windows 与 Winsock 返回值一致。原生拒绝连接误落入 connection_error，超时/不可达存在相同分类边界。
- 变更传播遗漏（C）：新验收助手再次嵌入 bytes.Buffer，提升 ReadFrom 导致 io.Copy 绕过 Write 限额；已有 GUI 规范曾记录同类风险，通用质量规范尚未覆盖验收助手。

## 2. 首轮检查为何未发现
Linux 自动测试和 Windows 交叉构建验证了语法、ABI 及模拟结果，不能执行 Windows 延迟身份/WSA 行为。原生失败后先区分生产取样与测试取样，没有放宽 conflict/refused 断言。助手输出捕获缺陷由独立复审及真实 io.Copy 回归定位，不以事后长度断言作为捕获上限。

## 3. 防止重现

| 优先级 | 机制 | 实际措施 |
| --- | --- | --- |
| P0 | 身份捕获 | Windows statPath 通过不跟随 reparse 的短期句柄立即 f.Stat；打开前/复核及日志快照均使用它 |
| P0 | 严格回归 | 同内容/同大小/mtime 不变的替换仍须冲突；打开期间换成目录/特殊类型也拒绝 |
| P0 | 平台分类 | 分平台识别 WSA 常量；用 net.OpError/os.SyscallError 包装后的 refused/unreachable/timeout 断言 |
| P0 | 捕获上限 | 缓冲作为私有字段，真实 io.Copy 绕过 WriterTo 的测试证明限额不能被提升方法绕开 |
| P0 | 原生验收 | Windows 预编译包测试及隔离服务旧/P0 Token/匿名四轮，清理后再确认用户入口可用 |

## 4. 系统性核对
将文件身份检查扩展至 logstream 的打开和每轮路径快照，避免只修失败测试而漏掉同根因。保留 Windows 子进程、ConPTY、文件替换与权限的独立原生证据；Windows/arm64 只有构建结果时仍如实标记。远程 Python 缺失由标准库 Go 助手解决，PowerShell 明确 UTF-8，不安装新依赖或忽略编码错误。

## 5. 知识固化
已更新 backend/remote-debug.md 的 Windows 快照与 WSA 契约、backend/quality-guidelines.md 的原生远程验收及有界 writer 回归。项目没有 src/templates/markdown/spec 模板目录，不创建无关模板；规范随本轮具体提交计划一并提交，确认前不提交、不推送。
