package forwarding

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func register[I, O any](s *mcp.Server, name, description string, fn func(I) (O, error)) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: description}, func(_ context.Context, _ *mcp.CallToolRequest, in I) (*mcp.CallToolResult, O, error) {
		out, err := fn(in)
		return nil, out, err
	})
}
func (m *Manager) Register(s *mcp.Server) {
	register(s, "port_forward_create", "Create a TCP forward on the machine running remote-mcp. Success only confirms that the listener is ready; replace wildcard addresses with an IP reachable by the agent. The MCP token and entry-point TLS do not protect this port; the target must provide its own authentication.", m.Create)
	register(s, "port_forward_list", "List active TCP forwards and completed forwards within the retention period.", func(struct{}) (ListResult, error) { return m.List(), nil })
	register(s, "port_forward_status", "Query a TCP forward, its connection counts, and its last error code.", m.Status)
	register(s, "port_forward_stop", "Stop a TCP forward and release its listener and existing connections. Repeated calls are safe; MCP disconnection does not stop it automatically.", m.Stop)
}
