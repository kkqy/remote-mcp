package fileops

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
	register(s, "file_list", "List a bounded directory page in file system enumeration order. Offsets may shift when entries change; symbolic link targets are rejected.", m.List)
	register(s, "file_read", "Read bounded UTF-8 text by original line number and return the complete file SHA-256. LF and CRLF bytes are preserved; a partial line keeps next_line at that line.", m.Read)
	register(s, "file_search", "Search literal UTF-8 text with entry, depth, file, scan-byte, match and output budgets. Issues and truncated report skipped or incomplete text; symbolic links are not followed.", m.Search)
	register(s, "file_patch", "Patch an existing regular UTF-8 file using ordered original-line edits and expected SHA-256. Replacement text must include desired newlines explicitly. Conflicts preserve the target; external non-cooperating writers are not protected by a strict atomic compare-and-swap.", m.Patch)
}
