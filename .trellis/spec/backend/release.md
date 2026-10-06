# GitHub Actions 编译发布契约

## 1. 范围与触发

`check.yml` 保留分支 push 与 PR 验证。正式发布只允许 push 到 `refs/tags/v*`，并依赖 `native` 的 Linux/Windows 验证和 `cross-build` 全部成功。交叉构建仅代表目标可构建，不能当作 arm64 原生运行或 Windows GUI 验收。

## 2. 命令签名

- `python3 scripts/release.py package --tag TAG --dist-dir dist --output-dir .tmp/release`
- `python3 scripts/release.py publish --tag TAG --asset-dir .tmp/release --repo OWNER/REPO`
- 构建仍使用 `bash scripts/build.sh`，不新增生产 Go 入口或依赖。

## 3. 输入、产物与环境契约

打包读取 Linux/Windows × amd64/arm64 四组目录，每组仅含 `remote-mcp`、`remote-mcp-transfer`，Windows 后缀为 `.exe`。Linux 使用 `.tar.gz` 且保留可执行位，Windows 使用 `.zip`；包名包含标签、系统及架构，另有四行 `SHA256SUMS` 对应实际压缩文件。打包输出目录必须为新的目录，避免混入旧附件。

标签满足 `v[A-Za-z0-9][A-Za-z0-9._-]{0,62}`，总长不超过 64 个字符。事件值通过环境变量传递给脚本，禁止插入 shell 代码。临时文件、测试夹具和 Go 缓存统一在仓库 `.tmp/`。打包与校验分别读取、写入明确的五文件集合，发布前再次核对 SHA256SUMS 和附件完整性。

顶层 `contents: read`，仅发布 job 拥有 `contents: write`。发布使用 `GH_TOKEN` 接收内置 `GITHUB_TOKEN`，不打印凭据；从本轮具名 artifact 下载附件。每个 ref 的发布并发组不取消正在进行的发布。

GraphQL 的 query 固定在脚本内，owner/name/tag 通过 `gh api graphql -f` 传递为字符串，不能用 `-F` 将数字或 true/null 仓库名自动转换成其他类型。常规发布先把工作流及版本代码合入并推送默认分支 main，再从该提交推送标签；不要通过新增个人令牌绕过工作流权限边界。

## 4. 验证与错误行为

| 条件 | 行为 |
| --- | --- |
| 非标签 push、PR、非 v 标签 | 执行现有验证，跳过发布 |
| 原生检查或构建失败 | 发布 job 不运行 |
| 非法标签、缺失程序、输出目录已存在 | 非零退出及具体英文原因 |
| 既有 Release，包括失败草稿 | 非零退出，不覆盖或删除 |
| 单请求查询失败、无效 JSON、GraphQL errors 或错误结构 | 具体英文原因并停止，不创建草稿 |
| 创建请求出现认证、网络等错误 | 具体 CLI 原因及非零退出，不执行公开步骤；超时需核对远端状态 |
| 创建草稿或附件上传失败 | 不执行公开步骤；保留明确英文原因 |
| 上传全部完成 | 执行 `gh release edit --draft=false` 公开正式版本 |

失败草稿由用户核对后手动删除再重跑，不默认使用 `--clobber`；已发布版本应使用新标签。配置存在、离线 stub 成功均不代表真实 Release 已发布。

## 5. 正常与边界案例

正常：已推送 v 版本标签，全套检查成功，单请求查询明确不存在，四个包和校验文件上传后公开。边界：普通 main 提交仍上传原二进制 artifact，但不发布正式版本。失败：查询返回已有草稿或正式版本，脚本停止并保留旧版本。

## 6. 必需检查与断言

运行打包/发布离线测试；独立读取真实四组合构建后的压缩包，逐项核对程序原字节、Linux 执行位及实际 SHA-256。缺失输入、非法标签和既有输出必须失败。使用 gh stub 验证已有草稿/正式版本、查询失败或错误 JSON、认证失败、草稿创建/附件上传失败时均无公开调用，成功顺序为单请求查询、草稿创建、公开。完整 Actions 语法检查需专用工具，不能仅以普通 YAML 解析代替。

## 7. 错误与正确方式

错误：独立发布工作流立即发布，未等 Windows 测试；把 `gh release view` 的聚合 `release not found` 当作两条底层查询均健康；上传原二进制 artifact 后依赖其执行权限；直接替换公开版本附件。

正确：发布在同一工作流通过 needs 等待原生及构建验证；使用固定 GraphQL `repository.release(tagName)` 单请求查询草稿及公开版本。仅无 errors、repository 非空且 release 为 null 才允许创建，异常响应一律停止；打包保存 Linux 执行权限；全量附件上传成功后再公开，既有版本保留。相同 ref 的发布串行；查询与创建仍不提供对并行手工发布的原子锁，不依赖草稿标签唯一性的假设。
