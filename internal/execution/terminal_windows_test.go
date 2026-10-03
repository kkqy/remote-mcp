//go:build windows

package execution

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func sendWindows(t *testing.T, m *Manager, id, text string) {
	t.Helper()
	_, err := m.Write(context.Background(), WriteInput{id, base64.StdEncoding.EncodeToString([]byte(text))})
	if err != nil {
		t.Fatal(err)
	}
}
func readWindows(t *testing.T, m *Manager, id, needle string) string {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		r, err := m.Read(ReadInput{ID: id})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(r.Text, needle) {
			return r.Text
		}
		time.Sleep(20 * time.Millisecond)
	}
	r, _ := m.Read(ReadInput{ID: id})
	t.Fatalf("未发现 %q，输出 %q", needle, r.Text)
	return ""
}
func TestWindowsTerminalStateResizeInterrupt(t *testing.T) {
	m := manager(t)
	in := TerminalInput{RequestID: "windows-shell", Command: "cmd.exe", Args: []string{"/Q"}, Dir: t.TempDir()}
	s, err := m.OpenTerminal(in)
	if err != nil {
		t.Fatal(err)
	}
	same, err := m.OpenTerminal(in)
	if err != nil || same.ID != s.ID {
		t.Fatalf("终端去重错误: %v", err)
	}
	sendWindows(t, m, s.ID, "set MCP_MARK=kept\r\n")
	sendWindows(t, m, s.ID, "echo STATE:%MCP_MARK%\r\n")
	readWindows(t, m, s.ID, "STATE:kept")
	if _, err = m.Resize(ResizeInput{s.ID, 100, 40}); err != nil {
		t.Fatal(err)
	}
	sendWindows(t, m, s.ID, "ping -t 127.0.0.1 >nul\r\n")
	time.Sleep(300 * time.Millisecond)
	sendWindows(t, m, s.ID, "\x03")
	time.Sleep(100 * time.Millisecond)
	sendWindows(t, m, s.ID, "echo AFTER:%MCP_MARK%\r\n")
	readWindows(t, m, s.ID, "AFTER:kept")
	if _, err = m.Stop(IDInput{s.ID}); err != nil {
		t.Fatal(err)
	}
	awaitExit(t, m, s.ID)
}
func TestWindowsJobTree(t *testing.T) {
	for _, mode := range []string{"spawn-wait", "spawn-exit"} {
		t.Run(mode, func(t *testing.T) {
			m := manager(t)
			in := helperInput(mode, mode)
			in.WaitMS = 0
			s, err := m.Start(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			output := readWindows(t, m, s.ID, "CHILD:")
			var pid int
			if _, err = fmt.Sscanf(output, "CHILD:%d", &pid); err != nil || pid == 0 {
				t.Fatalf("子进程编号错误: %q", output)
			}
			handle, openErr := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
			if openErr == nil {
				defer windows.CloseHandle(handle)
			}
			if mode == "spawn-wait" {
				if _, err = m.Stop(IDInput{s.ID}); err != nil {
					t.Fatal(err)
				}
			}
			awaitExit(t, m, s.ID)
			if openErr == nil {
				status, err := windows.WaitForSingleObject(handle, 3000)
				if err != nil || status == uint32(windows.WAIT_TIMEOUT) {
					t.Fatalf("子进程仍存活: %v %v", status, err)
				}
			}
		})
	}
}
func TestWindowsTerminalIdleCleanup(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TerminalIdleTimeout = 150 * time.Millisecond
	cfg.SweepInterval = 20 * time.Millisecond
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	s, err := m.OpenTerminal(TerminalInput{RequestID: "windows-idle", Command: "cmd.exe"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	s = awaitExit(t, m, s.ID)
	if s.Reason != "idle_timeout" {
		t.Fatalf("空闲清理错误: %+v", s)
	}
}
