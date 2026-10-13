# GHCR 镜像发布与 host 网络部署模板

## 目标
用户已明确要求创建任务并编写自动发布 GHCR 镜像的 GitHub Actions，以及部署 remote-mcp 的 Docker Compose 模板；网络必须使用 host。

## 已确认背景
- 现有 `.github/workflows/check.yml` 在 push/PR 执行 Linux、Windows 原生测试与四组合构建，v* 标签在验证成功后发布二进制。
- `internal/config/config.go` 默认监听 0.0.0.0:8080，REMOTE_MCP_TOKEN 可选；容器不改变应用鉴权语义。
- `internal/execution/platform_unix.go` 默认执行 /bin/sh，进程回收依赖 /bin/ps，镜像须保留实际运行依赖。

## 要求
1. 自动构建 Linux amd64/arm64 镜像到 ghcr.io/<小写仓库路径>，发布前通过既有验证与容器验证。
2. 默认分支 push 发布 latest 与 sha-<完整提交哈希>；v* 标签 push 发布同名标签和 SHA 标签。其他分支与 PR 只验证，不登录或推送 GHCR。
3. 使用内置 GITHUB_TOKEN；packages: write 仅授予镜像发布作业。保留现有二进制发布行为。
4. 提供可本地构建的 Dockerfile、构建上下文排除配置和 compose.yaml；明确 network_mode: host，不配置 ports。
5. 镜像保留终端、进程清理、文件传输所需运行环境，服务和辅助程序可用。Compose 可配置镜像、监听地址、Token、挂载目录，支持持久文件与正常停止。
6. 中文说明覆盖首次发布、拉取权限、启动、更新、回滚与容器的文件/进程边界；所有新增文档和注释用中文。

## 验收标准
- Actions 专用语法检查通过；发布作业依赖现有 native/cross-build 和容器验证。
- Docker Compose config 可解析默认值及自定义值，host 模式无端口映射。
- 可用环境内验证镜像构建、服务初始化、鉴权、工具调用、终端及停止；无法运行的环境限制须明确记录，不能冒充实际 GHCR 发布成功。
- 默认分支、版本标签、PR 的标签和写权限行为与文档一致。

## 范围之外
不变更 Go 协议与工具行为，不创建 Windows 容器，不默认开放宿主文件系统/PID/Docker socket/特权模式，不自动部署远端，不自动推送 git 或改变 GHCR 可见性。
