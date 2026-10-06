package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"remote-mcp/internal/config"
)

type p0Logs struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *p0Logs) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}
func (b *p0Logs) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.buffer.String() }

type p0Protocol struct {
	t                        *testing.T
	client                   *http.Client
	endpoint, session, token string
	sequence                 int
	logs                     *p0Logs
	app                      *App
	server                   *httptest.Server
}

func newP0Protocol(t *testing.T) *p0Protocol {
	t.Helper()
	c := config.Default()
	c.Token = testToken
	logs := &p0Logs{}
	app, err := New(c, logs)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(app.Handler)
	t.Cleanup(func() { app.Close(); ts.Close() })
	client := ts.Client()
	client.Timeout = 10 * time.Second
	response, body := rpcWithToken(t, client, ts.URL+"/mcp", "", initialize, c.Token)
	if response.StatusCode != 200 || body["error"] != nil {
		t.Fatal(body)
	}
	p := &p0Protocol{t: t, client: client, endpoint: ts.URL + "/mcp", session: response.Header.Get("Mcp-Session-Id"), token: c.Token, logs: logs, app: app, server: ts}
	rpcWithToken(t, client, p.endpoint, p.session, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, p.token)
	return p
}
func (p *p0Protocol) request(method string, params any) map[string]any {
	p.t.Helper()
	p.sequence++
	encoded, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": p.sequence + 1, "method": method, "params": params})
	if err != nil {
		p.t.Fatal(err)
	}
	response, body := rpcWithToken(p.t, p.client, p.endpoint, p.session, string(encoded), p.token)
	if response.StatusCode != 200 || body["error"] != nil {
		p.t.Fatalf("协议请求失败: %d %+v", response.StatusCode, body)
	}
	return body["result"].(map[string]any)
}
func (p *p0Protocol) call(name string, input any, failed bool) (map[string]any, string) {
	p.t.Helper()
	before := len(p.logs.String())
	result := p.request("tools/call", map[string]any{"name": name, "arguments": input})
	line := p.logs.String()[before:]
	if (result["isError"] == true) != failed {
		p.t.Fatalf("工具错误状态不符 %s: %+v", name, result)
	}
	if !strings.Contains(line, "tool="+name) || !strings.Contains(line, fmt.Sprintf("failed=%t", failed)) {
		p.t.Fatalf("工具诊断丢失: %s", line)
	}
	if failed {
		if !strings.Contains(line, "error_code=") || !strings.Contains(line, "error_message=") {
			p.t.Fatalf("缺少具体错误: %s", line)
		}
	} else if strings.Contains(line, "error_code=") || strings.Contains(line, "error_message=") {
		p.t.Fatalf("成功附带错误诊断: %s", line)
	}
	if strings.Contains(line, p.token) {
		p.t.Fatal("普通日志泄漏虚构凭据")
	}
	if structured, ok := result["structuredContent"].(map[string]any); ok {
		return structured, line
	}
	if failed {
		text := result["content"].([]any)[0].(map[string]any)["text"].(string)
		var business map[string]any
		if json.Unmarshal([]byte(text), &business) != nil {
			p.t.Fatalf("业务错误缺少稳定结构: %s", text)
		}
		return business, line
	}
	p.t.Fatal("成功工具缺少 structuredContent")
	return nil, line
}
func p0Hash(text string) string {
	value := sha256.Sum256([]byte(text))
	return hex.EncodeToString(value[:])
}

func TestP0IndependentDiscoveryAndFiles(t *testing.T) {
	p := newP0Protocol(t)
	listed := p.request("tools/list", map[string]any{})
	tools := listed["tools"].([]any)
	if len(tools) != 40 {
		t.Fatalf("工具数量: %d", len(tools))
	}
	names := map[string]map[string]any{}
	for _, raw := range tools {
		tool := raw.(map[string]any)
		names[tool["name"].(string)] = tool
	}
	for _, name := range []string{"file_list", "file_read", "file_search", "file_patch", "log_open", "log_read", "log_close", "environment_inspect", "process_inspect", "network_listeners", "network_probe"} {
		tool := names[name]
		if tool == nil || tool["description"] == nil || tool["inputSchema"] == nil || tool["outputSchema"] == nil {
			t.Fatalf("发现/schema缺失: %s %+v", name, tool)
		}
	}
	for _, name := range []string{"process_read", "terminal_read", "log_read"} {
		properties := names[name]["inputSchema"].(map[string]any)["properties"].(map[string]any)
		if properties["wait_ms"] == nil {
			t.Fatalf("等待参数缺失: %s", name)
		}
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "private-file-path-012345.txt")
	original := "中文首行\r\nprivate-query-012345\r\n末尾无换行"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	out, line := p.call("file_list", map[string]any{"path": dir, "limit": 1}, false)
	if len(out["entries"].([]any)) != 1 {
		t.Fatal(out)
	}
	if strings.Contains(line, path) {
		t.Fatal("列表日志泄漏路径")
	}
	out, line = p.call("file_read", map[string]any{"path": path, "start_line": 1, "line_count": 1}, false)
	if out["text"] != "中文首行\r\n" || out["sha256"] != p0Hash(original) {
		t.Fatal(out)
	}
	if strings.Contains(line, path) || strings.Contains(line, "中文首行") {
		t.Fatal("读取日志泄漏内容")
	}
	out, line = p.call("file_search", map[string]any{"path": dir, "query": "private-query-012345"}, false)
	if len(out["matches"].([]any)) != 1 {
		t.Fatal(out)
	}
	if strings.Contains(line, "private-query-012345") || strings.Contains(line, path) {
		t.Fatal("搜索日志泄漏输入")
	}
	edits := []map[string]any{{"start_line": 2, "delete_count": 1, "text": "新的中文行\r\n"}}
	out, line = p.call("file_patch", map[string]any{"path": path, "expected_sha256": p0Hash(original), "edits": edits}, false)
	want := "中文首行\r\n新的中文行\r\n末尾无换行"
	if out["sha256"] != p0Hash(want) {
		t.Fatal(out)
	}
	out, line = p.call("file_patch", map[string]any{"path": path, "expected_sha256": p0Hash(original), "edits": edits}, true)
	if out["code"] != "conflict" || !strings.Contains(line, "error_code=conflict") || !strings.Contains(line, "expected SHA-256") {
		t.Fatal(out, line)
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != want {
		t.Fatal("冲突破坏中文/CRLF文件")
	}
	for _, secret := range []string{path, p0Hash(original), "新的中文行"} {
		if strings.Contains(line, secret) {
			t.Fatal("补丁错误日志泄漏输入")
		}
	}
}

func TestP0IndependentInspectionAndStages(t *testing.T) {
	p := newP0Protocol(t)
	out, _ := p.call("environment_inspect", map[string]any{"runtimes": []string{}}, false)
	if out["os"] == "" || out["arch"] == "" || out["ok"] != true {
		t.Fatal(out)
	}
	out, _ = p.call("process_inspect", map[string]any{"root_pid": os.Getpid()}, false)
	found := false
	for _, raw := range out["processes"].([]any) {
		if int(raw.(map[string]any)["pid"].(float64)) == os.Getpid() {
			found = true
		}
	}
	if !found {
		t.Fatal("进程树没有当前进程", out)
	}
	port := p.server.Listener.Addr().(*net.TCPAddr).Port
	out, _ = p.call("network_listeners", map[string]any{"port": port, "pid": os.Getpid()}, false)
	found = false
	for _, raw := range out["listeners"].([]any) {
		listener := raw.(map[string]any)
		if int(listener["port"].(float64)) == port {
			for _, pid := range listener["pids"].([]any) {
				if int(pid.(float64)) == os.Getpid() {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("监听端口没有当前PID", out)
	}
	httpTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	defer httpTarget.Close()
	out, line := p.call("network_probe", map[string]any{"mode": "http", "target": httpTarget.URL + "/private-url-012345"}, false)
	if out["status_code"] != float64(204) || out["ok"] != true {
		t.Fatal(out)
	}
	if strings.Contains(line, httpTarget.URL) || strings.Contains(line, "private-url-012345") {
		t.Fatal("探测日志泄漏目标")
	}
	tlsTarget := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer tlsTarget.Close()
	out, line = p.call("network_probe", map[string]any{"mode": "http", "target": tlsTarget.URL + "/private-url-012345"}, true)
	if out["code"] != "tls_failed" || out["failed_stage"] != "tls" || len(out["stages"].([]any)) < 3 || !strings.Contains(line, "error_code=tls_failed") {
		t.Fatal("错误阶段/结构化数据丢失", out, line)
	}
	if strings.Contains(line, tlsTarget.URL) || strings.Contains(line, "private-url-012345") {
		t.Fatal("探测错误日志泄漏目标")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedAddress := listener.Addr().String()
	listener.Close()
	out, line = p.call("network_probe", map[string]any{"mode": "tcp", "target": closedAddress}, true)
	if out["code"] != "tcp_failed" || out["failed_stage"] != "tcp" || len(out["stages"].([]any)) < 2 || !strings.Contains(line, "error_code=tcp_failed") {
		t.Fatal(out, line)
	}
}

// 子进程只在明确测试环境中运行，门文件使延迟输出验收不依赖调度速度。
func TestP0ProcessHelper(t *testing.T) {
	if os.Getenv("REMOTE_MCP_P0_HELPER") != "1" {
		return
	}
	gate := os.Args[len(os.Args)-1]
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		if _, err := os.Stat(gate); err == nil {
			fmt.Print("等待后的中文输出\n")
			os.Exit(0)
		}
		time.Sleep(5 * time.Millisecond)
	}
	os.Exit(2)
}

func TestP0IndependentWaitAndLogRotation(t *testing.T) {
	p := newP0Protocol(t)
	for _, kind := range []string{"process", "terminal"} {
		t.Run(kind, func(t *testing.T) {
			gate := filepath.Join(t.TempDir(), "gate")
			input := map[string]any{"request_id": "p0-wait-" + kind, "command": os.Args[0], "args": []string{"-test.run=^TestP0ProcessHelper$", "--", gate}, "env": map[string]string{"REMOTE_MCP_P0_HELPER": "1"}}
			start := kind + "_start"
			if kind == "terminal" {
				start = "terminal_open"
			} else {
				input["background"] = true
			}
			opened, _ := p.call(start, input, false)
			id := opened["id"].(string)
			arguments := map[string]any{"id": id}
			if kind == "process" {
				arguments["stream"] = "stdout"
			}
			immediate, _ := p.call(kind+"_read", arguments, false)
			if immediate["reason"] != "immediate" {
				t.Fatal(immediate)
			}
			arguments["cursor"] = immediate["next_cursor"]
			arguments["wait_ms"] = 2000
			written := make(chan error, 1)
			go func() { time.Sleep(75 * time.Millisecond); written <- os.WriteFile(gate, []byte("go"), 0600) }()
			out, _ := p.call(kind+"_read", arguments, false)
			if err := <-written; err != nil {
				t.Fatal(err)
			}
			if out["reason"] != "output" || !strings.Contains(out["text"].(string), "等待后的中文输出") {
				t.Fatal("等待没有返回延迟输出", out)
			}
			closeTool := kind + "_stop"
			if kind == "terminal" {
				closeTool = "terminal_close"
			}
			p.call(closeTool, map[string]any{"id": id}, false)
		})
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "private-log-path-012345")
	if err := os.WriteFile(path, []byte("旧代\n"), 0600); err != nil {
		t.Fatal(err)
	}
	opened, _ := p.call("log_open", map[string]any{"path": path}, false)
	id := opened["id"].(string)
	arguments := map[string]any{"id": id, "generation": opened["generation"], "cursor": opened["end_cursor"], "wait_ms": 2000}
	written := make(chan error, 1)
	go func() {
		time.Sleep(75 * time.Millisecond)
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err == nil {
			_, err = f.WriteString("追加中文\n")
			f.Close()
		}
		written <- err
	}()
	out, line := p.call("log_read", arguments, false)
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	if out["reason"] != "output" || out["text"] != "追加中文\n" {
		t.Fatal(out)
	}
	if strings.Contains(line, path) || strings.Contains(line, "追加中文") {
		t.Fatal("日志跟踪泄漏正文")
	}
	if err := os.Rename(path, filepath.Join(dir, "rotated")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("新代中文\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out, _ = p.call("log_read", map[string]any{"id": id, "generation": out["generation"], "cursor": out["next_cursor"]}, false)
	if out["rotated"] != true || out["text"] != "新代中文\n" || out["generation"] == opened["generation"] {
		t.Fatal("轮转没有明确换代", out)
	}
	p.call("log_close", map[string]any{"id": id}, false)
	out, line = p.call("log_open", map[string]any{"path": filepath.Join(dir, "private-missing-log-012345")}, true)
	if out["code"] != "not_found" || !strings.Contains(line, "error_code=not_found") || strings.Contains(line, dir) {
		t.Fatal(out, line)
	}
}
