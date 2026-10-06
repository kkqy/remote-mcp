# Windows GUI 图形操作与截图能力

## 目标与已确认决策
让Agent通过MCP观察Windows当前用户图形桌面、执行键鼠和中文输入，并再次截图确认结果。用户于2026-10-07明确取消Linux的全部GUI能力，只保留Windows图形界面操作；此决定覆盖此前首版Wayland/GNOME/KDE/X11要求。Linux继续提供文件、普通进程、PTY终端、TCP转发及P0远程调试工具。

程序错误、提示、日志、CLI帮助及MCP/schema描述使用英文；会话、文档和注释中文，实际用户UTF-8内容保持不变。GUI仍使用现有/mcp、匿名或可选Token、当前用户交互会话，不增加网页、认证或控制端口。Windows/amd64已有当前构建Token/匿名单屏原生验证，Windows/arm64及多屏/混合DPI等缺口分别保留。

## 当前实施范围
| 编号 | 可观察行为 |
| --- | --- |
| GUI-01 | Windows公开gui_status/open/close/screenshot/mouse/key/text七工具，状态查询不申请系统权限。 |
| GUI-02 | Linux不注册任何gui_*工具、不创建图形管理器、不探测桌面或启动GUI辅助进程；Linux CLI不显示或接受gui-*参数。 |
| GUI-03 | 删除Linux X11/Wayland/Portal/KScreen/媒体采集与剪贴板图形后端及专属测试，移除只供它们使用的Go依赖。 |
| GUI-04 | Windows PNG图片块、裁剪和坐标元数据、负桌面坐标、DPI/布局变化保护沿用现有契约。 |
| GUI-05 | Windows鼠标移动/点击/双击/拖拽/横纵滚动，单键/组合键及中文emoji原生Unicode输入；串行和部分失败说明保持。 |
| GUI-06 | Windows当前直接文本路线不读取或修改Clipboard；显式不支持的模式返回具体英文原因。 |
| GUI-07 | 会话和截图记录有界，输入取消不提前释放占用，重复关闭及服务停止清理保持。 |
| GUI-08 | README、规格、验证入口不再承诺或指导Linux图形操作；Linux宿主仍可通过HTTP/部署脚本验证远端Windows。 |
| LOG-01 | MCP失败向Agent及日志返回稳定错误码与具体英文原因，不泄漏文本/图片/凭据，所有旧模块保持。 |

## 验收标准
| 编号 | 验收 |
| --- | --- |
| AC-01 | Linux真实HTTP tools/list无gui_*，调用已移除工具为协议未知工具；全部命令行/P0工具仍可发现并使用。 |
| AC-02 | Linux --help无gui-*，显式传入该类参数稳定英文失败；GUI配置不影响Linux服务启动。Windows参数校验保持。 |
| AC-03 | Go依赖图无Linux图形专属库/后端，四组合×两个入口CGO_ENABLED=0构建通过。 |
| AC-04 | Windows七GUI工具及独立HTTP图片/坐标/英文错误回归通过，Windows键鼠/中文输入代码保持；适当复核当前Windows实际构建。 |
| AC-05 | gofmt、vet、test、race及相关Python离线检查通过；Linux实际旧功能/P0的Token/匿名闭环通过。 |
| AC-06 | 历史Linux研究及运行结果保留为历史，不算当前支持或待交付；真实Windows结果与仅构建平台分别记账。 |

## 范围外与完成边界
Linux及macOS GUI、OCR/可访问性树、网页远程桌面、系统提权/锁屏安全桌面、跨用户选择及持久GUI资源均范围外。不修改用户.opencode/package.json，不替换Windows旧8080主服务、不自动提交/推送。多屏/混合DPI、撤权/断线、实际客户端图片展示及Windows/arm64原生结果仍据实际环境记录，不夸大验证范围。

## 状态
用户明确指示缩减平台范围，直接在现有in_progress任务实施；开发分支codex/gui-native-validation。当前PRD取代旧平台要求，旧规划快照及研究留存供追溯。
