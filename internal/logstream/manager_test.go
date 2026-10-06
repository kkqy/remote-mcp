package logstream

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func manager(t *testing.T, cfg Config) *Manager {
	t.Helper()
	m, err := New(cfg)
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
func openLog(t *testing.T, m *Manager, data string) (string, OpenResult) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "日志.txt")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := m.Open(context.Background(), OpenInput{path})
	if err != nil {
		t.Fatal(err)
	}
	return path, out
}
func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != code {
		t.Fatalf("错误码期待%s: %v", code, err)
	}
}
func TestReadBytesBoundsAndGeneration(t *testing.T) {
	m := manager(t, DefaultConfig())
	_, opened := openLog(t, m, "中文\xff"+strings.Repeat("x", 70000))
	out, err := m.Read(context.Background(), ReadInput{ID: opened.ID, Generation: opened.Generation})
	if err != nil || out.Reason != "immediate" || out.NextCursor != 65536 || out.EndCursor != 70007 || out.ValidUTF8 {
		t.Fatalf("读取边界: %+v %v", out, err)
	}
	raw, _ := base64.StdEncoding.DecodeString(out.DataBase64)
	if !strings.HasPrefix(string(raw), "中文\xff") {
		t.Fatal("原始内容没有保留")
	}
	for _, in := range []ReadInput{{ID: opened.ID, Cursor: -1}, {ID: opened.ID, WaitMS: 30001}, {ID: opened.ID, WaitMS: -1}, {ID: opened.ID, Limit: 65537}, {ID: opened.ID, Generation: 2}, {ID: opened.ID, Cursor: opened.EndCursor + 1}, {ID: opened.ID, Generation: 1, Cursor: opened.EndCursor + 1}} {
		_, err = m.Read(context.Background(), in)
		requireCode(t, err, "invalid_argument")
	}
	next, err := m.Read(context.Background(), ReadInput{ID: opened.ID, Generation: out.Generation, Cursor: out.NextCursor})
	if err != nil || next.StartCursor != 65536 || next.NextCursor != opened.EndCursor {
		t.Fatalf("增量读取: %+v %v", next, err)
	}
}
func TestWaitAppendTimeoutCancel(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PollInterval = 5 * time.Millisecond
	m := manager(t, cfg)
	path, opened := openLog(t, m, "old")
	in := ReadInput{ID: opened.ID, Generation: opened.Generation, Cursor: 3, WaitMS: 1000}
	go func() {
		time.Sleep(30 * time.Millisecond)
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		if _, err = f.WriteString("中文"); err != nil {
			t.Error(err)
		}
	}()
	out, err := m.Read(context.Background(), in)
	if err != nil || out.Reason != "output" || out.Text != "中文" || out.StartCursor != 3 || out.NextCursor != 9 {
		t.Fatalf("追加等待: %+v %v", out, err)
	}
	in.Cursor = 9
	in.WaitMS = 20
	out, err = m.Read(context.Background(), in)
	if err != nil || out.Reason != "timeout" || out.NextCursor != 9 {
		t.Fatalf("超时: %+v %v", out, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	in.WaitMS = 30000
	out, err = m.Read(ctx, in)
	if err != nil || out.Reason != "cancelled" || out.NextCursor != 9 {
		t.Fatalf("取消: %+v %v", out, err)
	}
}
func TestRotationAndTruncation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PollInterval = 5 * time.Millisecond
	m := manager(t, cfg)
	path, opened := openLog(t, m, "original")
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("新的"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := m.Read(context.Background(), ReadInput{ID: opened.ID, Generation: 1, Cursor: 8, WaitMS: 1000})
	if err != nil || out.Reason != "rotated" || !out.Rotated || !out.Truncated || out.Generation != 2 || out.Text != "新的" || out.StartCursor != 0 {
		t.Fatalf("轮转: %+v %v", out, err)
	}
	// 游标为零仍应依据上次采样大小识别截断。
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	out, err = m.Read(context.Background(), ReadInput{ID: opened.ID, Generation: 2, Cursor: 0, WaitMS: 1000})
	if err != nil || out.Reason != "truncated" || out.Rotated || !out.Truncated || out.Generation != 3 || out.NextCursor != 0 {
		t.Fatalf("截断: %+v %v", out, err)
	}
}
func TestRotationDuringWaitWithoutGeneration(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PollInterval = 5 * time.Millisecond
	m := manager(t, cfg)
	path, opened := openLog(t, m, "old")
	done := make(chan ReadResult, 1)
	go func() {
		out, err := m.Read(context.Background(), ReadInput{ID: opened.ID, Cursor: 3, WaitMS: 2000})
		if err != nil {
			t.Error(err)
		}
		done <- out
	}()
	awaitActive(t, m, opened.ID)
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case out := <-done:
		if out.Reason != "rotated" || out.Generation != 2 || out.NextCursor != 0 {
			t.Fatalf("空文件轮转: %+v", out)
		}
	case <-time.After(time.Second):
		t.Fatal("轮转未唤醒")
	}
}
func awaitActive(t *testing.T, m *Manager, id string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		m.mu.Lock()
		r := m.resources[id]
		active := 0
		if r != nil {
			r.mu.Lock()
			active = r.active
			r.mu.Unlock()
		}
		m.mu.Unlock()
		if active > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("读取未开始")
		}
		time.Sleep(time.Millisecond)
	}
}
func TestCloseCancelsWaitAndIsIdempotent(t *testing.T) {
	for _, serverClose := range []bool{false, true} {
		t.Run(map[bool]string{false: "resource", true: "manager"}[serverClose], func(t *testing.T) {
			m := manager(t, DefaultConfig())
			_, opened := openLog(t, m, "old")
			done := make(chan ReadResult, 1)
			go func() {
				out, err := m.Read(context.Background(), ReadInput{ID: opened.ID, Cursor: 3, WaitMS: 30000})
				if err != nil {
					t.Error(err)
				}
				done <- out
			}()
			awaitActive(t, m, opened.ID)
			// 读取初始快照完成后才关闭资源。
			time.Sleep(10 * time.Millisecond)
			if serverClose {
				var wg sync.WaitGroup
				for i := 0; i < 5; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						if err := m.Close(); err != nil {
							t.Error(err)
						}
					}()
				}
				wg.Wait()
			} else {
				for i := 0; i < 2; i++ {
					out, err := m.CloseLog(context.Background(), IDInput{opened.ID})
					if err != nil || !out.Closed {
						t.Fatalf("关闭: %+v %v", out, err)
					}
				}
			}
			select {
			case out := <-done:
				if out.Reason != "cancelled" || out.NextCursor != 3 {
					t.Fatalf("关闭取消: %+v", out)
				}
			case <-time.After(time.Second):
				t.Fatal("关闭未解除等待")
			}
		})
	}
}
func TestMissingNonregularPermissionAndResourceLimits(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxOpen = 1
	m := manager(t, cfg)
	_, err := m.Open(context.Background(), OpenInput{filepath.Join(t.TempDir(), "missing")})
	requireCode(t, err, "not_found")
	_, err = m.Open(context.Background(), OpenInput{t.TempDir()})
	requireCode(t, err, "not_regular")
	requireCode(t, fileError(os.ErrPermission), "permission_denied")
	path, opened := openLog(t, m, "old")
	_, err = m.Open(context.Background(), OpenInput{path})
	requireCode(t, err, "resource_limit")
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	_, err = m.Read(context.Background(), ReadInput{ID: opened.ID})
	requireCode(t, err, "not_found")
	m.CloseLog(context.Background(), IDInput{opened.ID})
	if err = os.WriteFile(path, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = m.Open(context.Background(), OpenInput{path})
	if err != nil {
		t.Fatal(err)
	}
}
func TestIdleExpiryPreservesActiveWait(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IdleTimeout = 20 * time.Millisecond
	cfg.SweepInterval = 5 * time.Millisecond
	cfg.PollInterval = 5 * time.Millisecond
	m := manager(t, cfg)
	_, opened := openLog(t, m, "")
	out, err := m.Read(context.Background(), ReadInput{ID: opened.ID, WaitMS: 60})
	if err != nil || out.Reason != "timeout" {
		t.Fatalf("活跃等待被回收: %+v %v", out, err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		n := len(m.resources)
		m.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("空闲资源没有回收")
}
func TestRejectSymbolicLink(t *testing.T) {
	m := manager(t, DefaultConfig())
	path, _ := openLog(t, m, "old")
	link := path + ".link"
	if err := os.Symlink(path, link); err != nil {
		t.Skip("当前账号无法创建符号链接")
	}
	_, err := m.Open(context.Background(), OpenInput{link})
	requireCode(t, err, "not_regular")
}

func TestRotationMissingWindowWaitsForReplacement(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PollInterval = 2 * time.Millisecond
	m := manager(t, cfg)
	path, opened := openLog(t, m, "old")
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	// 资源已存在且开始等待时路径已经缺失，同样保留跟踪。
	done := make(chan ReadResult, 1)
	go func() {
		out, err := m.Read(context.Background(), ReadInput{ID: opened.ID, Generation: 1, Cursor: 3, WaitMS: 2000})
		if err != nil {
			t.Error(err)
		}
		done <- out
	}()
	awaitActive(t, m, opened.ID)
	time.Sleep(20 * time.Millisecond)
	select {
	case out := <-done:
		t.Fatalf("路径重建前错误结束: %+v", out)
	default:
	}
	if err := os.WriteFile(path, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case out := <-done:
		if out.Reason != "rotated" || out.Text != "new" || out.Generation != 2 {
			t.Fatalf("延迟轮转: %+v", out)
		}
	case <-time.After(time.Second):
		t.Fatal("路径重建未唤醒")
	}
}
func TestRotationMissingWindowTimeoutRetainsCursor(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PollInterval = 2 * time.Millisecond
	m := manager(t, cfg)
	path, opened := openLog(t, m, "old")
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	out, err := m.Read(context.Background(), ReadInput{ID: opened.ID, Generation: 1, Cursor: 3, WaitMS: 20})
	if err != nil || out.Reason != "timeout" || out.NextCursor != 3 || out.Generation != 1 {
		t.Fatalf("路径缺失超时: %+v %v", out, err)
	}
}
func TestPollCloseRacePreservesCancelledResult(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PollInterval = time.Millisecond
	m := manager(t, cfg)
	for i := 0; i < 100; i++ {
		_, opened := openLog(t, m, "old")
		done := make(chan ReadResult, 1)
		go func() {
			out, err := m.Read(context.Background(), ReadInput{ID: opened.ID, Cursor: 3, WaitMS: 30000})
			if err != nil {
				t.Error(err)
			}
			done <- out
		}()
		awaitActive(t, m, opened.ID)
		time.Sleep(time.Millisecond)
		m.CloseLog(context.Background(), IDInput{opened.ID})
		select {
		case out := <-done:
			if out.Reason != "cancelled" || out.NextCursor != 3 {
				t.Fatalf("poll/close竞态: %+v", out)
			}
		case <-time.After(time.Second):
			t.Fatal("关闭未唤醒")
		}
	}
}
func TestRotationOldHandleCloseFailureKeepsReplacement(t *testing.T) {
	m := manager(t, DefaultConfig())
	path, opened := openLog(t, m, "old")
	m.mu.Lock()
	r := m.resources[opened.ID]
	m.mu.Unlock()
	r.mu.Lock()
	r.file.Close()
	r.mu.Unlock()
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := m.Read(context.Background(), ReadInput{ID: opened.ID, Generation: 1})
	requireCode(t, err, "io_error")
	out, err := m.Read(context.Background(), ReadInput{ID: opened.ID, Generation: 1})
	if err != nil || out.Text != "new" || !out.Rotated || out.Generation != 2 {
		t.Fatalf("旧句柄失败后新资源不可用: %+v %v", out, err)
	}
}

func TestIdleCloseFailureIsReportedAtManagerClose(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IdleTimeout = 10 * time.Millisecond
	cfg.SweepInterval = 2 * time.Millisecond
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	_, opened := openLog(t, m, "old")
	m.mu.Lock()
	r := m.resources[opened.ID]
	m.mu.Unlock()
	r.mu.Lock()
	r.file.Close()
	r.mu.Unlock()
	deadline := time.Now().Add(time.Second)
	for {
		m.mu.Lock()
		remaining := len(m.resources)
		m.mu.Unlock()
		if remaining == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("没有回收资源")
		}
		time.Sleep(time.Millisecond)
	}
	requireCode(t, m.Close(), "io_error")
	requireCode(t, m.Close(), "io_error")
}
