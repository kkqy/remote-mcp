package transfer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

var ctx = context.Background()

func manager(t *testing.T) *Manager {
	t.Helper()
	m, err := New(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	return m
}
func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func create(t *testing.T, m *Manager, path string, data []byte, overwrite bool) Result {
	t.Helper()
	r := m.UploadCreate(ctx, UploadInput{RequestID: newID(), Path: path, Size: int64(len(data)), SHA256: digest(data), Overwrite: overwrite})
	if !r.OK {
		t.Fatal(r)
	}
	return r
}
func writeAll(t *testing.T, m *Manager, r Result, data []byte) {
	t.Helper()
	for offset := 0; offset < len(data); {
		end := min(offset+r.ChunkSize, len(data))
		result := m.UploadWrite(ctx, WriteInput{ID: r.ID, Offset: int64(offset), Data: base64.StdEncoding.EncodeToString(data[offset:end])})
		if !result.OK {
			t.Fatal(result)
		}
		offset = end
	}
}
func noTemps(t *testing.T, dir string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, ".remote-mcp-*"))
	if err != nil || len(files) != 0 {
		t.Fatalf("临时文件未清理: %v %v", files, err)
	}
}
func TestRoundTrip(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("中文测试\x00\xff"), bytes.Repeat([]byte{0, 1, 2, 255}, 200000)} {
		t.Run(string(rune('a'+len(data)%26)), func(t *testing.T) {
			m := manager(t)
			dir := t.TempDir()
			path := filepath.Join(dir, "file")
			u := create(t, m, path, data, false)
			writeAll(t, m, u, data)
			r := m.UploadFinish(ctx, IDInput{u.ID})
			if !r.OK || r.State != "completed" {
				t.Fatal(r)
			}
			if !m.UploadFinish(ctx, IDInput{u.ID}).OK {
				t.Fatal("完成重试失败")
			}
			noTemps(t, dir)
			actual, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(actual, data) {
				t.Fatalf("上传数据不同: %v", err)
			}
			d := m.DownloadOpen(ctx, PathInput{path})
			if !d.OK || d.SHA256 != digest(data) {
				t.Fatal(d)
			}
			var out []byte
			var offset int64
			for {
				r := m.DownloadRead(ctx, ReadInput{d.ID, offset, d.ChunkSize})
				if !r.OK {
					t.Fatal(r)
				}
				block, err := base64.StdEncoding.DecodeString(r.Data)
				if err != nil {
					t.Fatal(err)
				}
				out = append(out, block...)
				offset = r.Offset
				if r.EOF {
					break
				}
			}
			if !bytes.Equal(out, data) {
				t.Fatal("下载数据不同")
			}
			if !m.DownloadClose(ctx, IDInput{d.ID}).OK || !m.DownloadClose(ctx, IDInput{d.ID}).OK {
				t.Fatal("关闭下载失败")
			}
		})
	}
}
func TestUploadValidationAndCleanup(t *testing.T) {
	m := manager(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	data := []byte("new")
	r := create(t, m, path, data, false)
	for _, tt := range []struct {
		input WriteInput
		code  string
	}{
		{WriteInput{r.ID, 1, "YQ=="}, "INVALID_OFFSET"},
		{WriteInput{r.ID, 0, "!"}, "INVALID_ARGUMENT"},
		{WriteInput{r.ID, 0, ""}, "INVALID_ARGUMENT"},
		{WriteInput{r.ID, 0, "YWFhYQ=="}, "LIMIT_EXCEEDED"},
		{WriteInput{r.ID, 0, base64.StdEncoding.EncodeToString(make([]byte, r.ChunkSize+1))}, "LIMIT_EXCEEDED"},
	} {
		if got := m.UploadWrite(ctx, tt.input); got.Code != tt.code {
			t.Fatalf("需要 %s: %+v", tt.code, got)
		}
	}
	if got := m.UploadFinish(ctx, IDInput{r.ID}); got.Code != "CHECKSUM_MISMATCH" {
		t.Fatal(got)
	}
	noTemps(t, dir)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("失败上传留下目标")
	}
	r = create(t, m, path, data, false)
	if !m.UploadCancel(ctx, IDInput{r.ID}).OK {
		t.Fatal("取消失败")
	}
	noTemps(t, dir)
}
func TestOverwriteAndCommitConflict(t *testing.T) {
	m := manager(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	data := []byte("new")
	old := []byte("old")
	r := create(t, m, path, data, false)
	writeAll(t, m, r, data)
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	if got := m.UploadFinish(ctx, IDInput{r.ID}); got.Code != "CONFLICT" {
		t.Fatal(got)
	}
	noTemps(t, dir)
	r = create(t, m, path, data, true)
	writeAll(t, m, r, []byte("bad"))
	if got := m.UploadFinish(ctx, IDInput{r.ID}); got.Code != "CHECKSUM_MISMATCH" {
		t.Fatal(got)
	}
	actual, _ := os.ReadFile(path)
	if !bytes.Equal(actual, old) {
		t.Fatal("失败覆盖损坏旧目标")
	}
	r = create(t, m, path, data, true)
	writeAll(t, m, r, data)
	if got := m.UploadFinish(ctx, IDInput{r.ID}); !got.OK {
		t.Fatal(got)
	}
	actual, _ = os.ReadFile(path)
	if !bytes.Equal(actual, data) {
		t.Fatal("覆盖内容错误")
	}
	noTemps(t, dir)
}
func TestIdempotencyAndLimits(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxTransfers = 1
	cfg.MaxRecords = 2
	cfg.MaxFileSize = 3
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	path := filepath.Join(t.TempDir(), "file")
	input := UploadInput{"request", path, 3, digest([]byte("new")), false}
	var wg sync.WaitGroup
	ids := make(chan string, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := m.UploadCreate(ctx, input)
			if !r.OK {
				t.Error(r)
			}
			ids <- r.ID
		}()
	}
	wg.Wait()
	close(ids)
	id := ""
	for got := range ids {
		if id != "" && got != id {
			t.Fatal("并发重复创建")
		}
		id = got
	}
	input.Size = 2
	if r := m.UploadCreate(ctx, input); r.Code != "CONFLICT" {
		t.Fatal(r)
	}
	input.RequestID = "second"
	input.Size = 4
	if r := m.UploadCreate(ctx, input); r.Code != "LIMIT_EXCEEDED" {
		t.Fatal(r)
	}
	input.Size = 3
	if r := m.UploadCreate(ctx, input); r.Code != "LIMIT_EXCEEDED" {
		t.Fatal(r)
	}
	_ = m.UploadCancel(ctx, IDInput{id})
	r := m.UploadCreate(ctx, input)
	if !r.OK {
		t.Fatal(r)
	}
	_ = m.UploadCancel(ctx, IDInput{r.ID})
	input.RequestID = "third"
	if r := m.UploadCreate(ctx, input); r.Code != "LIMIT_EXCEEDED" {
		t.Fatal(r)
	}
}
func TestExpiration(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IdleTimeout = 20 * time.Millisecond
	cfg.Retention = 30 * time.Millisecond
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	dir := t.TempDir()
	r := create(t, m, filepath.Join(dir, "file"), []byte("a"), false)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		n := len(m.entries)
		m.mu.Unlock()
		if n == 0 {
			noTemps(t, dir)
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("上传未过期: %s", r.ID)
}
func TestDownloadChanged(t *testing.T) {
	m := manager(t)
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	r := m.DownloadOpen(ctx, PathInput{path})
	if !r.OK {
		t.Fatal(r)
	}
	if err := os.WriteFile(path, []byte("longer"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := m.DownloadRead(ctx, ReadInput{r.ID, 0, 3}); got.Code != "SOURCE_CHANGED" {
		t.Fatal(got)
	}
	if got := m.DownloadClose(ctx, IDInput{r.ID}); got.OK {
		t.Fatal("变化源文件关闭不应成功")
	}
	r = m.DownloadOpen(ctx, PathInput{path})
	if !r.OK {
		t.Fatal(r)
	}
	if err := os.WriteFile(path, []byte("short"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := m.DownloadClose(ctx, IDInput{r.ID}); got.Code != "SOURCE_CHANGED" {
		t.Fatal(got)
	}
}
func TestCloseCancelsHash(t *testing.T) {
	m := manager(t)
	file, err := os.Create(filepath.Join(t.TempDir(), "large"))
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(512 << 20); err != nil {
		t.Fatal(err)
	}
	file.Close()
	finished := make(chan Result, 1)
	go func() { finished <- m.DownloadOpen(ctx, PathInput{file.Name()}) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		m.mu.Lock()
		n := len(m.entries)
		m.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("未预留下载名额")
		}
		runtime.Gosched()
	}
	// 初始化在锁外，因此取消无需等待整份文件的哈希。
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-finished:
		if got.OK {
			t.Fatal("关闭后仍成功打开")
		}
	case <-time.After(time.Second):
		t.Fatal("哈希未取消")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
}
