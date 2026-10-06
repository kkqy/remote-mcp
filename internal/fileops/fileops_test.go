package fileops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func managerForTest(t *testing.T, cfg Config) *Manager {
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
func writeText(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0640); err != nil {
		t.Fatal(err)
	}
}
func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}

func TestReadPreservesTextAndFullHash(t *testing.T) {
	m := managerForTest(t, DefaultConfig())
	path := filepath.Join(t.TempDir(), "中文.txt")
	text := "甲\r\n乙\r\n末尾"
	writeText(t, path, text)
	out, err := m.Read(context.Background(), ReadInput{Path: path, StartLine: 2, LineCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != "乙\r\n" || out.SHA256 != digest([]byte(text)) || out.TotalLines != 3 || out.EndLine != 2 || out.NextLine != 3 || !out.Truncated || out.Reason != "line_limit" {
		t.Fatalf("unexpected read: %+v", out)
	}
	out, err = m.Read(context.Background(), ReadInput{Path: path, StartLine: 3})
	if err != nil || out.Text != "末尾" || out.Truncated {
		t.Fatalf("tail: %+v %v", out, err)
	}
	writeText(t, path, "")
	out, err = m.Read(context.Background(), ReadInput{Path: path})
	if err != nil || out.Text != "" || out.TotalLines != 0 || out.EndLine != 0 || out.SHA256 != digest(nil) {
		t.Fatalf("empty: %+v %v", out, err)
	}
}

func TestReadBoundsLongLinesAndUTF8(t *testing.T) {
	m := managerForTest(t, DefaultConfig())
	path := filepath.Join(t.TempDir(), "text")
	text := strings.Repeat("中", 30000) + "\n终"
	writeText(t, path, text)
	out, err := m.Read(context.Background(), ReadInput{Path: path, MaxBytes: 10})
	if err != nil || out.Text != "中中中" || !out.Truncated || out.Reason != "line_too_long" || out.NextLine != 1 || out.SHA256 != digest([]byte(text)) {
		t.Fatalf("long: %+v %v", out, err)
	}
	// UTF-8 字符跨越内部 32 KiB 分块时仍完整校验。
	text = strings.Repeat("a", (32<<10)-1) + "中文\n"
	writeText(t, path, text)
	out, err = m.Read(context.Background(), ReadInput{Path: path})
	if err != nil || out.Text != text {
		t.Fatalf("split rune: %v", err)
	}
	if err = os.WriteFile(path, []byte{'a', 0xff}, 0600); err != nil {
		t.Fatal(err)
	}
	_, err = m.Read(context.Background(), ReadInput{Path: path})
	requireCode(t, err, "invalid_utf8")
	writeText(t, path, strings.Repeat("x", int(m.cfg.MaxFileBytes)+1))
	_, err = m.Read(context.Background(), ReadInput{Path: path})
	requireCode(t, err, "file_too_large")
}

func TestListPaginationAndBudgets(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 7; i++ {
		writeText(t, filepath.Join(dir, fmt.Sprintf("item-%d", i)), "x")
	}
	m := managerForTest(t, DefaultConfig())
	seen := map[string]bool{}
	offset := 0
	for {
		out, err := m.List(context.Background(), ListInput{Path: dir, Offset: offset, Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range out.Entries {
			if seen[entry.Name] {
				t.Fatalf("duplicate %s", entry.Name)
			}
			seen[entry.Name] = true
		}
		if !out.Truncated {
			break
		}
		if out.NextOffset <= offset {
			t.Fatal("no progress")
		}
		offset = out.NextOffset
	}
	if len(seen) != 7 {
		t.Fatalf("found %d", len(seen))
	}
	cfg := DefaultConfig()
	cfg.MaxScanEntries = 3
	m2 := managerForTest(t, cfg)
	out, err := m2.List(context.Background(), ListInput{Path: dir})
	if err != nil || !out.Truncated || out.Reason != "scan_entry_limit" || out.ScannedEntries != 3 {
		t.Fatalf("scan budget: %+v %v", out, err)
	}
	large := t.TempDir()
	for i := 0; i < 20; i++ {
		writeText(t, filepath.Join(large, fmt.Sprintf("%02d-%s", i, strings.Repeat("名", 50))), "")
	}
	cfg = DefaultConfig()
	cfg.MaxResultBytes = 800
	m3 := managerForTest(t, cfg)
	out, err = m3.List(context.Background(), ListInput{Path: large})
	encoded, _ := json.Marshal(out)
	if err != nil || !out.Truncated || out.Reason != "byte_limit" || len(encoded) > 800 {
		t.Fatalf("result budget (%d): %+v %v", len(encoded), out, err)
	}
}

func TestSearchUTF8AndLongLineTailMatch(t *testing.T) {
	m := managerForTest(t, DefaultConfig())
	dir := t.TempDir()
	writeText(t, filepath.Join(dir, "中文.txt"), "第一行\r\n目标值\r\n末行")
	writeText(t, filepath.Join(dir, "long.txt"), strings.Repeat("x", 100000)+"目标值\n")
	out, err := m.Search(context.Background(), SearchInput{Path: dir, Query: "目标值"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Matches) != 2 || len(out.Issues) != 0 || !out.Truncated {
		t.Fatalf("search: %+v", out)
	}
	for _, match := range out.Matches {
		if filepath.Base(match.Path) == "long.txt" && (!match.Truncated || match.Line != 1) {
			t.Fatalf("long result: %+v", match)
		}
	}
	encoded, _ := json.Marshal(out)
	if len(encoded) > m.cfg.MaxResultBytes {
		t.Fatalf("encoded bytes %d", len(encoded))
	}
	// 字面 query 横跨两个块时不能漏报。
	writeText(t, filepath.Join(dir, "boundary"), strings.Repeat("x", (32<<10)-2)+"abcdef\n")
	out, err = m.Search(context.Background(), SearchInput{Path: dir, Query: "abcdef"})
	if err != nil || len(out.Matches) != 1 {
		t.Fatalf("boundary: %+v %v", out, err)
	}
}

func TestSearchBudgetsAndIssues(t *testing.T) {
	dir := t.TempDir()
	writeText(t, filepath.Join(dir, "good"), "find\nfind\nfind\n")
	if err := os.WriteFile(filepath.Join(dir, "invalid"), []byte{'f', 0xff}, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	writeText(t, filepath.Join(dir, "nested", "file"), "find")
	m := managerForTest(t, DefaultConfig())
	out, err := m.Search(context.Background(), SearchInput{Path: dir, Query: "find"})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Truncated || len(out.Issues) != 1 || out.Issues[0].Code != "invalid_utf8" || len(out.Matches) != 4 {
		t.Fatalf("issues: %+v", out)
	}
	depth := 0
	out, err = m.Search(context.Background(), SearchInput{Path: dir, Query: "find", MaxDepth: &depth})
	if err != nil || !out.Truncated || len(out.Matches) != 3 {
		t.Fatalf("depth: %+v %v", out, err)
	}
	for _, in := range []SearchInput{{Path: dir, Query: "find", MaxEntries: 1}, {Path: dir, Query: "find", MaxScanBytes: 1}, {Path: dir, Query: "find", MaxMatches: 1}, {Path: dir, Query: "find", MaxFileBytes: 1}} {
		out, err = m.Search(context.Background(), in)
		if err != nil || !out.Truncated {
			t.Fatalf("budget: %+v %v", out, err)
		}
		if in.MaxEntries > 0 && out.ScannedEntries > in.MaxEntries {
			t.Fatal("entry budget exceeded")
		}
		if in.MaxScanBytes > 0 && out.ScannedBytes > in.MaxScanBytes {
			t.Fatal("scan bytes exceeded")
		}
		if in.MaxMatches > 0 && len(out.Matches) > in.MaxMatches {
			t.Fatal("matches exceeded")
		}
	}
	_, err = m.Search(context.Background(), SearchInput{Path: filepath.Join(dir, "absent"), Query: "x"})
	requireCode(t, err, "not_found")
}

func TestPatchPreservesExactUneditedBytes(t *testing.T) {
	m := managerForTest(t, DefaultConfig())
	for _, tc := range []struct {
		name, original, want string
		edits                []Edit
	}{
		{"crlf", "甲\r\n乙\r\n末尾", "甲\r\n替换\r\n末尾", []Edit{{2, 1, "替换\r\n"}}},
		{"no-newline", "甲\n末尾", "甲\n终", []Edit{{2, 1, "终"}}},
		{"empty", "", "中文", []Edit{{1, 0, "中文"}}},
		{"append", "x", "xy", []Edit{{2, 0, "y"}}},
		{"ordered", "a\nb\nc\nd\n", "A\nb\nD", []Edit{{1, 1, "A\n"}, {3, 2, "D"}}},
		{"delete-all", "甲\r\n乙", "", []Edit{{1, 2, ""}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "file")
			writeText(t, path, tc.original)
			out, err := m.Patch(context.Background(), PatchInput{path, digest([]byte(tc.original)), tc.edits})
			if err != nil {
				t.Fatal(err)
			}
			actual, err := os.ReadFile(path)
			if err != nil || string(actual) != tc.want || out.SHA256 != digest(actual) || out.PreviousSHA256 != digest([]byte(tc.original)) {
				t.Fatalf("patch: %q %+v %v", actual, out, err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if runtime.GOOS != "windows" && info.Mode().Perm() != 0640 {
				t.Fatalf("permissions %v", info.Mode())
			}
			assertNoTemp(t, dir)
		})
	}
}

func assertNoTemp(t *testing.T, dir string) {
	t.Helper()
	entries, err := filepath.Glob(filepath.Join(dir, ".remote-mcp-patch-*"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary files: %v %v", entries, err)
	}
}

func TestPatchFailuresPreserveTarget(t *testing.T) {
	m := managerForTest(t, DefaultConfig())
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	original := "one\r\ntwo"
	writeText(t, path, original)
	for _, tc := range []struct {
		code string
		in   PatchInput
	}{
		{"conflict", PatchInput{path, digest([]byte("wrong")), []Edit{{1, 1, "x"}}}},
		{"invalid_patch", PatchInput{path, digest([]byte(original)), []Edit{{1, 2, "x"}, {2, 0, "y"}}}},
		{"invalid_patch", PatchInput{path, digest([]byte(original)), []Edit{{3, 1, "x"}}}},
		{"invalid_patch", PatchInput{path, digest([]byte(original)), []Edit{{1, 1, string([]byte{0xff})}}}},
		{"invalid_patch", PatchInput{path, digest([]byte(original)), []Edit{{2, 0, "a"}, {1, 0, "b"}}}},
	} {
		_, err := m.Patch(context.Background(), tc.in)
		requireCode(t, err, tc.code)
		actual, _ := os.ReadFile(path)
		if string(actual) != original {
			t.Fatal("target damaged")
		}
		assertNoTemp(t, dir)
	}
	m.publish = func(string, string, bool) error { return errors.New("simulated publication failure") }
	_, err := m.Patch(context.Background(), PatchInput{path, digest([]byte(original)), []Edit{{1, 1, "replace\n"}}})
	requireCode(t, err, "publish_failed")
	actual, _ := os.ReadFile(path)
	if string(actual) != original {
		t.Fatal("failed publication damaged target")
	}
	assertNoTemp(t, dir)
	// 最终发布失败同样可表现为取消；临时文件必须回收。
	m.publish = func(string, string, bool) error { return os.ErrPermission }
	_, err = m.Patch(context.Background(), PatchInput{path, digest([]byte(original)), []Edit{{1, 1, "replace\n"}}})
	requireCode(t, err, "publish_failed")
	assertNoTemp(t, dir)
}

func TestConcurrentPatchDetectsConflict(t *testing.T) {
	m := managerForTest(t, DefaultConfig())
	path := filepath.Join(t.TempDir(), "file")
	writeText(t, path, "original")
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, text := range []string{"first", "second"} {
		wg.Add(1)
		go func(text string) {
			defer wg.Done()
			_, err := m.Patch(context.Background(), PatchInput{path, digest([]byte("original")), []Edit{{1, 1, text}}})
			results <- err
		}(text)
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else {
			requireCode(t, err, "conflict")
			conflicts++
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
}

func TestCancellationCloseAndSymlink(t *testing.T) {
	m := managerForTest(t, DefaultConfig())
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	writeText(t, path, "中文")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := m.List(ctx, ListInput{Path: dir})
	requireCode(t, err, "cancelled")
	_, err = m.Read(ctx, ReadInput{Path: path})
	requireCode(t, err, "cancelled")
	_, err = m.Search(ctx, SearchInput{Path: dir, Query: "x"})
	requireCode(t, err, "cancelled")
	_, err = m.Patch(ctx, PatchInput{path, digest([]byte("中文")), []Edit{{1, 1, "x"}}})
	requireCode(t, err, "cancelled")
	deadline, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelDeadline()
	_, err = m.Read(deadline, ReadInput{Path: path})
	requireCode(t, err, "timeout")
	link := filepath.Join(dir, "link")
	if err = os.Symlink(path, link); err == nil {
		_, err = m.Read(context.Background(), ReadInput{Path: link})
		requireCode(t, err, "symlink")
		_, err = m.Patch(context.Background(), PatchInput{link, digest([]byte("中文")), []Edit{{1, 1, "x"}}})
		requireCode(t, err, "symlink")
	}
	// 锁等待必须随 manager Close 取消，不能阻塞退出。
	m.patchGate <- struct{}{}
	waiting := make(chan error, 1)
	go func() {
		_, err := m.Patch(context.Background(), PatchInput{path, digest([]byte("中文")), []Edit{{1, 1, "x"}}})
		waiting <- err
	}()
	closed := make(chan struct{})
	go func() { m.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close blocked")
	}
	select {
	case err = <-waiting:
		var typed *Error
		if !errors.As(err, &typed) || (typed.Code != "closed" && typed.Code != "cancelled") {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("patch wait blocked")
	}
	<-m.patchGate
	_, err = m.Read(context.Background(), ReadInput{Path: path})
	requireCode(t, err, "closed")
}

func TestLimitsAndPermissionFailure(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxDepth = 17
	_, err := New(cfg)
	requireCode(t, err, "invalid_argument")
	m := managerForTest(t, DefaultConfig())
	path := filepath.Join(t.TempDir(), "private")
	writeText(t, path, "private")
	if runtime.GOOS == "windows" {
		t.Skip("Unix mode permission check")
	}
	if err = os.Chmod(path, 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0600)
	_, err = m.Read(context.Background(), ReadInput{Path: path})
	if err == nil {
		t.Skip("Current account bypasses file mode permissions")
	}
	requireCode(t, err, "permission_denied")
}

func TestRevalidationDetectsIdentityAndHashChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	writeText(t, path, "original")
	before, err := statPath(path)
	if err != nil {
		t.Fatal(err)
	}
	writeText(t, path, "modified")
	if err = os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	requireCode(t, verifyTarget(context.Background(), path, before, digest([]byte("original")), DefaultConfig()), "conflict")
	writeText(t, path, "original")
	before, err = statPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path, filepath.Join(dir, "old")); err != nil {
		t.Fatal(err)
	}
	writeText(t, path, "original")
	if err = os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	requireCode(t, verifyTarget(context.Background(), path, before, digest([]byte("original")), DefaultConfig()), "conflict")
}

func TestStreamingCancellationAndWriteFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	original := strings.Repeat("line\n", 10000)
	writeText(t, path, original)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ctx, cancel := context.WithCancel(context.Background())
	_, err = scanText(ctx, f, DefaultConfig().MaxFileBytes, 64<<10, func(int, []byte, bool) error { cancel(); return nil })
	requireCode(t, err, "cancelled")
	// 真实只读句柄上的写失败必须有稳定原因，不能改变文件。
	f, err = os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	requireCode(t, writeTemporary(context.Background(), f, []byte("overwrite")), "write_failed")
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != original {
		t.Fatal("failed write damaged file")
	}
	m := managerForTest(t, DefaultConfig())
	m.patchGate <- struct{}{}
	defer func() { <-m.patchGate }()
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err = m.Patch(ctx, PatchInput{path, digest([]byte(original)), []Edit{{1, 1, "x"}}})
	requireCode(t, err, "timeout")
	assertNoTemp(t, dir)
}
