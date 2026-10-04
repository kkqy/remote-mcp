package forwarding

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

func manager(t *testing.T, change func(*Config)) *Manager {
	t.Helper()
	c := DefaultConfig()
	c.ListenHost = "127.0.0.1"
	if change != nil {
		change(&c)
	}
	m, e := New(c)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { m.Close() })
	return m
}
func target(t *testing.T, host string) net.Listener {
	t.Helper()
	l, e := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			go func() {
				defer c.Close()
				c.SetDeadline(time.Now().Add(5 * time.Second))
				b, e := io.ReadAll(c)
				if e == nil {
					c.Write(append([]byte("响应:"), b...))
				}
			}()
		}
	}()
	return l
}
func input(l net.Listener, key string) CreateInput {
	h, p, _ := net.SplitHostPort(l.Addr().String())
	n, _ := strconv.Atoi(p)
	return CreateInput{RequestID: key, TargetHost: h, TargetPort: n}
}
func exchange(address string, b []byte) error {
	c, e := net.DialTimeout("tcp", address, time.Second)
	if e != nil {
		return e
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, e = c.Write(b); e != nil {
		return e
	}
	if e = c.(*net.TCPConn).CloseWrite(); e != nil {
		return e
	}
	got, e := io.ReadAll(c)
	if e != nil {
		return e
	}
	if !bytes.Equal(got, append([]byte("响应:"), b...)) {
		return fmt.Errorf("字节不一致")
	}
	return nil
}
func code(t *testing.T, e error, want string) {
	t.Helper()
	if e == nil {
		t.Fatalf("未返回 %s", want)
	}
	v, ok := e.(*Error)
	if !ok || v.Code != want {
		t.Fatalf("错误 %v，预期 %s", e, want)
	}
}
func await(t *testing.T, fn func() bool) {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		if fn() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("等待状态超时")
}
func TestTransferAndConcurrentIdempotency(t *testing.T) {
	m := manager(t, nil)
	l := target(t, "127.0.0.1")
	in := input(l, "one")
	var wg sync.WaitGroup
	ids := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, e := m.Create(in)
			if e != nil {
				t.Error(e)
				return
			}
			ids <- s.ID
		}()
	}
	wg.Wait()
	close(ids)
	var id string
	for next := range ids {
		if id != "" && next != id {
			t.Fatal("重复监听")
		}
		id = next
	}
	s, _ := m.Status(IDInput{id})
	if len(m.List().Forwards) != 1 {
		t.Fatal("记录重复")
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := exchange(s.ListenAddress, bytes.Repeat([]byte{0, 255, 3, 4}, 256<<10)); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	await(t, func() bool { s, _ := m.Status(IDInput{id}); return s.ActiveConnections == 0 })
	in.ListenHost = "127.0.0.1"
	same, e := m.Create(in)
	if e != nil || same.ID != id {
		t.Fatal("默认值未规范化", e)
	}
	in.TargetPort++
	_, e = m.Create(in)
	code(t, e, "conflict")
	stopped, e := m.Stop(IDInput{id})
	if e != nil || stopped.State != "stopped" {
		t.Fatal(stopped, e)
	}
	m.Stop(IDInput{id})
	same, e = m.Create(input(l, "one"))
	if e != nil || same.State != "stopped" {
		t.Fatal("终态重试错误")
	}
	bound, e := net.Listen("tcp", s.ListenAddress)
	if e != nil {
		t.Fatal("端口未释放", e)
	}
	bound.Close()
}
func TestFailuresLimitsRecovery(t *testing.T) {
	m := manager(t, func(c *Config) { c.MaxRules = 1; c.MaxConnections = 1; c.MaxRecords = 1 })
	l := target(t, "127.0.0.1")
	in := input(l, "one")
	in.ListenPort = l.Addr().(*net.TCPAddr).Port
	_, e := m.Create(in)
	code(t, e, "listen_failed")
	in.ListenPort = 0
	s, e := m.Create(in)
	if e != nil {
		t.Fatal(e)
	}
	other := in
	other.RequestID = "two"
	_, e = m.Create(other)
	code(t, e, "resource_limit")
	c, e := net.Dial("tcp", s.ListenAddress)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	await(t, func() bool { s, _ := m.Status(IDInput{s.ID}); return s.ActiveConnections == 1 })
	rejected, e := net.Dial("tcp", s.ListenAddress)
	if e != nil {
		t.Fatal(e)
	}
	rejected.SetReadDeadline(time.Now().Add(time.Second))
	b := make([]byte, 1)
	if _, e = rejected.Read(b); e == nil {
		t.Fatal("超限连接未关闭")
	}
	rejected.Close()
	await(t, func() bool { s, _ := m.Status(IDInput{s.ID}); return s.LastErrorCode == "connection_limit" })
	m.Stop(IDInput{s.ID})
	c.SetReadDeadline(time.Now().Add(time.Second))
	if _, e = c.Read(b); e == nil {
		t.Fatal("停止未关闭连接")
	}
	s, e = m.Create(other)
	if e != nil {
		t.Fatal(e)
	}
	_, e = m.Status(IDInput{"unknown"})
	code(t, e, "not_found")
	if len(m.List().Forwards) != 1 {
		t.Fatal("记录未淘汰")
	}
	m.Stop(IDInput{s.ID})
	// 目标先不可用，重新监听后原规则仍可使用。
	addr := l.Addr().String()
	l.Close()
	other.RequestID = "three"
	s, e = m.Create(other)
	if e != nil {
		t.Fatal(e)
	}
	c, e = net.Dial("tcp", s.ListenAddress)
	if e != nil {
		t.Fatal(e)
	}
	c.SetReadDeadline(time.Now().Add(time.Second))
	c.Read(b)
	c.Close()
	await(t, func() bool { s, _ := m.Status(IDInput{s.ID}); return s.LastErrorCode == "target_connect_failed" })
	recovered, e := net.Listen("tcp", addr)
	if e != nil {
		t.Fatal(e)
	}
	defer recovered.Close()
	go func() {
		c, e := recovered.Accept()
		if e == nil {
			defer c.Close()
			c.Write([]byte("恢复"))
		}
	}()
	c, e = net.Dial("tcp", s.ListenAddress)
	if e != nil {
		t.Fatal(e)
	}
	c.SetReadDeadline(time.Now().Add(time.Second))
	got := make([]byte, len("恢复"))
	_, e = io.ReadFull(c, got)
	c.Close()
	if e != nil || string(got) != "恢复" {
		t.Fatal("目标恢复失败", e)
	}
}
func TestValidationRetentionAndCloseRace(t *testing.T) {
	m := manager(t, func(c *Config) { c.Retention = 10 * time.Millisecond; c.SweepInterval = time.Millisecond })
	for _, h := range []string{"", "http://localhost", "localhost:80", "a/b", "a b", "-bad"} {
		_, e := m.Create(CreateInput{RequestID: "bad", TargetHost: h, TargetPort: 80})
		code(t, e, "invalid_argument")
	}
	l := target(t, "127.0.0.1")
	s, e := m.Create(input(l, "one"))
	if e != nil {
		t.Fatal(e)
	}
	m.Stop(IDInput{s.ID})
	await(t, func() bool { return len(m.List().Forwards) == 0 })
	s, e = m.Create(input(l, "two"))
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, e := net.DialTimeout("tcp", s.ListenAddress, 100*time.Millisecond)
			if e == nil {
				c.Close()
			}
			m.Stop(IDInput{s.ID})
			m.Close()
		}()
	}
	wg.Wait()
	_, e = m.Create(input(l, "three"))
	code(t, e, "closed")
}
func TestNonLoopbackAndIPv6(t *testing.T) {
	addresses, _ := net.InterfaceAddrs()
	host := ""
	for _, a := range addresses {
		ip, _, e := net.ParseCIDR(a.String())
		if e == nil && ip.To4() != nil && !ip.IsLoopback() {
			host = ip.String()
			break
		}
	}
	t.Run("非回环网卡", func(t *testing.T) {
		if host == "" {
			t.Skip("无非回环网卡")
		}
		m := manager(t, nil)
		s, e := m.Create(input(target(t, host), "lan"))
		if e != nil {
			t.Fatal(e)
		}
		if e = exchange(s.ListenAddress, []byte("内网")); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("IPv6", func(t *testing.T) {
		probe, e := net.Listen("tcp6", "[::1]:0")
		if e != nil {
			t.Skip("无 IPv6 回环")
		}
		probe.Close()
		m := manager(t, nil)
		in := input(target(t, "::1"), "ipv6")
		in.ListenHost = "::1"
		s, e := m.Create(in)
		if e != nil {
			t.Fatal(e)
		}
		if e = exchange(s.ListenAddress, []byte("IPv6")); e != nil {
			t.Fatal(e)
		}
	})
}

func TestCloseReleasesRunningRulesAndPendingDial(t *testing.T) {
	m := manager(t, func(c *Config) { c.MaxConnections = 1 })
	started := make(chan struct{})
	canceled := make(chan struct{})
	// 在规则创建前设置可控拨号，避免依赖网络立即拒绝或超时的环境差异。
	m.dialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	}
	probe, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	s, e := m.Create(CreateInput{RequestID: "pending", TargetHost: "127.0.0.1", TargetPort: 65000, ListenPort: port})
	if e != nil {
		t.Fatal(e)
	}
	if s.ListenAddress != net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) {
		t.Fatal("指定端口未生效")
	}
	c, e := net.Dial("tcp", s.ListenAddress)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("拨号未开始")
	}
	pending, _ := m.Status(IDInput{s.ID})
	if pending.ActiveConnections != 1 {
		t.Fatal("拨号未计入连接限额", pending)
	}
	rejected, e := net.Dial("tcp", s.ListenAddress)
	if e != nil {
		t.Fatal(e)
	}
	defer rejected.Close()
	rejected.SetReadDeadline(time.Now().Add(time.Second))
	if _, e = rejected.Read(make([]byte, 1)); e == nil {
		t.Fatal("拨号中连接未占用限额")
	} else if timeout, ok := e.(net.Error); ok && timeout.Timeout() {
		t.Fatal("超限连接未及时关闭")
	}
	await(t, func() bool { v, _ := m.Status(IDInput{s.ID}); return v.LastErrorCode == "connection_limit" })
	done := make(chan struct{})
	go func() { m.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("关闭未取消拨号")
	}
	select {
	case <-canceled:
	default:
		t.Fatal("关闭未等待拨号取消")
	}
	v, _ := m.Status(IDInput{s.ID})
	if v.State != "stopped" || v.ActiveConnections != 0 {
		t.Fatal(v)
	}
	bound, e := net.Listen("tcp", s.ListenAddress)
	if e != nil {
		t.Fatal(e)
	}
	bound.Close()
	fresh := manager(t, nil)
	if len(fresh.List().Forwards) != 0 {
		t.Fatal("新管理器继承了旧记录")
	}
}
