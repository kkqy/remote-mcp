package inspection

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLinuxProcStatAndTCPFixtures(t *testing.T) {
	p, err := parseProcStat([]byte("123 (中文 name ) (x)) S 42 0 0\n"))
	if err != nil || p.PID != 123 || p.PPID != 42 || p.Name != "中文 name ) (x)" {
		t.Fatal(p, err)
	}
	for _, s := range []string{"bad", "1 (name) S x", "0 (name) S 1", "1 (name) S -1"} {
		if _, err := parseProcStat([]byte(s)); err == nil {
			t.Fatal("malformed stat accepted", s)
		}
	}
	for _, test := range []struct {
		endpoint, address string
		ipv6              bool
	}{{"0100007F:1F90", "127.0.0.1", false}, {"00000000000000000000000001000000:1F90", "::1", true}, {"B80D0120000000000000000001000000:1F90", "2001:db8::1", true}} {
		row := procTCPLine(test.endpoint, "123")
		item, ok, err := parseProcTCP(row, test.ipv6)
		if err != nil || !ok || item.Address != test.address || item.Port != 8080 || item.inode != "123" {
			t.Fatalf("TCP fixture %+v %v", item, err)
		}
	}
	if _, ok, err := parseProcTCP(strings.Replace(procTCPLine("0100007F:1F90", "123"), "0A", "01", 1), false); err != nil || ok {
		t.Fatal(ok, err)
	}
	for _, s := range []string{"bad", procTCPLine("GG00007F:1F90", "123"), procTCPLine("0100007F:FFFFF", "123"), procTCPLine("0100007F:1F90", "bad")} {
		if _, _, err := parseProcTCP(s, false); err == nil {
			t.Fatal("malformed tcp accepted", s)
		}
	}
}
func procTCPLine(endpoint, inode string) string {
	return "0: " + endpoint + " 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 " + inode + " 1\n"
}
func makeFakeProc(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "net"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tcp", "tcp6"} {
		data := "header\n"
		if name == "tcp" {
			data += procTCPLine("0100007F:1F90", "123")
		}
		if err := os.WriteFile(filepath.Join(root, "net", name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, pid := range []string{"10", "11"} {
		if err := os.MkdirAll(filepath.Join(root, pid, "fd"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, pid, "stat"), []byte(pid+" (中文 process) S 1 0 0"), 0600); err != nil {
			t.Fatal(err)
		}
		for _, fd := range []string{"1", "2"} {
			if err := os.Symlink("socket:[123]", filepath.Join(root, pid, "fd", fd)); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}
func TestLinuxOwnershipAndScanLimits(t *testing.T) {
	root := makeFakeProc(t)
	cfg := DefaultConfig()
	out := ListenersResult{Result: Result{OK: true}}
	items := listenersAt(context.Background(), cfg, &out, root)
	if len(items) != 1 || len(items[0].PIDs) != 2 || out.ScannedFDs != 4 {
		t.Fatalf("owners not deduplicated: %+v %+v", items, out)
	}
	cfg.MaxFDs = 1
	out = ListenersResult{Result: Result{OK: true}}
	items = listenersAt(context.Background(), cfg, &out, root)
	if !out.Partial || !out.Truncated || out.ScannedFDs != 1 || len(items) != 1 || items[0].PIDVisibility != "partial" {
		t.Fatalf("FD budget: %+v %+v", out, items)
	}
	cfg = DefaultConfig()
	cfg.MaxProcesses = 1
	processOut := ProcessesResult{Result: Result{OK: true}}
	p := processesAt(context.Background(), cfg, &processOut, root)
	if len(p) != 1 || !processOut.Partial || !processOut.Truncated {
		t.Fatalf("PID budget: %+v %+v", processOut, p)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	processOut = ProcessesResult{Result: Result{OK: true}}
	p = processesAt(ctx, DefaultConfig(), &processOut, root)
	if processOut.Code != "cancelled" || len(p) != 0 {
		t.Fatal(processOut)
	}
}
func TestLinuxMissingInterfacesAndMalformedTables(t *testing.T) {
	root := t.TempDir()
	out := ListenersResult{Result: Result{OK: true}}
	items := listenersAt(context.Background(), DefaultConfig(), &out, root)
	if out.OK || out.Code != "dependency_missing" || len(items) != 0 {
		t.Fatal(out)
	}
	root = makeFakeProc(t)
	if err := os.WriteFile(filepath.Join(root, "net", "tcp"), []byte("header\nbad\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out = ListenersResult{Result: Result{OK: true}}
	items = listenersAt(context.Background(), DefaultConfig(), &out, root)
	if !out.Partial || len(items) != 0 {
		t.Fatal(out)
	}
	if err := os.WriteFile(filepath.Join(root, "net", "tcp"), []byte("header\n"+strings.Repeat(procTCPLine("0100007F:1F90", "123"), 30)), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.MaxCommandBytes = 200
	out = ListenersResult{Result: Result{OK: true}}
	listenersAt(context.Background(), cfg, &out, root)
	if !out.Partial || !out.Truncated {
		t.Fatal("byte limit hidden", out)
	}
}

func TestLinuxIPv6TablePermissionReason(t *testing.T) {
	root := makeFakeProc(t)
	path := filepath.Join(root, "net", "tcp6")
	if err := os.Chmod(path, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0600) })
	if f, err := os.Open(path); err == nil {
		f.Close()
		t.Skip("Current account bypasses file mode permissions")
	}
	out := ListenersResult{Result: Result{OK: true}}
	items := listenersAt(context.Background(), DefaultConfig(), &out, root)
	found := false
	for _, warning := range out.Warnings {
		if warning.Message == "The IPv6 TCP system table is not readable" {
			if warning.Code != "permission_denied" {
				t.Fatalf("incorrect permission reason: %+v", warning)
			}
			found = true
		}
	}
	if !out.OK || !out.Partial || len(items) != 1 || !found {
		t.Fatalf("partial IPv4 result was lost: %+v %+v", out, items)
	}
}

func TestLinuxOwnershipDirectoryPermissionReason(t *testing.T) {
	root := makeFakeProc(t)
	if err := os.Chmod(root, 0100); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(root, 0700) })
	if f, err := os.Open(root); err == nil {
		f.Close()
		t.Skip("Current account bypasses directory mode permissions")
	}
	out := ListenersResult{Result: Result{OK: true}}
	items := listenersAt(context.Background(), DefaultConfig(), &out, root)
	found := false
	for _, warning := range out.Warnings {
		if warning.Message == "The process directory is not readable; listener ownership is incomplete" {
			if warning.Code != "permission_denied" {
				t.Fatalf("incorrect ownership permission reason: %+v", warning)
			}
			found = true
		}
	}
	if !out.OK || !out.Partial || out.PIDVisibility != "partial" || len(items) != 1 || !found {
		t.Fatalf("partial listener result was lost: %+v %+v", out, items)
	}
}
func TestLinuxRealParentChildAndOutputLimit(t *testing.T) {
	m := testManager(t)
	cmd := exec.Command("sleep", "5")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	out := m.Processes(context.Background(), SnapshotInput{RootPID: os.Getpid(), Limit: 4096})
	found := false
	for _, p := range out.Processes {
		if p.PID == cmd.Process.Pid && p.PPID == os.Getpid() {
			found = true
		}
	}
	if !found {
		t.Fatalf("real child %d absent: %+v", cmd.Process.Pid, out)
	}
	out = m.Processes(context.Background(), SnapshotInput{RootPID: os.Getpid(), Limit: 1})
	if len(out.Processes) != 1 || !out.Truncated {
		t.Fatal(out)
	}
	bytes, err := json.Marshal(out)
	if err != nil || len(bytes) > m.cfg.MaxResultBytes {
		t.Fatal("output limit", len(bytes), err)
	}
}
func TestRuntimeFixedCommandsMissingFailureTimeoutAndStderr(t *testing.T) {
	m := testManager(t)
	root := t.TempDir()
	t.Setenv("PATH", root)
	missing := m.inspectRuntime(context.Background(), "node")
	if missing.Code != "dependency_missing" || missing.Available {
		t.Fatal(missing)
	}
	write := func(name, body string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte("#!/bin/sh\n"+body), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(root, name), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("node", "echo hidden\n", 0600)
	out := m.inspectRuntime(context.Background(), "node")
	if out.Code != "permission_denied" {
		t.Fatal(out)
	}
	write("node", "echo '中文 runtime'\nexit 9\n", 0700)
	out = m.inspectRuntime(context.Background(), "node")
	if out.Code != "io_error" || out.Version != "中文 runtime" {
		t.Fatal(out)
	}
	write("java", "echo 'java stderr version' >&2\n", 0700)
	out = m.inspectRuntime(context.Background(), "java")
	if out.Code != "" || out.Version != "java stderr version" {
		t.Fatal(out)
	}
	write("python", "echo fallback-version\n", 0700)
	out = m.inspectRuntime(context.Background(), "python")
	if out.Code != "" || !strings.HasSuffix(out.Path, "python") || out.Version != "fallback-version" {
		t.Fatal(out)
	}
	write("node", "exec /bin/sleep 2\n", 0700)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	start := time.Now()
	out = m.inspectRuntime(ctx, "node")
	cancel()
	if out.Code != "timeout" || time.Since(start) > 500*time.Millisecond {
		t.Fatal(out)
	}
	write("node", "i=0; while [ $i -lt 1000 ]; do echo '1234567890'; i=$((i+1)); done\n", 0700)
	m.cfg.MaxCommandBytes = 100
	out = m.inspectRuntime(context.Background(), "node")
	if !out.Truncated || len(out.Version) > 100 {
		t.Fatal(out)
	}
	write("go", "/bin/sleep 0.5 &\necho version\n", 0700)
	start = time.Now()
	_, _, err := runCommand(context.Background(), 1024, filepath.Join(root, "go"))
	if err == nil || time.Since(start) > 300*time.Millisecond {
		t.Fatal("inherited pipe wait not bounded", err, time.Since(start))
	}
	if len(out.Path) > m.cfg.MaxNameBytes {
		t.Fatal(strconv.Itoa(len(out.Path)))
	}
}
