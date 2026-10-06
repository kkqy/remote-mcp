# Windows-only GUI执行计划

## 已授权变化边界
行为差距：当前Linux仍公开并实现GUI，用户明确要求移除。实际边界位于gui平台实现、server注册和config CLI；只在工具处理器里拒绝调用不足以移除能力。预计修改这些接入和对应测试、删除Linux后端/专属依赖与验证分支，并同步README和规格。Windows后端及已有CLI/P0语义保持，不进行公共工具重命名或Windows部署升级。

## 实施与责任
1. 主会话停止当前KDE测试并保存历史结果；当前无继续Linux GUI验收要求。
2. trellis-implement负责Go：统一Windows支持条件，Linux不创建/注册GUI，不暴露GUI CLI，正确nil清理，删除Linux实现/测试/专属依赖，适配协议发现和日志测试。仅其修改go.mod/go.sum。
3. 第二trellis-implement负责scripts/gui-smoke.py及相关GUI Python夹具/测试清理，保留远端Windows通用截图验证和已完成Windows原生部署助手。必要的原生助手变更须与Go负责人协调。
4. 主会话同步README及当前backend规格，将历史Linux研究明确为不再活动的支持要求。
5. trellis-check全范围审阅所有现存批次，加载各变更包spec index与质量规范，直接修机械问题。

## 质量门槛
显式TMPDIR/TMP/TEMP及Go cache置工作目录.tmp；不访问系统临时目录。gofmt、go test ./...、go test -race ./...、go vet ./...，四平台架构×两个入口无CGO构建，相关Python离线回归。实际Linux二进制独立HTTP检查无gui_*，--help/拒绝GUI参数，旧功能/P0的Token/匿名四轮。Windows构建+模拟协议和先前实际Token/匿名原生结果分别记账，平台边界影响注册时可复核新的Windows服务。

## 收尾
记录当前删除范围及真实验证结果，spec同步后按Phase3.4处理当前批次提交，未授权提交前不自动提交/推送。原Windows-arm64、多屏、混合DPI、撤权与图片客户端缺口保留为Windows验收边界，Linux GUI不再属于待交付。
