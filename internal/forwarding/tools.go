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
	register(s, "port_forward_create", "在运行 remote-mcp 的机器上创建 TCP 转发。成功仅代表监听成功；全接口地址须替换为 Agent 可达 IP。MCP Token 和入口 TLS 不保护此端口，目标自行鉴权。", m.Create)
	register(s, "port_forward_list", "列出活跃及保留期内已结束的 TCP 转发。", func(struct{}) (ListResult, error) { return m.List(), nil })
	register(s, "port_forward_status", "查询 TCP 转发及连接计数、最后错误码。", m.Status)
	register(s, "port_forward_stop", "停止 TCP 转发并释放监听和现有连接，重复调用安全；MCP 断连不会自动停止。", m.Stop)
}
