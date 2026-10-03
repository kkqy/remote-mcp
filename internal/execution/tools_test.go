package execution

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPToolsAndReconnect(t *testing.T) {
	m := manager(t)
	server := mcp.NewServer(&mcp.Implementation{Name: "execution-test", Version: "0.1.0"}, nil)
	m.Register(server)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connect := func() *mcp.ClientSession {
		t.Helper()
		a, b := mcp.NewInMemoryTransports()
		ss, err := server.Connect(ctx, a, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ss.Close() })
		client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1.0"}, nil)
		cs, err := client.Connect(ctx, b, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cs.Close() })
		return cs
	}
	cs := connect()
	list, err := cs.ListTools(ctx, nil)
	if err != nil || len(list.Tools) != 10 {
		t.Fatalf("工具发现失败: %v %+v", err, list)
	}
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "process_start", Arguments: helperInput("mcp-start", "output")})
	if err != nil || result.IsError {
		t.Fatalf("调用失败: %v %+v", err, result)
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var s Status
	if err = json.Unmarshal(encoded, &s); err != nil {
		t.Fatal(err)
	}
	if s.ID == "" {
		t.Fatal("缺少应用资源ID")
	}
	cs.Close()
	cs = connect()
	result, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "process_status", Arguments: IDInput{s.ID}})
	if err != nil || result.IsError {
		t.Fatalf("重连后不能访问: %v %+v", err, result)
	}
	result, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "process_status", Arguments: IDInput{"missing"}})
	if err != nil || !result.IsError {
		t.Fatalf("业务失败未转为工具错误: %v %+v", err, result)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("业务错误缺少文本结果")
	}
	var toolErr Error
	if err = json.Unmarshal([]byte(text.Text), &toolErr); err != nil || toolErr.Code != "not_found" {
		t.Fatalf("稳定错误码丢失: %q %v", text.Text, err)
	}
}
