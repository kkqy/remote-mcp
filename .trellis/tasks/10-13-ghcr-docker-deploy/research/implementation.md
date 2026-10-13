# 实现与验证证据

2026-10-13，实现代理完成以下产品文件：

- `Dockerfile`、`.dockerignore`：Go 1.27 多阶段构建，构建缓存和临时目录位于 `/src/.tmp`，Debian slim 包含 Bash、完整 ps、CA 证书及两个程序，默认账号 10001:10001。
- `scripts/docker-entrypoint.sh`：在挂载工作目录创建 `.tmp` 后 exec 服务。
- `compose.yaml`：host 网络、init、30 秒停止期限、可配置镜像/监听/Token/UID/GID/持久目录；无端口映射、特权或宿主 PID 设置。
- `.github/workflows/check.yml`：增加容器验证和有 native/cross-build/container 门禁的 GHCR 双架构发布，默认分支与 v 标签采用不同标签策略，只有发布镜像 job 授予 packages:write。
- `scripts/docker-smoke.py`：复用既有独立 JSON-RPC 客户端和有界启动读取，实际验证容器能力。
- `README.zh-CN.md` 与 `README.md`：中文部署说明及入口链接。
- 检查代理追加 `.gitignore` 的 `/data/`，并修正测试容器启动失败清理和 PID 文件发布竞态。

## 已完成验证

- `.tmp/release-tools/actionlint .github/workflows/check.yml` 通过。
- `docker compose config --quiet`、脚本内默认值与全套自定义值解析断言通过。
- `sh -n scripts/docker-entrypoint.sh`、Python AST 语法解析、`git diff --check` 通过。
- 最终 Dockerfile 实际 Linux/amd64 构建通过：`docker --config .tmp/docker-tools buildx build --network host --load --tag remote-mcp:smoke .`。
- 镜像配置 ID：`sha256:f2e7a855292a55bb9292c7a15b93a4ca51308cc910c41e2d6b94422d3305ecd5`。
- 初版和检查修正后的最终版 `PYTHONDONTWRITEBYTECODE=1 python3 scripts/docker-smoke.py --image remote-mcp:smoke` 均通过，最终摘要：`{"ok": true, "authentication": ["token", "anonymous"], "network": "host", "service_shutdown": "graceful"}`。
- 真实验证涵盖：启动配置和 MCP 初始化/工具发现、Token 缺失/错误的 401、匿名连接、镜像内 helper 经 MCP 上传下载 UTF-8 和二进制数据并核对 SHA-256、普通进程输出、实际 PTY 输入输出、完整 ps 可用、关闭终端后后代退出、活跃进程和终端存在时 SIGTERM 后 exit 0、普通日志无 Token、停止后挂载文件保留。成功摘要只在自己的容器清理后输出。

## 环境边界与未验证项

- 初次默认桥接网络构建失败，明确原因是本机内核创建 veth 返回 `operation not supported`；仅本机验证改用 `--network host` 后成功，产品 Dockerfile 和 Actions 没有因此加入特殊网络配置。
- 本机 Docker daemon 需提升工具执行权限才能访问；Buildx 由主会话下载到仓库 `.tmp/docker-tools` 并校验官方哈希。
- `docker buildx inspect` 仅报告 linux/amd64 及 v2/v3/v4，本机没有 arm64 模拟能力。本次未构建或运行 arm64 完整镜像，Actions 已配置 QEMU；不能将配置等同于已验证结果。
- 未提交、推送 Git，未触发真实 Actions，未向 GHCR 发布镜像，未改变包权限或可见性。
- 无生产 Go 代码修改，没有重复执行完整 Go 质量矩阵；既有 Actions 继续执行原生测试和四组合构建。
