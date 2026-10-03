package server

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"remote-mcp/internal/config"
)

const testToken = "fictional-server-test-token"

func testApp(t *testing.T) (*App, *httptest.Server) {
	t.Helper()
	c := config.Default()
	c.Token = testToken
	c.AllowedOrigins = []string{"https://agent.example"}
	app, err := New(c, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(app.Handler)
	t.Cleanup(func() { app.Close(); ts.Close() })
	return app, ts
}
func rpc(t *testing.T, client *http.Client, endpoint, session, body string) (*http.Response, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", ProtocolVersion)
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if len(data) > 0 {
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatalf("响应非JSON：状态%d %s", response.StatusCode, data)
		}
	}
	return response, envelope
}

const initialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"independent-test","version":"1"}}}`

func TestIndependentProtocol(t *testing.T) {
	_, ts := testApp(t)
	client := ts.Client()
	client.Timeout = 5 * time.Second
	response, body := rpc(t, client, ts.URL+"/mcp", "", initialize)
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode, body)
	}
	result := body["result"].(map[string]any)
	if result["protocolVersion"] != ProtocolVersion {
		t.Fatal(result)
	}
	session := response.Header.Get("Mcp-Session-Id")
	if session == "" {
		t.Fatal("缺少协议会话ID")
	}
	response, _ = rpc(t, client, ts.URL+"/mcp", session, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if response.StatusCode != 202 {
		t.Fatal(response.StatusCode)
	}
	_, body = rpc(t, client, ts.URL+"/mcp", session, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	result = body["result"].(map[string]any)
	if len(result["tools"].([]any)) < 18 {
		t.Fatal("工具未完整注册", result)
	}
	params, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "file_stat", "arguments": map[string]any{"path": t.TempDir()}}})
	_, body = rpc(t, client, ts.URL+"/mcp", session, string(params))
	if body["error"] != nil {
		t.Fatal(body)
	}
	// 每次请求仍要求Token，包括已知会话ID。
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(`{}`))
	req.Header.Set("Mcp-Session-Id", session)
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal(res.StatusCode)
	}
}

func TestHTTPBoundaries(t *testing.T) {
	_, ts := testApp(t)
	for _, tt := range []struct {
		name, token, origin string
		status              int
	}{{"missing", "", "", 401}, {"wrong", "wrong", "", 401}, {"origin", testToken, "https://evil.example", 403}, {"empty-origin", testToken, "EMPTY", 403}, {"allowed", testToken, "https://agent.example", 200}, {"nonbrowser", testToken, "", 200}} {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(initialize))
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			if tt.origin != "" {
				v := tt.origin
				if v == "EMPTY" {
					v = ""
				}
				req.Header.Set("Origin", v)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			res, err := ts.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			body, _ := io.ReadAll(res.Body)
			if res.StatusCode != tt.status {
				t.Fatalf("状态%d，预期%d: %s", res.StatusCode, tt.status, body)
			}
			if bytes.Contains(body, []byte(testToken)) {
				t.Fatal("响应泄漏Token")
			}
		})
	}
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(strings.Repeat("x", 2<<20)))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 413 {
		t.Fatal("超限未拒绝", res.StatusCode)
	}
}

type startupSink struct{ ready chan []byte }

func (s startupSink) Write(p []byte) (int, error) {
	s.ready <- append([]byte(nil), p...)
	return len(p), nil
}
func TestRunDynamicPortAndShutdown(t *testing.T) {
	c := config.Default()
	c.Token = testToken
	c.Listen = "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan []byte, 8)
	done := make(chan error, 1)
	var diagnostics bytes.Buffer
	go func() { done <- Run(ctx, c, startupSink{ready}, &diagnostics) }()
	var data []byte
	select {
	case data = <-ready:
	case err := <-done:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("启动超时")
	}
	var conf ClientConfig
	if err := json.Unmarshal(data, &conf); err != nil {
		t.Fatal(err)
	}
	endpoint := conf.Servers["remote-mcp"].URL
	if strings.Contains(endpoint, ":0/") {
		t.Fatal("输出了配置端口0")
	}
	response, _ := rpc(t, &http.Client{Timeout: 5 * time.Second}, endpoint, "", initialize)
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("关闭超时")
	}
	if strings.Contains(diagnostics.String(), testToken) {
		t.Fatal("普通日志泄露Token")
	}
}

func TestRunBindingAndTLSFailure(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	c := config.Default()
	c.Token = testToken
	c.Listen = listener.Addr().String()
	var out, diag bytes.Buffer
	if err := Run(context.Background(), c, &out, &diag); err == nil || out.Len() != 0 {
		t.Fatal("绑定失败输出了配置")
	}
	c.Listen = "127.0.0.1:0"
	c.TLSCert = "missing"
	c.TLSKey = "missing"
	if err := Run(context.Background(), c, &out, &diag); err == nil || out.Len() != 0 {
		t.Fatal("证书失败输出了配置")
	}
}

func TestRunTLS(t *testing.T) {
	fixture := httptest.NewTLSServer(http.NotFoundHandler())
	certificate := fixture.TLS.Certificates[0]
	client := fixture.Client()
	fixture.Close()
	key, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	c := config.Default()
	c.Token = testToken
	c.Listen = "127.0.0.1:0"
	c.TLSCert = filepath.Join(directory, "cert.pem")
	c.TLSKey = filepath.Join(directory, "key.pem")
	if err := os.WriteFile(c.TLSCert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.TLSKey, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan []byte, 8)
	done := make(chan error, 1)
	go func() { done <- Run(ctx, c, startupSink{ready}, io.Discard) }()
	var data []byte
	select {
	case data = <-ready:
	case err := <-done:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("TLS启动超时")
	}
	var conf ClientConfig
	if err := json.Unmarshal(data, &conf); err != nil {
		t.Fatal(err)
	}
	endpoint := conf.Servers["remote-mcp"].URL
	if !strings.HasPrefix(endpoint, "https://") {
		t.Fatal("TLS配置协议错误")
	}
	client.Timeout = 5 * time.Second
	response, _ := rpc(t, client, endpoint, "", initialize)
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("TLS关闭超时")
	}
}
