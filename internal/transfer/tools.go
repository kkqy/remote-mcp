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
	register(s, "file_stat", "查询普通文件大小", m.Stat)
	register(s, "upload_create", "创建顺序分块上传；request_id 用于有界幂等；默认禁止覆盖", m.UploadCreate)
	register(s, "upload_write", "写入 Base64 分块；offset 为原始字节偏移", m.UploadWrite)
	register(s, "upload_finish", "校验长度与 SHA-256 后发布文件", m.UploadFinish)
	register(s, "upload_cancel", "取消上传并清理临时文件", m.UploadCancel)
	register(s, "download_open", "打开普通文件并流式计算 SHA-256", m.DownloadOpen)
	register(s, "download_read", "按原始字节偏移读取 Base64 分块", m.DownloadRead)
	register(s, "download_close", "检查源文件变化并释放下载句柄", m.DownloadClose)
}
