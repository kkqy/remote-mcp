package inspection

import (
	"context"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testManager(t *testing.T) *Manager {
	t.Helper()
	m, err := New(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	return m
}
func TestEnvironmentAndValidation(t *testing.T) {
	m := testManager(t)
	out := m.Environment(context.Background(), EnvironmentInput{Runtimes: []string{"go"}})
	if !out.OK || out.OS != runtime.GOOS || out.Arch != runtime.GOARCH || len(out.Runtimes) != 1 || !out.Runtimes[0].Available || !strings.Contains(out.Runtimes[0].Version, "go version") {
		t.Fatalf("unexpected environment: %+v", out)
	}
	out = m.Environment(context.Background(), EnvironmentInput{Runtimes: []string{"go", "go"}})
	if out.Code != "invalid_argument" {
		t.Fatalf("duplicate runtime accepted: %+v", out)
	}
	out = m.Environment(context.Background(), EnvironmentInput{Runtimes: []string{"sh"}})
	if out.Code != "invalid_argument" {
		t.Fatal(out)
	}
	if m.Processes(context.Background(), SnapshotInput{Limit: 4097}).Code != "invalid_argument" {
		t.Fatal("invalid limit accepted")
	}
	if m.Listeners(context.Background(), ListenersInput{Port: 65536}).Code != "invalid_argument" {
		t.Fatal("invalid port accepted")
	}
	if m.Probe(context.Background(), ProbeInput{Mode: "dns", Target: "localhost", TimeoutMS: 1 << 62}).Code != "invalid_argument" {
		t.Fatal("overflow timeout accepted")
	}
}

func TestEnvironmentKernelFailureKeepsBasicSystemData(t *testing.T) {
	m := testManager(t)
	m.kernel = func() (string, error) {
		return "", fmt.Errorf("private-kernel-query-012345: %w", os.ErrPermission)
	}
	out := m.Environment(context.Background(), EnvironmentInput{Runtimes: []string{"go"}})
	if !out.OK || !out.Partial || out.OS != runtime.GOOS || out.Arch != runtime.GOARCH || out.Kernel != "" || len(out.Runtimes) != 1 {
		t.Fatalf("basic environment result was lost: %+v", out)
	}
	if len(out.Warnings) != 1 || out.Warnings[0].Code != "permission_denied" || out.Warnings[0].Message != "The operating system kernel version could not be read" {
		t.Fatalf("kernel failure reason was lost or exposed: %+v", out.Warnings)
	}
}
func TestManagerConcurrencyCancellationAndClose(t *testing.T) {
	m := testManager(t)
	m.lookup = func(ctx context.Context, _ string) ([]net.IPAddr, error) { <-ctx.Done(); return nil, ctx.Err() }
	results := make(chan ProbeResult, 4)
	for i := 0; i < 4; i++ {
		go func() { results <- m.Probe(context.Background(), ProbeInput{Mode: "dns", Target: "blocked.example"}) }()
	}
	deadline := time.Now().Add(time.Second)
	for {
		m.mu.Lock()
		n := m.active
		m.mu.Unlock()
		if n == 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("calls did not start")
		}
		time.Sleep(time.Millisecond)
	}
	out := m.Probe(context.Background(), ProbeInput{Mode: "dns", Target: "blocked.example"})
	if out.Code != "resource_limit" {
		t.Fatal(out)
	}
	m.Close()
	for i := 0; i < 4; i++ {
		out := <-results
		if out.Code != "cancelled" || out.FailedStage != "dns" {
			t.Fatal(out)
		}
	}
	if m.Environment(context.Background(), EnvironmentInput{}).Code != "closed" {
		t.Fatal("closed manager accepted call")
	}
}
func TestProbeDNSModesAndFailure(t *testing.T) {
	m := testManager(t)
	m.lookup = func(context.Context, string) ([]net.IPAddr, error) { return nil, &net.DNSError{IsNotFound: true} }
	out := m.Probe(context.Background(), ProbeInput{Mode: "http", Target: "http://missing.example/"})
	if out.OK || out.Code != "dns_failed" || out.FailedStage != "dns" || out.Stages[1].State != "skipped" {
		t.Fatal(out)
	}
	out = m.Probe(context.Background(), ProbeInput{Mode: "dns", Target: "127.0.0.1"})
	if !out.OK || len(out.Addresses) != 1 || out.Stages[0].State != "skipped" {
		t.Fatal(out)
	}
	m.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		a := make([]net.IPAddr, 20)
		for i := range a {
			a[i].IP = net.IPv4(127, 0, 0, byte(i+1))
		}
		return a, nil
	}
	out = m.Probe(context.Background(), ProbeInput{Mode: "dns", Target: "fixture.example"})
	if !out.OK || !out.Truncated || len(out.Addresses) != 16 {
		t.Fatal(out)
	}
	for _, in := range []ProbeInput{{Mode: "invalid", Target: "localhost"}, {Mode: "http", Target: "http://user:secret@localhost/"}, {Mode: "http", Target: "http://localhost/#x"}, {Mode: "http", Target: "http://localhost:"}, {Mode: "tcp", Target: "localhost:0"}, {Mode: "tcp", Target: "localhost:65536"}} {
		if m.Probe(context.Background(), in).Code != "invalid_argument" {
			t.Fatalf("accepted %+v", in)
		}
	}
}
func TestProbeHTTPUsesOneConnectionNoProxyRedirectOrBody(t *testing.T) {
	m := testManager(t)
	var requests, connections atomic.Int32
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/redirect":
			w.Header().Set("Location", "http://invalid.example/")
			w.WriteHeader(302)
		case "/body":
			w.Header().Set("Content-Length", "999999999")
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		default:
			w.WriteHeader(503)
		}
	}))
	s.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	s.Start()
	defer s.Close()
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	for _, test := range []struct {
		path string
		code int
	}{{"/", 503}, {"/redirect", 302}, {"/body", 200}} {
		start := time.Now()
		out := m.Probe(context.Background(), ProbeInput{Mode: "http", Target: s.URL + test.path, TimeoutMS: 1000})
		if !out.OK || out.StatusCode != test.code || out.Stages[1].State != "ok" || out.Stages[2].State != "skipped" || out.Stages[3].State != "ok" {
			t.Fatalf("HTTP stage: %+v", out)
		}
		if time.Since(start) > 700*time.Millisecond {
			t.Fatal("response body was read")
		}
	}
	if requests.Load() != 3 || connections.Load() != 3 {
		t.Fatalf("unexpected requests/connections %d/%d", requests.Load(), connections.Load())
	}
}
func TestProbeTLSVerifiedAndStagePreserved(t *testing.T) {
	m := testManager(t)
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }))
	defer s.Close()
	out := m.Probe(context.Background(), ProbeInput{Mode: "http", Target: s.URL})
	if out.OK || out.Code != "tls_failed" || out.FailedStage != "tls" || out.Stages[1].State != "ok" || out.Stages[3].State != "skipped" {
		t.Fatalf("untrusted TLS: %+v", out)
	}
	roots := x509.NewCertPool()
	roots.AddCert(s.Certificate())
	m.rootCAs = roots
	out = m.Probe(context.Background(), ProbeInput{Mode: "http", Target: s.URL})
	if !out.OK || out.StatusCode != 404 || out.TLSVersion == "" || out.CipherSuite == "" {
		t.Fatalf("verified TLS: %+v", out)
	}
	out = m.Probe(context.Background(), ProbeInput{Mode: "tls", Target: strings.TrimPrefix(s.URL, "https://")})
	if !out.OK || out.Stages[2].State != "ok" || out.Stages[3].State != "skipped" {
		t.Fatal(out)
	}
}
func TestProbeTCPRefusalAndOverallTimeout(t *testing.T) {
	m := testManager(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	target := listener.Addr().String()
	listener.Close()
	out := m.Probe(context.Background(), ProbeInput{Mode: "tcp", Target: target})
	if out.Code != "tcp_failed" || out.Stages[1].Reason != "refused" {
		t.Fatal(out)
	}
	m.lookup = func(ctx context.Context, _ string) ([]net.IPAddr, error) { <-ctx.Done(); return nil, ctx.Err() }
	start := time.Now()
	out = m.Probe(context.Background(), ProbeInput{Mode: "tcp", Target: "wait.example:80", TimeoutMS: 20})
	if out.Code != "timeout" || out.FailedStage != "dns" || time.Since(start) > time.Second {
		t.Fatal(out)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out = m.Probe(ctx, ProbeInput{Mode: "tcp", Target: target})
	if out.Code != "cancelled" || len(out.Attempts) != 0 {
		t.Fatal(out)
	}
}
func TestProbeHeaderLimitAndCancellation(t *testing.T) {
	m := testManager(t)
	m.cfg.MaxHeaderBytes = 1024
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/large" {
			w.Header().Set("X-Large", strings.Repeat("x", 2048))
			w.WriteHeader(200)
			return
		}
		<-r.Context().Done()
	}))
	defer s.Close()
	out := m.Probe(context.Background(), ProbeInput{Mode: "http", Target: s.URL + "/large"})
	if out.Code != "http_failed" || out.Stages[3].Reason != "headers_too_large" || out.Stages[1].State != "ok" {
		t.Fatal(out)
	}
	out = m.Probe(context.Background(), ProbeInput{Mode: "http", Target: s.URL + "/slow", TimeoutMS: 20})
	if out.Code != "timeout" || out.FailedStage != "http" {
		t.Fatal(out)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	out = m.Probe(ctx, ProbeInput{Mode: "http", Target: s.URL + "/slow"})
	if out.Code != "cancelled" || out.FailedStage != "http" {
		t.Fatal(out)
	}
}
func TestWindowsTCPFixturesAndBoundedAPI(t *testing.T) {
	for _, ipv6 := range []bool{false, true} {
		size := 24
		if ipv6 {
			size = 56
		}
		b := make([]byte, 4+size)
		binary.LittleEndian.PutUint32(b, 1)
		row := b[4:]
		if ipv6 {
			copy(row[:16], net.ParseIP("::1").To16())
			binary.BigEndian.PutUint32(row[16:20], 12)
			binary.BigEndian.PutUint16(row[20:22], 8443)
			binary.LittleEndian.PutUint32(row[48:52], 2)
			binary.LittleEndian.PutUint32(row[52:56], 123)
		} else {
			binary.LittleEndian.PutUint32(row[:4], 2)
			copy(row[4:8], []byte{127, 0, 0, 1})
			binary.BigEndian.PutUint16(row[8:10], 8443)
			binary.LittleEndian.PutUint32(row[20:24], 123)
		}
		items, truncated, err := parseWindowsTCP(context.Background(), b, ipv6, 10)
		if err != nil || truncated || len(items) != 1 || items[0].Port != 8443 || items[0].PIDs[0] != 123 {
			t.Fatalf("fixture failed: %+v %v", items, err)
		}
		binary.LittleEndian.PutUint32(b, 2)
		if _, _, err = parseWindowsTCP(context.Background(), b, ipv6, 10); err == nil {
			t.Fatal("malformed size accepted")
		}
	}
	calls := 0
	_, err := queryTCPTable(context.Background(), 1024, func(b []byte, n *uint32) uint32 { calls++; *n = 2048; return 122 })
	if err.(*Error).Code != "resource_limit" || calls != 1 {
		t.Fatalf("unbounded allocation: %d %v", calls, err)
	}
	calls = 0
	_, err = queryTCPTable(context.Background(), 1024, func(b []byte, n *uint32) uint32 { calls++; *n = 28; return 122 })
	if err.(*Error).Code != "resource_limit" || calls != 4 {
		t.Fatalf("unbounded retry: %d %v", calls, err)
	}
	_, err = queryTCPTable(context.Background(), 1024, func(b []byte, n *uint32) uint32 { return 5 })
	if err.(*Error).Code != "permission_denied" {
		t.Fatal(err)
	}
}
func TestMacOSParserFixtures(t *testing.T) {
	items, truncated, err := parsePS(context.Background(), []byte(" 10 1 /Applications/中文 app\n11 10 child\n"), 10)
	if err != nil || truncated || len(items) != 2 || items[0].Name != "/Applications/中文 app" || items[1].PPID != 10 {
		t.Fatalf("ps fixture: %+v %v", items, err)
	}
	listeners, truncated, err := parseLsof(context.Background(), []byte("p10\x00c中文 app\x00\nf1\x00n127.0.0.1:8080\x00\nf2\x00n127.0.0.1:8080\x00\np11\x00f3\x00n[::1]:8443\x00\n"), 10)
	if err != nil || truncated || len(listeners) != 2 || len(listeners[0].PIDs) != 1 || listeners[1].Address != "::1" {
		t.Fatalf("lsof fixture %+v %v", listeners, err)
	}
	_, _, err = parseLsof(context.Background(), []byte("p2\x00nmalformed\x00"), 10)
	if err == nil {
		t.Fatal("invalid endpoint accepted")
	}
}
func TestRealProcessAndListener(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Native PID/listener acceptance is tracked separately")
	}
	m := testManager(t)
	out := m.Processes(context.Background(), SnapshotInput{RootPID: os.Getpid()})
	found := false
	for _, p := range out.Processes {
		if p.PID == os.Getpid() && p.PPID == os.Getppid() {
			found = true
		}
	}
	if !out.OK || !found {
		t.Fatalf("current process absent: %+v", out)
	}
	for _, network := range []string{"tcp4", "tcp6"} {
		host := "127.0.0.1:0"
		if network == "tcp6" {
			host = "[::1]:0"
		}
		listener, err := net.Listen(network, host)
		if err != nil {
			if network == "tcp6" {
				t.Log("IPv6 is unavailable:", err)
				continue
			}
			t.Fatal(err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		out := m.Listeners(context.Background(), ListenersInput{PID: os.Getpid(), Port: port})
		listener.Close()
		found = false
		for _, item := range out.Listeners {
			if item.Port == port {
				for _, pid := range item.PIDs {
					if pid == os.Getpid() {
						found = true
					}
				}
			}
		}
		if !out.OK || !found {
			t.Fatalf("listener PID absent for %s:%s: %+v", network, strconv.Itoa(port), out)
		}
	}
}
func TestBoundedString(t *testing.T) {
	s, truncated := boundedString("中文数据", 5)
	if !truncated || s != "中" {
		t.Fatal(s, truncated)
	}
}

func TestFixedCommandErrorReasonsAndCaptureStreams(t *testing.T) {
	e := fixedCommandError(context.Background(), os.ErrInvalid, []byte("lsof: permission denied: sensitive-path"))
	if e.Code != "permission_denied" || strings.Contains(e.Message, "sensitive-path") {
		t.Fatal(e)
	}
	e = fixedCommandError(context.Background(), os.ErrInvalid, []byte("operation not permitted"))
	if e.Code != "permission_denied" {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if fixedCommandError(ctx, os.ErrPermission, nil).Code != "cancelled" {
		t.Fatal("cancel lost")
	}
}
func TestLsofIPv6WildcardAndMultiOwner(t *testing.T) {
	items, truncated, err := parseLsof(context.Background(), []byte("p10\x00f1\x00tIPv6\x00n*:8080\x00\np11\x00f2\x00tIPv6\x00n*:8080\x00\n"), 10)
	if err != nil || truncated || len(items) != 1 || items[0].Address != "::" || len(items[0].PIDs) != 2 {
		t.Fatalf("IPv6 wildcard fixture %+v %v", items, err)
	}
}
func TestTLSFailureReasons(t *testing.T) {
	cases := []struct {
		err    error
		reason string
	}{{x509.UnknownAuthorityError{}, "certificate_untrusted"}, {x509.HostnameError{}, "certificate_name_mismatch"}, {x509.CertificateInvalidError{}, "certificate_invalid"}, {os.ErrInvalid, "protocol_error"}}
	for _, test := range cases {
		e, reason := probeError(context.Background(), "tls", test.err)
		if e.Code != "tls_failed" || reason != test.reason {
			t.Fatal(e, reason)
		}
	}
}
