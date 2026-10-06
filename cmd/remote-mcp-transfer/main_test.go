package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"remote-mcp/internal/config"
	"remote-mcp/internal/server"
	"remote-mcp/internal/transfer"
)

func endpoint(t *testing.T) string {
	t.Helper()
	manager, err := transfer.New(transfer.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close() })
	server := mcp.NewServer(&mcp.Implementation{Name: "文件测试", Version: "1"}, &mcp.ServerOptions{SupportedProtocolVersions: []string{"2025-11-25"}})
	manager.Register(server)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{JSONResponse: true})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "未授权", 401)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	t.Setenv("REMOTE_MCP_TOKEN", "test-token")
	return ts.URL
}
func execute(t *testing.T, args ...string) summary {
	t.Helper()
	var out, stderr bytes.Buffer
	if status := run(context.Background(), args, &out, &stderr); status != 0 {
		t.Fatalf("命令失败: %s %s", out.String(), stderr.String())
	}
	var result summary
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || !result.OK {
		t.Fatalf("摘要无效: %s %v", out.String(), err)
	}
	return result
}
func TestCLITransfer(t *testing.T) {
	url := endpoint(t)
	dir := t.TempDir()
	for _, data := range [][]byte{nil, []byte("中文内容\x00\xff"), bytes.Repeat([]byte{1, 2, 3, 4}, 200000)} {
		source := filepath.Join(dir, "source")
		remote := filepath.Join(dir, "remote")
		target := filepath.Join(dir, "target")
		if err := os.WriteFile(source, data, 0600); err != nil {
			t.Fatal(err)
		}
		uploaded := execute(t, "upload", "--url", url, "--overwrite", source, remote)
		downloaded := execute(t, "download", "--url", url, "--overwrite", remote, target)
		sum := sha256.Sum256(data)
		if uploaded.SHA256 != hex.EncodeToString(sum[:]) || downloaded.SHA256 != uploaded.SHA256 || uploaded.Size != int64(len(data)) {
			t.Fatal("传输校验不一致")
		}
		got, err := os.ReadFile(target)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatal("落盘字节不一致")
		}
	}
	files, _ := filepath.Glob(filepath.Join(dir, ".remote-mcp-*"))
	if len(files) > 0 {
		t.Fatalf("临时文件残留: %v", files)
	}
}
func TestCLIDownloadRefusesOverwrite(t *testing.T) {
	url := endpoint(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	target := filepath.Join(dir, "target")
	os.WriteFile(source, []byte("new"), 0600)
	os.WriteFile(target, []byte("old"), 0600)
	var out bytes.Buffer
	if status := run(context.Background(), []string{"download", "--url", url, source, target}, &out, io.Discard); status == 0 {
		t.Fatal("未拒绝已有目标")
	}
	got, _ := os.ReadFile(target)
	if string(got) != "old" {
		t.Fatal("旧文件被破坏")
	}
}
func TestConnectionErrorsDoNotLeakToken(t *testing.T) {
	t.Setenv("REMOTE_MCP_TOKEN", "credential-should-not-appear")
	var out, stderr bytes.Buffer
	status := run(context.Background(), []string{"upload", "--url", "https://credential-should-not-appear@localhost/mcp", "a", "b"}, &out, &stderr)
	if status == 0 || bytes.Contains(out.Bytes(), []byte("credential-should-not-appear")) || bytes.Contains(stderr.Bytes(), []byte("credential-should-not-appear")) {
		t.Fatal("错误输出泄漏凭据")
	}
}

func TestArgumentErrorsDoNotLeakToken(t *testing.T) {
	const token = "credential-review-placeholder"
	t.Setenv("REMOTE_MCP_TOKEN", token)
	t.Setenv("REMOTE_MCP_URL", "https://"+token+"@localhost/mcp")
	for _, args := range [][]string{
		{"upload", "--timeout", token, "a", "b"},
		{"upload", "--" + token, "a", "b"},
		{"download", "--overwrite=" + token, "a", "b"},
		{"upload", "--help"},
	} {
		var out, stderr bytes.Buffer
		status := run(context.Background(), args, &out, &stderr)
		if bytes.Contains(out.Bytes(), []byte(token)) || bytes.Contains(stderr.Bytes(), []byte(token)) {
			t.Fatal("参数错误或帮助输出泄漏凭据")
		}
		if args[1] == "--help" {
			if status != 0 || !bytes.Contains(stderr.Bytes(), []byte("-url")) {
				t.Fatal("帮助应成功显示选项")
			}
		} else if status == 0 {
			t.Fatal("错误参数应失败")
		}
	}
}
func TestTLSAndRedirect(t *testing.T) {
	called := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 302) }))
	defer origin.Close()
	client, err := clientFor(origin.URL, "secret", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Get(origin.URL); err == nil || called {
		t.Fatal("凭据请求不应跟随重定向")
	}
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer tlsServer.Close()
	client, err = clientFor(tlsServer.URL, "secret", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Get(tlsServer.URL); err == nil {
		t.Fatal("不得忽略未信任证书")
	}
}

// TestLargeStreaming 使用实际 MCP HTTP 往返验证大文件与堆内存上限，按需执行。
func TestLargeStreaming(t *testing.T) {
	if os.Getenv("RUN_LARGE_TRANSFER") != "1" {
		t.Skip("设置 RUN_LARGE_TRANSFER=1 运行 257 MiB 双向传输")
	}
	url := endpoint(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	remote := filepath.Join(dir, "remote")
	target := filepath.Join(dir, "target")
	file, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(257 << 20); err != nil {
		t.Fatal(err)
	}
	file.Close()
	runtime.GC()
	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)
	var peak atomic.Uint64
	peak.Store(baseline.HeapAlloc)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				var stats runtime.MemStats
				runtime.ReadMemStats(&stats)
				if stats.HeapAlloc > peak.Load() {
					peak.Store(stats.HeapAlloc)
				}
			}
		}
	}()
	var stopOnce sync.Once
	stopSampling := func() { stopOnce.Do(func() { close(stop); <-done }) }
	defer stopSampling()
	uploaded := execute(t, "upload", "--url", url, source, remote)
	downloaded := execute(t, "download", "--url", url, remote, target)
	stopSampling()
	if uploaded.Size != 257<<20 || downloaded.Size != uploaded.Size || downloaded.SHA256 != uploaded.SHA256 {
		t.Fatal("大文件完整性错误")
	}
	file, err = os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	hash, err := transfer.HashFile(context.Background(), file)
	if err != nil || hash != uploaded.SHA256 {
		t.Fatal("本地下载哈希错误")
	}
	increase := peak.Load() - baseline.HeapAlloc
	t.Logf("257 MiB 双向 MCP 传输：峰值堆增量 %.2f MiB", float64(increase)/(1<<20))
	if increase > 128<<20 {
		t.Fatalf("堆内存增量过高: %d", increase)
	}
}

type fakeCaller func(context.Context, *mcp.CallToolParams) (*mcp.CallToolResult, error)

func (f fakeCaller) CallTool(ctx context.Context, p *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	return f(ctx, p)
}
func TestDownloadFailuresPreserveTarget(t *testing.T) {
	for _, mode := range []string{"checksum", "close_changed", "late_conflict"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "target")
			if mode != "late_conflict" {
				os.WriteFile(target, []byte("old"), 0600)
			}
			good := []byte("new")
			sum := sha256.Sum256(good)
			digest := hex.EncodeToString(sum[:])
			closed := false
			fake := fakeCaller(func(_ context.Context, p *mcp.CallToolParams) (*mcp.CallToolResult, error) {
				result := transfer.Result{OK: true, ID: "fake", Size: 3, SHA256: digest, ChunkSize: 256 << 10, State: "active"}
				switch p.Name {
				case "download_read":
					result.Offset = 3
					result.Data = "bmV3"
					if mode == "checksum" {
						result.Data = "YmFk"
					}
				case "download_close":
					closed = true
					if mode == "close_changed" {
						result.OK = false
						result.Code = "SOURCE_CHANGED"
						result.Message = "源文件已变化"
					}
					if mode == "late_conflict" {
						os.WriteFile(target, []byte("old"), 0600)
					}
				}
				return &mcp.CallToolResult{StructuredContent: result, IsError: !result.OK}, nil
			})
			if _, err := download(context.Background(), fake, "source", target, mode != "late_conflict", io.Discard); err == nil {
				t.Fatal("无效下载应失败")
			}
			actual, _ := os.ReadFile(target)
			if string(actual) != "old" || !closed {
				t.Fatal("失败下载损坏目标或未关闭远端句柄")
			}
			files, _ := filepath.Glob(filepath.Join(dir, ".remote-mcp-*"))
			if len(files) > 0 {
				t.Fatal("失败下载残留临时文件")
			}
		})
	}
}
func TestUploadIDAcrossMCPSessions(t *testing.T) {
	url := endpoint(t)
	httpClient, err := clientFor(url, "test-token", "")
	if err != nil {
		t.Fatal(err)
	}
	defer httpClient.CloseIdleConnections()
	connect := func() *mcp.ClientSession {
		client := mcp.NewClient(&mcp.Implementation{Name: "跨会话测试", Version: "1"}, nil)
		s, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: url, HTTPClient: httpClient, DisableStandaloneSSE: true}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	first := connect()
	path := filepath.Join(t.TempDir(), "file")
	sum := sha256.Sum256(nil)
	input := transfer.UploadInput{RequestID: "same-request", Path: path, SHA256: hex.EncodeToString(sum[:])}
	created, err := invoke(context.Background(), first, "upload_create", input)
	if err != nil {
		t.Fatal(err)
	}
	first.Close()
	second := connect()
	defer second.Close()
	replayed, err := invoke(context.Background(), second, "upload_create", input)
	if err != nil || replayed.ID != created.ID {
		t.Fatalf("跨会话幂等失败: %+v %v", replayed, err)
	}
	if _, err := invoke(context.Background(), second, "upload_finish", transfer.IDInput{ID: created.ID}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() != 0 {
		t.Fatal("跨会话未发布文件")
	}
}

func TestAnonymousCLITransfer(t *testing.T) {
	t.Setenv("REMOTE_MCP_TOKEN", "")
	app, err := server.New(config.Default(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	var requests atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if _, exists := r.Header["Authorization"]; exists {
			t.Error("匿名辅助命令不应发送 Authorization")
			http.Error(w, "意外凭据", 400)
			return
		}
		app.Handler.ServeHTTP(w, r)
	}))
	defer ts.Close()
	dir := t.TempDir()
	source, remote, target := filepath.Join(dir, "source"), filepath.Join(dir, "remote"), filepath.Join(dir, "target")
	data := bytes.Repeat([]byte("匿名文件\x00\xff"), 40000)
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	uploaded := execute(t, "upload", "--url", ts.URL+"/mcp", source, remote)
	downloaded := execute(t, "download", "--url", ts.URL+"/mcp", remote, target)
	got, err := os.ReadFile(target)
	sum := sha256.Sum256(data)
	if err != nil || !bytes.Equal(got, data) || uploaded.SHA256 != hex.EncodeToString(sum[:]) || downloaded.SHA256 != uploaded.SHA256 || downloaded.Size != int64(len(data)) || requests.Load() == 0 {
		t.Fatal("匿名双向传输校验失败")
	}
}

func TestCLIRejectsExplicitInvalidTokenFile(t *testing.T) {
	t.Setenv("REMOTE_MCP_TOKEN", "fictional-token")
	file := filepath.Join(t.TempDir(), "empty-token")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", file, filepath.Join(t.TempDir(), "missing")} {
		var out, diagnostic bytes.Buffer
		if run(context.Background(), []string{"upload", "--url", "http://127.0.0.1:1/mcp", "--token-file", path, "a", "b"}, &out, &diagnostic) == 0 || !bytes.Contains(bytes.ToLower(diagnostic.Bytes()), []byte("token file")) {
			t.Fatal("显式凭据文件错误应在连接前报告", diagnostic.String())
		}
	}
}
