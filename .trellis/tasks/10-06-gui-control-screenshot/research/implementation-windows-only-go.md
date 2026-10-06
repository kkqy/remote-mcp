# Windows-only Go 实施记录

日期：2026-10-07。用户明确取消 Linux 全部 GUI，本记录对应这次收敛后的冻结代码；历史 KDE/Wayland 验收不再属于产品交付要求。

## 冻结范围

- `internal/gui/platform_support_windows.go` 和 `platform_other.go` 定义唯一编译目标条件 `gui.Supported`。Windows 为 true，非 Windows 为 false；条件本身不探测桌面。非 Windows 工厂仅保留返回 `unsupported` 的构建桩，共享管理器及 `NewWithBackend` 仍可用于显式模拟测试。
- 删除 `platform_linux.go`、`x11_linux.go`、`x11_connection_linux.go`、`wayland_linux.go`、`clipboard_portal_linux.go`、`layout_portal_linux.go`、`linux_test.go`、`portal_protocol_linux_test.go`，以及本轮已有的未跟踪 Linux 专属 `layout_portal_linux_test.go`。产品不再包含 X11、Wayland、Portal、KScreen 图形实现。
- `internal/gui/tools.go` 的 `gui_open` 英文说明改为 Windows 当前用户交互桌面，不再提及 Wayland。Windows 原生后端及其原有测试保持未修改。
- `internal/config/config.go` 用同一平台条件控制五个 `gui-*` 参数及 GUI 配置校验。Linux 帮助不显示这些参数，传入时通过既有安全英文参数错误拒绝；未使用的程序构造 GUI 配置不影响 Linux 启动。Windows 参数与限额校验保留。
- `internal/server/server.go` 仅在 Windows 创建 GUI manager 和注册七工具；Linux 的 `app.gui` 为 nil。初始化失败回收和正常/重复关闭均可安全处理不存在的 GUI manager。
- config/server 相关测试覆盖平台帮助及拒绝、Linux 33/Windows 40 个工具、七工具有无、Linux 七个 GUI 调用的 SDK 未注册协议错误和受控日志、无效未使用 GUI 配置、匿名/Token、nil 清理，以及显式注入模拟 GUI 的独立 HTTP 图片/元数据/输入/关闭契约。
- `go.mod/go.sum` 移除仅供已删除后端使用的 `github.com/godbus/dbus/v5` 和 `github.com/jezek/xgb`；`go mod tidy` 完成，无其他依赖变化。
- 主会话追加授权后，`scripts/p0-smoke.py` 用远端 `environment_inspect.os` 核对精确工具数量与 GUI 集合：Linux 33 且无 `gui_*`，Windows 40 且精确七个 GUI；重复工具名与未知/未确认平台拒绝。`scripts/test_p0_smoke.py` 增加对应离线边界，未按 Python 宿主 OS 推断服务平台。

本子代理未修改 README、当前规格、其他 GUI Python 脚本、Windows 原生助手、`.opencode`，未提交或推送。没有执行 GUI、Clipboard、Portal 或远端操作。

## 验证环境与命令

本次所有新临时文件和构建缓存均在工作目录 `.tmp`，未访问系统临时目录。执行 Go 命令前设置：

```bash
export TMPDIR="$PWD/.tmp/temp" TMP="$PWD/.tmp/temp" TEMP="$PWD/.tmp/temp"
export GOTMPDIR="$PWD/.tmp/go-tmp" GOCACHE="$PWD/.tmp/go-cache"
export GOMODCACHE="$PWD/.tmp/go-mod-cache"
export GOPROXY=file:///home/user/go/pkg/mod/cache/download GOSUMDB=off
```

依赖从已存在的本机模块下载缓存读取到项目模块缓存；本轮不通过网络安装依赖。HTTP/TCP/PTY 测试在获得本机执行审批后运行，沙箱内最初的回环监听限制不计为通过。

| 检查 | 最终结果 |
| --- | --- |
| 修改 Go 文件 `gofmt` | 通过 |
| `go mod tidy` | 通过，仅移除两个 Linux 图形依赖 |
| `go test ./...` | 通过，包含 `scripts/native-smoke` 包 |
| `go vet ./...` | 通过 |
| `go test -race ./...` | 通过 |
| `bash scripts/build.sh` | 通过，四个 OS/架构组合、两个命令入口共八个 CGO_ENABLED=0 产物 |
| Windows amd64/arm64 的 gui/config/server 六个测试程序 `go test -c` | 通过；只编译，没有在 Windows 执行 |
| Windows amd64/arm64 的 gui/config/server `go vet` | 通过 |
| `python3 scripts/test_p0_smoke.py` | 9 项通过，0.028 秒；未启动服务 |
| 两个 P0 Python 文件 AST 解析、`git diff --check` | 通过 |

Windows 定向检查使用下列循环，输出位于项目临时目录：

```bash
for target_arch in amd64 arm64; do
  for package in gui config server; do
    CGO_ENABLED=0 GOOS=windows GOARCH="$target_arch" go test -c \
      -o ".tmp/windows-only-tests/${package}-${target_arch}.test.exe" "./internal/$package"
  done
  CGO_ENABLED=0 GOOS=windows GOARCH="$target_arch" \
    go vet ./internal/gui ./internal/config ./internal/server
done
```

Python 离线检查同样显式设置项目 `TMPDIR/TMP/TEMP`，并设置 `PYTHONDONTWRITEBYTECODE=1`。

## 实际 Linux CLI 与依赖图

实际产物 `dist/linux-amd64/remote-mcp --help` 正常完成，输出不包含 `-gui-`；传入 `--gui-idle 1m` 非零退出，最终英文说明为 `Startup failed: Invalid command-line arguments; use --help for usage`。两份输出保存在 `.tmp/windows-only-tests/linux-help.txt` 与 `linux-rejected-gui.txt`。

`go list -deps ./cmd/remote-mcp` 的实际 Linux 依赖图不包含 godbus 或 xgb，有限依赖列表保存在 `.tmp/windows-only-tests/linux-deps.txt`。共享 `gui.Error` 与模拟测试仍保留，不代表 Linux 产品创建或公开 GUI 功能。

主会话后续报告：Linux 实际二进制的旧功能/P0 × Token/匿名四轮全部通过，服务停止为 `graceful`；Windows 原生助手两架构已复建通过。上述实跑及助手复建由主会话执行，本子代理没有重复运行或以交叉构建替代 Windows 原生验收。全范围 reviewer 正在进行。

## 冻结产物 SHA-256

| 产物 | SHA-256 |
| --- | --- |
| `dist/linux-amd64/remote-mcp` | `ef0365014e59e7c76b436ff6987b13c87429b84d8e3f5519879831d5351ccf13` |
| `dist/linux-amd64/remote-mcp-transfer` | `bfc9a6ba19aea492a4fbbd90fce149fc528bc119b83f09e7721e8442f9e08d1e` |
| `dist/linux-arm64/remote-mcp` | `cc467f97164a34018f0644bd498785a6895629134e1d135cbbee17c62bb819ee` |
| `dist/linux-arm64/remote-mcp-transfer` | `c51d875dc87107f744cd1d151b212422df939137fe34c19b0cd27a91ede5c3aa` |
| `dist/windows-amd64/remote-mcp.exe` | `4d52b0a31ab275fc4579294fba1a8e931b1f422c3a17acc9515e845aada5901d` |
| `dist/windows-amd64/remote-mcp-transfer.exe` | `57d09df2cbec8c0d39228b60e9663076ec2a619cde9a20983290f109c39d253d` |
| `dist/windows-arm64/remote-mcp.exe` | `490ae42735dd166676bceab39cee031c4b46321bd42e5b2af497d66cce7620d6` |
| `dist/windows-arm64/remote-mcp-transfer.exe` | `a8613c730e8e24c42b57e6b5d4c3950eaf70606ef6a0bbb64233d2366c1e6433` |

补充冻结脚本：`scripts/p0-smoke.py` 为 `740f6332068e0aaaaf65ac85d9c1222d9d771735dbb28f9daf4de1acef8cd3f9`，`scripts/test_p0_smoke.py` 为 `c155c14b4d9876efbb49093c4fa88e36d7a4175216599507c84ee01b087358f9`。

## 保留的验收边界

四组合构建和 Windows 测试编译不等于 Windows 原生运行。本轮 Go 收敛不改变已有 Windows amd64 单屏原生结果；Windows arm64、多屏/混合 DPI、撤权及不同图片客户端等既有未验证项仍按当前任务记录。Linux GUI 已取消，不再列为待验收能力。
