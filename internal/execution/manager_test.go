package execution

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHelperProcess(t *testing.T) {
	if os.Getenv("REMOTE_MCP_EXEC_HELPER") != "1" {
		return
	}
	mode := os.Getenv("REMOTE_MCP_EXEC_MODE")
	switch mode {
	case "output":
		fmt.Fprint(os.Stdout, "hello\xff")
		fmt.Fprint(os.Stderr, "error")
		os.Exit(7)
	case "flood":
		fmt.Fprint(os.Stdout, strings.Repeat("x", 20000))
		os.Exit(0)
	case "spawn-exit", "spawn-wait":
		child := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
		child.Env = append(os.Environ(), "REMOTE_MCP_EXEC_MODE=sleep")
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(3)
		}
		fmt.Fprintf(os.Stdout, "CHILD:%d\n", child.Process.Pid)
		if mode == "spawn-wait" {
			child.Wait()
		}
		os.Exit(0)
	case "delayed-output":
		time.Sleep(100 * time.Millisecond)
		fmt.Fprint(os.Stdout, "稍后输出")
		time.Sleep(100 * time.Millisecond)
		os.Exit(0)
	case "sleep":
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(2)
}
func helperInput(id, mode string) StartInput {
	return StartInput{RequestID: id, Command: os.Args[0], Args: []string{"-test.run=^TestHelperProcess$"}, Env: map[string]string{"REMOTE_MCP_EXEC_HELPER": "1", "REMOTE_MCP_EXEC_MODE": mode}, WaitMS: 1000}
}
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
func awaitExit(t *testing.T, m *Manager, id string) Status {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		s, err := m.Status(IDInput{id})
		if err != nil {
			t.Fatal(err)
		}
		if s.State == "exited" {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("进程未结束")
	return Status{}
}
func TestProcessOutputAndExit(t *testing.T) {
	m := manager(t)
	s, err := m.Start(context.Background(), helperInput("output", "output"))
	if err != nil {
		t.Fatal(err)
	}
	s = awaitExit(t, m, s.ID)
	if s.ExitCode == nil || *s.ExitCode != 7 {
		t.Fatalf("退出码错误: %+v", s)
	}
	out, err := m.Read(ReadInput{ID: s.ID})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(out.DataBase64)
	if string(raw) != "hello\xff" || out.ValidUTF8 {
		t.Fatalf("字节未保留: %+v", out)
	}
	stderr, err := m.Read(ReadInput{ID: s.ID, Stream: "stderr"})
	if err != nil || stderr.Text != "error" {
		t.Fatalf("stderr: %+v %v", stderr, err)
	}
	empty, err := m.Read(ReadInput{ID: s.ID, Cursor: out.NextCursor})
	if err != nil || empty.DataBase64 != "" {
		t.Fatalf("游标增量错误: %+v %v", empty, err)
	}
}
func TestBoundedOutputAndInvalidCursor(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OutputBytes = 128
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	s, err := m.Start(context.Background(), helperInput("flood", "flood"))
	if err != nil {
		t.Fatal(err)
	}
	awaitExit(t, m, s.ID)
	out, err := m.Read(ReadInput{ID: s.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Truncated || out.StartCursor != 20000-64 || out.EndCursor != 20000 || len(out.Text) != 64 {
		t.Fatalf("截断错误: %+v", out)
	}
	if _, err = m.Read(ReadInput{ID: s.ID, Cursor: 20001}); err == nil {
		t.Fatal("未来游标应失败")
	}
}
func TestTimeoutDedupAndLimit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxProcesses = 1
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	timeout := int64(250)
	in := helperInput("sleep", "sleep")
	in.WaitMS = 0
	in.TimeoutMS = &timeout
	s, err := m.Start(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := m.Start(context.Background(), in)
	if err != nil || repeated.ID != s.ID {
		t.Fatalf("去重失败: %+v %v", repeated, err)
	}
	conflict := in
	conflict.Args = append(conflict.Args, "different")
	if _, err = m.Start(context.Background(), conflict); err == nil {
		t.Fatal("参数冲突未拒绝")
	}
	another := in
	another.RequestID = "another"
	if _, err = m.Start(context.Background(), another); err == nil {
		t.Fatal("并发限额未生效")
	}
	s = awaitExit(t, m, s.ID)
	if s.Reason != "timeout" {
		t.Fatalf("超时原因: %+v", s)
	}
}
func TestConcurrentStopReadClose(t *testing.T) {
	m := manager(t)
	in := helperInput("race", "sleep")
	in.WaitMS = 0
	s, err := m.Start(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); m.Read(ReadInput{ID: s.ID}); m.Stop(IDInput{s.ID}) }()
	}
	wg.Wait()
	wg.Add(2)
	go func() { defer wg.Done(); m.Close() }()
	go func() { defer wg.Done(); m.Close() }()
	wg.Wait()
}
func TestRetentionEvictsRequests(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Retention = 20 * time.Millisecond
	cfg.SweepInterval = 5 * time.Millisecond
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	s, err := m.Start(context.Background(), helperInput("retained", "output"))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, err = m.Status(IDInput{s.ID})
		if err != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err == nil {
		t.Fatal("终态资源没有清理")
	}
	s2, err := m.Start(context.Background(), helperInput("retained", "output"))
	if err != nil || s2.ID == s.ID {
		t.Fatalf("去重记录未清理: %v", err)
	}
}
func TestOutputBufferConcurrent(t *testing.T) {
	b := &outputBuffer{capacity: 32}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); b.Write([]byte(strconv.Itoa(i))); b.read(0, 5) }(i)
	}
	wg.Wait()
	r, err := b.read(0, 32)
	if err != nil || r.EndCursor != 10 {
		t.Fatalf("并发缓冲错误: %+v %v", r, err)
	}
}
