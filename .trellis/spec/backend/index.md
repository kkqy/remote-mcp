# 后端开发规范

本项目是 Go 编写的远程调试 MCP 服务，模块名为 `remote-mcp`。文档和注释使用中文；不存在前端或数据库层。

## 开发前必读

1. 阅读当前任务的需求、技术设计和执行计划。
2. 阅读 [目录结构](directory-structure.md) 与 [MCP 契约](mcp-contracts.md)。
3. 涉及错误或输出时，阅读 [错误处理](error-handling.md) 和 [日志规则](logging-guidelines.md)。
4. 涉及资源生命周期时，阅读 [状态存储](database-guidelines.md)。

## 质量检查

按 [质量规范](quality-guidelines.md) 执行格式、静态检查、测试和平台构建。三平台运行结果与交叉编译结果必须分别记录。

## 规范来源

规范来自 `internal/config`、`internal/server`、`internal/transfer`、`internal/execution` 及两个命令入口的实际实现。公共行为以任务验收和测试为准，不能把初始化模板或计划中的能力写成已验证事实。
