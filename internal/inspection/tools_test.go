package inspection

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestLargeFailurePreservesTrustedErrorAndStructuredOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	server := mcp.NewServer(&mcp.Implementation{Name: "inspection-test", Version: "1"}, nil)
	original := ProcessesResult{Result: Result{OK: false, Code: "timeout", Message: "The total inspection time budget expired", Partial: true}, RootPID: 42, Processes: []Process{}, Scanned: 100, Visibility: "best_effort"}
	for i := 0; i < 100; i++ {
		original.Processes = append(original.Processes, Process{PID: i + 42, PPID: 42, Name: strings.Repeat("中文进程", 30)})
	}
	register(server, "process_inspect", "Inspect a test process snapshot.", func(context.Context, SnapshotInput) ProcessesResult { return original })
	type observed struct {
		business *Error
		raw      []byte
		isError  bool
		err      error
	}
	captured := make(chan observed, 1)
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, request)
			if method == "tools/call" {
				got := observed{err: err}
				if call, ok := result.(*mcp.CallToolResult); ok {
					got.business, _ = call.GetError().(*Error)
					got.isError = call.IsError
					got.raw, _ = json.Marshal(call.StructuredContent)
				}
				captured <- got
			}
			return result, err
		}
	})
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "inspection-test-client", Version: "1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	clientResult, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "process_inspect", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	got := <-captured
	if got.err != nil || !got.isError || got.business == nil || got.business.Code != original.Code || got.business.Message != original.Message {
		t.Fatalf("trusted server error lost: %+v", got)
	}
	if len(got.raw) <= 4096 {
		t.Fatalf("fixture did not exceed the diagnostic projection limit: %d", len(got.raw))
	}
	var serverOutput ProcessesResult
	if err := json.Unmarshal(got.raw, &serverOutput); err != nil {
		t.Fatal(err)
	}
	if len(serverOutput.Processes) != 100 || serverOutput.Processes[99].Name != original.Processes[99].Name || serverOutput.Code != "timeout" || !serverOutput.Partial {
		t.Fatal("server partial structured output was lost")
	}
	if !clientResult.IsError || clientResult.GetError() != nil {
		t.Fatal("client error contract changed")
	}
	clientRaw, err := json.Marshal(clientResult.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var clientOutput ProcessesResult
	if err := json.Unmarshal(clientRaw, &clientOutput); err != nil {
		t.Fatal(err)
	}
	if len(clientOutput.Processes) != 100 || clientOutput.Processes[99].Name != original.Processes[99].Name || clientOutput.Code != "timeout" || clientOutput.Message != original.Message {
		t.Fatal("client did not receive the complete partial result")
	}
	text, ok := clientResult.Content[0].(*mcp.TextContent)
	if !ok || text.Text != "timeout: "+original.Message {
		t.Fatal("client failure text changed")
	}
}
