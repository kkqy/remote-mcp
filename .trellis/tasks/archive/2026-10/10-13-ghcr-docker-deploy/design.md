# 技术设计

## 边界与变更位置
缺口是容器交付配置，行为位于 Dockerfile、compose.yaml 和现有 Actions 工作流；不修改生产 Go 代码。新增 .dockerignore 限定构建上下文、.gitignore 排除默认 data 运行目录，README.zh-CN.md 记录中文部署指南，README.md 仅增加中文部署文档链接，避免新写英文文档。

## 镜像
多阶段构建，Go 构建版本与现有 CI 一致（1.27），BUILDPLATFORM 构建器交叉编译 TARGETOS/TARGETARCH，CGO_ENABLED=0。运行阶段用 Debian slim，安装 ca-certificates、bash、procps（/bin/ps）等必要工具；保留 /bin/sh。两个可执行程序进入 /usr/local/bin。工作目录 /workspace，默认普通用户运行；挂载目录权限通过文档和 Compose 可配置 UID/GID 解决。临时目录使用工作目录下 .tmp；需要时入口脚本只做目录准备并 exec 服务，不修改 CLI 语义。

## CI 数据流
复用 check.yml。容器验证 job 在 Linux runner 构建本机镜像并运行真实 MCP 验证；PR 无发布权限。镜像发布 job needs native/cross-build/容器验证，在默认分支 push 或 v* push 运行，QEMU+Buildx 生成双平台镜像，使用 GHCR login 与内置 GITHUB_TOKEN。仓库路径转小写，标签由环境变量安全生成，latest 只跟踪默认分支，v* 原样保留；SHA 使用完整哈希。发布并发串行不取消中途推送。只给发布 job packages: write，顶层 contents: read。

## 部署
compose.yaml 使用 network_mode: host、init、restart: unless-stopped、合理 stop_grace_period。不设置 ports/networks、privileged、pid: host 或 Docker socket。默认镜像 ghcr.io/kkqy/remote-mcp:latest。持久宿主目录挂载 /workspace，监听地址经 --listen 传入；Token 经环境变量传入，保留应用匿名默认语义并解释其访问能力。命令/进程/文件处于容器边界，host 只共享网络，宿主其他路径需显式挂载。主目标为 Linux Docker Engine。

## 发布与回滚
说明首次 GHCR 包可能为私有、改公开或登录后拉取；已有包须授权该仓库 Actions。模板不会替用户改变权限。部署可通过完整版本标签或 digest 固定镜像，修改镜像并 pull/up 回滚。构建/远端发布未实测时明确列为未验证。
