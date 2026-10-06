package execution

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

type resource struct {
	mu          sync.Mutex
	status      Status
	process     running
	out, err    *outputBuffer
	done        chan struct{}
	lastCall    time.Time
	requestID   string
	fingerprint [32]byte
	inputBusy   chan struct{}
}
type Manager struct {
	mu          sync.Mutex
	cfg         Config
	resources   map[string]*resource
	requests    map[string]*resource
	closed      bool
	stop        chan struct{}
	sweeperDone chan struct{}
	closeDone   chan struct{}
}

func New(cfg Config) (*Manager, error) {
	if cfg.DefaultTimeout <= 0 || cfg.TerminalIdleTimeout <= 0 || cfg.Retention <= 0 || cfg.MaxProcesses <= 0 || cfg.MaxTerminals <= 0 || cfg.OutputBytes < 2 || cfg.ReadBytes <= 0 || cfg.SweepInterval <= 0 {
		return nil, failure("invalid_argument", "Execution resource settings must be positive and the output buffer must be at least 2 bytes")
	}
	m := &Manager{cfg: cfg, resources: map[string]*resource{}, requests: map[string]*resource{}, stop: make(chan struct{}), sweeperDone: make(chan struct{}), closeDone: make(chan struct{})}
	go m.sweep()
	return m, nil
}
func digest(v any) [32]byte       { b, _ := json.Marshal(v); return sha256.Sum256(b) }
func snapshot(r *resource) Status { r.mu.Lock(); defer r.mu.Unlock(); return r.status }
func (m *Manager) create(requestID, kind string, fp [32]byte, start func(*resource) (running, error), timeout time.Duration) (*resource, error) {
	if requestID == "" || len(requestID) > 128 {
		return nil, failure("invalid_argument", "request_id must be between 1 and 128 bytes")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, failure("closed", "The execution manager is closed")
	}
	if r := m.requests[requestID]; r != nil {
		if r.fingerprint != fp || r.status.Kind != kind {
			return nil, failure("conflict", "request_id was already used with different parameters")
		}
		r.mu.Lock()
		r.lastCall = time.Now()
		r.mu.Unlock()
		return r, nil
	}
	active := 0
	for _, r := range m.resources {
		s := snapshot(r)
		if s.Kind == kind && s.State == "running" {
			active++
		}
	}
	maximum := m.cfg.MaxProcesses
	if kind == "terminal" {
		maximum = m.cfg.MaxTerminals
	}
	if active >= maximum {
		return nil, failure("resource_limit", "The concurrent resource limit has been reached")
	}
	// 终态记录有数量上限，活跃记录不驱逐。
	for len(m.resources) >= 4*(m.cfg.MaxProcesses+m.cfg.MaxTerminals) {
		if !m.evictOldest() {
			return nil, failure("resource_limit", "The resource record limit has been reached")
		}
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, failure("internal", "Failed to generate a resource ID")
	}
	capacity := m.cfg.OutputBytes
	if kind == "process" {
		capacity /= 2
	}
	now := time.Now()
	r := &resource{status: Status{ID: hex.EncodeToString(idBytes), Kind: kind, State: "running", StartedAt: now}, out: &outputBuffer{capacity: capacity}, err: &outputBuffer{capacity: capacity}, done: make(chan struct{}), lastCall: now, requestID: requestID, fingerprint: fp, inputBusy: make(chan struct{}, 1)}
	p, err := start(r)
	if err != nil {
		return nil, err
	}
	r.process = p
	r.status.PID = p.PID()
	m.resources[r.status.ID] = r
	m.requests[requestID] = r
	go m.reap(r, timeout)
	return r, nil
}
func (m *Manager) evictOldest() bool {
	var oldest *resource
	for _, r := range m.resources {
		s := snapshot(r)
		if s.EndedAt != nil && (oldest == nil || s.EndedAt.Before(*snapshot(oldest).EndedAt)) {
			oldest = r
		}
	}
	if oldest == nil {
		return false
	}
	delete(m.resources, oldest.status.ID)
	delete(m.requests, oldest.requestID)
	return true
}
func (m *Manager) reap(r *resource, timeout time.Duration) {
	var timer *time.Timer
	if timeout > 0 {
		timer = time.AfterFunc(timeout, func() { _ = stopResource(r, "timeout") })
	}
	code, err := r.process.Wait()
	if timer != nil {
		timer.Stop()
	}
	// 主进程退出后仍需回收同组子进程，再结束输出读循环。
	_ = r.process.Kill()
	_ = r.process.Close()
	r.mu.Lock()
	now := time.Now()
	r.status.State = "exited"
	r.status.EndedAt = &now
	r.status.ExitCode = &code
	if err != nil && r.status.Reason == "" {
		r.status.Reason = "exit_error"
	}
	r.mu.Unlock()
	close(r.done)
}
func stopResource(r *resource, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status.State != "running" {
		return nil
	}
	if r.status.Reason == "" {
		r.status.Reason = reason
	}
	if err := r.process.Kill(); err != nil {
		return failure("io_error", "Failed to terminate the process tree")
	}
	return nil
}
func (m *Manager) Start(ctx context.Context, in StartInput) (Status, error) {
	if in.WaitMS < 0 || in.WaitMS > 10000 {
		return Status{}, failure("invalid_argument", "wait_ms must be between 0 and 10000")
	}
	timeout := m.cfg.DefaultTimeout
	if in.TimeoutMS != nil {
		if *in.TimeoutMS < 0 || *in.TimeoutMS > int64((1<<63-1)/time.Millisecond) || (*in.TimeoutMS == 0 && !in.Background) {
			return Status{}, failure("invalid_argument", "Invalid timeout; only background processes may disable the timeout")
		}
		timeout = time.Duration(*in.TimeoutMS) * time.Millisecond
	}
	cmd, err := command(in.Command, in.Args, in.Dir, in.Env)
	if err != nil {
		return Status{}, err
	}
	r, err := m.create(in.RequestID, "process", digest(in), func(r *resource) (running, error) { return startProcess(cmd, r.out, r.err) }, timeout)
	if err != nil {
		return Status{}, err
	}
	if in.WaitMS > 0 {
		timer := time.NewTimer(time.Duration(in.WaitMS) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-r.done:
		case <-ctx.Done():
		case <-timer.C:
		}
	}
	return snapshot(r), nil
}
func (m *Manager) OpenTerminal(in TerminalInput) (Status, error) {
	if in.Columns == 0 {
		in.Columns = 80
	}
	if in.Rows == 0 {
		in.Rows = 24
	}
	if !validSize(in.Columns, in.Rows) {
		return Status{}, failure("invalid_argument", "Terminal dimensions must be between 1 and 32767")
	}
	if in.Command == "" {
		in.Command = defaultShell()
	}
	cmd, err := command(in.Command, in.Args, in.Dir, in.Env)
	if err != nil {
		return Status{}, err
	}
	r, err := m.create(in.RequestID, "terminal", digest(in), func(r *resource) (running, error) { return startTerminal(cmd, in.Columns, in.Rows, r.out) }, 0)
	if err != nil {
		return Status{}, err
	}
	return snapshot(r), nil
}
func validSize(columns, rows int) bool {
	return columns > 0 && rows > 0 && columns <= 32767 && rows <= 32767
}
func (m *Manager) get(id string) (*resource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, failure("closed", "The execution manager is closed")
	}
	r := m.resources[id]
	if r == nil {
		return nil, failure("not_found", "The resource does not exist or has expired")
	}
	r.mu.Lock()
	r.lastCall = time.Now()
	r.mu.Unlock()
	return r, nil
}
func (m *Manager) Status(in IDInput) (Status, error) {
	r, err := m.get(in.ID)
	if err != nil {
		return Status{}, err
	}
	return snapshot(r), nil
}
func (m *Manager) Stop(in IDInput) (Status, error) {
	r, err := m.get(in.ID)
	if err != nil {
		return Status{}, err
	}
	if err = stopResource(r, "stopped"); err != nil {
		return Status{}, err
	}
	return snapshot(r), nil
}
func (m *Manager) Read(in ReadInput) (ReadResult, error) {
	in.WaitMS = 0
	return m.ReadContext(context.Background(), in)
}

func (m *Manager) ReadContext(ctx context.Context, in ReadInput) (ReadResult, error) {
	if in.WaitMS < 0 || in.WaitMS > 30000 {
		return ReadResult{}, failure("invalid_argument", "wait_ms must be between 0 and 30000")
	}
	r, err := m.get(in.ID)
	if err != nil {
		return ReadResult{}, err
	}
	if in.Limit == 0 {
		in.Limit = m.cfg.ReadBytes
	}
	if in.Limit < 1 || in.Limit > m.cfg.ReadBytes {
		return ReadResult{}, failure("invalid_argument", "The read length exceeds the configured limit")
	}
	state := snapshot(r)
	b := r.out
	if state.Kind == "terminal" {
		if in.Stream != "" && in.Stream != "terminal" {
			return ReadResult{}, failure("invalid_argument", "Terminals provide only a merged output stream")
		}
		in.Stream = "terminal"
	} else {
		if in.Stream == "" {
			in.Stream = "stdout"
		}
		switch in.Stream {
		case "stdout":
		case "stderr":
			b = r.err
		default:
			return ReadResult{}, failure("invalid_argument", "The output stream must be stdout or stderr")
		}
	}
	var timer *time.Timer
	var deadline <-chan time.Time
	if in.WaitMS > 0 {
		timer = time.NewTimer(time.Duration(in.WaitMS) * time.Millisecond)
		defer timer.Stop()
		deadline = timer.C
	}
	finishReason := ""
	for {
		out, changed, err := b.readSubscribe(in.Cursor, in.Limit)
		if err != nil {
			return ReadResult{}, err
		}
		out.ID, out.Stream, out.State = in.ID, in.Stream, snapshot(r).State
		switch {
		case in.WaitMS == 0:
			out.Reason = "immediate"
		case finishReason != "":
			out.Reason = finishReason
		case out.NextCursor > out.StartCursor:
			out.Reason = "output"
		case out.State == "exited":
			// 退出状态在输出排空后发布，再读一次确保末尾字节没有遗漏。
			out, err = b.read(in.Cursor, in.Limit)
			out.ID, out.Stream, out.State = in.ID, in.Stream, "exited"
			out.Reason = "exit"
			if out.NextCursor > out.StartCursor {
				out.Reason = "output"
			}
			return out, err
		}
		if out.Reason != "" {
			return out, nil
		}
		select {
		case <-changed:
		case <-r.done:
		case <-m.stop:
			finishReason = "cancelled"
		case <-ctx.Done():
			finishReason = "cancelled"
		case <-deadline:
			finishReason = "timeout"
		}
	}
}
func (m *Manager) Write(ctx context.Context, in WriteInput) (WriteResult, error) {
	r, err := m.get(in.ID)
	if err != nil {
		return WriteResult{}, err
	}
	s := snapshot(r)
	if s.Kind != "terminal" || s.State != "running" {
		return WriteResult{}, failure("invalid_state", "Input is only allowed for a running terminal")
	}
	if len(in.DataBase64) > base64.StdEncoding.EncodedLen(m.cfg.ReadBytes) {
		return WriteResult{}, failure("resource_limit", "Terminal input exceeds the per-call limit")
	}
	data, err := base64.StdEncoding.DecodeString(in.DataBase64)
	if err != nil || len(data) > m.cfg.ReadBytes {
		return WriteResult{}, failure("invalid_argument", "Terminal input Base64 is invalid or too large")
	}
	select {
	case r.inputBusy <- struct{}{}:
	default:
		return WriteResult{}, failure("busy", "The previous terminal input is still in progress")
	}
	type outcome struct {
		n   int
		err error
	}
	result := make(chan outcome, 1)
	go func() {
		n, err := r.process.Write(data)
		// 完成通知必须晚于占用释放；取消等待时仍由底层写入负责释放。
		<-r.inputBusy
		result <- outcome{n, err}
	}()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case out := <-result:
		if out.err != nil {
			return WriteResult{out.n}, failure("io_error", "Terminal write failed; some bytes may already have been delivered")
		}
		return WriteResult{out.n}, nil
	case <-ctx.Done():
		return WriteResult{}, failure("interrupted", "Waiting for input was canceled; bytes may still be delivered. Do not blindly retry")
	case <-timer.C:
		return WriteResult{}, failure("timeout", "Input is still in progress. Do not blindly retry; close the terminal to interrupt it")
	}
}
func (m *Manager) Resize(in ResizeInput) (Status, error) {
	if !validSize(in.Columns, in.Rows) {
		return Status{}, failure("invalid_argument", "Terminal dimensions must be between 1 and 32767")
	}
	r, err := m.get(in.ID)
	if err != nil {
		return Status{}, err
	}
	s := snapshot(r)
	if s.Kind != "terminal" || s.State != "running" {
		return Status{}, failure("invalid_state", "Only a running terminal can be resized")
	}
	if err = r.process.Resize(in.Columns, in.Rows); err != nil {
		return Status{}, failure("io_error", "Failed to resize the terminal")
	}
	return snapshot(r), nil
}
func (m *Manager) sweep() {
	defer close(m.sweeperDone)
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
				expired := r.status.EndedAt != nil && now.Sub(*r.status.EndedAt) >= m.cfg.Retention
				idle := r.status.Kind == "terminal" && r.status.State == "running" && now.Sub(r.lastCall) >= m.cfg.TerminalIdleTimeout
				r.mu.Unlock()
				if expired {
					delete(m.resources, id)
					delete(m.requests, r.requestID)
				}
				if idle {
					_ = stopResource(r, "idle_timeout")
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
		return nil
	}
	m.closed = true
	close(m.stop)
	list := make([]*resource, 0, len(m.resources))
	for _, r := range m.resources {
		list = append(list, r)
	}
	m.mu.Unlock()
	var errs []error
	for _, r := range list {
		if err := stopResource(r, "server_shutdown"); err != nil {
			errs = append(errs, err)
		}
	}
	for _, r := range list {
		<-r.done
	}
	<-m.sweeperDone
	m.mu.Lock()
	clear(m.resources)
	clear(m.requests)
	m.mu.Unlock()
	close(m.closeDone)
	return errors.Join(errs...)
}
