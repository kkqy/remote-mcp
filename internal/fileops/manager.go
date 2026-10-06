package fileops

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"

	"remote-mcp/internal/transfer"
)

type Manager struct {
	cfg       Config
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	closed    bool
	wg        sync.WaitGroup
	patchGate chan struct{}
	publish   func(string, string, bool) error
}

func New(cfg Config) (*Manager, error) {
	max := DefaultConfig()
	if cfg.MaxFileBytes <= 0 || cfg.MaxFileBytes > max.MaxFileBytes || cfg.MaxResultBytes < 512 || cfg.MaxResultBytes > max.MaxResultBytes || cfg.MaxListEntries <= 0 || cfg.MaxListEntries > max.MaxListEntries || cfg.MaxScanEntries <= 0 || cfg.MaxScanEntries > max.MaxScanEntries || cfg.MaxScanBytes <= 0 || cfg.MaxScanBytes > max.MaxScanBytes || cfg.MaxDepth < 0 || cfg.MaxDepth > max.MaxDepth || cfg.MaxMatches <= 0 || cfg.MaxMatches > max.MaxMatches {
		return nil, failure("invalid_argument", "Invalid file operation limits")
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{cfg: cfg, ctx: ctx, cancel: cancel, patchGate: make(chan struct{}, 1), publish: transfer.Publish}, nil
}

func (m *Manager) begin(ctx context.Context) (context.Context, func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, nil, failure("closed", "File operation manager is closed")
	}
	m.wg.Add(1)
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(m.ctx, cancel)
	return ctx, func() { stop(); cancel(); m.wg.Done() }, nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	m.closed = true
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
	return nil
}

func contextError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return failure("timeout", "File operation deadline exceeded")
	}
	if ctx.Err() != nil {
		return failure("cancelled", "File operation was cancelled")
	}
	return nil
}

func ioError(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return failure("not_found", "File or directory does not exist")
	}
	if errors.Is(err, os.ErrPermission) {
		return failure("permission_denied", "Permission denied while accessing the file or directory")
	}
	return failure("io_error", "File system operation failed")
}

func openPath(path string, directory bool) (*os.File, os.FileInfo, error) {
	return openPathWith(path, directory, openFile)
}

// 打开器单独传入供竞争窗口回归注入，正式路径使用拒绝链接的非阻塞平台打开。
func openPathWith(path string, directory bool, open func(string) (*os.File, error)) (*os.File, os.FileInfo, error) {
	if strings.TrimSpace(path) == "" || strings.ContainsRune(path, '\x00') {
		return nil, nil, failure("invalid_argument", "A non-empty valid path is required")
	}
	before, err := statPath(path)
	if err != nil {
		return nil, nil, ioError(err)
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return nil, nil, failure("symlink", "Symbolic link targets are not supported")
	}
	if directory && !before.IsDir() {
		return nil, nil, failure("not_directory", "Target is not a directory")
	}
	if !directory && !before.Mode().IsRegular() {
		return nil, nil, failure("not_regular", "Target is not a regular file")
	}
	f, err := open(path)
	if err != nil {
		return nil, nil, ioError(err)
	}
	after, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, ioError(err)
	}
	if !os.SameFile(before, after) || (directory && !after.IsDir()) || (!directory && !after.Mode().IsRegular()) {
		f.Close()
		return nil, nil, failure("conflict", "Target changed while it was being opened")
	}
	return f, after, nil
}

func entryKind(info os.FileInfo) string {
	if info.Mode()&os.ModeSymlink != 0 {
		return "symlink"
	}
	if info.IsDir() {
		return "directory"
	}
	if info.Mode().IsRegular() {
		return "file"
	}
	return "other"
}

func (m *Manager) List(ctx context.Context, in ListInput) (out ListResult, err error) {
	ctx, done, err := m.begin(ctx)
	if err != nil {
		return out, err
	}
	defer done()
	if in.Limit == 0 {
		in.Limit = m.cfg.MaxListEntries
	}
	if in.Offset < 0 || in.Offset > m.cfg.MaxScanEntries || in.Limit < 1 || in.Limit > m.cfg.MaxListEntries {
		return out, failure("invalid_argument", "Invalid directory offset or limit")
	}
	if err = contextError(ctx); err != nil {
		return out, err
	}
	f, _, err := openPath(in.Path, true)
	if err != nil {
		return out, err
	}
	defer f.Close()
	out.Entries = []Entry{}
	resultBytes := 512
	for out.ScannedEntries < m.cfg.MaxScanEntries {
		if err = contextError(ctx); err != nil {
			return ListResult{}, err
		}
		entries, readErr := f.ReadDir(1)
		if readErr != nil && readErr != io.EOF {
			return ListResult{}, ioError(readErr)
		}
		if len(entries) == 0 {
			out.NextOffset = out.ScannedEntries
			return out, nil
		}
		index := out.ScannedEntries
		out.ScannedEntries++
		if index < in.Offset {
			continue
		}
		if len(out.Entries) == in.Limit {
			out.Truncated = true
			out.Reason = "entry_limit"
			out.NextOffset = index
			return out, nil
		}
		info, statErr := entries[0].Info()
		if statErr != nil {
			return ListResult{}, ioError(statErr)
		}
		entry := Entry{info.Name(), entryKind(info), info.Size()}
		encoded, _ := json.Marshal(entry)
		if resultBytes+len(encoded)+1 > m.cfg.MaxResultBytes {
			out.Truncated = true
			out.Reason = "byte_limit"
			out.NextOffset = index
			return out, nil
		}
		out.Entries = append(out.Entries, entry)
		resultBytes += len(encoded) + 1
		out.NextOffset = index + 1
	}
	out.Truncated = true
	out.Reason = "scan_entry_limit"
	return out, nil
}
