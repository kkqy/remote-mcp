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
		return nil, failure("invalid_argument", "执行资源配置必须为正数，输出缓冲至少为2字节")
	}
	m := &Manager{cfg: cfg, resources: map[string]*resource{}, requests: map[string]*resource{}, stop: make(chan struct{}), sweeperDone: make(chan struct{}), closeDone: make(chan struct{})}
	go m.sweep()
	return m, nil
}
func digest(v any) [32]byte       { b, _ := json.Marshal(v); return sha256.Sum256(b) }
func snapshot(r *resource) Status { r.mu.Lock(); defer r.mu.Unlock(); return r.status }
func (m *Manager) create(requestID, kind string, fp [32]byte, start func(*resource) (running, error), timeout time.Duration) (*resource, error) {
	if requestID == "" || len(requestID) > 128 {
		return nil, failure("invalid_argument", "request_id 长度必须在1到128字节之间")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, failure("closed", "执行管理器已关闭")
	}
	if r := m.requests[requestID]; r != nil {
		if r.fingerprint != fp || r.status.Kind != kind {
			return nil, failure("conflict", "request_id 已用于不同参数")
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
		return nil, failure("resource_limit", "已达到并发资源上限")
	}
	// 终态记录有数量上限，活跃记录不驱逐。
	for len(m.resources) >= 4*(m.cfg.MaxProcesses+m.cfg.MaxTerminals) {
		if !m.evictOldest() {
			return nil, failure("resource_limit", "资源记录已满")
		}
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, failure("internal", "生成资源编号失败")
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
		return failure("io_error", "终止进程树失败")
	}
	return nil
}
func (m *Manager) Start(ctx context.Context, in StartInput) (Status, error) {
	if in.WaitMS < 0 || in.WaitMS > 10000 {
		return Status{}, failure("invalid_argument", "wait_ms 必须在0到10000之间")
	}
	timeout := m.cfg.DefaultTimeout
	if in.TimeoutMS != nil {
		if *in.TimeoutMS < 0 || *in.TimeoutMS > int64((1<<63-1)/time.Millisecond) || (*in.TimeoutMS == 0 && !in.Background) {
			return Status{}, failure("invalid_argument", "超时无效，仅后台进程可禁用超时")
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
		return Status{}, failure("invalid_argument", "终端尺寸必须在1到32767之间")
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
		return nil, failure("closed", "执行管理器已关闭")
	}
	r := m.resources[id]
	if r == nil {
		return nil, failure("not_found", "资源不存在或已过期")
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
	r, err := m.get(in.ID)
	if err != nil {
		return ReadResult{}, err
	}
	if in.Limit == 0 {
		in.Limit = m.cfg.ReadBytes
	}
	if in.Limit < 1 || in.Limit > m.cfg.ReadBytes {
		return ReadResult{}, failure("invalid_argument", "读取长度超过配置上限")
	}
	state := snapshot(r)
	b := r.out
	if state.Kind == "terminal" {
		if in.Stream != "" && in.Stream != "terminal" {
			return ReadResult{}, failure("invalid_argument", "终端仅提供合并输出流")
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
			return ReadResult{}, failure("invalid_argument", "输出流必须为 stdout 或 stderr")
		}
	}
	out, err := b.read(in.Cursor, in.Limit)
	out.ID = in.ID
	out.Stream = in.Stream
	out.State = state.State
	return out, err
}
func (m *Manager) Write(ctx context.Context, in WriteInput) (WriteResult, error) {
	r, err := m.get(in.ID)
	if err != nil {
		return WriteResult{}, err
	}
	s := snapshot(r)
	if s.Kind != "terminal" || s.State != "running" {
		return WriteResult{}, failure("invalid_state", "仅运行中的终端可输入")
	}
	if len(in.DataBase64) > base64.StdEncoding.EncodedLen(m.cfg.ReadBytes) {
		return WriteResult{}, failure("resource_limit", "终端输入超过单次限额")
	}
	data, err := base64.StdEncoding.DecodeString(in.DataBase64)
	if err != nil || len(data) > m.cfg.ReadBytes {
		return WriteResult{}, failure("invalid_argument", "终端输入 Base64 无效或过大")
	}
	select {
	case r.inputBusy <- struct{}{}:
	default:
		return WriteResult{}, failure("busy", "上一终端输入尚未完成")
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
			return WriteResult{out.n}, failure("io_error", "终端写入失败，部分字节可能已送达")
		}
		return WriteResult{out.n}, nil
	case <-ctx.Done():
		return WriteResult{}, failure("interrupted", "输入等待取消，字节仍可能送达，请勿盲目重试")
	case <-timer.C:
		return WriteResult{}, failure("timeout", "输入仍在进行，请勿盲目重试；可关闭终端中断")
	}
}
func (m *Manager) Resize(in ResizeInput) (Status, error) {
	if !validSize(in.Columns, in.Rows) {
		return Status{}, failure("invalid_argument", "终端尺寸必须在1到32767之间")
	}
	r, err := m.get(in.ID)
	if err != nil {
		return Status{}, err
	}
	s := snapshot(r)
	if s.Kind != "terminal" || s.State != "running" {
		return Status{}, failure("invalid_state", "仅运行中的终端可调整尺寸")
	}
	if err = r.process.Resize(in.Columns, in.Rows); err != nil {
		return Status{}, failure("io_error", "终端尺寸调整失败")
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
