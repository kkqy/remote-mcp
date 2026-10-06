package inspection

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func parseTarget(in ProbeInput) (host, port string, u *url.URL, secure bool, err error) {
	invalid := func() { err = failure("invalid_argument", "The probe mode or target format is invalid") }
	if len(in.Target) == 0 || len(in.Target) > 8192 || strings.ContainsAny(in.Target, "\r\n\t ") {
		invalid()
		return
	}
	switch in.Mode {
	case "dns":
		host = in.Target
		if strings.ContainsAny(host, "/:@?#") {
			if net.ParseIP(host) == nil {
				invalid()
			}
		}
	case "tcp", "tls":
		host, port, err = net.SplitHostPort(in.Target)
		if err != nil {
			invalid()
			return
		}
		secure = in.Mode == "tls"
	case "http":
		u, err = url.Parse(in.Target)
		if err != nil {
			invalid()
			return
		}
		if (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" || u.Opaque != "" || u.Host == "" {
			invalid()
			return
		}
		host = u.Hostname()
		port = u.Port()
		secure = u.Scheme == "https"
		if port == "" {
			port = "80"
			if secure {
				port = "443"
			}
		}
		if strings.HasSuffix(u.Host, ":") {
			invalid()
			return
		}
	default:
		invalid()
		return
	}
	if len(host) == 0 || len(host) > 253 || strings.ContainsAny(host, "/@?#%\\") {
		invalid()
		return
	}
	if port != "" {
		p, e := strconv.Atoi(port)
		if e != nil || p < 1 || p > 65535 {
			invalid()
			return
		}
	}
	return
}
func (m *Manager) Probe(ctx context.Context, in ProbeInput) ProbeResult {
	out := ProbeResult{Result: Result{OK: true}, Addresses: []string{}, Attempts: []Attempt{}, Stages: []Stage{{Stage: "dns", State: "skipped"}, {Stage: "tcp", State: "skipped"}, {Stage: "tls", State: "skipped"}, {Stage: "http", State: "skipped"}}}
	host, port, u, secure, err := parseTarget(in)
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
	fail := func(index int, start time.Time, err error) {
		e, reason := probeError(ctx, out.Stages[index].Stage, err)
		out.fail(e)
		out.FailedStage = out.Stages[index].Stage
		out.Stages[index] = Stage{Stage: out.FailedStage, State: "failed", ElapsedMS: time.Since(start).Milliseconds(), Code: e.Code, Message: e.Message, Reason: reason}
	}
	success := func(index int, start time.Time) {
		out.Stages[index].State = "ok"
		out.Stages[index].ElapsedMS = time.Since(start).Milliseconds()
	}
	start := time.Now()
	var addresses []net.IPAddr
	if ip := net.ParseIP(host); ip != nil {
		addresses = []net.IPAddr{{IP: ip}}
	} else {
		addresses, err = m.lookup(ctx, host)
		if err != nil {
			fail(0, start, err)
			return out
		}
		if len(addresses) == 0 {
			fail(0, start, &net.DNSError{IsNotFound: true})
			return out
		}
		success(0, start)
	}
	if len(addresses) > 16 {
		addresses = addresses[:16]
		out.Truncated = true
	}
	seen := map[string]bool{}
	for _, a := range addresses {
		s := a.String()
		if !seen[s] {
			out.Addresses = append(out.Addresses, s)
			seen[s] = true
		}
	}
	if in.Mode == "dns" {
		return out
	}
	start = time.Now()
	var conn net.Conn
	for _, addr := range out.Addresses {
		if ctx.Err() != nil {
			err = ctx.Err()
			break
		}
		target := net.JoinHostPort(addr, port)
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", target)
		attempt := Attempt{Address: target, State: "ok"}
		if err != nil {
			attempt.State = "failed"
			_, attempt.Reason = probeError(ctx, "tcp", err)
		}
		out.Attempts = append(out.Attempts, attempt)
		if err == nil {
			break
		}
	}
	if err != nil || conn == nil {
		if err == nil {
			err = &net.OpError{}
		}
		fail(1, start, err)
		return out
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)
	out.LocalAddress = conn.LocalAddr().String()
	out.RemoteAddress = conn.RemoteAddr().String()
	success(1, start)
	if in.Mode == "tcp" {
		return out
	}
	var wire net.Conn = conn
	if secure {
		start = time.Now()
		t := tls.Client(conn, &tls.Config{RootCAs: m.rootCAs, ServerName: host, MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}})
		if err = t.HandshakeContext(ctx); err != nil {
			fail(2, start, err)
			return out
		}
		wire = t
		state := t.ConnectionState()
		out.TLSVersion = tls.VersionName(state.Version)
		out.CipherSuite = tls.CipherSuiteName(state.CipherSuite)
		success(2, start)
	}
	if in.Mode == "tls" {
		return out
	}
	// Transport 使用已探测连接；重试拨号不能扩展到新连接或触发二次 DNS。
	start = time.Now()
	used := false
	dial := func(context.Context, string, string) (net.Conn, error) {
		if used {
			return nil, failure("http_failed", "HTTP attempted an additional connection")
		}
		used = true
		return wire, nil
	}
	transport := &http.Transport{Proxy: nil, DialContext: dial, DialTLSContext: dial, DisableKeepAlives: true, MaxResponseHeaderBytes: m.cfg.MaxHeaderBytes, ResponseHeaderTimeout: time.Until(deadline), ForceAttemptHTTP2: false, TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{}}
	defer transport.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		fail(3, start, err)
		return out
	}
	response, err := transport.RoundTrip(req)
	if err != nil {
		fail(3, start, err)
		return out
	}
	out.StatusCode = response.StatusCode
	response.Body.Close()
	success(3, start)
	return out
}
func probeError(ctx context.Context, stage string, err error) (*Error, string) {
	if ctx.Err() != nil {
		return contextError(ctx.Err()), contextError(ctx.Err()).Code
	}
	if errors.Is(err, context.Canceled) {
		return contextError(err), "cancelled"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return failure("timeout", "The network stage exceeded the total time budget"), "timeout"
	}
	switch stage {
	case "dns":
		var de *net.DNSError
		if errors.As(err, &de) && de.IsNotFound {
			return failure("dns_failed", "DNS name resolution failed because no address was found"), "not_found"
		}
		return failure("dns_failed", "DNS name resolution failed"), "resolution_error"
	case "tcp":
		if errors.Is(err, syscall.ECONNREFUSED) {
			return failure("tcp_failed", "The TCP connection was refused"), "refused"
		}
		if errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH) {
			return failure("tcp_failed", "The TCP target is unreachable"), "unreachable"
		}
		return failure("tcp_failed", "The TCP connection could not be established"), "connection_error"
	case "tls":
		var unknown x509.UnknownAuthorityError
		var hostname x509.HostnameError
		var invalid x509.CertificateInvalidError
		if errors.As(err, &unknown) {
			return failure("tls_failed", "The TLS certificate is not trusted"), "certificate_untrusted"
		}
		if errors.As(err, &hostname) {
			return failure("tls_failed", "The TLS certificate does not match the target hostname"), "certificate_name_mismatch"
		}
		if errors.As(err, &invalid) {
			return failure("tls_failed", "The TLS certificate is expired or invalid"), "certificate_invalid"
		}
		return failure("tls_failed", "The TLS handshake failed"), "protocol_error"
	default:
		if strings.Contains(err.Error(), "response headers exceeded") {
			return failure("http_failed", "HTTP response headers exceeded the configured limit"), "headers_too_large"
		}
		return failure("http_failed", "A valid HTTP response could not be read"), "protocol_error"
	}
}
