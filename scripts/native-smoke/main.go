package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"
)

type object = map[string]any

func ensure(ok bool, message string) {
	if !ok {
		panic(message)
	}
}
func check(err error) {
	if err != nil {
		panic("Native operation failed")
	}
}
func number(v any) int64           { n, ok := v.(float64); ensure(ok, "Missing numeric result"); return int64(n) }
func hash(data []byte) string      { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func put(path string, data []byte) { check(os.WriteFile(path, data, 0600)) }
func read(path string) []byte      { data, err := os.ReadFile(path); check(err); return data }
func obj(v any) object             { o, ok := v.(map[string]any); ensure(ok, "Missing object result"); return o }
func rows(v any) []any             { a, ok := v.([]any); ensure(ok, "Missing list result"); return a }
func data(o object) []byte {
	b, err := base64.StdEncoding.DecodeString(o["data_base64"].(string))
	check(err)
	return b
}

type protocol struct {
	url, token, session string
	sequence            int
	client              *http.Client
}

func (p *protocol) rpc(method string, params object, notification bool) object {
	defer func() {
		if value := recover(); value != nil {
			panic(fmt.Sprintf("RPC %s failed: %v", method, value))
		}
	}()
	p.sequence++
	payload := object{"jsonrpc": "2.0", "method": method, "params": params}
	if !notification {
		payload["id"] = p.sequence
	}
	body, err := json.Marshal(payload)
	check(err)
	request, err := http.NewRequest("POST", p.url, bytes.NewReader(body))
	check(err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("MCP-Protocol-Version", "2025-11-25")
	if p.token != "" {
		request.Header.Set("Authorization", "Bearer "+p.token)
	}
	if p.session != "" {
		request.Header.Set("Mcp-Session-Id", p.session)
	}
	response, err := p.client.Do(request)
	check(err)
	defer response.Body.Close()
	ensure(response.StatusCode == 200 || response.StatusCode == 202, "Unexpected protocol HTTP status")
	if session := response.Header.Get("Mcp-Session-Id"); session != "" {
		p.session = session
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024+1))
	check(err)
	ensure(len(raw) <= 2*1024*1024 && utf8.Valid(raw), "Protocol response exceeded bounds or was not UTF-8")
	if len(raw) == 0 {
		return object{}
	}
	var envelope object
	check(json.Unmarshal(raw, &envelope))
	ensure(envelope["error"] == nil, "JSON-RPC error")
	return obj(envelope["result"])
}
func (p *protocol) tool(name string, args object, failed bool) object {
	defer func() {
		if value := recover(); value != nil {
			panic(fmt.Sprintf("Tool %s failed: %v", name, value))
		}
	}()
	result := p.rpc("tools/call", object{"name": name, "arguments": args}, false)
	return decodeToolResult(result, name, failed)
}

// 已有 SDK 错误可能只有文本 JSON；只有预期失败才允许这种协议兼容。
func decodeToolResult(result object, name string, failed bool) object {
	isError, _ := result["isError"].(bool)
	ensure(isError == failed, "Unexpected tool status: "+name)
	if structured, exists := result["structuredContent"]; exists {
		return obj(structured)
	}
	ensure(failed, "Successful tool response lacked structuredContent")
	content := rows(result["content"])
	ensure(len(content) > 0, "Failed tool response lacked content")
	block := obj(content[0])
	text, ok := block["text"].(string)
	ensure(ok && block["type"] == "text" && len(text) <= 65536 && utf8.ValidString(text), "Failed tool text was invalid")
	var output object
	check(json.Unmarshal([]byte(text), &output))
	ensure(output != nil, "Failed tool text was not an object")
	return output
}
func (p *protocol) closeProcess(kind, id string) {
	name := "process_stop"
	if kind == "terminal" {
		name = "terminal_close"
	}
	p.tool(name, object{"id": id}, false)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s := p.tool(kind+"_status", object{"id": id}, false)
		if s["state"] == "exited" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	panic("Resource did not reach an exited state")
}

// boundedBuffer 在捕获时限制输出，而不是等子进程完成后再检查。
type boundedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	if len(data) > b.limit-b.buffer.Len() {
		b.exceeded = true
		return 0, fmt.Errorf("Output exceeded the limit")
	}
	return b.buffer.Write(data)
}
func (b *boundedBuffer) Bytes() []byte { return b.buffer.Bytes() }
func transfer(binary, operation, source, target, url, token string) object {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, operation, "--url", url, source, target)
	command.Env = append(os.Environ(), "REMOTE_MCP_TOKEN="+token)
	stdout, stderr := &boundedBuffer{limit: 65536}, &boundedBuffer{limit: 65536}
	command.Stdout, command.Stderr = stdout, stderr
	check(command.Run())
	ensure(!stdout.exceeded && !stderr.exceeded && utf8.Valid(stdout.Bytes()) && utf8.Valid(stderr.Bytes()), "Transfer output was invalid")
	var summary object
	check(json.Unmarshal(stdout.Bytes(), &summary))
	ensure(summary["ok"] == true, "Transfer failed")
	return summary
}
func oldSmoke(p *protocol, root, binary string) {
	source := filepath.Join(root, "source.ps1")
	target := filepath.Join(root, "uploaded.ps1")
	artifact := filepath.Join(root, "artifact.txt")
	downloaded := filepath.Join(root, "downloaded.txt")
	put(source, []byte("[IO.File]::WriteAllText($args[0], \"remote-mcp-native-result`n\", [Text.UTF8Encoding]::new($false))\n"))
	sent := transfer(binary, "upload", source, target, p.url, p.token)
	ensure(sent["sha256"] == hash(read(source)), "Upload hash mismatch")
	job := p.tool("process_start", object{"request_id": "native-uploaded", "command": "powershell.exe", "args": []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", target, artifact}, "wait_ms": 10000}, false)
	defer p.closeProcess("process", job["id"].(string))
	ensure(job["state"] == "exited" && number(job["exit_code"]) == 0, "Uploaded process failed")
	got := transfer(binary, "download", artifact, downloaded, p.url, p.token)
	expected := []byte("remote-mcp-native-result\n")
	ensure(bytes.Equal(read(downloaded), expected) && got["sha256"] == hash(expected), "Download mismatch")
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	check(err)
	defer listener.Close()
	done := make(chan bool, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			done <- false
			return
		}
		defer connection.Close()
		connection.SetDeadline(time.Now().Add(5 * time.Second))
		in, err := io.ReadAll(io.LimitReader(connection, 1024))
		if err != nil || !bytes.Equal(in, []byte("request\x00\xff")) {
			done <- false
			return
		}
		_, err = connection.Write([]byte("response\x00\xff"))
		done <- err == nil
	}()
	forwarding := p.tool("port_forward_create", object{"request_id": "native-forward", "target_host": "127.0.0.1", "target_port": listener.Addr().(*net.TCPAddr).Port}, false)
	defer func() {
		stopped := p.tool("port_forward_stop", object{"id": forwarding["id"]}, false)
		ensure(stopped["state"] == "stopped", "Forwarding cleanup failed")
	}()
	connection, err := net.DialTimeout("tcp", forwarding["listen_address"].(string), 5*time.Second)
	check(err)
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(5 * time.Second))
	_, err = connection.Write([]byte("request\x00\xff"))
	check(err)
	check(connection.(*net.TCPConn).CloseWrite())
	response, err := io.ReadAll(io.LimitReader(connection, 1024))
	check(err)
	ensure(bytes.Equal(response, []byte("response\x00\xff")), "Forwarding bytes changed")
	ensure(<-done, "Forwarding fixture failed")
}
func files(p *protocol, root string) {
	directory := filepath.Join(root, "files")
	check(os.Mkdir(directory, 0700))
	path := filepath.Join(directory, "private-native-path.txt")
	original := []byte("中文首行\r\nprivate-native-query\r\n末尾无换行")
	put(path, original)
	listing := p.tool("file_list", object{"path": directory, "limit": 1}, false)
	ensure(len(rows(listing["entries"])) == 1, "Directory listing failed")
	r := p.tool("file_read", object{"path": path, "start_line": 2, "line_count": 1}, false)
	ensure(r["text"] == "private-native-query\r\n" && r["sha256"] == hash(original), "File read bytes changed")
	search := p.tool("file_search", object{"path": directory, "query": "private-native-query"}, false)
	ensure(len(rows(search["matches"])) == 1 && number(obj(rows(search["matches"])[0])["line"]) == 2, "File search failed")
	args := object{"path": path, "expected_sha256": r["sha256"], "edits": []object{{"start_line": 2, "delete_count": 1, "text": "新的中文行\r\n"}}}
	patched := p.tool("file_patch", args, false)
	expected := []byte("中文首行\r\n新的中文行\r\n末尾无换行")
	ensure(bytes.Equal(read(path), expected) && patched["sha256"] == hash(expected), "Patch changed unedited bytes")
	conflict := p.tool("file_patch", args, true)
	ensure(conflict["code"] == "conflict" && bytes.Equal(read(path), expected), "Conflict damaged target")
	names, err := filepath.Glob(filepath.Join(directory, ".remote-mcp-patch-*"))
	check(err)
	ensure(len(names) == 0, "Patch retained temporary files")
}
func waitFile(path string) {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	panic("Fixture gate timed out")
}
func waiting(p *protocol, root, self string) {
	for _, kind := range []string{"process", "terminal"} {
		gate := filepath.Join(root, kind+"-gate")
		ready := filepath.Join(root, kind+"-ready")
		exit := filepath.Join(root, kind+"-exit")
		args := object{"request_id": "native-wait-" + kind, "command": self, "args": []string{"worker", gate, ready, exit}}
		start := "process_start"
		if kind == "process" {
			args["background"] = true
		} else {
			start = "terminal_open"
		}
		resource := p.tool(start, args, false)
		id := resource["id"].(string)
		func() {
			defer p.closeProcess(kind, id)
			waitFile(ready)
			a := object{"id": id}
			if kind == "process" {
				a["stream"] = "stdout"
			}
			immediate := p.tool(kind+"_read", a, false)
			ensure(immediate["reason"] == "immediate", "Default read did not remain immediate")
			a["cursor"] = immediate["next_cursor"]
			a["wait_ms"] = 150
			count := len(data(immediate))
			deadline := time.Now().Add(5 * time.Second)
			for {
				ensure(time.Now().Before(deadline), "Terminal did not become idle")
				before := time.Now()
				empty := p.tool(kind+"_read", a, false)
				if empty["reason"] == "timeout" {
					ensure(time.Since(before) >= 100*time.Millisecond && len(data(empty)) == 0, "Waiting timeout failed")
					break
				}
				ensure(kind == "terminal" && empty["reason"] == "output", "Unexpected idle read result")
				count += len(data(empty))
				ensure(count <= 65536, "Terminal startup exceeded bounds")
				a["cursor"] = empty["next_cursor"]
			}
			timer := time.AfterFunc(100*time.Millisecond, func() { put(gate, []byte("go")) })
			defer timer.Stop()
			a["wait_ms"] = 3000
			output := []byte{}
			deadline = time.Now().Add(5 * time.Second)
			for !bytes.Contains(output, []byte("private-native-wait-output")) {
				ensure(time.Now().Before(deadline), "Waiting output was absent")
				r := p.tool(kind+"_read", a, false)
				ensure(r["reason"] == "output", "Waiting output did not wake")
				output = append(output, data(r)...)
				ensure(len(output) <= 65536, "Waiting output exceeded bounds")
				a["cursor"] = r["next_cursor"]
			}
			put(exit, []byte("exit"))
			deadline = time.Now().Add(5 * time.Second)
			for {
				ensure(time.Now().Before(deadline), "Waiting exit was absent")
				r := p.tool(kind+"_read", a, false)
				a["cursor"] = r["next_cursor"]
				if r["reason"] == "exit" {
					ensure(len(data(r)) == 0, "Exit returned unread data")
					break
				}
				ensure(r["reason"] == "output", "Waiting exit failed")
			}
			status := p.tool(kind+"_status", object{"id": id}, false)
			ensure(status["state"] == "exited" && number(status["exit_code"]) == 0, "Waiting fixture did not exit normally")
		}()
	}
}
func logs(p *protocol, root string) {
	path := filepath.Join(root, "private-native-log")
	put(path, []byte("旧代\n"))
	opened := p.tool("log_open", object{"path": path}, false)
	defer func() {
		ensure(p.tool("log_close", object{"id": opened["id"]}, false)["closed"] == true, "Log cleanup failed")
	}()
	timer := time.AfterFunc(100*time.Millisecond, func() {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
		check(err)
		_, err = f.Write([]byte("追加中文\n"))
		check(err)
		check(f.Close())
	})
	defer timer.Stop()
	r := p.tool("log_read", object{"id": opened["id"], "generation": opened["generation"], "cursor": opened["end_cursor"], "wait_ms": 3000}, false)
	ensure(r["reason"] == "output" && r["text"] == "追加中文\n", "Log append wait failed")
	check(os.Rename(path, filepath.Join(root, "old-log")))
	put(path, []byte("新代中文\n"))
	rotated := p.tool("log_read", object{"id": opened["id"], "generation": r["generation"], "cursor": r["next_cursor"]}, false)
	ensure(rotated["rotated"] == true && rotated["generation"] != r["generation"] && rotated["text"] == "新代中文\n", "Log rotation failed")
	put(path, []byte("短\n"))
	short := p.tool("log_read", object{"id": opened["id"], "generation": rotated["generation"], "cursor": rotated["next_cursor"]}, false)
	ensure(short["truncated"] == true && short["rotated"] != true && short["generation"] != rotated["generation"] && short["text"] == "短\n", "Log truncation failed")
	idle := p.tool("log_read", object{"id": opened["id"], "generation": short["generation"], "cursor": short["next_cursor"], "wait_ms": 150}, false)
	ensure(idle["reason"] == "timeout" && len(data(idle)) == 0, "Log timeout failed")
}
func inspection(p *protocol, pid int) {
	e := p.tool("environment_inspect", object{"runtimes": []string{"go", "python"}}, false)
	ensure(e["ok"] == true && e["os"] == "windows" && e["arch"] == runtime.GOARCH, "Native environment mismatch")
	process := p.tool("process_inspect", object{"root_pid": pid}, false)
	found := false
	for _, r := range rows(process["processes"]) {
		if number(obj(r)["pid"]) == int64(pid) {
			ensure(number(obj(r)["ppid"]) == int64(os.Getpid()), "Native parent-child PID relationship was missing")
			found = true
		}
	}
	ensure(found, "Native process PID was missing")
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	check(err)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })}
	go server.Serve(listener)
	defer server.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	l := p.tool("network_listeners", object{"port": port, "pid": os.Getpid()}, false)
	found = false
	for _, r := range rows(l["listeners"]) {
		row := obj(r)
		for _, owner := range rows(row["pids"]) {
			if number(owner) == int64(os.Getpid()) && number(row["port"]) == int64(port) {
				found = true
			}
		}
	}
	ensure(found, "Windows listening PID was missing")
	dns := p.tool("network_probe", object{"mode": "dns", "target": "localhost"}, false)
	ensure(dns["ok"] == true && len(rows(dns["addresses"])) > 0, "Native DNS probe failed")
	response := p.tool("network_probe", object{"mode": "http", "target": fmt.Sprintf("http://127.0.0.1:%d/private-native-url", port)}, false)
	ensure(response["ok"] == true && number(response["status_code"]) == 204, "Native HTTP probe failed")
	tls := p.tool("network_probe", object{"mode": "tls", "target": fmt.Sprintf("127.0.0.1:%d", port)}, true)
	ensure(tls["code"] == "tls_failed" && tls["failed_stage"] == "tls" && len(rows(tls["stages"])) >= 3, "Native TLS stages were lost")
	closed, err := net.Listen("tcp4", "127.0.0.1:0")
	check(err)
	refused := closed.Addr().String()
	check(closed.Close())
	failure := p.tool("network_probe", object{"mode": "tcp", "target": refused}, true)
	ensure(failure["code"] == "tcp_failed" && failure["failed_stage"] == "tcp" && len(rows(failure["stages"])) >= 2, "Native TCP stages were lost")
}
func round(binaryDir, root, self, kind string, tokenMode bool) {
	token := ""
	if tokenMode {
		token = "fictional-native-" + filepath.Base(root)
	}
	logPath := filepath.Join(root, "server.log")
	log, err := os.Create(logPath)
	check(err)
	command := exec.Command(filepath.Join(binaryDir, "remote-mcp.exe"), "--listen", "127.0.0.1:0")
	command.Env = append(os.Environ(), "REMOTE_MCP_TOKEN="+token)
	command.Stderr = log
	stdout, err := command.StdoutPipe()
	check(err)
	check(command.Start())
	done := make(chan error, 1)
	stopped, logClosed := false, false
	defer func() {
		if !stopped {
			if err := command.Process.Kill(); err != nil {
				panic("Service cleanup failed")
			}
			<-done
		}
		if !logClosed {
			check(log.Close())
		}
	}()
	go func() { done <- command.Wait() }()
	startup := make(chan object, 1)
	go func() {
		scanner := bufio.NewScanner(io.LimitReader(stdout, 65537))
		scanner.Buffer(make([]byte, 4096), 65537)
		raw := []byte{}
		for scanner.Scan() {
			raw = append(raw, scanner.Bytes()...)
			raw = append(raw, '\n')
			if len(raw) > 65536 || !utf8.Valid(raw) {
				startup <- nil
				return
			}
			if json.Valid(raw) {
				var configuration object
				if json.Unmarshal(raw, &configuration) != nil {
					startup <- nil
				} else {
					startup <- configuration
				}
				return
			}
		}
		startup <- nil
	}()
	var config object
	select {
	case config = <-startup:
	case <-time.After(15 * time.Second):
		panic("Service startup timed out")
	}
	ensure(config != nil, "Service startup failed")
	entry := obj(obj(config["mcp"])["remote-mcp"])
	ensure(entry["oauth"] == false && entry["type"] == "remote", "Startup configuration mismatch")
	if tokenMode {
		ensure(obj(entry["headers"])["Authorization"] == "Bearer "+token, "Token startup mismatch")
	} else {
		ensure(entry["headers"] == nil, "Anonymous startup contained headers")
	}
	client := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	p := &protocol{url: entry["url"].(string), token: token, client: client}
	if tokenMode {
		request, err := http.NewRequest("POST", p.url, strings.NewReader("{}"))
		check(err)
		response, err := client.Do(request)
		check(err)
		check(response.Body.Close())
		ensure(response.StatusCode == 401, "Missing token was accepted")
	}
	initialized := p.rpc("initialize", object{"protocolVersion": "2025-11-25", "capabilities": object{}, "clientInfo": object{"name": "independent-windows-native", "version": "1"}}, false)
	ensure(initialized["protocolVersion"] == "2025-11-25", "Protocol mismatch")
	p.rpc("notifications/initialized", object{}, true)
	ensure(len(rows(p.rpc("tools/list", object{}, false)["tools"])) == 40, "Registered tool count mismatch")
	if kind == "legacy" {
		oldSmoke(p, root, filepath.Join(binaryDir, "remote-mcp-transfer.exe"))
	} else {
		files(p, root)
		waiting(p, root, self)
		logs(p, root)
		inspection(p, command.Process.Pid)
	}
	select {
	case <-done:
		stopped = true
		panic("Service exited before cleanup")
	default:
	}
	check(command.Process.Kill())
	err = <-done
	stopped = true
	ensure(err != nil && command.ProcessState.ExitCode() == 1, "Unexpected service termination")
	check(log.Close())
	logClosed = true
	content := read(logPath)
	ensure(utf8.Valid(content), "Ordinary log was not UTF-8")
	for _, forbidden := range []string{token, root, "private-native-query", "新的中文行", "追加中文", "新代中文", "private-native-url", "private-native-wait-output"} {
		if forbidden != "" {
			ensure(!bytes.Contains(content, []byte(forbidden)), "Ordinary log leaked credentials or content")
		}
	}
	if kind == "p0" {
		for _, code := range []string{"conflict", "tls_failed", "tcp_failed"} {
			ensure(bytes.Contains(content, []byte("error_code="+code)), "Failure diagnostics were missing")
		}
	}
}
func main() {
	if len(os.Args) > 1 && os.Args[1] == "worker" {
		put(os.Args[3], []byte("ready"))
		waitFile(os.Args[2])
		fmt.Println("private-native-wait-output")
		waitFile(os.Args[4])
		return
	}
	defer func() {
		if value := recover(); value != nil {
			fmt.Fprintln(os.Stderr, value)
			os.Exit(1)
		}
	}()
	ensure(runtime.GOOS == "windows" && len(os.Args) == 2, "Windows runner arguments were invalid")
	base := os.Args[1]
	self, err := os.Executable()
	check(err)
	results := []object{}
	for _, kind := range []string{"legacy", "p0"} {
		for _, enabled := range []bool{true, false} {
			root, err := os.MkdirTemp(base, "round-")
			check(err)
			func() { defer func() { check(os.RemoveAll(root)) }(); round(base, root, self, kind, enabled) }()
			results = append(results, object{"suite": kind, "authentication": map[bool]string{true: "token", false: "anonymous"}[enabled], "ok": true, "service_shutdown": "terminate_process"})
		}
	}
	check(json.NewEncoder(os.Stdout).Encode(object{"ok": true, "platform": runtime.GOOS, "arch": runtime.GOARCH, "protocol_version": "2025-11-25", "gui_exercised": false, "native_conpty": true, "inspection": true, "waiting_timeout": true, "waiting_exit": true, "log_rotation": true, "log_truncation": true, "hash_patch": true, "rounds": results}))
}
