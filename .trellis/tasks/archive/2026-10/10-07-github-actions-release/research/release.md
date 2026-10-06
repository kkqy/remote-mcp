# 官方发布依据
核对日期：2026-10-07。
- [工作流触发](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow)：未限制的 push 包含分支与标签；发布 job 可按事件和 ref 条件过滤。
- [release create](https://cli.github.com/manual/gh_release_create)：--verify-tag 拒绝不存在的远端标签；附带文件时先草稿，上传后公开。已有版本拒绝自动覆盖。
- [release upload](https://cli.github.com/manual/gh_release_upload)：--clobber 先删除旧附件，上传失败可能丢失旧文件，不使用该模式。
- [download-artifact](https://github.com/actions/download-artifact)：指定名称下载本轮 artifact；压缩包承载 Linux 权限，不能依赖原始二进制 artifact 恢复权限。
本机没有 gh，离线验证不代表实际 Release 已发布。

## 复审发现与修正
官方 [gh FetchRelease 源码](https://raw.githubusercontent.com/cli/cli/trunk/pkg/cmd/release/shared/fetch.go) 同时查公开版本与草稿，错误合并可能将一个不存在和另一个网络错误表现为 release not found。不可将该输出解释成两条请求均健康。最终改为固定 GraphQL repository.release(tagName) 单请求，成功且明确不存在才创建，错误及无效响应均停止。草稿 create 不保证标签唯一性，不能依赖假设的重复标签 422 来保护既有版本。查询先拒绝既有草稿/正式版本，创建或附件上传失败均不公开，保留具体 CLI 原因。
