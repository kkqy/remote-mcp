# 实施计划

## 顺序与所有权
- [x] 读取 AGENTS、工作流、后端规范；核对用户脏文件和旧任务。
- [x] 创建单个三模块集成任务，写 PRD/设计/计划；以用户现有推进授权进入实现，无需重复确认。
- [x] 巡检研究写 research/inspection.md；细化平台能力与预算。
- [x] trellis-implement A 仅写 internal/inspection，含平台适配和测试；局部 test/race/vet 与六平台包编译通过。
- [x] trellis-implement B 仅写 internal/execution 与 internal/logstream，含等待/轮转/生命周期测试；局部 test/race/vet 通过。
- [x] trellis-implement C 仅写 internal/fileops，含有界读搜和哈希补丁测试；局部 test/race/vet 已通过。
- [x] 串行集成代理写 internal/server 接入、日志诊断及独立协议测试，更新 README 两份（中文文档规则）；局部 test/race/vet 和独立二进制 P0 Token/匿名冒烟通过。不并行修改 go.mod、server、README。
- [x] trellis-check 全范围自修并复核，不派生 implement/check；全量 test/vet/race 通过。
- [x] 更新实际契约 spec，记录已验证与原生缺口；最终复审继续核对。
- [x] 呈现具体提交计划，用户回复“是”一次确认，两笔工作提交按计划执行；绝不推送。

## 验证
`gofmt`；`go vet ./...`；`go test ./...`；`go test -race ./...`；`bash scripts/build.sh`；`python3 scripts/smoke.py --bin-dir dist/linux-amd64`。新增各模块测试覆盖 PRD AC1–AC7，并使用独立 JSON-RPC 请求完成工具发现和真实调用。

## 风险和回滚点
- 等待订阅锁序、退出时末尾输出、取消与 Close 竞态需先做局部测试再集成。
- 日志轮转不可把新文件 offset 当旧文件 offset，明确 generation；无法观测两个采样间瞬间截断又恢复的情况需文档声明。
- 外部非合作修改无法严格 CAS；测试可观察冲突，不能以复核冒充不可竞争提交。
- 非普通文件、符号链接、扫描上限、HTTP 重定向及大响应禁止绕过预算。
- `.opencode/package.json` 不归本任务；GUI 任务保持 in_progress。

## 最终执行状态
- [x] 六组合两个入口无 CGO 构建通过。
- [x] dist 最终产物旧功能/P0 × Token/匿名四轮冒烟通过，Python 31 项回归通过。
- [x] 检查用户文件与旧 GUI task 哈希未变，更新最终验证记录和具体提交计划。
- [x] 用户一次确认 commit-plan.md，执行两笔工作提交；实现提交 0337ba2，文档按第二笔计划提交，不推送。
- [ ] Windows/macOS 原生验收；任务暂不归档，GUI 原任务亦保持进行中。
