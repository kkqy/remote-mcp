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
	"remote-mcp/internal/transfer"
)

const ProtocolVersion = "2025-11-25"

type App struct {
	config    config.Config
	MCP       *mcp.Server
	Handler   http.Handler
	files     *transfer.Manager
	execution *execution.Manager
	closing   atomic.Bool
	once      sync.Once
	closeErr  error
}

func New(c config.Config, diagnostics io.Writer) (*App, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	files, err := transfer.New(c.Transfer)
	if err != nil {
		return nil, fmt.Errorf("文件传输配置无效: %w", err)
	}
	processes, err := execution.New(c.Execution)
	if err != nil {
		files.Close()
		return nil, fmt.Errorf("执行配置无效: %w", err)
	}
	app := &App{config: c, files: files, execution: processes}
	// SDK 调试日志可能携带远端参数，运行日志只使用下方固定元数据。
	silent := slog.New(slog.NewTextHandler(io.Discard, nil))
	app.MCP = mcp.NewServer(&mcp.Implementation{Name: "remote-mcp", Version: "0.1.0"}, &mcp.ServerOptions{SupportedProtocolVersions: []string{ProtocolVersion}, Logger: silent})
	files.Register(app.MCP)
	processes.Register(app.MCP)
	logger := slog.New(slog.NewTextHandler(diagnostics, nil))
	app.MCP.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			start := time.Now()
			result, err := next(ctx, method, req)
			if method == "tools/call" {
				name := "未知工具"
				if request, ok := req.(*mcp.CallToolRequest); ok && request != nil && request.Params != nil {
					candidate := request.Params.Name
					if len(candidate) <= 64 && (c.Token == "" || !strings.Contains(candidate, c.Token)) && strings.Trim(candidate, "abcdefghijklmnopqrstuvwxyz0123456789_") == "" {
						name = candidate
					}
				}
				failed := err != nil
				if call, ok := result.(*mcp.CallToolResult); ok && call != nil {
					failed = failed || call.IsError
				}
				logger.Info("工具调用完成", "tool", name, "elapsed", time.Since(start), "failed", failed)
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
			http.Error(w, "服务正在关闭", http.StatusServiceUnavailable)
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
				http.Error(w, "Token 无效或缺失", http.StatusUnauthorized)
				return
			}
		}
		if origin, exists := r.Header["Origin"]; exists && (len(origin) != 1 || !origins[origin[0]]) {
			http.Error(w, "Origin 不被允许", http.StatusForbidden)
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
		a.closeErr = errors.Join(a.execution.Close(), a.files.Close())
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
			return errors.New("无法加载 TLS 证书和私钥")
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
		return errors.New("监听失败，请检查地址、端口和权限")
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
			return errors.New("HTTP 服务停止运行")
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
			return errors.New("关闭部分资源失败")
		}
		fmt.Fprintln(diagnostics, "服务已关闭，关联资源已清理。")
		return nil
	}
}
