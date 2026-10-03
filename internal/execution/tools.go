package execution

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func register[I, O any](s *mcp.Server, name, description string, fn func(context.Context, I) (O, error)) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: description}, func(ctx context.Context, _ *mcp.CallToolRequest, in I) (*mcp.CallToolResult, O, error) {
		out, err := fn(ctx, in)
		return nil, out, err
	})
}
func (m *Manager) Register(s *mcp.Server) {
	register(s, "process_start", "执行程序；显式 shell 才解释管道。返回资源ID，HTTP断线不停止进程。", m.Start)
	register(s, "process_read", "按 stdout/stderr 原始字节游标读取输出，Base64 保留非UTF-8字节。", func(_ context.Context, in ReadInput) (ReadResult, error) { return m.Read(in) })
	register(s, "process_status", "查询进程运行状态和退出码。", func(_ context.Context, in IDInput) (Status, error) { return m.Status(in) })
	register(s, "process_stop", "停止进程及其子进程树，重复调用安全。", func(_ context.Context, in IDInput) (Status, error) { return m.Stop(in) })
	register(s, "terminal_open", "创建真实交互终端；会话跨MCP连接保持，输出包含VT控制序列。", func(_ context.Context, in TerminalInput) (Status, error) { return m.OpenTerminal(in) })
	register(s, "terminal_write", "输入Base64原始字节，Ctrl+C使用Aw==；失败不应盲目重试输入。", m.Write)
	register(s, "terminal_read", "读取合并终端流，ANSI/VT和原始字节保持不变。", func(_ context.Context, in ReadInput) (ReadResult, error) { return m.Read(in) })
	register(s, "terminal_resize", "调整终端列数和行数。", func(_ context.Context, in ResizeInput) (Status, error) { return m.Resize(in) })
	register(s, "terminal_status", "查询终端状态并刷新空闲计时。", func(_ context.Context, in IDInput) (Status, error) { return m.Status(in) })
	register(s, "terminal_close", "关闭终端并清理关联进程树。", func(_ context.Context, in IDInput) (Status, error) { return m.Stop(in) })
}
