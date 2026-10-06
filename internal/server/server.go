package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"remote-mcp/internal/config"
	"remote-mcp/internal/execution"
	"remote-mcp/internal/fileops"
	"remote-mcp/internal/forwarding"
	"remote-mcp/internal/gui"
	"remote-mcp/internal/inspection"
	"remote-mcp/internal/logstream"
	"remote-mcp/internal/transfer"
)

const ProtocolVersion = "2025-11-25"

type App struct {
	config     config.Config
	MCP        *mcp.Server
	Handler    http.Handler
	files      *transfer.Manager
	execution  *execution.Manager
	forwarding *forwarding.Manager
	gui        *gui.Manager
	fileops    *fileops.Manager
	inspection *inspection.Manager
	logs       *logstream.Manager
	closing    atomic.Bool
	once       sync.Once
	closeErr   error
}

func New(c config.Config, diagnostics io.Writer) (*App, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	files, err := transfer.New(c.Transfer)
	if err != nil {
		return nil, fmt.Errorf("Invalid file transfer configuration: %w", err)
	}
	processes, err := execution.New(c.Execution)
	if err != nil {
		files.Close()
		return nil, fmt.Errorf("Invalid execution configuration: %w", err)
	}
	c.Forwarding.ListenHost, _, _ = net.SplitHostPort(c.Listen)
	forwards, err := forwarding.New(c.Forwarding)
	if err != nil {
		processes.Close()
		files.Close()
		return nil, fmt.Errorf("Invalid forwarding configuration: %w", err)
	}
	graphics, err := gui.New(c.GUI)
	if err != nil {
		forwards.Close()
		processes.Close()
		files.Close()
		return nil, fmt.Errorf("Invalid GUI configuration: %w", err)
	}
	fileTools, err := fileops.New(fileops.DefaultConfig())
	if err != nil {
		graphics.Close()
		forwards.Close()
		processes.Close()
		files.Close()
		return nil, fmt.Errorf("Invalid file operation configuration: %w", err)
	}
	inspector, err := inspection.New(inspection.DefaultConfig())
	if err != nil {
		fileTools.Close()
		graphics.Close()
		forwards.Close()
		processes.Close()
		files.Close()
		return nil, fmt.Errorf("Invalid inspection configuration: %w", err)
	}
	logFiles, err := logstream.New(logstream.DefaultConfig())
	if err != nil {
		inspector.Close()
		fileTools.Close()
		graphics.Close()
		forwards.Close()
		processes.Close()
		files.Close()
		return nil, fmt.Errorf("Invalid log stream configuration: %w", err)
	}
	app := &App{config: c, files: files, execution: processes, forwarding: forwards, gui: graphics, fileops: fileTools, inspection: inspector, logs: logFiles}
	// SDK 调试日志可能携带远端参数，运行日志只使用下方固定元数据。
	silent := slog.New(slog.NewTextHandler(io.Discard, nil))
	app.MCP = mcp.NewServer(&mcp.Implementation{Name: "remote-mcp", Version: "0.1.0"}, &mcp.ServerOptions{SupportedProtocolVersions: []string{ProtocolVersion}, Logger: silent})
	files.Register(app.MCP)
	processes.Register(app.MCP)
	forwards.Register(app.MCP)
	graphics.Register(app.MCP)
	fileTools.Register(app.MCP)
	inspector.Register(app.MCP)
	logFiles.Register(app.MCP)
	logger := slog.New(slog.NewTextHandler(diagnostics, nil))
	app.MCP.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			start := time.Now()
			result, err := next(ctx, method, req)
			if method == "tools/call" {
				name := redactToken("unknown_tool", c.Token)
				if request, ok := req.(*mcp.CallToolRequest); ok && request != nil && request.Params != nil {
					candidate := request.Params.Name
					if toolDomain(candidate) != "" && (c.Token == "" || !strings.Contains(candidate, c.Token)) {
						name = candidate
					}
				}
				failed := err != nil
				if call, ok := result.(*mcp.CallToolResult); ok && call != nil {
					failed = failed || call.IsError
				}
				attributes := []any{"tool", name, "elapsed", time.Since(start), "failed", failed}
				if failed {
					attributes = append(attributes, toolErrorAttributes(req, result, err, c.Token)...)
				}
				logger.Info("Tool call completed", attributes...)
			}
			return result, err
		}
	})
	protocol := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return app.MCP }, &mcp.StreamableHTTPOptions{JSONResponse: true, SessionTimeout: 30 * time.Minute, MaxRequestBodyBytes: c.MaxBodyBytes, Logger: silent})
	expected := sha256.Sum256([]byte(c.Token))
	origins := make(map[string]bool, len(c.AllowedOrigins))
	for _, origin := range c.AllowedOrigins {
		origins[origin] = true
	}
	app.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if app.closing.Load() {
			http.Error(w, "Service is shutting down", http.StatusServiceUnavailable)
			return
		}
		if c.Token != "" {
			authorization := r.Header.Values("Authorization")
			valid := false
			if len(authorization) == 1 {
				scheme, credential, found := strings.Cut(authorization[0], " ")
				if found && strings.EqualFold(scheme, "Bearer") {
					actual := sha256.Sum256([]byte(credential))
					valid = subtle.ConstantTimeCompare(expected[:], actual[:]) == 1
				}
			}
			if !valid {
				w.Header().Set("WWW-Authenticate", `Bearer realm="remote-mcp"`)
				http.Error(w, "Invalid or missing token", http.StatusUnauthorized)
				return
			}
		}
		if origin, exists := r.Header["Origin"]; exists && (len(origin) != 1 || !origins[origin[0]]) {
			http.Error(w, "Origin is not allowed", http.StatusForbidden)
			return
		}
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		protocol.ServeHTTP(w, r)
	})
	return app, nil
}

func (a *App) Close() error {
	a.once.Do(func() {
		a.closing.Store(true)
		a.closeErr = errors.Join(a.logs.Close(), a.inspection.Close(), a.fileops.Close(), a.gui.Close(), a.forwarding.Close(), a.execution.Close(), a.files.Close())
		for session := range a.MCP.Sessions() {
			a.closeErr = errors.Join(a.closeErr, session.Close())
		}
	})
	return a.closeErr
}

// Run 的 stdout 仅承载可复制的独立配置对象，监听或证书失败不输出配置。
func Run(ctx context.Context, c config.Config, out, diagnostics io.Writer) error {
	app, err := New(c, diagnostics)
	if err != nil {
		return err
	}
	defer app.Close()
	var tlsConfig *tls.Config
	if c.TLSCert != "" {
		certificate, err := tls.LoadX509KeyPair(c.TLSCert, c.TLSKey)
		if err != nil {
			return errors.New("Unable to load TLS certificate and private key")
		}
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
	}
	host, _, _ := net.SplitHostPort(c.Listen)
	network := "tcp4"
	if strings.Contains(host, ":") {
		network = "tcp6"
	}
	listener, err := net.Listen(network, c.Listen)
	if err != nil {
		return errors.New("Unable to listen; check address, port, and permissions")
	}
	defer listener.Close()
	bound := listener.Addr().(*net.TCPAddr)
	if tlsConfig != nil {
		listener = tls.NewListener(listener, tlsConfig)
	}
	if err := WriteStartup(out, diagnostics, bound, tlsConfig != nil, c.Token, LocalInterfaces); err != nil {
		return err
	}
	httpServer := &http.Server{Handler: app.Handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32 << 10, ErrorLog: log.New(io.Discard, "", 0)}
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return errors.New("HTTP service stopped")
		}
		return nil
	case <-ctx.Done():
		// 先关闭资源使等待调用退出，再关闭协议会话和 HTTP 连接。
		app.closing.Store(true)
		listener.Close()
		closeErr := app.Close()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			httpServer.Close()
		}
		<-done
		if closeErr != nil {
			return errors.New("Failed to close some resources")
		}
		fmt.Fprintln(diagnostics, "Service stopped; associated resources have been cleaned up.")
		return nil
	}
}
