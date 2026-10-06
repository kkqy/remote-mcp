# 文件操作与巡检局部复审

## 实质发现与修复
- `internal/inspection/platform_linux.go`：不可读 IPv6 表原先统一报告 `dependency_missing`，不可读进程目录统一报告 `io_error`；改为按实际系统错误分类。新增权限模式回归，保留可用 IPv4 地址及不完整 PID 可见性。本机两项权限回归实际运行通过，未跳过。
- `internal/fileops/types.go`：按主代理确认的既定接口补充英文 schema，明确 `next_line` 是首个未完整返回行，截断预览不可盲目拼接，超过 64 KiB 的长行无法用本工具完整返回；跳过用 `end_line+1`。同时明确 query 逐行匹配且不跨行。未新增字节续读接口。
- 全范围复审随后发现并修复打开前目标替换窗口、内核版本错误遗漏，详见 [最终复审](check-final.md)。

## 已核对行为与证据
- 中文、CRLF、末尾无换行、空文件、无效 UTF-8、长行后半及跨块命中、目录/搜索预算、完整原文件哈希、错误哈希和并发补丁冲突、发布失败后的目标及临时文件状态均有实际测试。
- 巡检真实当前父子 PID、IPv4/IPv6 监听 PID、Linux 表解析、Windows/macOS fixture、固定命令超量输出与遗留管道、总 deadline、网络各阶段失败、禁用代理/重定向/正文读取以及取消/Close 均已核对。
- 两包 `go vet`、`go test`、`go test -race` 通过；最终代码再次纳入全量检查，见最终复审记录。

## 保留限制
- 按行读取保留有限预览语义；完整超长行字节应使用既有下载工具。
- 非合作外部写入与最终替换之间不提供严格文件系统 CAS；Unix 权限位不代表 Windows 自定义 ACL 完整复制。
- Windows/macOS 原生 PID、端口和文件行为未在对应系统运行；fixture 或交叉编译不能代替原生验收。
