//go:build linux || darwin

package execution

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func send(t *testing.T, m *Manager, id, text string) {
	t.Helper()
	_, err := m.Write(context.Background(), WriteInput{id, base64.StdEncoding.EncodeToString([]byte(text))})
	if err != nil {
		t.Fatal(err)
	}
}
func readUntil(t *testing.T, m *Manager, id, needle string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r, err := m.Read(ReadInput{ID: id})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(r.Text, needle) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	r, _ := m.Read(ReadInput{ID: id})
	t.Fatalf("未发现 %q，输出 %q", needle, r.Text)
}
func TestTerminalStateResizeInterrupt(t *testing.T) {
	m := manager(t)
	dir := t.TempDir()
	in := TerminalInput{RequestID: "shell", Command: "/bin/sh", Dir: dir}
	s, err := m.OpenTerminal(in)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := m.OpenTerminal(in)
	if err != nil || duplicate.ID != s.ID {
		t.Fatalf("终端去重失败: %v", err)
	}
	send(t, m, s.ID, "stty -echo\n")
	send(t, m, s.ID, "export MCP_MARK=kept; cd /\n")
	send(t, m, s.ID, "printf 'STATE:%s:%s\\n' \"$MCP_MARK\" \"$PWD\"\n")
	readUntil(t, m, s.ID, "STATE:kept:/")
	if _, err = m.Resize(ResizeInput{s.ID, 100, 40}); err != nil {
		t.Fatal(err)
	}
	send(t, m, s.ID, "stty size\n")
	readUntil(t, m, s.ID, "40 100")
	send(t, m, s.ID, "sleep 30\n")
	time.Sleep(100 * time.Millisecond)
	send(t, m, s.ID, "\x03")
	send(t, m, s.ID, "printf 'AFTER_INTERRUPT\\n'\n")
	readUntil(t, m, s.ID, "AFTER_INTERRUPT")
	if _, err = m.Stop(IDInput{s.ID}); err != nil {
		t.Fatal(err)
	}
	awaitExit(t, m, s.ID)
}
func TestTerminalIdleCleanup(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TerminalIdleTimeout = 100 * time.Millisecond
	cfg.SweepInterval = 20 * time.Millisecond
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	s, err := m.OpenTerminal(TerminalInput{RequestID: "idle", Command: "/bin/sh"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	s = awaitExit(t, m, s.ID)
	if s.Reason != "idle_timeout" {
		t.Fatalf("空闲清理未生效: %+v", s)
	}
}
func waitPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(path)
		pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
		if pid > 0 {
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("子进程未创建")
	return 0
}
func assertGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		err := syscall.Kill(pid, 0)
		if err == syscall.ESRCH {
			return
		}
		if b, e := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat")); e == nil {
			tail := string(b[strings.LastIndex(string(b), ")")+1:])
			if strings.HasPrefix(strings.TrimSpace(tail), "Z") {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("子进程 %d 仍存活", pid)
}
func TestProcessTreeAndInheritedPipes(t *testing.T) {
	for _, exitParent := range []bool{false, true} {
		t.Run(strconv.FormatBool(exitParent), func(t *testing.T) {
			m := manager(t)
			path := filepath.Join(t.TempDir(), "pid")
			script := "sleep 60 & echo $! > \"$PID_FILE\"; wait"
			if exitParent {
				script = "sleep 60 & echo $! > \"$PID_FILE\"; exit"
			}
			s, err := m.Start(context.Background(), StartInput{RequestID: "tree", Command: "/bin/sh", Args: []string{"-c", script}, Env: map[string]string{"PID_FILE": path}})
			if err != nil {
				t.Fatal(err)
			}
			pid := waitPIDFile(t, path)
			if !exitParent {
				m.Stop(IDInput{s.ID})
			}
			awaitExit(t, m, s.ID)
			assertGone(t, pid)
		})
	}
}
func TestTerminalForegroundAndBackgroundTree(t *testing.T) {
	m := manager(t)
	path := filepath.Join(t.TempDir(), "pid")
	fgpath := filepath.Join(t.TempDir(), "pid")
	s, err := m.OpenTerminal(TerminalInput{RequestID: "terminal-tree", Command: "/bin/sh", Env: map[string]string{"PID_FILE": path, "FG_FILE": fgpath}})
	if err != nil {
		t.Fatal(err)
	}
	send(t, m, s.ID, "sleep 60 & echo $! > \"$PID_FILE\"\n")
	pid := waitPIDFile(t, path)
	send(t, m, s.ID, "sh -c 'echo $$ > \"$FG_FILE\"; exec sleep 60'\n")
	fgpid := waitPIDFile(t, fgpath)
	m.Stop(IDInput{s.ID})
	awaitExit(t, m, s.ID)
	assertGone(t, pid)
	assertGone(t, fgpid)
}

func TestTerminalParentExitReapsOrphanJob(t *testing.T) {
	m := manager(t)
	path := filepath.Join(t.TempDir(), "pid")
	s, err := m.OpenTerminal(TerminalInput{RequestID: "orphan-job", Command: "/bin/sh", Args: []string{"-m", "-c", "trap '' HUP; sleep 60 & echo $! > \"$PID_FILE\"; sleep 0.2; exit"}, Env: map[string]string{"PID_FILE": path}})
	if err != nil {
		t.Fatal(err)
	}
	pid := waitPIDFile(t, path)
	awaitExit(t, m, s.ID)
	assertGone(t, pid)
}
