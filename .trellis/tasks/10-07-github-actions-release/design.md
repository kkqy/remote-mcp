# 技术设计
扩展现有 check.yml，避免独立工作流绕过验证。cross-build 在 v* 标签 push 时打包并上传独立发布 artifact；release job 使用 needs: [native, cross-build] 和同一标签条件，下载本轮压缩包并发布。普通 push/PR 和 remote-mcp-binaries artifact 保持原有行为。

打包脚本读取 build.sh 的四组目录，仅包含两个已知程序；校验标签和必需输入，Linux tar 保留可执行位，Windows zip 保留 .exe，计算实际压缩包 SHA256SUMS。标签经环境变量/参数传递，不把事件值插入 shell 源码。临时文件在仓库 .tmp/。

GitHub CLI release create 使用 --verify-tag 和 --generate-notes；按官方说明附带文件时先草稿、再上传、最后公开。先通过固定 GraphQL repository.release(tagName) 单请求检查草稿及公开版本；仅 JSON 无 errors、repository 非空且 release 为 null 才允许创建。命令失败、无效 JSON 或错误结构均停止。已有 Release 不覆盖；中断草稿由用户核对并删除后重跑。不使用 gh release view，其上游并发查询可能把部分错误合并为版本不存在。每个 ref 设置发布并发组且不取消正在发布的 run。

顶层 contents: read，仅 release job 提升 contents: write；使用内置 GITHUB_TOKEN 和托管 Ubuntu runner 的 gh。回滚移除发布 job 和标签打包步骤；无生产 Go 代码变更。
