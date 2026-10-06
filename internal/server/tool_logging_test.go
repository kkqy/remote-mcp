package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"remote-mcp/internal/config"
	"remote-mcp/internal/fileops"
	"remote-mcp/internal/gui"
	"remote-mcp/internal/inspection"
	"remote-mcp/internal/logstream"
)

func TestToolFailureLoggingIndependentHTTP(t *testing.T) {
	for _, token := range []string{"", testToken} {
		t.Run(map[bool]string{true: "token", false: "anonymous"}[token != ""], func(t *testing.T) {
			c := config.Default()
			c.Token = token
			var logs bytes.Buffer
			app, err := New(c, &logs)
			if err != nil {
				t.Fatal(err)
			}
			defer app.Close()
			ts := httptest.NewServer(app.Handler)
			defer ts.Close()
			client := ts.Client()
			client.Timeout = 5 * time.Second
			response, _ := rpcWithToken(t, client, ts.URL+"/mcp", "", initialize, token)
			session := response.Header.Get("Mcp-Session-Id")
			rpcWithToken(t, client, ts.URL+"/mcp", session, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, token)
			const secret = "fictional-sensitive-content-012345"
			sequence := 1
			call := func(name string, arguments any) (map[string]any, string) {
				t.Helper()
				sequence++
				body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": sequence, "method": "tools/call", "params": map[string]any{"name": name, "arguments": arguments}})
				if err != nil {
					t.Fatal(err)
				}
				before := logs.Len()
				_, envelope := rpcWithToken(t, client, ts.URL+"/mcp", session, string(body), token)
				line := logs.String()[before:]
				if strings.Contains(line, secret) || token != "" && strings.Contains(line, token) {
					t.Fatal("错误日志泄漏虚构敏感内容或 Token")
				}
				if !strings.Contains(line, "elapsed=") || !strings.Contains(line, "failed=") {
					t.Fatal("原有日志元数据丢失")
				}
				return envelope, line
			}
			cases := []struct {
				name      string
				arguments any
				code      string
				reason    string
			}{
				{"gui_text", map[string]any{"id": secret, "text": secret, "mode": "clipboard"}, "not_found", "GUI session not found"},
				{"file_stat", map[string]any{"path": filepath.Join(t.TempDir(), secret)}, "NOT_FOUND", "The file or directory does not exist"},
				{"process_status", map[string]any{"id": secret}, "not_found", "The resource does not exist or has expired"},
				{"port_forward_status", map[string]any{"id": secret}, "not_found", "The forwarding rule does not exist"},
				{"unregistered_" + secret, map[string]any{"text": secret}, "unknown_tool", "Requested tool is not registered"},
				{"process_status", map[string]any{}, "missing_required_argument", "Missing required tool arguments"},
				{"process_start", map[string]any{"request_id": "log-schema", "command": "echo", "wait_ms": secret}, "argument_type_mismatch", "Tool argument type does not match its declaration"},
				{"process_status", secret, "invalid_argument_json", "Tool arguments must be a valid JSON object"},
			}
			for _, item := range cases {
				t.Run(item.name+"/"+item.code, func(t *testing.T) {
					envelope, line := call(item.name, item.arguments)
					if envelope["error"] == nil && envelope["result"].(map[string]any)["isError"] != true {
						t.Fatal("测试没有产生真实失败")
					}
					if !strings.Contains(line, "error_code="+item.code) || !strings.Contains(line, item.reason) || !strings.Contains(line, "failed=true") {
						t.Fatalf("未记录预期具体诊断：%s；得到%s", item.code, line)
					}
					if strings.HasPrefix(item.name, "unregistered_") && !strings.Contains(line, `tool=unknown_tool`) {
						t.Fatal("未知工具名不应进入可信日志元数据")
					}
					if item.name == "process_status" && item.code == "not_found" {
						// 客户端收到 SDK 的文本 JSON；中间件必须通过服务端 GetError 保留原始类型。
						result := envelope["result"].(map[string]any)
						var business map[string]any
						text := result["content"].([]any)[0].(map[string]any)["text"].(string)
						if json.Unmarshal([]byte(text), &business) != nil || business["code"] != "not_found" {
							t.Fatal("未覆盖 SDK 的业务错误转换链")
						}
					}
				})
			}
			file := filepath.Join(t.TempDir(), "regular-file")
			if err := os.WriteFile(file, []byte("验证成功调用"), 0600); err != nil {
				t.Fatal(err)
			}
			_, line := call("file_stat", map[string]any{"path": file})
			if !strings.Contains(line, "failed=false") || strings.Contains(line, "error_code=") || strings.Contains(line, "error_message=") {
				t.Fatal("成功调用不应附带错误字段")
			}
			// 有意注入诊断边界测试，不使用真实桌面或剪贴板。
			const cause = "Target format and ownership not confirmed: owner_unknown/timeout (epoch 3->4, formats 2)"
			mcp.AddTool(app.MCP, &mcp.Tool{Name: "gui_text"}, func(_ context.Context, _ *mcp.CallToolRequest, input gui.TextInput) (*mcp.CallToolResult, any, error) {
				business := &gui.Error{Code: "clipboard_restore_failed", Message: cause + "; " + token + "; " + input.Text + "\n\x00", InputMayHaveApplied: true, ClipboardRestore: "failed"}
				return &mcp.CallToolResult{IsError: true, StructuredContent: business}, nil, nil
			})
			_, line = call("gui_text", map[string]any{"id": "log-fixture", "mode": "clipboard", "text": secret})
			if !strings.Contains(line, cause) || !strings.Contains(line, "error_code=clipboard_restore_failed") || !strings.Contains(line, "clipboard_restore=failed") || !strings.Contains(line, "input_may_have_applied=true") {
				t.Fatal("公开 mode 枚举或短片段不应遮蔽 GUI 恢复原因")
			}
			if strings.Count(line, "\n") != 1 || strings.ContainsRune(line, '\x00') {
				t.Fatal("错误说明控制字符没有清理")
			}
			// 走真实SDK/HTTP序列化，核对日志处理不会反向修改调用方收到的错误对象。
			for _, message := range []string{strings.Repeat("诊断", 682) + "abcd", strings.Repeat("x", 4097)} {
				mcp.AddTool(app.MCP, &mcp.Tool{Name: "gui_text"}, func(context.Context, *mcp.CallToolRequest, gui.TextInput) (*mcp.CallToolResult, any, error) {
					business := &gui.Error{Code: "invalid_argument", Message: message}
					return &mcp.CallToolResult{IsError: true, StructuredContent: business}, nil, nil
				})
				envelope, line := call("gui_text", map[string]any{"id": "log-length", "text": secret})
				result := envelope["result"].(map[string]any)
				if result["isError"] != true || result["structuredContent"].(map[string]any)["message"] != message {
					t.Fatal("日志长度处理改变了原GUI错误响应")
				}
				if len(message) == 4096 {
					if !strings.Contains(line, "…") || strings.Contains(line, message) {
						t.Fatal("4096字节原说明没有在真实日志链截断")
					}
				} else if !strings.Contains(line, "Business error details exceed the log limit or have invalid encoding") || strings.Contains(line, message) {
					t.Fatal("超过原说明上限没有固定安全回退")
				}
			}
			mcp.AddTool(app.MCP, &mcp.Tool{Name: "gui_text"}, func(_ context.Context, _ *mcp.CallToolRequest, input gui.TextInput) (*mcp.CallToolResult, any, error) {
				business := &gui.Error{Code: "invalid_argument", Message: "Clipboard failure: foo" + input.Text + "; owner_unknown/timeout"}
				return &mcp.CallToolResult{IsError: true, StructuredContent: business}, nil, nil
			})
			envelope, line := call("gui_text", map[string]any{"id": "log-adjacent", "text": "foo["})
			if strings.Contains(line, "foo[") || !strings.Contains(line, "owner_unknown/timeout") {
				t.Fatal("真实HTTP日志标记拼接泄漏了候选内容或移除了公开原因")
			}
			if envelope["result"].(map[string]any)["structuredContent"].(map[string]any)["message"] != "Clipboard failure: foofoo[; owner_unknown/timeout" {
				t.Fatal("日志隔断改变了原业务响应")
			}
			for _, probe := range []struct {
				name, code string
				err        error
			}{
				{"untrusted_error_probe", "tool_error", errors.New(secret + " /private/path")},
				{"cancelled_error_probe", "cancelled", context.Canceled},
				{"timeout_error_probe", "timeout", context.DeadlineExceeded},
				{"protocol_error_probe", "invalid_params", &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: secret}},
			} {
				mcp.AddTool(app.MCP, &mcp.Tool{Name: probe.name}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
					return nil, nil, probe.err
				})
				_, line := call(probe.name, map[string]any{})
				if !strings.Contains(line, "error_code="+probe.code) || !strings.Contains(line, "tool=unknown_tool") {
					t.Fatal("未知工具原始错误没有安全分类")
				}
			}
			mcp.AddTool(app.MCP, &mcp.Tool{Name: "untrusted_payload_probe"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
				return &mcp.CallToolResult{IsError: true, StructuredContent: map[string]any{"code": "not_found", "message": secret}, Content: []mcp.Content{&mcp.TextContent{Text: secret}}}, nil, nil
			})
			_, line = call("untrusted_payload_probe", map[string]any{})
			if !strings.Contains(line, "error_code=tool_error") {
				t.Fatal("任意错误 JSON 形状不能获得可信日志权限")
			}
			mcp.AddTool(app.MCP, &mcp.Tool{Name: "untrusted_raw_probe"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
				payload, _ := json.Marshal(map[string]any{"ok": false, "code": "IO_ERROR", "message": secret})
				return &mcp.CallToolResult{IsError: true, StructuredContent: json.RawMessage(payload)}, nil, nil
			})
			_, line = call("untrusted_raw_probe", map[string]any{})
			if !strings.Contains(line, "error_code=tool_error") {
				t.Fatal("未知工具RawMessage不能借用文件错误形状获得原文日志权限")
			}
			mcp.AddTool(app.MCP, &mcp.Tool{Name: "untrusted_typed_probe"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
				return &mcp.CallToolResult{IsError: true, StructuredContent: &gui.Error{Code: "not_found", Message: secret}}, nil, nil
			})
			_, line = call("untrusted_typed_probe", map[string]any{})
			if !strings.Contains(line, "error_code=tool_error") {
				t.Fatal("未知工具仅借用已有错误类型不能获得可信日志权限")
			}
		})
	}
}

func TestDiagnosticBoundaries(t *testing.T) {
	arguments := json.RawMessage(`{"mode":"clipboard","keys":["Ctrl","V"],"text":"X11","id":"timeout"}`)
	message := "X11 剪贴板恢复失败：clipboard_restore_failed/timeout"
	if got := safeDiagnosticMessage(message, arguments, ""); got != message {
		t.Fatal("公开枚举、短值或匿名模式不应清空具体诊断", got)
	}
	large := strings.Repeat("中文诊断", 300)
	got := safeDiagnosticMessage(large, nil, "")
	if len(got) > 1024 || !strings.HasSuffix(got, "…") || !json.Valid([]byte(`"`+got+`"`)) {
		t.Fatal("截断必须有界且保持有效 UTF-8")
	}
	if got := safeDiagnosticMessage("前\n中\x00后\u202e说明", nil, ""); got != "前 中 后 说明" {
		t.Fatal("控制字符和Unicode格式字符没有替换为空格", got)
	}
	for _, value := range []string{strings.Repeat("x", 4097), string([]byte{0xff})} {
		if got := safeDiagnosticMessage(value, nil, ""); got != "Business error details exceed the log limit or have invalid encoding" {
			t.Fatal("原说明超过上限或无效编码应使用固定原因")
		}
	}
	if got := safeDiagnosticMessage("原因中包含fictional-token", nil, "fictional-token"); strings.Contains(got, "fictional-token") || !strings.Contains(got, "原因") {
		t.Fatal("非空 Token 脱敏不应删除整个具体原因")
	}
	if knownBusinessCode("gui", "fictional-sensitive-code") {
		t.Fatal("未知业务错误码不能进入可信诊断")
	}
	if got := protocolDiagnostic(&json.SyntaxError{Offset: 1}); got.code != "invalid_json" {
		t.Fatal("JSON 格式错误分类丢失")
	}
}

func TestRedactionMarkersDoNotRepeatSecrets(t *testing.T) {
	for _, token := range []string{"redacted", "[redacted]", "content", "*", "#", "a["} {
		t.Run(token, func(t *testing.T) {
			if got := redactToken("Failure: "+token+"; a"+token, token); strings.Contains(got, token) || !strings.Contains(got, "Failure:") {
				t.Fatal("隐藏标记不得重新输出凭据，且应保留原因", got)
			}
			arguments := json.RawMessage(`{"mode":"clipboard","text":"[content redacted]"}`)
			got := safeDiagnosticMessage("Clipboard failure: [content redacted]; "+token+"; owner_unknown/timeout (epoch 3->4, formats 2)", arguments, token)
			if strings.Contains(got, token) || strings.Contains(got, "[content redacted]") || !strings.Contains(got, "owner_unknown/timeout") {
				t.Fatal("隐藏标记不得带入凭据或输入内容，公开恢复原因应保留", got)
			}
		})
	}
	for _, message := range []string{strings.Repeat("x", 4097), "\x00"} {
		for _, token := range []string{"Business", "Tool"} {
			if got := safeDiagnosticMessage(message, nil, token); strings.Contains(got, token) {
				t.Fatal("安全回退说明也必须屏蔽凭据", got)
			}
		}
	}
	message := "Clipboard restoration failed: owner_unknown/timeout (epoch 3->4, formats 2)"
	if got := safeDiagnosticMessage(message, json.RawMessage(`{"mode":"clipboard"}`), ""); got != message {
		t.Fatal("空凭据不能遮蔽正常英文原因", got)
	}
}

func TestContentRedactionDoesNotReassembleSecrets(t *testing.T) {
	cases := []struct {
		name, value, message, token string
	}{
		{"adjacent-marker", "foo[", "Clipboard failure: foofoo[", ""},
		{"normalized-control", "foo bar", "Clipboard failure: foo\nbar", ""},
		{"token-marker", "reda", "Clipboard failure: fictional-token", "fictional-token"},
		{"truncation-suffix", "foo…", strings.Repeat("x", 1018) + "foo" + strings.Repeat("z", 100), ""},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			arguments, _ := json.Marshal(map[string]any{"text": item.value})
			got := safeDiagnosticMessage(item.message, arguments, item.token)
			if strings.Contains(got, item.value) || item.token != "" && strings.Contains(got, item.token) || len(got) > 1024 {
				t.Fatal("最终日志不得重新拼出被屏蔽内容或凭据，且必须有界", got)
			}
			if item.name == "truncation-suffix" && !strings.HasSuffix(got, "…") {
				t.Fatal("内容隔断后仍须保留截断省略号")
			}
		})
	}
	arguments, _ := json.Marshal(map[string]any{"text": "foo[", "args": []string{"*#~^…"}})
	got := safeDiagnosticMessage("Clipboard failure: foofoo[", arguments, "*")
	if got != "#" {
		t.Fatal("全部隔断与候选冲突时须保守降级，并避开凭据", got)
	}
}

func TestUnknownToolFallbackDoesNotMatchToken(t *testing.T) {
	c := config.Default()
	c.Token = "unknown_tool"
	var logs bytes.Buffer
	app, err := New(c, &logs)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	ts := httptest.NewServer(app.Handler)
	defer ts.Close()
	client := ts.Client()
	client.Timeout = 5 * time.Second
	response, _ := rpcWithToken(t, client, ts.URL+"/mcp", "", initialize, c.Token)
	session := response.Header.Get("Mcp-Session-Id")
	rpcWithToken(t, client, ts.URL+"/mcp", session, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, c.Token)
	_, envelope := rpcWithToken(t, client, ts.URL+"/mcp", session, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"not_registered","arguments":{}}}`, c.Token)
	if envelope["error"] == nil || strings.Contains(logs.String(), c.Token) || !strings.Contains(logs.String(), "tool=[redacted]") || !strings.Contains(logs.String(), "Requested tool is not registered") {
		t.Fatal("未知工具名称后备与完整Token碰撞时必须隐藏凭据并保留原因")
	}
}

func TestP0DiagnosticTypesAndProjectionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		domain string
		err    error
		code   string
	}{
		{"fileops", &fileops.Error{Code: "invalid_patch", Message: "Invalid patch"}, "invalid_patch"},
		{"logstream", &logstream.Error{Code: "permission_denied", Message: "Permission denied"}, "permission_denied"},
	} {
		diagnostic, trusted := businessDiagnostic(tc.domain, nil, tc.err)
		if !trusted || diagnostic.code != tc.code || diagnostic.message == "" {
			t.Fatal("新模块typed错误未进入受控诊断", diagnostic)
		}
	}
	for _, tc := range []struct {
		domain, payload string
		trusted         bool
	}{
		{"inspection", `{"ok":false,"code":"tls_failed","message":"TLS certificate verification failed","stages":[{"stage":"tcp","state":"ok","reason":"private-stage-data"}]}`, true},
		{"inspection", `{"ok":true,"partial":true,"code":"permission_denied","message":"Private successful result"}`, false},
		{"inspection", `{"code":"tls_failed","message":"Missing explicit outcome"}`, false},
		{"inspection", `{"ok":false,"code":"unrecognized","message":"Unknown code"}`, false},
		{"", `{"ok":false,"code":"tls_failed","message":"Fake third party payload"}`, false},
		{"inspection", `{"ok":false,"code":"tls_failed","message":"` + strings.Repeat("x", maxBusinessMessageBytes) + `"}`, false},
	} {
		diagnostic, trusted := businessDiagnostic(tc.domain, &mcp.CallToolResult{IsError: true, StructuredContent: json.RawMessage(tc.payload)}, nil)
		if trusted != tc.trusted {
			t.Fatalf("投影信任范围错误: %q %+v", tc.domain, diagnostic)
		}
		if strings.Contains(diagnostic.message, "private-stage-data") {
			t.Fatal("投影不能扩展到stage内容")
		}
	}
	const secret = "private-p0-input-012345"
	for _, key := range []string{"query", "target", "expected_sha256", "edits"} {
		value := any(secret)
		if key == "edits" {
			value = []any{map[string]any{"text": secret}}
		}
		arguments, _ := json.Marshal(map[string]any{key: value})
		message := safeDiagnosticMessage("Operation failed: "+secret, arguments, testToken)
		if strings.Contains(message, secret) || !strings.Contains(message, "Operation failed") {
			t.Fatal("新输入字段未过滤", key, message)
		}
	}
}

func TestP0LargeInspectionFailureUsesTrustedError(t *testing.T) {
	p := newP0Protocol(t)
	const reason = "TLS certificate is not trusted"
	const secret = "private-stage-data-012345"
	mcp.AddTool(p.app.MCP, &mcp.Tool{Name: "network_probe"}, func(_ context.Context, _ *mcp.CallToolRequest, _ inspection.ProbeInput) (*mcp.CallToolResult, inspection.ProbeResult, error) {
		out := inspection.ProbeResult{Result: inspection.Result{OK: false, Code: "tls_failed", Message: reason}, FailedStage: "tls", Stages: []inspection.Stage{{Stage: "tcp", State: "ok"}, {Stage: "tls", State: "failed", Reason: strings.Repeat(secret, 300)}}}
		result := &mcp.CallToolResult{}
		result.SetError(&inspection.Error{Code: "tls_failed", Message: reason})
		return result, out, nil
	})
	out, line := p.call("network_probe", map[string]any{"mode": "tls", "target": "private-target-012345:443"}, true)
	encoded, _ := json.Marshal(out)
	if len(encoded) <= maxBusinessMessageBytes || out["failed_stage"] != "tls" || len(out["stages"].([]any)) != 2 {
		t.Fatal("没有覆盖超出Raw投影预算的失败结构")
	}
	if !strings.Contains(line, "error_code=tls_failed") || !strings.Contains(line, reason) || strings.Contains(line, secret) || strings.Contains(line, "private-target-012345") {
		t.Fatal("大型失败未保留可信指针诊断或泄漏响应内容", line)
	}
}
