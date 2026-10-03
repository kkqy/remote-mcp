# 独立检查记录

日期：2026-10-04。检查范围为已批准的 PRD、design、implement、check.jsonl 引用、后端规范及全部本次业务实现；没有修改无关初始化配置。

## 已修复发现

### P2：辅助命令的参数错误和帮助可能回显凭据

- 文件：`cmd/remote-mcp-transfer/main.go`、`main_test.go`。
- 根因：`flag` 默认错误输出直接连接 stderr，错误参数值会原样输出；URL 的默认值直接取自环境，帮助输出也会显示该值。
- 复现：使用虚构值执行 `upload --timeout credential-review-placeholder a b`，错误原文包含该值；增加回归后旧实现测试失败。
- 修复：丢弃解析器原始错误，只返回固定中文说明；帮助仅使用固定默认值，解析结束后再读取环境 URL；帮助正常返回零退出码。
- 验证：无效超时、未知选项、无效布尔值及帮助的回归均通过，stdout/stderr 不含测试凭据。

### P2：单 IP 绑定跳过不可展示地址过滤

- 文件：`internal/server/startup.go`、`startup_test.go`。
- 根因：单地址分支在网卡过滤前直接返回，IPv6 链路本地地址会把服务端作用域编号放入供远端复制的 URL。
- 修复：单地址分支同样排除组播及 IPv6 链路本地地址，无候选地址时沿用已有诊断；显式回环和普通单 IP 仍保持原行为。
- 验证：IPv6 链路本地、IPv6/IPv4 组播三项回归先失败，修复后通过。

### P2：二进制冒烟脚本可能无限等待启动或遗留服务

- 文件：`scripts/smoke.py`、`scripts/test_smoke.py`、`.github/workflows/check.yml`。
- 根因：启动使用无超时的阻塞 readline；结束只 wait，没有超时后的强制回收，且未先检查服务是否已退出。
- 修复：启动配置读取限制为 15 秒；退出先检查状态，请求退出后最多等待 15 秒，超时强制结束并限时回收；日志句柄使用 finally 关闭。回归加入三平台 CI。
- 验证：无启动输出、服务提前退出、Unix 忽略 SIGTERM 三项生命周期测试通过；真实 Linux 二进制冒烟通过。

## 未修复发现与验收边界

- 没有遗留已确认的代码缺陷或需要改变公共接口的发现。
- Windows/macOS 没有原生运行环境；ConPTY/PTY、文件替换、Job Object/进程组回收和真实跨操作系统双端闭环仍待运行验收。本次不以交叉编译替代这些要求，也不声称 CI 已在远端运行。

## 检查结论与证据

- 已逐项审查每请求鉴权/Origin、IP 枚举和独立 JSON、上传校验与原子提交、下载源变化和辅助命令、创建去重、输出游标、Unix 作业组及 Windows 暂停创建后绑定 Job 的路径。
- 格式：`gofmt -l cmd internal` 无输出。
- 静态检查：`GOCACHE=/tmp/remote-mcp-go-cache go vet ./...` 通过。
- 类型与构建：`GOCACHE=/tmp/remote-mcp-go-cache bash scripts/build.sh` 通过，生成 Windows/Linux/macOS × amd64/arm64 的两个程序，共 12 个产物。
- 全量测试：`GOCACHE=/tmp/remote-mcp-go-cache go test -race -count=1 ./...` 全部通过。初次沙箱执行因禁止监听失败，使用工具自动审批允许临时回环监听后执行成功；这不是实现失败。
- 脚本回归：`python3 -B -m unittest discover -s scripts -p test_smoke.py` 三项通过。
- 实际二进制：`python3 -B scripts/smoke.py --bin-dir dist/linux-amd64` 通过；在 Linux x86_64 上初始化、发现工具、上传脚本、远程执行、下载 24 字节产物，SHA-256 为 `061df3cf2b104ef68a44dacdae1f4d2cf6c50b9348a5a20da8620114eed9e856`。
- 大文件：本轮未重复与修复无关的 257 MiB 测试，沿用文件实施代理已执行并记录于 verification.md 的结果，不将其记作本检查代理重新运行。
- 后端规范由主会话维护；已发送本次参数错误脱敏、帮助不展示环境 URL及单 IP 过滤规则供同步。

本次审查修复没有削减已批准功能、修改公开工具签名或扩大任务范围，没有执行 Git 提交。
