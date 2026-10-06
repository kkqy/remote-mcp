# 自动发布独立复审

## 范围

按 check.jsonl、PRD、设计、实施计划、后端索引质量检查与发布规范，复审工作流、两个发布 Python 文件、两份 README 及发布/目录/索引规范。最终结论：全范围复审通过，源码冻结，无确认未修缺陷。生产 Go 代码未变化，不重复无关全包检查。用户 `.opencode/package.json` 未修改，未创建标签、草稿或正式 Release，也未提交/推送。

## 已修发现

- 文件：`scripts/release.py`、`scripts/test_release.py` 与对应发布设计/规范。
- 问题：初版把 `gh release view` 退出 1、唯一英文 `release not found` 当作已明确不存在。当前官方 [FetchRelease 源码](https://github.com/cli/cli/blob/trunk/pkg/cmd/release/shared/fetch.go) 合并正式版本和草稿查询；某一路的不存在结果可能掩盖另一路网络/权限错误，不能据此证明底层查询均健康。
- 修复：复审反馈后由主会话确认方案、实施代理替换为固定 GraphQL `repository.release(tagName)` 单请求。仅命令成功、有效 JSON、无 errors、repository 为对象且 release 明确为 null 才创建；草稿/公开版存在、命令失败、超时和异常响应均停止。最终独立复审和定向回归通过，设计/规范同步。
- 参数边界：owner/name/tag 均使用 `-f` 原始字符串，数字及 true/null 形式的合法仓库名不被 `-F` 转成其他类型；新增回归覆盖该建议。[官方 gh api 手册](https://cli.github.com/manual/gh_api) 说明 raw-field 与 typed-field 的区别。

## 未修发现

无确认未修缺陷。相同 ref 的 CI 发布串行，但查询与创建不构成针对并行手工发布的远端原子锁；此边界已在发布规范与实施记录明确，不依赖草稿标签唯一性的假设。

## 已核对行为

- 普通分支 push、PR、非 v 标签不发布；同一工作流发布 job 的 needs 精确依赖整个 Linux/Windows native matrix 与 cross-build，保留默认成功条件，没有 always 绕过。
- 顶层 contents: read，仅 release job contents: write。每 ref 并发组不取消正在运行的发布。标签、仓库通过 env 和引用参数传递，run 内没有事件表达式插值；Python 调用 gh 使用参数数组、非 shell、有限超时。
- 交叉构建后打包，上传 remote-mcp-release，下载同一 run 的具名 artifact 到相同 .tmp/release 路径。include-hidden-files 明确启用，缺文件失败；原二进制和 P0 报告 artifact 保留。
- 四个压缩包各恰含两个已知程序。Linux tar 封装原可执行权限，避免依赖 artifact 外层文件模式；Windows ZIP 内程序后缀与构建目录一致。SHA256SUMS 对应四个实际压缩包，发布前再次校验。
- 非法标签、缺失/空输入、Linux 缺执行位、输出目录已存在、附件额外/缺失/篡改均失败；打包先完成五文件再发布目录。新临时文件/缓存均在项目 .tmp。
- 单请求查询已存在正式版本或草稿时直接拒绝，不删除或覆盖。显式 create --draft、五附件上传成功之后才 edit --draft=false，上传失败不调用公开；阶段错误、超时及公开结果未知均有英文原因和恢复说明。[官方 create 手册](https://cli.github.com/manual/gh_release_create) 与 [create 实现](https://github.com/cli/cli/blob/trunk/pkg/cmd/release/create/create.go) 支持此显式草稿行为。
- 两份 README 与当前 CLI、标签规则、附件、权限、重复版本/失败草稿恢复及未执行真实发布边界一致；新增先合入并推送 main、再从该提交打标签的说明，避免通过新增 PAT 绕过 Workflows 权限边界。索引与目录新增入口准确。

## 独立验证

- `python3 scripts/test_release.py`：最终 Linux 本机 12 项通过，0.027 秒；未连接 GitHub。查询新回归核对成功命令顺序 graphql→create→edit、全部查询错误无 create、创建/上传错误无 edit、字符串参数及既有草稿/公开版拒绝。Windows 两项 Unix 权限回归按平台跳过，未在本机冒充 Windows 已执行。
- 两个 Python 文件 AST 解析通过。没有新增 Python 静态类型检查器，AST 不等于静态类型检查。
- `.tmp/release-tools/actionlint -shellcheck= -pyflakes= .github/workflows/check.yml`：通过。Actions 语法/表达式检查已运行，shellcheck、pyflakes 明确关闭，未声称执行。
- 独立 PyYAML BaseLoader 断言：push/PR、两原生平台、needs/tag 条件、窄权限、并发、artifact path/hidden-file/缺文件失败及 run 无事件插值，全部通过；未以 YAML 解析代替 actionlint。
- 独立读取已真实构建的四包（不导入发布模块）：核对八程序 SHA 与原 dist、确切文件集合、大小、Linux 原权限与用户执行位、四行 SHA256SUMS，全部通过。证据 `.tmp/release-tools/check-package-verification.json`。实施代理的四组合 build 通过证据单独保留于 implementation.md，不把独立读包写成再次编译。
- `git diff --check`：通过。

## 验收边界

离线测试、最终工作流配置检查和真实包核验通过，上游查询问题已修复。新远端 Actions、Windows 新准备步骤及实际 GitHub 发布链路未执行，不能写成正式版本已发布。既有 Windows arm64 原生、多屏/混合 DPI 与 GUI 验收缺口不因构建或发布配置消失。

最终冻结 SHA-256：

- `scripts/release.py`：`75d7da19727cb1a4ada5f3df6f44b242351941c609b7e84d7d1988da1a418379`。
- `scripts/test_release.py`：`eb8d0f5e0fa01d377363c9bc016aff357fb94aad0b343e4a357aab150951a904`。
- `.github/workflows/check.yml`：`a48630a8ce2fe35b41145fc2836620cb1d74a75a60e50e21f74795df1085ef81`。
