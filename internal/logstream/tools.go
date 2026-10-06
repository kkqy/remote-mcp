package logstream

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
	register(s, "log_open", "Track an existing regular log file with a generation and raw byte cursors. The resource survives MCP reconnection.", m.Open)
	register(s, "log_read", "Read bounded raw log bytes and optionally wait for output, rotation, or truncation. Supply the previous generation and next_cursor; cancellation preserves the last read result.", m.Read)
	register(s, "log_close", "Close a tracked log file and cancel active waits; repeated calls are safe.", m.CloseLog)
}
