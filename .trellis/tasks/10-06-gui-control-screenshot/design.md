# Windows GUI与Linux命令行的边界设计

## 平台注册和生命周期
GUI能力的平台条件由internal/gui统一定义，配置和服务接入复用同一条件。Windows维持管理器创建、七工具注册、参数校验及关闭。Linux服务不创建GUI管理器或扫描桌面，不注册gui_*，清理分支正确处理不存在的GUI管理器。共享错误类型可供日志和模拟HTTP测试使用；非Windows平台仅保留必要的不可用构建桩，不保留Linux图形实现。

Linux CLI隐藏并拒绝所有gui-*选项，现有文件/执行/转发/P0配置保持。Windows公开GUI输入和schema兼容，gui_open说明改为Windows当前用户交互会话，不再提Wayland。共享NewWithBackend等测试入口仍可做无桌面的协议验证，不等同产品在Linux支持GUI。

## 删除范围及依赖
移除internal/gui中Linux专属平台选择、X11连接/后端、Wayland Portal、布局监控、剪贴板后端及专属测试；仅供它们使用的godbus/dbus和jezek/xgb从go.mod/go.sum清理。通用管理器、PNG/坐标/键名、Windows系统调用和Windows原生助手保持。公共Python验证入口去掉Linux原生窗口/Portal/Qt剪贴板相关分支，保留远端Windows截图/部署验证所需能力；无用途的独立Qt夹具及专属测试可删除。

## 协议与失败
GUI仍为Windows的七工具，标准PNG图片块和capture_id坐标约束保持。Linux客户端发现中没有GUI工具，调用gui_*由SDK返回未知工具原因；程序自有错误说明英文且日志有具体安全原因。平台范围变化不能降低旧工具的鉴权/限额/UTF-8与资源清理契约。

## 文件责任和历史
实施代理负责Go后端/接入/配置/测试及go.mod/go.sum；另一实施代理负责Python GUI验证脚本/测试。主会话负责README、任务规划、当前规格和已有KDE进程精确清理。各方不覆盖其他人改动，不动用户.opencode；历史research不改成成功或删除。当前Windows-only规格取代Linux相关规范，历史研究不注入实施/检查为活动要求。
