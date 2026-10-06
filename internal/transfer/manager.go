package transfer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type entry struct {
	id      string
	kind    string
	input   UploadInput
	path    string
	temp    string
	file    *os.File
	info    os.FileInfo
	size    int64
	offset  int64
	digest  string
	hash    hash.Hash
	state   string
	touched time.Time
}

// Manager 的锁串行化文件状态转换，哈希仅使用固定大小缓冲。
type Manager struct {
	mu       sync.Mutex
	cfg      Config
	entries  map[string]*entry
	requests map[string]string
	closed   bool
	stop     chan struct{}
	done     chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
	pending  sync.WaitGroup
}

func New(cfg Config) (*Manager, error) {
	if cfg.MaxFileSize <= 0 || cfg.ChunkSize <= 0 || cfg.ChunkSize > 16<<20 || cfg.MaxTransfers <= 0 || cfg.MaxRecords < cfg.MaxTransfers || cfg.IdleTimeout <= 0 || cfg.Retention <= 0 {
		return nil, errors.New("Invalid file transfer limits")
	}
	m := &Manager{cfg: cfg, entries: make(map[string]*entry), requests: make(map[string]string), stop: make(chan struct{}), done: make(chan struct{})}
	m.ctx, m.cancel = context.WithCancel(context.Background())
	interval := min(cfg.IdleTimeout, cfg.Retention) / 2
	if interval > time.Minute {
		interval = time.Minute
	}
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	go func() {
		defer close(m.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-m.stop:
				return
			case now := <-ticker.C:
				m.mu.Lock()
				m.sweep(now)
				m.mu.Unlock()
			}
		}
	}()
	return m, nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	var err error
	if !m.closed {
		m.closed = true
		m.cancel()
		close(m.stop)
		for _, e := range m.entries {
			err = errors.Join(err, m.release(e, "closed"))
		}
		clear(m.entries)
		clear(m.requests)
	}
	m.mu.Unlock()
	<-m.done
	m.pending.Wait()
	return err
}

func (m *Manager) release(e *entry, state string) error {
	var err error
	if e.file != nil {
		err = e.file.Close()
		e.file = nil
	}
	if e.temp != "" {
		if removeErr := os.Remove(e.temp); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, removeErr)
		} else {
			e.temp = ""
		}
	}
	e.state = state
	e.touched = time.Now()
	return err
}
func (m *Manager) sweep(now time.Time) {
	for id, e := range m.entries {
		if e.state == "active" && now.Sub(e.touched) >= m.cfg.IdleTimeout {
			_ = m.release(e, "expired")
		}
		if e.state != "active" && e.state != "initializing" && now.Sub(e.touched) >= m.cfg.Retention {
			if e.temp != "" {
				_ = m.release(e, e.state)
				continue
			}
			delete(m.entries, id)
			if e.input.RequestID != "" {
				delete(m.requests, e.input.RequestID)
			}
		}
	}
}
func failure(code, message string) Result { return Result{Code: code, Message: message} }
func ioFailure(err error) Result {
	switch {
	case errors.Is(err, os.ErrExist):
		return failure("CONFLICT", "The destination already exists")
	case errors.Is(err, os.ErrNotExist):
		return failure("NOT_FOUND", "The file or directory does not exist")
	case errors.Is(err, os.ErrPermission):
		return failure("PERMISSION_DENIED", "Insufficient file permissions")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return failure("CANCELED", "The operation was canceled or timed out")
	default:
		return failure("IO_ERROR", "The file operation failed")
	}
}
func (m *Manager) admission() Result {
	if m.closed {
		return failure("CLOSED", "The file manager is closed")
	}
	m.sweep(time.Now())
	active := 0
	for _, e := range m.entries {
		if e.state == "active" || e.state == "initializing" {
			active++
		}
	}
	if active >= m.cfg.MaxTransfers || len(m.entries) >= m.cfg.MaxRecords {
		return failure("LIMIT_EXCEEDED", "The transfer count or retained record limit has been reached")
	}
	return Result{OK: true}
}
func (m *Manager) result(e *entry) Result {
	return Result{OK: true, ID: e.id, Path: e.path, Size: e.size, SHA256: e.digest, ChunkSize: m.cfg.ChunkSize, Offset: e.offset, State: e.state}
}
func (m *Manager) lookup(id, kind string) (*entry, Result) {
	if m.closed {
		return nil, failure("CLOSED", "The file manager is closed")
	}
	e := m.entries[id]
	if e == nil || e.kind != kind {
		return nil, failure("NOT_FOUND", "The transfer does not exist")
	}
	if e.state == "active" && time.Since(e.touched) >= m.cfg.IdleTimeout {
		_ = m.release(e, "expired")
	}
	if e.state != "active" {
		return nil, failure("INVALID_STATE", "The transfer has ended: "+e.state)
	}
	e.touched = time.Now()
	return e, Result{}
}
func absolute(path string) (string, error) {
	if path == "" {
		return "", errors.New("The path is empty")
	}
	return filepath.Abs(path)
}
func newID() string { return hex.EncodeToString(randBytes()) }
func randBytes() []byte {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

func (m *Manager) Stat(_ context.Context, in PathInput) Result {
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return failure("CLOSED", "The file manager is closed")
	}
	path, err := absolute(in.Path)
	if err != nil {
		return failure("INVALID_ARGUMENT", "Invalid path")
	}
	info, err := os.Stat(path)
	if err != nil {
		return ioFailure(err)
	}
	if !info.Mode().IsRegular() {
		return failure("INVALID_ARGUMENT", "Only regular files are supported")
	}
	return Result{OK: true, Path: path, Size: info.Size()}
}
func (m *Manager) UploadCreate(_ context.Context, in UploadInput) Result {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return failure("CLOSED", "The file manager is closed")
	}
	path, err := absolute(in.Path)
	if err != nil {
		return failure("INVALID_ARGUMENT", "Invalid path")
	}
	digest, err := hex.DecodeString(in.SHA256)
	if in.RequestID == "" || len(in.RequestID) > 128 || in.Size < 0 || err != nil || len(digest) != sha256.Size {
		return failure("INVALID_ARGUMENT", "Invalid request_id, size, or sha256")
	}
	in.Path = path
	in.SHA256 = strings.ToLower(in.SHA256)
	m.sweep(time.Now())
	if id, ok := m.requests[in.RequestID]; ok {
		e := m.entries[id]
		if e.input != in {
			return failure("CONFLICT", "The parameters differ for the same request_id")
		}
		if e.state == "active" {
			e.touched = time.Now()
		}
		return m.result(e)
	}
	if in.Size > m.cfg.MaxFileSize {
		return failure("LIMIT_EXCEEDED", "The file exceeds the size limit")
	}
	if r := m.admission(); !r.OK {
		return r
	}
	if !in.Overwrite {
		if _, err := os.Lstat(path); err == nil {
			return failure("CONFLICT", "The destination already exists")
		} else if !errors.Is(err, os.ErrNotExist) {
			return ioFailure(err)
		}
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".remote-mcp-upload-*")
	if err != nil {
		return ioFailure(err)
	}
	e := &entry{id: newID(), kind: "upload", input: in, path: path, temp: file.Name(), file: file, size: in.Size, digest: in.SHA256, hash: sha256.New(), state: "active", touched: time.Now()}
	m.entries[e.id] = e
	m.requests[in.RequestID] = e.id
	return m.result(e)
}
func (m *Manager) UploadWrite(_ context.Context, in WriteInput) Result {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, r := m.lookup(in.ID, "upload")
	if e == nil {
		return r
	}
	if in.Offset != e.offset {
		return failure("INVALID_OFFSET", "The offset must match the next write position")
	}
	if len(in.Data) > base64.StdEncoding.EncodedLen(m.cfg.ChunkSize) {
		return failure("LIMIT_EXCEEDED", "The chunk exceeds the size limit")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(in.Data)
	if err != nil || len(data) == 0 {
		return failure("INVALID_ARGUMENT", "The chunk must be nonempty valid Base64")
	}
	if len(data) > m.cfg.ChunkSize || int64(len(data)) > e.size-e.offset {
		return failure("LIMIT_EXCEEDED", "The chunk exceeds the chunk limit or declared file length")
	}
	n, err := e.file.Write(data)
	if err != nil || n != len(data) {
		_ = m.release(e, "failed")
		return failure("IO_ERROR", "Failed to write the chunk")
	}
	_, _ = e.hash.Write(data)
	e.offset += int64(n)
	return m.result(e)
}
func (m *Manager) UploadFinish(_ context.Context, in IDInput) Result {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[in.ID]; !m.closed && e != nil && e.kind == "upload" && e.state == "completed" {
		return m.result(e)
	}
	e, r := m.lookup(in.ID, "upload")
	if e == nil {
		return r
	}
	if e.offset != e.size || hex.EncodeToString(e.hash.Sum(nil)) != e.digest {
		_ = m.release(e, "failed")
		return failure("CHECKSUM_MISMATCH", "The file length or SHA-256 does not match; the upload has been cleaned up")
	}
	if err := e.file.Sync(); err != nil {
		_ = m.release(e, "failed")
		return ioFailure(err)
	}
	if err := e.file.Close(); err != nil {
		e.file = nil
		_ = m.release(e, "failed")
		return ioFailure(err)
	}
	e.file = nil
	if err := Publish(e.temp, e.path, e.input.Overwrite); err != nil {
		_ = m.release(e, "failed")
		return ioFailure(err)
	}
	_ = m.release(e, "completed")
	return m.result(e)
}
func (m *Manager) UploadCancel(_ context.Context, in IDInput) Result {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[in.ID]
	if m.closed || e == nil || e.kind != "upload" {
		return failure("NOT_FOUND", "The upload does not exist")
	}
	if e.state == "active" {
		if err := m.release(e, "canceled"); err != nil {
			return ioFailure(err)
		}
	}
	return m.result(e)
}

// HashFile 从头流式计算哈希，不把文件整体装入内存。
func HashFile(ctx context.Context, file *os.File) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	reader := io.LimitReader(file, info.Size())
	h := sha256.New()
	buf := make([]byte, 256<<10)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := reader.Read(buf)
		if n > 0 {
			_, _ = h.Write(buf[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Unchanged 同时检查句柄和路径，能检测替换、长度和修改时间变化；不提供文件系统快照。
func Unchanged(file *os.File, path string, before os.FileInfo) bool {
	current, err := file.Stat()
	if err != nil {
		return false
	}
	named, err := os.Stat(path)
	if err != nil {
		return false
	}
	return os.SameFile(before, current) && os.SameFile(before, named) && before.Size() == current.Size() && before.Size() == named.Size() && before.ModTime().Equal(current.ModTime()) && before.ModTime().Equal(named.ModTime())
}
func (m *Manager) DownloadOpen(ctx context.Context, in PathInput) Result {
	path, err := absolute(in.Path)
	if err != nil {
		return failure("INVALID_ARGUMENT", "Invalid path")
	}
	m.mu.Lock()
	if r := m.admission(); !r.OK {
		m.mu.Unlock()
		return r
	}
	e := &entry{id: newID(), kind: "download", path: path, state: "initializing", touched: time.Now()}
	m.entries[e.id] = e
	m.pending.Add(1)
	workCtx, cancel := context.WithCancel(m.ctx)
	stopCancel := context.AfterFunc(ctx, cancel)
	m.mu.Unlock()
	defer m.pending.Done()
	defer cancel()
	defer stopCancel()
	committed := false
	defer func() {
		if !committed {
			m.mu.Lock()
			delete(m.entries, e.id)
			m.mu.Unlock()
		}
	}()
	// 初始化仅预留名额，长时间哈希不持有管理器锁，服务关闭可以取消它。
	info, err := os.Stat(path)
	if err != nil {
		return ioFailure(err)
	}
	if !info.Mode().IsRegular() {
		return failure("INVALID_ARGUMENT", "Only regular files are supported")
	}
	if info.Size() > m.cfg.MaxFileSize {
		return failure("LIMIT_EXCEEDED", "The file exceeds the size limit")
	}
	file, err := os.Open(path)
	if err != nil {
		return ioFailure(err)
	}
	defer func() {
		if !committed {
			_ = file.Close()
		}
	}()
	if !Unchanged(file, path, info) {
		return failure("SOURCE_CHANGED", "The source file has changed")
	}
	digest, err := HashFile(workCtx, file)
	if err != nil {
		return ioFailure(err)
	}
	if !Unchanged(file, path, info) {
		return failure("SOURCE_CHANGED", "The source file changed while its hash was being computed")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || workCtx.Err() != nil {
		return failure("CANCELED", "Download initialization was canceled")
	}
	e.file = file
	e.info = info
	e.size = info.Size()
	e.digest = digest
	e.state = "active"
	e.touched = time.Now()
	committed = true
	return m.result(e)
}
func (m *Manager) DownloadRead(_ context.Context, in ReadInput) Result {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, r := m.lookup(in.ID, "download")
	if e == nil {
		return r
	}
	if in.Offset < 0 || in.Offset > e.size || in.Length <= 0 || in.Length > m.cfg.ChunkSize {
		return failure("INVALID_ARGUMENT", "Invalid read offset or length")
	}
	if !Unchanged(e.file, e.path, e.info) {
		_ = m.release(e, "failed")
		return failure("SOURCE_CHANGED", "The download source file has changed")
	}
	data := make([]byte, min(int64(in.Length), e.size-in.Offset))
	n, err := e.file.ReadAt(data, in.Offset)
	if err != nil && err != io.EOF {
		_ = m.release(e, "failed")
		return ioFailure(err)
	}
	if n != len(data) || !Unchanged(e.file, e.path, e.info) {
		_ = m.release(e, "failed")
		return failure("SOURCE_CHANGED", "The download source file has changed")
	}
	e.offset = in.Offset + int64(n)
	r = m.result(e)
	r.Data = base64.StdEncoding.EncodeToString(data)
	r.EOF = e.offset == e.size
	return r
}
func (m *Manager) DownloadClose(_ context.Context, in IDInput) Result {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[in.ID]
	if m.closed || e == nil || e.kind != "download" {
		return failure("NOT_FOUND", "The download does not exist")
	}
	if e.state == "failed" {
		return failure("SOURCE_CHANGED", "The download source file has changed")
	}
	if e.state == "expired" {
		return failure("INVALID_STATE", "The download has expired")
	}
	if e.state == "active" {
		if !Unchanged(e.file, e.path, e.info) {
			_ = m.release(e, "failed")
			return failure("SOURCE_CHANGED", "The download source file has changed")
		}
		if err := m.release(e, "closed"); err != nil {
			return ioFailure(err)
		}
	}
	return m.result(e)
}
func (r Result) Error() string { return fmt.Sprintf("%s: %s", r.Code, r.Message) }
