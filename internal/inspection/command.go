package inspection

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var runtimeCommands = map[string][]string{"go": {"go", "version"}, "node": {"node", "--version"}, "python": {"python3", "--version"}, "java": {"java", "-version"}, "dotnet": {"dotnet", "--version"}}

// 两条流共享捕获限额；达到限额后取消命令，WaitDelay 限制遗留管道等待。
type commandCapture struct {
	mu             sync.Mutex
	stdout, stderr bytes.Buffer
	max            int
	truncated      bool
	cancel         context.CancelFunc
}
type commandWriter struct {
	capture *commandCapture
	stderr  bool
}

func (w commandWriter) Write(p []byte) (int, error) {
	b := w.capture
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	remaining := b.max - b.stdout.Len() - b.stderr.Len()
	dst := &b.stdout
	if w.stderr {
		dst = &b.stderr
	}
	if len(p) > remaining {
		dst.Write(p[:remaining])
		b.truncated = true
		b.cancel()
	} else {
		dst.Write(p)
	}
	return n, nil
}
func runCommandStreams(ctx context.Context, max int, path string, args ...string) ([]byte, []byte, bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	b := &commandCapture{max: max, cancel: cancel}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdout = commandWriter{b, false}
	cmd.Stderr = commandWriter{b, true}
	cmd.WaitDelay = 100 * time.Millisecond
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	err := cmd.Run()
	return b.stdout.Bytes(), b.stderr.Bytes(), b.truncated, err
}
func runCommand(ctx context.Context, max int, path string, args ...string) ([]byte, bool, error) {
	stdout, stderr, truncated, err := runCommandStreams(ctx, max, path, args...)
	return append(stdout, stderr...), truncated, err
}
func fixedCommandError(ctx context.Context, err error, stderr []byte) *Error {
	if ctx.Err() != nil {
		return contextError(ctx.Err())
	}
	text := strings.ToLower(string(stderr))
	if strings.Contains(text, "permission denied") || strings.Contains(text, "operation not permitted") {
		return failure("permission_denied", "The fixed system command could not access data with the current permissions")
	}
	return systemError(err)
}

// LookPath 在 Unix 上可能把存在但不可执行的文件合并为 ErrNotFound。
// 只对固定白名单名字补充权限识别，不执行 PATH 中不满足 LookPath 契约的候选。
func findExecutable(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err == nil || runtime.GOOS == "windows" || !errors.Is(err, exec.ErrNotFound) {
		return path, err
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		candidate := filepath.Join(dir, name)
		info, e := os.Stat(candidate)
		if errors.Is(e, os.ErrPermission) {
			return "", os.ErrPermission
		}
		if e == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 == 0 {
			return "", os.ErrPermission
		}
	}
	return "", err
}
func (m *Manager) inspectRuntime(ctx context.Context, name string) Runtime {
	r := Runtime{Name: name}
	spec := runtimeCommands[name]
	path, err := findExecutable(spec[0])
	if err != nil && name == "python" && errors.Is(err, exec.ErrNotFound) {
		path, err = findExecutable("python")
	}
	if err != nil {
		e := systemError(err)
		r.Code = e.Code
		r.Message = e.Message
		return r
	}
	r.Available = true
	r.Path, r.Truncated = boundedString(path, m.cfg.MaxNameBytes)
	b, truncated, err := runCommand(ctx, m.cfg.MaxCommandBytes, path, spec[1:]...)
	version, truncatedName := boundedString(strings.TrimSpace(string(b)), m.cfg.MaxNameBytes)
	r.Version = version
	r.Truncated = r.Truncated || truncated || truncatedName
	if ctx.Err() != nil {
		e := contextError(ctx.Err())
		r.Code = e.Code
		r.Message = e.Message
	} else if err != nil && !truncated {
		e := systemError(err)
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			e = failure("io_error", "The runtime version command exited with a nonzero status")
		}
		if errors.Is(err, exec.ErrWaitDelay) {
			e = failure("io_error", "The runtime version command left output pipes open beyond the wait limit")
		}
		r.Code = e.Code
		r.Message = e.Message
		if e.Code == "permission_denied" {
			r.Available = false
		}
	}
	return r
}
func readBounded(path string, max int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, int64(max+1)))
	if err != nil {
		return nil, err
	}
	if len(b) > max {
		return nil, failure("resource_limit", "A system data record exceeded the byte limit")
	}
	return b, nil
}
