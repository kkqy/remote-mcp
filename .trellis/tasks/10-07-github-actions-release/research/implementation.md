# 自动发布实施记录

## 实施范围

本批次只修改 `.github/workflows/check.yml`、两份 README，并新增 `scripts/release.py` 与 `scripts/test_release.py`。生产 Go 代码、依赖和用户 `.opencode/package.json` 未修改；未提交或推送。当前代码已冻结，等待独立复审。

`check.yml` 保留普通 push/PR、Linux/Windows 原生检查、P0 报告 artifact 和四组合原始二进制 artifact。仅 `push` 且 `refs/tags/v*` 执行打包并上传本轮 `remote-mcp-release` artifact；发布 job 使用 `needs: [native, cross-build]`，遵循默认成功依赖条件。顶层权限仍为 `contents: read`，仅发布 job 提升到 `contents: write`；同 ref 发布并发组使用 `cancel-in-progress: false`。事件标签及仓库名经 env 和引用参数传入，不插入 run 的 shell 源码。

原生和交叉构建 job 在 Go setup 前准备项目 `.tmp` 的临时目录、Go build/module cache；发布 job 同样设置 `.tmp` 临时目录。发布 artifact 只指向五附件目录，显式 `include-hidden-files: true` 防止 `.tmp` 被默认隐藏过滤，不上传其他缓存或状态。

## 脚本契约

- `package --tag TAG --dist-dir dist --output-dir .tmp/release`：预检四组目录的八个程序；Linux 输入必须具有用户执行位。标签限 `v` 加安全文件名字符，长至多 64；输出必须是项目 `.tmp` 内不存在的目录。四包与四行 `SHA256SUMS` 先在 `.tmp` 临时 staging 全部完成，再重命名发布目录；失败不留下部分附件目录。
- Linux tar.gz 仅包含两个程序，保留输入文件权限；Windows zip 仅包含两个 `.exe`。不打包 README、其他构建文件或缓存。哈希使用实际压缩文件的 SHA-256，有界读取。
- `publish --tag TAG --asset-dir .tmp/release --repo OWNER/REPO`：拒绝缺少、额外、空或链接附件，重新核对四项哈希；使用参数数组执行 gh，不使用 shell。
- 单次 `gh api graphql` 查询 `repository.release(tagName)`，固定 query 与 owner/name/tag 均通过 `-f` 字符串参数数组传入。只有无 GraphQL 错误、`data.repository` 为对象且 `release` 字段明确为 null 才允许创建；草稿与正式版本均拒绝。查询失败、超时、坏 JSON、缺字段或无权限仓库均停止。不会删除版本或使用 `--clobber`。
- 使用 `gh release create --draft --verify-tag --generate-notes` 附上五文件；其返回成功后才 `gh release edit --draft=false`。创建/上传失败不调用 edit；失败有阶段与 gh 退出码，超时明确说明需核对远端状态，尤其最终公开请求可能已生效。

README 增加四包下载、校验、标签示例、验证依赖、窄权限和失败恢复。失败草稿需维护者确认后手动删除、保留标签再重跑；已公开版本不覆盖，后续修正使用新版本。当前未指定真实发布标签，示例和本地验证标签均没有创建 Git 标签。

## 官方依据与边界

- [GitHub CLI create 手册](https://cli.github.com/manual/gh_release_create)：`--draft` 保留草稿，`--verify-tag` 检查远端标签，附文件流程分为创建、上传和公开。本实现显式分开公开命令。
- [官方 fetch.go](https://github.com/cli/cli/blob/trunk/pkg/cmd/release/shared/fetch.go) 的 `FetchRelease` 合并正式版本和草稿两个并发查询；两者失败时可能优先返回 `ErrReleaseNotFound`，掩盖另一查询的网络或权限错误。因此初版 `gh release view` 前置查询不足以证明全部查询正常且版本不存在，最终实现已移除此查询。
- 最终采用 `FetchRelease` 内部草稿查找所使用的单次 `repository.release(tagName)`，且不按 isDraft 过滤，因此同时识别公开版和草稿，避免双查询合并错误。未采用仅直接创建草稿来保护既有版本：CLI `--draft` 绕过公开版存在性预检，不能凭创建 API 假定同标签草稿唯一。查询与创建并非远端原子锁；CI 同 ref 串行，但仍存在维护者同时手工发布的窗口。
- [GitHub 创建 API](https://docs.github.com/en/rest/releases/releases#create-a-release) 说明标签目标相对默认分支的工作流变更可能要求 Workflows 权限；本流程不增加 PAT。README 明确先合入并推送 main，再从该提交打标签。
- [GitHub Actions needs](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#jobsjob_idneeds) 的默认依赖成功条件用于门控；没有 `always()` 绕过发布依赖。

本机没有 gh，因此发布阶段通过 mocked subprocess 离线校验，未调用真实 GitHub 发布 API，也没有创建标签、草稿或正式 Release。Windows runner 的新 PowerShell 准备步骤及发布权限尚未在新的远端 Actions 运行中执行；配置检查不能替代远端结果。

## 已执行命令与结果

所有新临时文件均在项目 `.tmp`；检查与构建设置：

```bash
export TMPDIR="$PWD/.tmp/temp" TMP="$PWD/.tmp/temp" TEMP="$PWD/.tmp/temp"
export GOTMPDIR="$PWD/.tmp/go-tmp" GOCACHE="$PWD/.tmp/go-cache"
export GOMODCACHE="$PWD/.tmp/go-mod-cache"
export GOPROXY=file:///home/user/go/pkg/mod/cache/download GOSUMDB=off
export PYTHONDONTWRITEBYTECODE=1
```

1. `python3 scripts/test_release.py` 与 `python3 -m unittest discover -s scripts -p 'test_release.py'`：12 项通过。覆盖标签/目录、缺失/空程序、Linux 执行位、压缩写失败、独立内容/权限/哈希、草稿与公开版存在、查询失败/超时/坏 JSON/GraphQL 错误/非法结构、数字和布尔形式仓库名的字符串参数、创建认证错误、上传及公开失败、上传超时、篡改/缺少/额外附件、CLI 失败无成功输出。最终成功命令序列为 graphql→create→edit；任何查询失败无 create，创建失败无 edit；gh stderr 直接继承，保留具体原因。Windows 只跳过两个依赖 Unix 权限的打包测试；其他回归照常执行。本轮在 Linux 运行，不能宣称 Windows 已执行过。
2. AST 解析两个 Python 文件、`git diff --check`：通过。
3. 经批准只读下载官方 [actionlint v1.7.12](https://github.com/rhysd/actionlint/releases/tag/v1.7.12) 到 `.tmp/release-tools`，按同版官方校验文件核对压缩包 SHA-256 `8aca8db96f1b94770f1b0d72b6dddcb1ebb8123cb3712530b08cc387b349a3d8`。`.tmp/release-tools/actionlint -shellcheck= -pyflakes= .github/workflows/check.yml`：退出 0。检查包含 Actions 语法、表达式和 job/step 结构；本机没有 shellcheck/pyflakes，未声称执行这两项附加 lint。
4. 另用 PyYAML BaseLoader 核对 push/PR、Linux/Windows matrix、精确 needs/tag if、顶层与发布权限、同 ref 不取消以及 run 内不含事件插值：通过。这是补充断言，未用 YAML parse 替代 actionlint。
5. `bash scripts/build.sh`：四组合、两个入口共八个实际 CGO_ENABLED=0 构建通过。
6. `python3 scripts/release.py package --tag v0.0.0-validation --dist-dir dist --output-dir .tmp/release-validation-v0.0.0`：通过。
7. 独立验证程序没有导入打包模块，打开四个压缩包、断言两个确切程序名并逐块与 dist 原文件比较，核对 Linux tar 权限完全一致且三执行位均存在；独立计算四项 SHA-256、核对恰五附件：通过。有限结果在 `.tmp/release-tools/package-verification.json`。

本批次没有生产 Go 变化，未重复无关 Go 全量测试；原生验证步骤继续由 workflow 执行。

## 本地验证附件哈希

| 文件 | SHA-256 |
| --- | --- |
| `remote-mcp-v0.0.0-validation-linux-amd64.tar.gz` | `95b1eb540b469bca75c59ef6cbf1e40a8fbc8b357f8927e88dc76c1540840ce4` |
| `remote-mcp-v0.0.0-validation-linux-arm64.tar.gz` | `9da5005840ca02f45cc54db6fc1e95af4f9f5600f0de6f69c150c594f5d545df` |
| `remote-mcp-v0.0.0-validation-windows-amd64.zip` | `3565d9bd3ec912e9e53af05b1f0de2b71327259d3188b12ca9222257b295b7e5` |
| `remote-mcp-v0.0.0-validation-windows-arm64.zip` | `6e90a562b6e2d7e0bbba0b2dc7e5c0fce32e86a8b7c49a926562a13b5658f53b` |

## 冻结文件哈希

- `scripts/release.py`：`75d7da19727cb1a4ada5f3df6f44b242351941c609b7e84d7d1988da1a418379`
- `scripts/test_release.py`：`eb8d0f5e0fa01d377363c9bc016aff357fb94aad0b343e4a357aab150951a904`
- `.github/workflows/check.yml`：`a48630a8ce2fe35b41145fc2836620cb1d74a75a60e50e21f74795df1085ef81`
- `README.md`：`57281c81b106af511862238147d054918268ef9285f869d60470e9bb72bac860`
- `README.zh-CN.md`：`025e58c1cc99da4ba216c16db1bec013b2df892d289bd781a996bc871282b593`

最终单查询修正后重新执行两种离线测试入口：12 项均通过（0.023/0.020 秒）；两个 Python AST、`git diff --check` 与专用 actionlint 检查再次通过。打包与构建代码未改变，四组合及压缩包保留前述真实核验结果，未重复构建。

四组合可构建、包内容正确、离线失败门槛通过，均不能替代真实正式发布，也不消除 Windows arm64、多屏/混合 DPI、GUI 原生验收等既有边界。
