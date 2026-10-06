package gui

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func toolResult(out any, err error) *mcp.CallToolResult {
	if err != nil {
		var result any = &Error{Code: "session_closed", Message: "图形操作失败"}
		var e *Error
		if errors.As(err, &e) {
			result = e
		}
		data, _ := json.Marshal(result)
		return &mcp.CallToolResult{IsError: true, StructuredContent: result, Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}
	}
	data, _ := json.Marshal(out)
	return &mcp.CallToolResult{StructuredContent: out, Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}
}
func register[I, O any](s *mcp.Server, name, description string, fn func(context.Context, I) (O, error)) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: description}, func(ctx context.Context, _ *mcp.CallToolRequest, in I) (*mcp.CallToolResult, any, error) {
		out, err := fn(ctx, in)
		return toolResult(out, err), nil, nil
	})
}
func (m *Manager) Register(s *mcp.Server) {
	register(s, "gui_status", "查询当前图形后端或会话能力；不触发桌面授权。", m.Status)
	register(s, "gui_open", "创建当前用户图形桌面会话，Wayland 可能要求本机用户确认授权。", m.Open)
	register(s, "gui_close", "关闭图形会话并释放桌面授权、输入及采集资源。", m.CloseSession)
	mcp.AddTool(s, &mcp.Tool{Name: "gui_screenshot", Description: "获取获授权显示器的原始 PNG 或区域截图；图片坐标用于后续鼠标操作。"}, func(ctx context.Context, _ *mcp.CallToolRequest, in ScreenshotInput) (*mcp.CallToolResult, any, error) {
		out, err := m.Screenshot(ctx, in)
		if err != nil {
			return toolResult(nil, err), nil, nil
		}
		result := toolResult(out.CaptureMetadata, nil)
		result.Content = append([]mcp.Content{&mcp.ImageContent{Data: out.PNG, MIMEType: "image/png"}}, result.Content...)
		return result, nil, nil
	})
	register(s, "gui_mouse", "使用有效截图内的像素坐标移动、点击、双击、拖拽或滚动；输入提交后请截图确认。", m.Mouse)
	register(s, "gui_key", "提交单键或组合键并释放本次按下的按键，失败可能已有部分事件生效。", m.Key)
	register(s, "gui_text", "输入 UTF-8 文本；剪贴板兼容默认保存并恢复，无法可靠保存时只有显式允许替换才继续。", m.Text)
}
