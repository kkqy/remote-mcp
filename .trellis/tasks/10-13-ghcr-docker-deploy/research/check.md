# 容器交付审查记录

审查日期：2026-10-13。范围覆盖 Dockerfile、.dockerignore、compose.yaml、入口脚本、容器冒烟脚本、Actions、两个 README 与后端容器规范。未修改生产 Go 代码；保留原有 `.opencode/package.json` 改动。

## 已修复

- `.gitignore`：Compose 默认绑定 `./data`，但该目录此前可能被加入 Git。新增 `/data/`，用 `git check-ignore data/check-fixture` 确认命中。
- `scripts/docker-smoke.py`：原先 `docker run` 失败后只有成功返回才登记容器清理；容器已创建但启动失败时可能遗留资源。拆成 `create` 和 `start`，在创建成功后立即登记清理。注入启动失败后断言命令序列为 `create → start → rm --force`，且删除的是本轮容器。
- `scripts/docker-smoke.py`：仅等待 PID 文件存在，可能在 shell 重定向已经创建空文件、尚未写入 PID 时读取。改为同目录临时文件写入后 `mv` 原子发布，保留有界等待。

## 契约核对

- `publish-image` 依赖 Linux/Windows 原生验证、四组合构建、容器验证；仅默认分支 push 或 v 标签 push 执行。PR 和其他分支没有登录及推送路径。原二进制发布行为保持不变。
- 顶层权限仍为 `contents: read`；仅镜像发布作业具有 `packages: write`，登录使用内置 Token。输入通过环境变量传入，仓库路径转小写；默认分支发布 latest 和完整 SHA，版本标签保留大小写且不更新 latest。
- 双架构发布声明为 linux/amd64、linux/arm64，使用 QEMU/Buildx；验证与发布分别使用 GHA 缓存 scope。同 ref 并发不取消进行中的推送。
- 运行镜像保留两个程序、sh、Bash、procps、CA；默认 UID/GID 10001。入口 exec 服务，工作目录与临时目录符合规范；构建上下文使用允许列表。
- Compose 使用 host、init、30 秒停止宽限和持久挂载，无端口映射、host PID、Docker socket 或默认特权。挂载源须预先存在；UID/GID、镜像、监听地址、Token 和目录均可配置。
- 中文说明覆盖匿名语义、访问权限、包授权、更新回滚、运行工具依赖与容器边界。新增容器规范、目录索引与实现一致，无需扩大生产 Go 变更。

## 本代理验证

- Actions 专用 lint：`.tmp/release-tools/actionlint .github/workflows/check.yml` 通过。
- Shell 语法：`sh -n scripts/docker-entrypoint.sh` 通过。
- Python 语法：新冒烟脚本 AST 解析通过；项目未配置独立 Python 类型检查。本次未改 Go 源码，Go 类型/编译证据复用实施代理实际镜像构建，不重复完整 Go 检查。
- Compose：默认配置和全部自定义环境变量断言通过，确认 host、无 ports/networks、账号和挂载路径；无需访问 daemon。
- 工作流标签脚本：实际执行默认分支、混合大小写版本、非法路径、超长版本四种输入；确认小写仓库路径、完整 SHA 和非法输入失败。
- 启动失败清理注入：通过，未启动真实容器。
- 既有 Python smoke 回归：49 项通过。
- 既有发布脚本回归：12 项通过。
- `git diff --check`：通过。

## 共享运行证据

- 实施代理已完成最终 Dockerfile 的本机 Linux/amd64 镜像构建，两个 Go 程序编译成功。本机内核不能创建 bridge veth，构建验证使用 `--network host`；不据此修改 CI 默认构建网络。
- 主会话通过真实模板执行 Compose up、MCP 初始化及进程工具写文件、stop、down；inspect 确认 host 与 init，退出码为 0，down 后宿主文件保留。摘要：`{"compose":"up-stop-down","network":"host","mcp":"ok","mount_persistence":"ok","exit_code":0}`。测试资源已清理。
- 实施代理已使用最终修正版冒烟脚本再次实际验证 Linux/amd64 镜像，Token/匿名两轮均通过，覆盖协议、鉴权、上传下载、命令、真实 PTY、子进程回收和正常停止。摘要：`{"ok":true,"authentication":["token","anonymous"],"network":"host","service_shutdown":"graceful"}`。完整命令和镜像标识见 `research/implementation.md`。

## 未修复问题与限制

未发现需要产品决策或超出范围的代码问题。本机实际验证仅覆盖 Linux/amd64；Linux/arm64 容器构建/运行、远端 GitHub Actions、GHA 缓存读写及 GHCR 推送尚未执行，不将配置或本机结果记为远端成功。未运行 Windows 容器，产品范围不要求 Windows 容器。
