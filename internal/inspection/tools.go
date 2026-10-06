package inspection

import (
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func register[I any, O interface{ outcome() Result }](s *mcp.Server, name, description string, fn func(context.Context, I) O) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: description}, func(ctx context.Context, _ *mcp.CallToolRequest, in I) (*mcp.CallToolResult, O, error) {
		out := fn(ctx, in)
		r := out.outcome()
		b, _ := json.Marshal(out)
		text := string(b)
		if !r.OK {
			text = r.Code + ": " + r.Message
		}
		result := &mcp.CallToolResult{IsError: !r.OK, Content: []mcp.Content{&mcp.TextContent{Text: text}}}
		if !r.OK {
			// 服务端诊断读取可信错误对象，客户端仍取得完整的结构化部分结果。
			result.SetError(&Error{Code: r.Code, Message: r.Message})
		}
		return result, out, nil
	})
}
func (m *Manager) Register(s *mcp.Server) {
	register(s, "environment_inspect", "Inspect the operating system, architecture and fixed runtime versions within one bounded time budget. Runtime lookup uses the service account PATH.", m.Environment)
	register(s, "process_inspect", "Read a bounded visible PID/PPID/name snapshot, optionally restricted to a root process and descendants. Partial results indicate visibility or scan limits.", m.Processes)
	register(s, "network_listeners", "Inspect visible IPv4 and IPv6 TCP LISTEN endpoints and owning PIDs with bounded scanning. Missing results do not prove that an endpoint does not exist.", m.Listeners)
	register(s, "network_probe", "Probe one target through DNS, TCP, verified TLS and HTTP stages within a total deadline. HTTP performs one GET without proxy, redirects or body reads. Failures retain completed stages.", m.Probe)
}
