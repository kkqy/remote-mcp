# 官方文档与代码证据

2026-10-13 已使用 Context7 /docker/docs 和官方网页核对：
- https://docs.docker.com/build/ci/github-actions/push-multi-registries/ 当前示例为 docker/login-action@v4、setup-qemu-action@v4、setup-buildx-action@v4、build-push-action@v7，平台 linux/amd64,linux/arm64。
- https://docs.docker.com/engine/network/drivers/host/ host 共享网络命名空间，Compose 不能同时配置端口映射；主要部署目标 Linux Engine。
- https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry 支持 GITHUB_TOKEN 发布关联仓库包；首次发布默认私有；既有包需授予仓库权限；OCI source 标签关联仓库。
- Context7 Compose services 文档确认 network_mode: host 不能同时使用 networks 或 ports。
- internal/execution/platform_unix.go 使用 /bin/ps 扫描受管子进程，不能使用缺少完整 ps 的 scratch 或单纯 busybox 镜像。
- internal/config/config.go 实际监听参数是 --listen，没有 --work-dir；工作目录通过容器 WORKDIR/working_dir 配置。

环境：Go 1.27.2；Docker 29.9.0 CLI 和 Compose 5.6.0 可用；普通调用无法访问 /var/run/docker.sock，未安装 buildx 插件。现有 actionlint 在 .tmp/release-tools/actionlint。此次用户直接授权创建任务并编写实现，开发者要求完成已授权工作，无需重复确认；已形成并检查完整规划后启动任务。
