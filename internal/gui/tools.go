package gui

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func toolResult(out any, err error) *mcp.CallToolResult {
	if err != nil {
		var result any = &Error{Code: "session_closed", Message: "GUI operation failed"}
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
	register(s, "gui_status", "Query the current GUI backend or session capabilities without requesting desktop authorization.", m.Status)
	register(s, "gui_open", "Open a GUI session for the current user; Wayland may require local authorization.", m.Open)
	register(s, "gui_close", "Close a GUI session and release desktop authorization, input, and capture resources.", m.CloseSession)
	mcp.AddTool(s, &mcp.Tool{Name: "gui_screenshot", Description: "Capture an authorized display or a region as a PNG at its original size; use its image coordinates for subsequent mouse operations."}, func(ctx context.Context, _ *mcp.CallToolRequest, in ScreenshotInput) (*mcp.CallToolResult, any, error) {
		out, err := m.Screenshot(ctx, in)
		if err != nil {
			return toolResult(nil, err), nil, nil
		}
		result := toolResult(out.CaptureMetadata, nil)
		result.Content = append([]mcp.Content{&mcp.ImageContent{Data: out.PNG, MIMEType: "image/png"}}, result.Content...)
		return result, nil, nil
	})
	register(s, "gui_mouse", "Move, click, double-click, drag, or scroll using pixel coordinates in a valid screenshot; capture again to verify the result.", m.Mouse)
	register(s, "gui_key", "Submit a single key or key combination and release the keys pressed by this call; some events may take effect before a failure.", m.Key)
	register(s, "gui_text", "Enter UTF-8 text. Clipboard mode preserves and restores existing content by default; if preservation is unreliable, replacement requires explicit permission.", m.Text)
}
