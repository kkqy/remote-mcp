package fileops

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSDKMappingAndSchema(t *testing.T) {
	m := managerForTest(t, DefaultConfig())
	server := mcp.NewServer(&mcp.Implementation{Name: "fileops-test", Version: "0.1.0"}, nil)
	m.Register(server)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, b := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1.0"}, nil)
	cs, err := client.Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	list, err := cs.ListTools(ctx, nil)
	if err != nil || len(list.Tools) != 4 {
		t.Fatalf("discovery: %+v %v", list, err)
	}
	path := filepath.Join(t.TempDir(), "file")
	writeText(t, path, "中文\r\n")
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "file_read", Arguments: map[string]any{"path": path}})
	if err != nil || result.IsError {
		t.Fatalf("call: %+v %v", result, err)
	}
	encoded, _ := json.Marshal(result.StructuredContent)
	var out map[string]any
	if err = json.Unmarshal(encoded, &out); err != nil || out["text"] != "中文\r\n" || out["sha256"] != digest([]byte("中文\r\n")) {
		t.Fatalf("content: %s %v", encoded, err)
	}
	result, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "file_patch", Arguments: map[string]any{"path": path, "expected_sha256": digest([]byte("other")), "edits": []map[string]any{{"start_line": 1, "delete_count": 1, "text": "x"}}}})
	if err != nil || !result.IsError {
		t.Fatalf("error mapping: %+v %v", result, err)
	}
	content, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("missing typed error text")
	}
	var toolError map[string]string
	if err = json.Unmarshal([]byte(content.Text), &toolError); err != nil || toolError["code"] != "conflict" || toolError["message"] == "" {
		t.Fatalf("stable error: %s %v", content.Text, err)
	}
}
