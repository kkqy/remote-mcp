package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
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
	"unicode/utf8"

	"remote-mcp/internal/config"
	"remote-mcp/internal/gui"
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
	wantCount := 33
	if gui.Supported {
		wantCount += 7
	}
	if len(tools) != wantCount {
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
	out, _ := p.call("environment_inspect", map[string]any{"runtimes": []string{"go"}}, false)
	if out["os"] == "" || out["arch"] == "" || out["ok"] != true {
		t.Fatal(out)
	}
	runtimes := out["runtimes"].([]any)
	if len(runtimes) != 1 || runtimes[0].(map[string]any)["name"] != "go" {
		t.Fatal("显式运行时子集没有按请求返回", out)
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

// 子进程只在明确测试环境中运行，三个闸门分开确认就绪、输出和正常退出。
func TestP0ProcessHelper(t *testing.T) {
	if os.Getenv("REMOTE_MCP_P0_HELPER") != "1" {
		return
	}
	args := os.Args[len(os.Args)-3:]
	until := time.Now().Add(20 * time.Second)
	wait := func(path string) {
		for time.Now().Before(until) {
			if _, err := os.Stat(path); err == nil {
				return
			} else if !os.IsNotExist(err) {
				os.Exit(2)
			}
			time.Sleep(5 * time.Millisecond)
		}
		os.Exit(2)
	}
	if err := os.WriteFile(args[1], []byte("ready"), 0600); err != nil {
		os.Exit(2)
	}
	wait(args[0])
	fmt.Print("等待后的中文输出\n")
	wait(args[2])
	os.Exit(0)
}

// 原始字节是权威数据；单次 text 可能因 UTF-8 分块而带替代字符。
func p0ReadBytes(t *testing.T, out map[string]any, cursor float64) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(out["data_base64"].(string))
	if err != nil || out["valid_utf8"] != utf8.Valid(data) || out["truncated"] != false || out["start_cursor"] != cursor || out["next_cursor"] != cursor+float64(len(data)) || out["end_cursor"].(float64) < out["next_cursor"].(float64) {
		t.Fatal("输出字节或连续游标不符", out, err)
	}
	return data
}

func TestP0IndependentWaitAndLogRotation(t *testing.T) {
	for _, kind := range []string{"process", "terminal"} {
		t.Run(kind, func(t *testing.T) {
			// 每个子测试持有自己的 t、协议会话与清理，失败不会对父 t 调用 Fatal。
			p := newP0Protocol(t)
			dir := t.TempDir()
			gate, ready, exit := filepath.Join(dir, "gate"), filepath.Join(dir, "ready"), filepath.Join(dir, "exit")
			input := map[string]any{"request_id": "p0-wait-" + kind, "command": os.Args[0], "args": []string{"-test.run=^TestP0ProcessHelper$", "--", gate, ready, exit}, "env": map[string]string{"REMOTE_MCP_P0_HELPER": "1"}}
			start := kind + "_start"
			if kind == "terminal" {
				start = "terminal_open"
			} else {
				input["background"] = true
			}
			opened, _ := p.call(start, input, false)
			id := opened["id"].(string)
			closeTool := kind + "_stop"
			if kind == "terminal" {
				closeTool = "terminal_close"
			}
			t.Cleanup(func() { p.call(closeTool, map[string]any{"id": id}, false) })
			deadline := time.Now().Add(5 * time.Second)
			for {
				if !time.Now().Before(deadline) {
					t.Fatal("等待夹具没有在期限内就绪")
				}
				if _, err := os.Stat(ready); err == nil {
					break
				} else if !os.IsNotExist(err) {
					t.Fatal(err)
				}
				time.Sleep(10 * time.Millisecond)
			}
			arguments := map[string]any{"id": id}
			if kind == "process" {
				arguments["stream"] = "stdout"
			}
			immediate, _ := p.call(kind+"_read", arguments, false)
			if immediate["reason"] != "immediate" {
				t.Fatal(immediate)
			}
			marker := []byte("等待后的中文输出")
			startup := p0ReadBytes(t, immediate, 0)
			cursor := immediate["next_cursor"].(float64)
			arguments["wait_ms"] = 150
			deadline = time.Now().Add(5 * time.Second)
			for calls := 0; ; calls++ {
				if calls >= 128 || !time.Now().Before(deadline) || len(startup) > 65536 || bytes.Contains(startup, marker) {
					t.Fatal("输出闸门闭合时未得到有界空等待")
				}
				arguments["cursor"] = cursor
				waiting := time.Now()
				out, _ := p.call(kind+"_read", arguments, false)
				data := p0ReadBytes(t, out, cursor)
				if !time.Now().Before(deadline) {
					t.Fatal("初始化输出消费超过期限")
				}
				if out["reason"] == "timeout" {
					if time.Since(waiting) < 100*time.Millisecond || len(data) != 0 || out["state"] != "running" {
						t.Fatal("闸门闭合时没有真实等待超时", out)
					}
					break
				}
				if kind != "terminal" || out["reason"] != "output" || len(data) == 0 {
					t.Fatal("初始化阶段出现业务输出或提前退出", out)
				}
				startup = append(startup, data...)
				cursor = out["next_cursor"].(float64)
			}
			written := make(chan struct{})
			var writeErr error
			timer := time.AfterFunc(100*time.Millisecond, func() {
				writeErr = os.WriteFile(gate, []byte("go"), 0600)
				close(written)
			})
			t.Cleanup(func() {
				if !timer.Stop() {
					<-written
				}
			})
			// 单字节读取刻意跨越中文 UTF-8 边界，不依赖每个响应的 text/valid_utf8。
			arguments["limit"], arguments["wait_ms"] = 1, 2000
			deadline = time.Now().Add(5 * time.Second)
			var output []byte
			for calls := 0; !bytes.Contains(output, marker); calls++ {
				if calls >= 128 || !time.Now().Before(deadline) {
					t.Fatal("没有在有界读取中得到完整中文业务输出")
				}
				arguments["cursor"] = cursor
				out, _ := p.call(kind+"_read", arguments, false)
				data := p0ReadBytes(t, out, cursor)
				if out["reason"] != "output" || len(data) != 1 || len(output)+len(data) > 65536 || !time.Now().Before(deadline) {
					t.Fatal("延迟输出等待不符或夹具提前退出", out)
				}
				output = append(output, data...)
				cursor = out["next_cursor"].(float64)
			}
			<-written
			if writeErr != nil {
				t.Fatal(writeErr)
			}
			status, _ := p.call(kind+"_status", map[string]any{"id": id}, false)
			if status["state"] != "running" {
				t.Fatal("完整业务输出确认前夹具已经退出", status)
			}
			if err := os.WriteFile(exit, []byte("exit"), 0600); err != nil {
				t.Fatal(err)
			}
			delete(arguments, "limit")
			deadline = time.Now().Add(5 * time.Second)
			for calls := 0; ; calls++ {
				if calls >= 128 || !time.Now().Before(deadline) {
					t.Fatal("没有在期限内读取夹具正常退出")
				}
				arguments["cursor"] = cursor
				out, _ := p.call(kind+"_read", arguments, false)
				data := p0ReadBytes(t, out, cursor)
				if len(output)+len(data) > 65536 || !time.Now().Before(deadline) {
					t.Fatal("退出尾部输出超过限额")
				}
				output = append(output, data...)
				cursor = out["next_cursor"].(float64)
				if out["reason"] == "exit" {
					if len(data) != 0 || out["state"] != "exited" {
						t.Fatal("退出时仍有未读取字节", out)
					}
					break
				}
				if out["reason"] != "output" || len(data) == 0 {
					t.Fatal("输出闸门打开后退出等待不符", out)
				}
			}
			status, _ = p.call(kind+"_status", map[string]any{"id": id}, false)
			if status["state"] != "exited" || status["exit_code"] != float64(0) || bytes.Count(output, marker) != 1 || !utf8.Valid(output) {
				t.Fatal("夹具未正常退出或中文业务输出重复", status)
			}
		})
	}
	p := newP0Protocol(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "private-log-path-012345")
	if err := os.WriteFile(path, []byte("旧代\n"), 0600); err != nil {
		t.Fatal(err)
	}
	opened, _ := p.call("log_open", map[string]any{"path": path}, false)
	id := opened["id"].(string)
	t.Cleanup(func() { p.call("log_close", map[string]any{"id": id}, false) })
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
