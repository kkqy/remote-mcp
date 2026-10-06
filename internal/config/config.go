package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"remote-mcp/internal/execution"
	"remote-mcp/internal/forwarding"
	"remote-mcp/internal/gui"
	"remote-mcp/internal/transfer"
	"strings"
)

type Config struct {
	Listen         string
	Token          string
	TLSCert        string
	TLSKey         string
	AllowedOrigins []string
	MaxBodyBytes   int64
	Transfer       transfer.Config
	Execution      execution.Config
	Forwarding     forwarding.Config
	GUI            gui.Config
}

func Default() Config {
	return Config{Listen: "0.0.0.0:8080", MaxBodyBytes: 1 << 20, Transfer: transfer.DefaultConfig(), Execution: execution.DefaultConfig(), Forwarding: forwarding.DefaultConfig(), GUI: gui.DefaultConfig()}
}

func Parse(args []string, stderr io.Writer) (Config, error) {
	c := Default()
	fs := flag.NewFlagSet("remote-mcp", flag.ContinueOnError)
	// flag 的默认解析错误可能回显参数值；只输出经过筛选的错误。
	fs.SetOutput(io.Discard)
	var tokenFile, origins string
	fs.StringVar(&c.Listen, "listen", c.Listen, "Listen IP and port; port 0 selects an available port")
	fs.StringVar(&tokenFile, "token-file", "", "Optional token file; defaults to REMOTE_MCP_TOKEN; allows anonymous access when unset")
	fs.StringVar(&c.TLSCert, "tls-cert", "", "TLS certificate file")
	fs.StringVar(&c.TLSKey, "tls-key", "", "TLS private key file")
	fs.StringVar(&origins, "allowed-origins", "", "Allowed Origins, separated by commas; all requests with Origin are rejected by default")
	fs.Int64Var(&c.MaxBodyBytes, "max-body-bytes", c.MaxBodyBytes, "Maximum HTTP request body size in bytes")
	fs.Int64Var(&c.Transfer.MaxFileSize, "max-file-bytes", c.Transfer.MaxFileSize, "Maximum file size in bytes")
	fs.IntVar(&c.Transfer.ChunkSize, "chunk-bytes", c.Transfer.ChunkSize, "Maximum raw chunk size in bytes")
	fs.IntVar(&c.Transfer.MaxTransfers, "max-transfers", c.Transfer.MaxTransfers, "Maximum concurrent transfers")
	fs.DurationVar(&c.Transfer.IdleTimeout, "transfer-idle", c.Transfer.IdleTimeout, "Transfer idle timeout")
	fs.DurationVar(&c.Transfer.Retention, "transfer-retention", c.Transfer.Retention, "Completed transfer record retention")
	fs.IntVar(&c.Transfer.MaxRecords, "max-transfer-records", c.Transfer.MaxRecords, "Maximum transfer records")
	fs.DurationVar(&c.Execution.DefaultTimeout, "command-timeout", c.Execution.DefaultTimeout, "Default command timeout")
	fs.DurationVar(&c.Execution.TerminalIdleTimeout, "terminal-idle", c.Execution.TerminalIdleTimeout, "Terminal timeout without calls")
	fs.DurationVar(&c.Execution.Retention, "process-retention", c.Execution.Retention, "Completed process and terminal record retention")
	fs.DurationVar(&c.Execution.SweepInterval, "sweep-interval", c.Execution.SweepInterval, "Execution resource cleanup interval")
	fs.IntVar(&c.Execution.MaxProcesses, "max-processes", c.Execution.MaxProcesses, "Maximum concurrent processes")
	fs.IntVar(&c.Execution.MaxTerminals, "max-terminals", c.Execution.MaxTerminals, "Maximum concurrent terminals")
	fs.IntVar(&c.Execution.OutputBytes, "output-bytes", c.Execution.OutputBytes, "Output buffer size per process or terminal in bytes")
	fs.IntVar(&c.Execution.ReadBytes, "read-bytes", c.Execution.ReadBytes, "Maximum output bytes per read")
	fs.IntVar(&c.Forwarding.MaxRules, "max-forwards", c.Forwarding.MaxRules, "Maximum active TCP forwarding rules")
	fs.IntVar(&c.Forwarding.MaxConnections, "max-forward-connections", c.Forwarding.MaxConnections, "Global TCP forwarding connection limit, including pending connections")
	fs.IntVar(&c.Forwarding.MaxRecords, "max-forward-records", c.Forwarding.MaxRecords, "Maximum TCP forwarding records")
	fs.DurationVar(&c.Forwarding.DialTimeout, "forward-dial-timeout", c.Forwarding.DialTimeout, "TCP forwarding target connection timeout")
	fs.DurationVar(&c.Forwarding.Retention, "forward-retention", c.Forwarding.Retention, "Completed TCP forwarding record retention")
	fs.DurationVar(&c.Forwarding.SweepInterval, "forward-sweep-interval", c.Forwarding.SweepInterval, "TCP forwarding record cleanup interval")
	fs.DurationVar(&c.GUI.IdleTimeout, "gui-idle", c.GUI.IdleTimeout, "GUI session timeout without calls")
	fs.DurationVar(&c.GUI.AuthorizationTimeout, "gui-authorize-timeout", c.GUI.AuthorizationTimeout, "Total GUI authorization timeout")
	fs.DurationVar(&c.GUI.OperationTimeout, "gui-operation-timeout", c.GUI.OperationTimeout, "Screenshot and input operation timeout")
	fs.IntVar(&c.GUI.MaxPixels, "gui-max-pixels", c.GUI.MaxPixels, "Maximum pixels in a raw display screenshot")
	fs.IntVar(&c.GUI.MaxPNGBytes, "gui-max-png-bytes", c.GUI.MaxPNGBytes, "Maximum PNG screenshot size in bytes")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: remote-mcp [options]; startup prints an alternative MCP JSON configuration for each IP.")
		fs.SetOutput(stderr)
		fs.PrintDefaults()
		fs.SetOutput(io.Discard)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return c, err
		}
		return c, errors.New("Invalid command-line arguments; use --help for usage")
	}
	if fs.NArg() != 0 {
		return c, errors.New("Positional arguments are not accepted")
	}
	tokenFileSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "token-file" {
			tokenFileSet = true
		}
	})
	if tokenFileSet && tokenFile == "" {
		return c, errors.New("Token file path must not be empty")
	}
	var err error
	c.Token, err = LoadToken(tokenFile)
	if err != nil {
		return c, err
	}
	if origins != "" {
		c.AllowedOrigins = strings.Split(origins, ",")
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
	if err := ValidateToken(c.Token); err != nil {
		return err
	}
	host, port, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return errors.New("Listen address must be IP:port")
	}
	if _, err = netip.ParseAddr(host); err != nil {
		return errors.New("Listen address must use an explicit IPv4 or IPv6 address")
	}
	if _, err = net.LookupPort("tcp", port); err != nil {
		return errors.New("Invalid listen port")
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return errors.New("TLS certificate and private key must be provided together")
	}
	if c.MaxBodyBytes <= 0 || c.Transfer.ChunkSize <= 0 || int64(c.Transfer.ChunkSize) > (c.MaxBodyBytes-4096)/4*3 {
		return errors.New("Request body limit must fit a Base64 chunk and at least 4 KiB of metadata")
	}
	for _, origin := range c.AllowedOrigins {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("Origin must be an HTTP(S) origin without a path")
		}
	}
	if err := c.Forwarding.Validate(); err != nil {
		return err
	}
	return c.GUI.Validate()
}
