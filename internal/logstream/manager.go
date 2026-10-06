package logstream

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type resource struct {
	mu         sync.Mutex
	path       string
	file       *os.File
	identity   os.FileInfo
	generation uint64
	size       int64
	change     string
	lastCall   time.Time
	active     int
	closed     bool
	done       chan struct{}
}
type Manager struct {
	mu        sync.Mutex
	cfg       Config
	resources map[string]*resource
	closed    bool
	stop      chan struct{}
	sweepDone chan struct{}
	closeDone chan struct{}
	closeErr  error
	sweepErr  error
}

func New(cfg Config) (*Manager, error) {
	if cfg.MaxOpen < 1 || cfg.ReadBytes < 1 || cfg.ReadBytes > 64<<10 || cfg.IdleTimeout <= 0 || cfg.PollInterval < time.Millisecond || cfg.PollInterval > time.Second || cfg.SweepInterval <= 0 {
		return nil, failure("invalid_argument", "Log resource settings are invalid; read bytes must not exceed 65536 and polling must be between 1 and 1000 milliseconds")
	}
	m := &Manager{cfg: cfg, resources: map[string]*resource{}, stop: make(chan struct{}), sweepDone: make(chan struct{}), closeDone: make(chan struct{})}
	go m.sweep()
	return m, nil
}
func fileError(err error) error {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return failure("not_found", "The log file does not exist")
	case errors.Is(err, os.ErrPermission):
		return failure("permission_denied", "Permission to access the log file was denied")
	default:
		return failure("io_error", "Failed to access the log file")
	}
}
func openRegular(path string) (*os.File, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, fileError(err)
	}
	if !info.Mode().IsRegular() {
		return nil, nil, failure("not_regular", "The log path must identify a regular file without a symbolic link")
	}
	f, err := openFile(path)
	if err != nil {
		return nil, nil, fileError(err)
	}
	actual, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, fileError(err)
	}
	if !actual.Mode().IsRegular() || !os.SameFile(info, actual) {
		f.Close()
		return nil, nil, failure("not_regular", "The log path changed while opening the regular file")
	}
	return f, actual, nil
}
func (m *Manager) Open(_ context.Context, in OpenInput) (OpenResult, error) {
	if in.Path == "" || len(in.Path) > 32768 {
		return OpenResult{}, failure("invalid_argument", "The log path must contain between 1 and 32768 bytes")
	}
	path, err := filepath.Abs(in.Path)
	if err != nil {
		return OpenResult{}, failure("invalid_argument", "The log path is invalid")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return OpenResult{}, failure("closed", "The log manager is closed")
	}
	if len(m.resources) >= m.cfg.MaxOpen {
		return OpenResult{}, failure("resource_limit", "The open log resource limit has been reached")
	}
	f, info, err := openRegular(path)
	if err != nil {
		return OpenResult{}, err
	}
	var idBytes [16]byte
	if _, err = rand.Read(idBytes[:]); err != nil {
		f.Close()
		return OpenResult{}, failure("internal", "Failed to generate a log resource ID")
	}
	id := hex.EncodeToString(idBytes[:])
	m.resources[id] = &resource{path: path, file: f, identity: info, generation: 1, size: info.Size(), lastCall: time.Now(), done: make(chan struct{})}
	return OpenResult{id, 1, info.Size()}, nil
}
func (m *Manager) acquire(id string) (*resource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, failure("closed", "The log manager is closed")
	}
	r := m.resources[id]
	if r == nil {
		return nil, failure("not_found", "The log resource does not exist or has expired")
	}
	r.mu.Lock()
	r.active++
	r.lastCall = time.Now()
	r.mu.Unlock()
	return r, nil
}

// 调用始终提供代际时才能在并发读取和轮转之间保留准确游标。
func (r *resource) read(in ReadInput) (ReadResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ReadResult{}, failure("closed", "The log resource is closed")
	}
	info, err := os.Lstat(r.path)
	if err != nil {
		return ReadResult{}, fileError(err)
	}
	if !info.Mode().IsRegular() {
		return ReadResult{}, failure("not_regular", "The log path must identify a regular file without a symbolic link")
	}
	changed := false
	if !os.SameFile(r.identity, info) {
		f, actual, err := openRegular(r.path)
		if err != nil {
			return ReadResult{}, err
		}
		old := r.file
		r.file, r.identity, info = f, actual, actual
		r.generation++
		r.change = "rotated"
		changed = true
		// 切换后再关闭旧句柄；失败仍保留可用的新资源供下一次读取。
		if err = old.Close(); err != nil {
			r.size = actual.Size()
			return ReadResult{}, failure("io_error", "Failed to close the previous log file after rotation; tracking continues with the replacement file")
		}
	} else if info.Size() < r.size {
		r.generation++
		r.change = "truncated"
		changed = true
	}
	r.size = info.Size()
	out := ReadResult{ID: in.ID, Generation: r.generation, EndCursor: r.size, ValidUTF8: true}
	cursor := in.Cursor
	if in.Generation > r.generation {
		return ReadResult{}, failure("invalid_argument", "The log generation is outside the available range")
	}
	if (in.Generation != 0 && in.Generation != r.generation) || (in.Generation == 0 && changed) {
		cursor = 0
		out.Truncated = true
		out.Rotated = r.change == "rotated"
	}
	if cursor > r.size {
		return ReadResult{}, failure("invalid_argument", "The cursor is outside the log file range")
	}
	n := min(int64(in.Limit), r.size-cursor)
	data := make([]byte, int(n))
	read, err := r.file.ReadAt(data, cursor)
	if err != nil && !errors.Is(err, io.EOF) {
		return ReadResult{}, fileError(err)
	}
	data = data[:read]
	out.StartCursor, out.NextCursor = cursor, cursor+int64(read)
	out.DataBase64 = base64.StdEncoding.EncodeToString(data)
	out.Text = strings.ToValidUTF8(string(data), "�")
	out.ValidUTF8 = utf8.Valid(data)
	return out, nil
}
func hasCode(err error, code string) bool {
	var typed *Error
	return errors.As(err, &typed) && typed.Code == code
}

// 路径暂缺时保留上一代坐标，不从已经移走的文件偷偷返回旧内容。
func (r *resource) snapshot(in ReadInput) (ReadResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if in.Generation > r.generation {
		return ReadResult{}, failure("invalid_argument", "The log generation is outside the available range")
	}
	cursor := in.Cursor
	out := ReadResult{ID: in.ID, Generation: r.generation, EndCursor: r.size, ValidUTF8: true}
	if in.Generation != 0 && in.Generation != r.generation {
		cursor = 0
		out.Truncated, out.Rotated = true, r.change == "rotated"
	}
	if cursor > r.size {
		return ReadResult{}, failure("invalid_argument", "The cursor is outside the log file range")
	}
	out.StartCursor, out.NextCursor = cursor, cursor
	return out, nil
}
func (m *Manager) Read(ctx context.Context, in ReadInput) (ReadResult, error) {
	if in.Cursor < 0 || in.WaitMS < 0 || in.WaitMS > 30000 {
		return ReadResult{}, failure("invalid_argument", "The cursor must be nonnegative and wait_ms must be between 0 and 30000")
	}
	if in.Limit == 0 {
		in.Limit = m.cfg.ReadBytes
	}
	if in.Limit < 1 || in.Limit > m.cfg.ReadBytes {
		return ReadResult{}, failure("invalid_argument", "The read length exceeds the configured limit")
	}
	r, err := m.acquire(in.ID)
	if err != nil {
		return ReadResult{}, err
	}
	defer func() { r.mu.Lock(); r.active--; r.lastCall = time.Now(); r.mu.Unlock() }()
	initial, err := r.read(in)
	if err != nil {
		if in.WaitMS == 0 || (!hasCode(err, "not_found") && !hasCode(err, "closed")) {
			return ReadResult{}, err
		}
		closed := hasCode(err, "closed")
		initial, err = r.snapshot(in)
		if err != nil {
			return ReadResult{}, err
		}
		if closed {
			initial.Reason = "cancelled"
			return initial, nil
		}
	}
	// 固定本次等待的代际，轮转后不将旧游标用于新文件。
	if in.Generation == 0 {
		in.Generation = initial.Generation
	}
	if in.WaitMS == 0 {
		initial.Reason = "immediate"
		return initial, nil
	}
	timer := time.NewTimer(time.Duration(in.WaitMS) * time.Millisecond)
	defer timer.Stop()
	poll := time.NewTicker(m.cfg.PollInterval)
	defer poll.Stop()
	out := initial
	finish := ""
	for {
		switch {
		case finish != "":
			out.Reason = finish
		case out.Rotated:
			out.Reason = "rotated"
		case out.Truncated:
			out.Reason = "truncated"
		case out.NextCursor > out.StartCursor:
			out.Reason = "output"
		}
		if out.Reason != "" {
			return out, nil
		}
		select {
		case <-r.done:
			out.Reason = "cancelled"
			return out, nil
		case <-ctx.Done():
			finish = "cancelled"
		case <-timer.C:
			finish = "timeout"
		case <-poll.C:
		}
		next, err := r.read(in)
		if err != nil {
			if hasCode(err, "closed") {
				out.Reason = "cancelled"
				return out, nil
			}
			if finish != "" {
				out.Reason = finish
				return out, nil
			}
			// 轮转常有短暂路径缺失；保持旧快照，在本次预算内等待重建。
			if hasCode(err, "not_found") {
				continue
			}
			return ReadResult{}, err
		}
		out = next
	}
}
func closeResource(r *resource) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	close(r.done)
	if err := r.file.Close(); err != nil {
		return failure("io_error", "Failed to close the log file")
	}
	return nil
}
func (m *Manager) CloseLog(_ context.Context, in IDInput) (CloseResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.resources[in.ID]
	if r != nil {
		delete(m.resources, in.ID)
		if err := closeResource(r); err != nil {
			return CloseResult{}, err
		}
	}
	return CloseResult{in.ID, true}, nil
}
func (m *Manager) sweep() {
	defer close(m.sweepDone)
	ticker := time.NewTicker(m.cfg.SweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case now := <-ticker.C:
			m.mu.Lock()
			for id, r := range m.resources {
				r.mu.Lock()
				expired := r.active == 0 && now.Sub(r.lastCall) >= m.cfg.IdleTimeout
				r.mu.Unlock()
				if expired {
					if err := closeResource(r); err != nil && m.sweepErr == nil {
						m.sweepErr = err
					}
					delete(m.resources, id)
				}
			}
			m.mu.Unlock()
		}
	}
}
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		<-m.closeDone
		return m.closeErr
	}
	m.closed = true
	close(m.stop)
	var errs []error
	if m.sweepErr != nil {
		errs = append(errs, m.sweepErr)
	}
	for id, r := range m.resources {
		if err := closeResource(r); err != nil {
			errs = append(errs, err)
		}
		delete(m.resources, id)
	}
	m.mu.Unlock()
	<-m.sweepDone
	m.closeErr = errors.Join(errs...)
	close(m.closeDone)
	return m.closeErr
}
