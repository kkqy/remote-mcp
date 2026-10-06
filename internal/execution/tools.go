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
	register(s, "process_start", "Run a program; pipes require an explicit shell. Return a resource ID; HTTP disconnection does not stop the process.", m.Start)
	register(s, "process_read", "Read stdout or stderr using raw byte cursors; optionally wait up to 30000 milliseconds for output or exit. Cancellation preserves the read result and does not stop the process. Base64 preserves non-UTF-8 bytes.", m.ReadContext)
	register(s, "process_status", "Query process state and exit code.", func(_ context.Context, in IDInput) (Status, error) { return m.Status(in) })
	register(s, "process_stop", "Stop a process and its child process tree; repeated calls are safe.", func(_ context.Context, in IDInput) (Status, error) { return m.Stop(in) })
	register(s, "terminal_open", "Create a native interactive terminal; its session survives MCP reconnection and its output includes VT control sequences.", func(_ context.Context, in TerminalInput) (Status, error) { return m.OpenTerminal(in) })
	register(s, "terminal_write", "Write Base64-encoded raw bytes; use Aw== for Ctrl+C. Do not blindly retry input after a failure.", m.Write)
	register(s, "terminal_read", "Read the merged terminal stream, preserving ANSI/VT sequences and raw bytes; optionally wait up to 30000 milliseconds for output or exit. Cancellation preserves the read result and does not close the terminal.", m.ReadContext)
	register(s, "terminal_resize", "Resize the terminal columns and rows.", func(_ context.Context, in ResizeInput) (Status, error) { return m.Resize(in) })
	register(s, "terminal_status", "Query terminal state and refresh its idle timer.", func(_ context.Context, in IDInput) (Status, error) { return m.Status(in) })
	register(s, "terminal_close", "Close a terminal and clean up its associated process tree.", func(_ context.Context, in IDInput) (Status, error) { return m.Stop(in) })
}
