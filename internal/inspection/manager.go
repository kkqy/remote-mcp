package inspection

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type Manager struct {
	cfg     Config
	mu      sync.Mutex
	closed  bool
	active  int
	cancels map[uint64]context.CancelFunc
	next    uint64
	done    *sync.Cond
	lookup  func(context.Context, string) ([]net.IPAddr, error)
	kernel  func() (string, error)
	rootCAs *x509.CertPool
}

func New(cfg Config) (*Manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	m := &Manager{cfg: cfg, cancels: make(map[uint64]context.CancelFunc), lookup: net.DefaultResolver.LookupIPAddr, kernel: platformKernel}
	m.done = sync.NewCond(&m.mu)
	return m, nil
}
func (m *Manager) begin(ctx context.Context, ms int) (context.Context, func(), error) {
	if ms < 0 || ms > 10000 || time.Duration(ms)*time.Millisecond > m.cfg.MaxTimeout {
		return nil, nil, failure("invalid_argument", "The total timeout is outside the supported range")
	}
	budget := m.cfg.DefaultTimeout
	if ms != 0 {
		budget = time.Duration(ms) * time.Millisecond
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, contextError(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, nil, failure("closed", "The inspection manager is closed")
	}
	if m.active >= m.cfg.MaxConcurrent {
		return nil, nil, failure("resource_limit", "The maximum number of inspections is already running")
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	m.next++
	id := m.next
	m.cancels[id] = cancel
	m.active++
	return ctx, func() {
		cancel()
		m.mu.Lock()
		delete(m.cancels, id)
		m.active--
		m.done.Broadcast()
		m.mu.Unlock()
	}, nil
}
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	for _, cancel := range m.cancels {
		cancel()
	}
	for m.active != 0 {
		m.done.Wait()
	}
	return nil
}
func contextError(err error) *Error {
	if errors.Is(err, context.Canceled) {
		return failure("cancelled", "The inspection was cancelled")
	}
	return failure("timeout", "The total inspection time budget expired")
}
func systemError(err error) *Error {
	if e, ok := err.(*Error); ok {
		return e
	}
	switch {
	case errors.Is(err, exec.ErrDot):
		return failure("permission_denied", "An executable resolved from the current directory is not allowed")
	case errors.Is(err, os.ErrPermission):
		return failure("permission_denied", "System data or an executable is not accessible with the current permissions")
	case errors.Is(err, os.ErrNotExist), errors.Is(err, exec.ErrNotFound):
		return failure("dependency_missing", "A required system interface or executable was not found")
	default:
		return failure("io_error", "System data could not be read or a fixed command failed")
	}
}
func boundedString(s string, n int) (string, bool) {
	s = strings.ToValidUTF8(s, "�")
	if len(s) <= n {
		return s, false
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s, true
}
func limitValue(n int) (int, error) {
	if n < 0 || n > 4096 {
		return 0, failure("invalid_argument", "The snapshot limit must be between 1 and 4096")
	}
	if n == 0 {
		n = 256
	}
	return n, nil
}
func (m *Manager) Environment(ctx context.Context, in EnvironmentInput) EnvironmentResult {
	out := EnvironmentResult{Result: Result{OK: true}, OS: runtime.GOOS, Arch: runtime.GOARCH, Runtimes: []Runtime{}, Capabilities: []string{"runtime_versions", "process_snapshot", "tcp_listeners", "dns", "tcp", "tls", "http"}}
	names := in.Runtimes
	if len(names) == 0 {
		names = []string{"go", "node", "python", "java", "dotnet"}
	}
	seen := map[string]bool{}
	for _, name := range names {
		if _, ok := runtimeCommands[name]; !ok || seen[name] {
			out.fail(failure("invalid_argument", "Runtime names must be a unique subset of go, node, python, java, dotnet"))
			return out
		}
		seen[name] = true
	}
	ctx, end, err := m.begin(ctx, in.TimeoutMS)
	if err != nil {
		out.fail(err)
		return out
	}
	defer end()
	host, err := os.Hostname()
	if err != nil {
		out.warn("io_error", "The system hostname could not be read")
	}
	out.Hostname, out.Truncated = boundedString(host, m.cfg.MaxNameBytes)
	out.Kernel, err = m.kernel()
	if err != nil {
		out.warn(systemError(err).Code, "The operating system kernel version could not be read")
	}
	for _, name := range names {
		r := m.inspectRuntime(ctx, name)
		out.Runtimes = append(out.Runtimes, r)
		out.Truncated = out.Truncated || r.Truncated
		encoded, _ := json.Marshal(out)
		if len(encoded) > m.cfg.MaxResultBytes-512 {
			out.Runtimes = out.Runtimes[:len(out.Runtimes)-1]
			out.Truncated = true
			out.warn("resource_limit", "The environment result byte limit was reached")
			break
		}
		if r.Code != "" {
			out.Partial = true
		}
		if ctx.Err() != nil {
			out.fail(contextError(ctx.Err()))
			break
		}
	}
	return out
}
func (m *Manager) Processes(ctx context.Context, in SnapshotInput) ProcessesResult {
	out := ProcessesResult{Result: Result{OK: true}, RootPID: in.RootPID, Processes: []Process{}, Visibility: "current_user_and_namespace"}
	limit, err := limitValue(in.Limit)
	if err == nil && in.RootPID < 0 {
		err = failure("invalid_argument", "The root PID must not be negative")
	}
	if err != nil {
		out.fail(err)
		return out
	}
	ctx, end, err := m.begin(ctx, in.TimeoutMS)
	if err != nil {
		out.fail(err)
		return out
	}
	defer end()
	items := platformProcesses(ctx, m.cfg, &out)
	if ctx.Err() != nil {
		out.warn(contextError(ctx.Err()).Code, contextError(ctx.Err()).Message)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].PID < items[j].PID })
	// 根筛选在完整的有界快照上构建父子索引，不依赖 PID 大小与树序一致。
	children := map[int][]int{}
	for _, p := range items {
		children[p.PPID] = append(children[p.PPID], p.PID)
	}
	wanted := map[int]bool{}
	if in.RootPID > 0 {
		queue := []int{in.RootPID}
		wanted[in.RootPID] = true
		for i := 0; i < len(queue); i++ {
			if ctx.Err() != nil {
				break
			}
			for _, pid := range children[queue[i]] {
				if !wanted[pid] {
					wanted[pid] = true
					queue = append(queue, pid)
				}
			}
		}
	}
	size := 1024
	for _, p := range items {
		if ctx.Err() != nil {
			break
		}
		if in.RootPID != 0 && !wanted[p.PID] {
			continue
		}
		var nameTruncated bool
		p.Name, nameTruncated = boundedString(p.Name, m.cfg.MaxNameBytes)
		out.Truncated = out.Truncated || nameTruncated
		encoded, _ := json.Marshal(p)
		if len(out.Processes) >= limit || size+len(encoded) > m.cfg.MaxResultBytes-4096 {
			out.Truncated = true
			break
		}
		size += len(encoded)
		out.Processes = append(out.Processes, p)
	}
	if ctx.Err() != nil {
		out.fail(contextError(ctx.Err()))
	}
	return out
}
func (m *Manager) Listeners(ctx context.Context, in ListenersInput) ListenersResult {
	out := ListenersResult{Result: Result{OK: true}, Listeners: []Listener{}, PIDVisibility: "complete"}
	limit, err := limitValue(in.Limit)
	if err == nil && (in.PID < 0 || in.Port < 0 || in.Port > 65535) {
		err = failure("invalid_argument", "The PID or TCP port filter is outside the supported range")
	}
	if err != nil {
		out.fail(err)
		return out
	}
	ctx, end, err := m.begin(ctx, in.TimeoutMS)
	if err != nil {
		out.fail(err)
		return out
	}
	defer end()
	items := platformListeners(ctx, m.cfg, &out)
	sort.Slice(items, func(i, j int) bool {
		if items[i].Address == items[j].Address {
			return items[i].Port < items[j].Port
		}
		return items[i].Address < items[j].Address
	})
	size := 1024
	for _, item := range items {
		if ctx.Err() != nil {
			break
		}
		if in.Port != 0 && item.Port != in.Port {
			continue
		}
		if in.PID != 0 {
			found := false
			for _, pid := range item.PIDs {
				if pid == in.PID {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		encoded, _ := json.Marshal(item)
		if len(out.Listeners) >= limit || size+len(encoded) > m.cfg.MaxResultBytes-4096 {
			out.Truncated = true
			break
		}
		size += len(encoded)
		out.Listeners = append(out.Listeners, item)
	}
	if ctx.Err() != nil {
		out.fail(contextError(ctx.Err()))
	}
	return out
}
