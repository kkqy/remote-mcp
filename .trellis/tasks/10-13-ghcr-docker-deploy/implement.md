# 执行计划

- [x] 实现 Dockerfile、.dockerignore、必要入口脚本与 compose.yaml。
- [x] 扩展 check.yml，加入容器验证与带门禁的 GHCR 双架构发布。
- [x] 补充中文部署说明和英文 README 中的中文指南链接。
- [x] 检查 Actions（actionlint）、Compose 默认/覆盖配置、镜像构建与真实 MCP 行为；仅运行与本改动相关的现有回归。
- [x] Trellis check 子代理审查整体契约、运行依赖、标签触发、权限与文档一致性。
- [x] 更新容器发布规范与验收记录。保留既有 .opencode/package.json 改动。
- [x] 用户已授权提交、合并到 main 并删除开发分支；本次完成本地提交与任务归档，不推送远端。

## 验证安排
临时文件及工具缓存在仓库 .tmp。已有 .tmp/release-tools/actionlint 可用。Docker CLI/Compose 存在，普通权限无法访问 daemon，buildx 插件缺失；先验证 Compose、actionlint，再按环境能力申请直接 Docker 命令所需权限。实际镜像构建或 daemon 仍不可用时记录具体阻塞，并让 CI 执行容器验证。
