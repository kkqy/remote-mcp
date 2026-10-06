package transfer

import (
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func register[I any](s *mcp.Server, name, description string, fn func(context.Context, I) Result) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: description}, func(ctx context.Context, _ *mcp.CallToolRequest, in I) (*mcp.CallToolResult, Result, error) {
		result := fn(ctx, in)
		encoded, _ := json.Marshal(result)
		text := string(encoded)
		if !result.OK {
			text = result.Error()
		}
		return &mcp.CallToolResult{IsError: !result.OK, Content: []mcp.Content{&mcp.TextContent{Text: text}}}, result, nil
	})
}
func (m *Manager) Register(s *mcp.Server) {
	register(s, "file_stat", "Query the size of a regular file", m.Stat)
	register(s, "upload_create", "Create a sequential chunked upload; request_id provides bounded idempotency; overwriting is disabled by default", m.UploadCreate)
	register(s, "upload_write", "Write a Base64 chunk; offset is the raw byte offset", m.UploadWrite)
	register(s, "upload_finish", "Publish the file after validating its length and SHA-256", m.UploadFinish)
	register(s, "upload_cancel", "Cancel an upload and remove its temporary file", m.UploadCancel)
	register(s, "download_open", "Open a regular file and compute its SHA-256 using streaming reads", m.DownloadOpen)
	register(s, "download_read", "Read a Base64 chunk at the raw byte offset", m.DownloadRead)
	register(s, "download_close", "Check for source file changes and release the download handle", m.DownloadClose)
}
