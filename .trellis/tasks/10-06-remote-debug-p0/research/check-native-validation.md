# 原生验收入口续接复审

日期：2026-10-06；复审范围为 `.github/workflows/check.yml`、`scripts/p0-smoke.py`、`scripts/test_p0_smoke.py`、两份 README，以及本轮 task/spec 新增验收段。按 Trellis check 角色直接复审，没有派生代理、提交、推送或触发远程 CI。

## 发现

没有发现需要修复的本轮代码问题，没有修改产品代码或验收脚本。

- 三平台 native job 均包含旧功能/P0 × Token/匿名四轮入口。`CGO_ENABLED=0` 只存在于 native 产物构建步骤；race 步骤没有该覆盖。六组合 cross-build 保持独立，P0 artifact 只收集有限 JSON 报告。
- 等待夹具先用 ready 文件确认运行，并保持输出闸门关闭；真正读到 timeout 后再打开输出闸门。ConPTY 初始化序列及输出分块有界消耗，打开退出闸门后继续读取至 exit，再核对 state=exited、exit_code=0。实际实现先完成进程 Wait、Kill/Close 和输出清理，再发布 exited，成功路径不会只以异步 stop 请求代替资源终态。
- 日志实际完成追加、路径轮转、同文件缩短至旧 cursor 之前的截断；断言 generation 变化、坐标重置、中文原始内容及新的空等待超时，最后核对 log_close 的 closed。
- Windows 只在停止前仍运行且终止返回码为 1 时接受 TerminateProcess 清理；报告标为 terminate_process，提前以 1 退出的离线回归会失败。Linux/macOS 正常信号停止返回 0 时报告 graceful。Windows 原生行为尚未实际运行。
- 启动输出、协议响应、日志与报告显式严格 UTF-8；文件路径、用户内容和凭据不进入报告。旧报告在启动前删除，成功摘要在业务断言、资源关闭、服务清理、日志过滤及临时目录清理之后发布。报告同目录临时写入并替换，发布失败清理临时文件且不输出成功。
- 七项新测试覆盖两种鉴权、默认非 UTF-8 编码、无效 UTF-8/凭据泄漏/业务或清理失败、Windows 提前退出、报告替换失败和旧报告删除失败；协议测试直接核对匿名请求没有 Authorization。它们仅隔离外部服务调用，保留真实报告、日志和编码处理，不替代原生功能验收。
- README、design/implement 与 quality-guidelines 的本轮契约一致，明确保留 Windows/macOS 未验标记。原有用户文件与 GUI task 的 SHA-256 未改变。

## 验证

- `python3 -m unittest discover -s scripts -p 'test*smoke.py'`：38 项通过。
- `python3 -m py_compile scripts/p0-smoke.py scripts/test_p0_smoke.py`：通过。
- `python3 scripts/p0-smoke.py --help`：通过，帮助包含报告参数且为英文。
- `git diff --check`：通过。
- 使用本机已安装 PyYAML BaseLoader 读取工作流，独立断言三平台矩阵、四轮命令、build 局部 CGO 设置、race 无 CGO 覆盖、always artifact 和 cross-build 入口：通过。没有 actionlint，未宣称远程工作流已经执行。
- 核对实现代理最新 Linux/amd64 实际二进制两模式报告：`/tmp/remote-debug-p0-linux-token.json`、`/tmp/remote-debug-p0-linux-anonymous.json`。报告生成时间晚于当前脚本最后修改时间，所有 P0 检查为 true、service_shutdown=graceful；未无依据重复两轮集成。
- 本轮没有 Go 文件或依赖变化；前轮最终代码的 `go vet ./...`、`go test ./...`、`go test -race ./...` 和六组合构建已通过，见 check-final.md/validation.md。本轮 Python/YAML 没有配置独立 lint/type-check 工具，语法及结构检查通过。

## 尚未验证

Windows/macOS 原生 PID、端口、ConPTY/PTY、日志轮转截断和文件替换尚未执行。用户已确认存在对应环境，但系统、架构和连接方式仍待提供；这是环境验收缺口，不以离线 mock、Linux 报告或 CI 配置消除。本任务及原 GUI 任务仍须保持进行中。

复审完成，停止写入，交回主代理续接原生环境验收。
