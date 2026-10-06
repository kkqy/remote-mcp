package forwarding

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type connection struct{ source, target net.Conn }
type rule struct {
	input       CreateInput
	status      Status
	listener    net.Listener
	ctx         context.Context
	cancel      context.CancelFunc
	connections map[*connection]struct{}
	workers     sync.WaitGroup
	done        chan struct{}
}

// mu 统一保护规则状态和连接登记；等待、拨号与复制均在锁外进行。
type Manager struct {
	mu          sync.Mutex
	cfg         Config
	dialContext func(context.Context, string, string) (net.Conn, error)
	rules       map[string]*rule
	requests    map[string]*rule
	connections int
	closed      bool
	stop        chan struct{}
	swept       chan struct{}
	once        sync.Once
}

func New(c Config) (*Manager, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	m := &Manager{cfg: c, rules: map[string]*rule{}, requests: map[string]*rule{}, stop: make(chan struct{}), swept: make(chan struct{})}
	m.dialContext = (&net.Dialer{Timeout: c.DialTimeout}).DialContext
	go m.sweep()
	return m, nil
}
func (m *Manager) normalize(in CreateInput) (CreateInput, error) {
	if in.ListenHost == "" {
		in.ListenHost = m.cfg.ListenHost
	}
	ip, err := netip.ParseAddr(in.ListenHost)
	if err != nil || ip.IsMulticast() {
		return in, failure("invalid_argument", "The listen address must be a unicast or wildcard IP address")
	}
	in.ListenHost = ip.String()
	if in.RequestID == "" || len(in.RequestID) > 256 || strings.TrimSpace(in.RequestID) != in.RequestID || in.ListenPort < 0 || in.ListenPort > 65535 || in.TargetPort < 1 || in.TargetPort > 65535 {
		return in, failure("invalid_argument", "Invalid creation key or port")
	}
	if ip, err := netip.ParseAddr(in.TargetHost); err == nil {
		in.TargetHost = ip.String()
	} else {
		if len(in.TargetHost) == 0 || len(in.TargetHost) > 253 {
			return in, failure("invalid_argument", "Invalid target hostname")
		}
		for _, label := range strings.Split(strings.TrimSuffix(in.TargetHost, "."), ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return in, failure("invalid_argument", "Invalid target hostname")
			}
			for _, ch := range label {
				if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-') {
					return in, failure("invalid_argument", "Invalid target hostname")
				}
			}
		}
		in.TargetHost = strings.ToLower(in.TargetHost)
	}
	return in, nil
}
func (m *Manager) Create(in CreateInput) (Status, error) {
	in, err := m.normalize(in)
	if err != nil {
		return Status{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Status{}, failure("closed", "The forwarding manager is closed")
	}
	if r := m.requests[in.RequestID]; r != nil {
		if r.input != in {
			return Status{}, failure("conflict", "The creation key was already used with different parameters")
		}
		return r.status, nil
	}
	m.pruneLocked(time.Now(), false)
	active := 0
	for _, r := range m.rules {
		if r.status.EndedAt == nil {
			active++
		}
	}
	if active >= m.cfg.MaxRules {
		return Status{}, failure("resource_limit", "The active forwarding rule limit has been reached")
	}
	if len(m.rules) >= m.cfg.MaxRecords {
		m.pruneLocked(time.Now(), true)
	}
	if len(m.rules) >= m.cfg.MaxRecords {
		return Status{}, failure("resource_limit", "The forwarding record limit has been reached")
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Status{}, failure("internal", "Failed to generate a resource ID")
	}
	network := "tcp4"
	if strings.Contains(in.ListenHost, ":") {
		network = "tcp6"
	}
	// 监听仅使用已验证的数字 IP；锁内绑定保证幂等创建与关闭原子化。
	listener, err := net.Listen(network, net.JoinHostPort(in.ListenHost, strconv.Itoa(in.ListenPort)))
	if err != nil {
		return Status{}, failure("listen_failed", "Failed to listen; check the address, port, and permissions")
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &rule{input: in, status: Status{ID: hex.EncodeToString(id[:]), State: "running", ListenAddress: listener.Addr().String(), TargetAddress: net.JoinHostPort(in.TargetHost, strconv.Itoa(in.TargetPort)), CreatedAt: time.Now()}, listener: listener, ctx: ctx, cancel: cancel, connections: map[*connection]struct{}{}, done: make(chan struct{})}
	m.rules[r.status.ID] = r
	m.requests[in.RequestID] = r
	r.workers.Add(1)
	go m.accept(r)
	return r.status, nil
}
func (m *Manager) accept(r *rule) {
	defer r.workers.Done()
	for {
		source, err := r.listener.Accept()
		if err != nil {
			m.mu.Lock()
			if r.status.State == "running" {
				r.status.LastErrorCode = "accept_failed"
				m.stopLocked(r, "failed")
			}
			m.mu.Unlock()
			return
		}
		m.mu.Lock()
		if r.status.State != "running" {
			m.mu.Unlock()
			source.Close()
			return
		}
		r.status.TotalConnections++
		if m.connections >= m.cfg.MaxConnections {
			r.status.FailedConnections++
			r.status.LastErrorCode = "connection_limit"
			m.mu.Unlock()
			source.Close()
			continue
		}
		c := &connection{source: source}
		r.connections[c] = struct{}{}
		m.connections++
		r.status.ActiveConnections++
		r.workers.Add(1)
		m.mu.Unlock()
		go m.forward(r, c)
	}
}
func (m *Manager) forward(r *rule, c *connection) {
	defer r.workers.Done()
	defer func() {
		m.mu.Lock()
		c.source.Close()
		if c.target != nil {
			c.target.Close()
		}
		delete(r.connections, c)
		m.connections--
		r.status.ActiveConnections--
		m.mu.Unlock()
	}()
	target, err := m.dialContext(r.ctx, "tcp", r.status.TargetAddress)
	m.mu.Lock()
	if r.status.State != "running" {
		m.mu.Unlock()
		if target != nil {
			target.Close()
		}
		return
	}
	if err != nil {
		r.status.FailedConnections++
		r.status.LastErrorCode = "target_connect_failed"
		m.mu.Unlock()
		return
	}
	c.target = target
	m.mu.Unlock()
	results := make(chan error, 2)
	copyHalf := func(dst, src net.Conn) {
		_, err := io.CopyBuffer(dst, src, make([]byte, 32<<10))
		if err == nil {
			err = dst.(*net.TCPConn).CloseWrite()
		}
		results <- err
	}
	go copyHalf(target, c.source)
	go copyHalf(c.source, target)
	first := <-results
	if first != nil {
		c.source.Close()
		target.Close()
	}
	second := <-results
	if first != nil || second != nil {
		m.mu.Lock()
		if r.status.State == "running" {
			r.status.FailedConnections++
			r.status.LastErrorCode = "connection_io_failed"
		}
		m.mu.Unlock()
	}
}

// stopLocked 先阻止新登记，再取消拨号并关闭现有连接；完成通知在全部工作退出后发送。
func (m *Manager) stopLocked(r *rule, final string) {
	if r.status.State != "running" {
		return
	}
	r.status.State = "stopping"
	r.listener.Close()
	r.cancel()
	for c := range r.connections {
		c.source.Close()
		if c.target != nil {
			c.target.Close()
		}
	}
	go func() {
		r.workers.Wait()
		m.mu.Lock()
		now := time.Now()
		r.status.State = final
		r.status.EndedAt = &now
		close(r.done)
		m.mu.Unlock()
	}()
}
func (m *Manager) Stop(in IDInput) (Status, error) {
	m.mu.Lock()
	r := m.rules[in.ID]
	if r == nil {
		m.mu.Unlock()
		return Status{}, failure("not_found", "The forwarding rule does not exist")
	}
	m.stopLocked(r, "stopped")
	m.mu.Unlock()
	<-r.done
	m.mu.Lock()
	defer m.mu.Unlock()
	return r.status, nil
}
func (m *Manager) Status(in IDInput) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.rules[in.ID]
	if r == nil {
		return Status{}, failure("not_found", "The forwarding rule does not exist")
	}
	return r.status, nil
}
func (m *Manager) List() ListResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := ListResult{Forwards: []Status{}}
	for _, r := range m.rules {
		out.Forwards = append(out.Forwards, r.status)
	}
	sort.Slice(out.Forwards, func(i, j int) bool {
		a, b := out.Forwards[i], out.Forwards[j]
		if a.CreatedAt.Equal(b.CreatedAt) {
			return a.ID < b.ID
		}
		return a.CreatedAt.Before(b.CreatedAt)
	})
	return out
}
func (m *Manager) pruneLocked(now time.Time, makeRoom bool) {
	var oldest *rule
	for id, r := range m.rules {
		if r.status.EndedAt == nil {
			continue
		}
		if now.Sub(*r.status.EndedAt) >= m.cfg.Retention {
			delete(m.rules, id)
			delete(m.requests, r.input.RequestID)
			continue
		}
		if oldest == nil || r.status.EndedAt.Before(*oldest.status.EndedAt) {
			oldest = r
		}
	}
	if makeRoom && len(m.rules) >= m.cfg.MaxRecords && oldest != nil {
		delete(m.rules, oldest.status.ID)
		delete(m.requests, oldest.input.RequestID)
	}
}
func (m *Manager) sweep() {
	defer close(m.swept)
	ticker := time.NewTicker(m.cfg.SweepInterval)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			m.mu.Lock()
			m.pruneLocked(now, false)
			m.mu.Unlock()
		case <-m.stop:
			return
		}
	}
}
func (m *Manager) Close() error {
	m.once.Do(func() {
		m.mu.Lock()
		m.closed = true
		close(m.stop)
		rules := make([]*rule, 0, len(m.rules))
		for _, r := range m.rules {
			m.stopLocked(r, "stopped")
			rules = append(rules, r)
		}
		m.mu.Unlock()
		for _, r := range rules {
			<-r.done
		}
		<-m.swept
	})
	return nil
}
