# 容器镜像与部署契约

## 1. 范围与触发

`.github/workflows/check.yml` 的 `container` 作业在 push/PR 验证容器；`publish-image` 依赖 `native`、`cross-build`、`container` 全部成功，仅默认分支 push 或 `refs/tags/v*` push 发布 GHCR。原有二进制 Release 契约保持独立。容器仅支持 Linux amd64/arm64；配置存在不代表已经执行远端发布。

## 2. 命令签名

- `docker buildx build --load -t remote-mcp:local .`：本机镜像。
- `docker compose config --quiet`：模板解析，不代表镜像或挂载权限已验收。
- `docker compose pull` 与 `docker compose up -d`：拉取并部署。
- `python3 scripts/docker-smoke.py --image remote-mcp:local`：实际容器能力验证，需要 Linux Docker Engine 与可访问的 daemon。

## 3. 配置与产物契约

镜像地址为 `ghcr.io/<小写 github.repository>`。默认分支更新 `latest` 和 `sha-<完整提交哈希>`，v* 标签更新原样版本标签和 SHA 标签，版本标签不更新 latest。版本名与二进制发布一致，满足 `v[A-Za-z0-9][A-Za-z0-9._-]{0,62}`。事件字段经环境变量输入 shell。GHCR 登录使用内置 `GITHUB_TOKEN`，仅镜像发布作业有 `packages: write`，同 ref 发布串行且不取消正在执行的发布。镜像带 OCI source/revision 标签。

Dockerfile 使用构建机原生 Go 工具链交叉编译目标 Linux 架构，无 CGO；运行镜像保留 `/bin/sh`、Bash、CA 和 procps。`internal/execution/platform_unix.go` 的子进程回收调用 `/bin/ps`，不能改为没有该依赖的 scratch 或仅 BusyBox 环境。镜像包含服务与传输辅助程序。构建上下文使用允许列表，不发送 `.git`、`.tmp`、`.env`、本地数据或任务记录。

Compose 固定 `network_mode: host`，不使用 `ports` 或 `networks`，不默认配置特权、host PID 或 Docker socket。`init: true`，`restart: unless-stopped`，停止宽限 30 秒。环境变量：

| 变量 | 默认值 / 行为 |
| --- | --- |
| `REMOTE_MCP_IMAGE` | `ghcr.io/kkqy/remote-mcp:latest`，可指定版本或 digest |
| `REMOTE_MCP_LISTEN` | `0.0.0.0:8080`，通过 `--listen` 传入 |
| `REMOTE_MCP_TOKEN` | 空，保留匿名模式；非空时使用既有 Token 校验 |
| `REMOTE_MCP_UID` / `REMOTE_MCP_GID` | `10001`，可匹配宿主目录属主 |
| `REMOTE_MCP_DATA_DIR` | `./data`，预先创建并授予运行用户写权限 |

宿主目录绑定至 `/workspace`，禁用自动创建宿主路径；工作目录和 HOME 为 `/workspace`。入口准备 `/workspace/.tmp` 并 exec 服务，TMPDIR/TMP/TEMP 均指向该目录。共享网络不共享宿主进程/文件系统；额外宿主路径须显式挂载。容器内命令使用镜像已有程序，需要其他运行时应派生镜像。

## 4. 验证与错误矩阵

| 条件 | 结果 |
| --- | --- |
| PR、非默认分支 push | 构建验证，无 GHCR 登录或推送 |
| native/cross-build/container 任一失败 | 不发布镜像 |
| 挂载源不存在 | Compose 拒绝启动，不自动建立 root 属主目录 |
| 挂载目录不可写 | 入口创建 .tmp 失败，不能假装启动成功 |
| 非法非空 Token 或监听地址 | 应用启动校验失败 |
| GHCR 首次发布为私有或既有包未授权 | 按实际权限拉取/推送；不能承诺匿名拉取 |

## 5. 正常、基础和失败案例

正常：默认分支检查成功，发布双架构 latest；创建工作目录并匹配 UID/GID 后部署，真实客户端可上传文件、打开终端并正常停止。基础：版本部署指定 `ghcr.io/kkqy/remote-mcp:v1.2.3` 或 digest，更新和回滚均通过显式镜像选择。失败：PR 代码不能登录 GHCR；工作目录无权限时应修正属主或运行 UID，不能默认添加 privileged 规避。

## 6. 必需验证

Actions 使用 actionlint 专用检查；Compose 检查默认和覆盖配置。镜像真实验收覆盖 Token/匿名、工具发现、上传下载校验、默认 shell 的 PTY、进程树回收、正常退出和绑定挂载文件持久性。缓存、临时文件及测试挂载统一使用项目 `.tmp/`。单平台原生结果、其他架构构建结果、远端 GHCR 发布结果分别记录；不能把配置解析当作运行通过。

## 7. 错误与正确方式

错误：host 模式仍写 `ports: 8080:8080`；使用 scratch 导致终端和 ps 不存在；把容器文件路径当宿主路径；在未经过 Windows 验证的独立流程立即发布。

正确：通过 `--listen` 改变 host 监听端口，使用保留 shell/procps 的 Linux 运行镜像，显式挂载需要的宿主目录，在同一工作流经完整 needs 门禁后发布。
