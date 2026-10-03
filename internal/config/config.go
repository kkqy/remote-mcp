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
}

func Default() Config {
	return Config{Listen: "0.0.0.0:8080", MaxBodyBytes: 1 << 20, Transfer: transfer.DefaultConfig(), Execution: execution.DefaultConfig()}
}

func Parse(args []string, stderr io.Writer) (Config, error) {
	c := Default()
	fs := flag.NewFlagSet("remote-mcp", flag.ContinueOnError)
	// flag 的默认解析错误可能回显参数值；只输出经过筛选的错误。
	fs.SetOutput(io.Discard)
	var tokenFile, origins string
	fs.StringVar(&c.Listen, "listen", c.Listen, "监听 IP 和端口；端口 0 自动分配")
	fs.StringVar(&tokenFile, "token-file", "", "Token 文件；默认读取 REMOTE_MCP_TOKEN")
	fs.StringVar(&c.TLSCert, "tls-cert", "", "TLS 证书文件")
	fs.StringVar(&c.TLSKey, "tls-key", "", "TLS 私钥文件")
	fs.StringVar(&origins, "allowed-origins", "", "允许的 Origin，逗号分隔；默认拒绝所有带 Origin 的请求")
	fs.Int64Var(&c.MaxBodyBytes, "max-body-bytes", c.MaxBodyBytes, "HTTP 请求体最大字节数")
	fs.Int64Var(&c.Transfer.MaxFileSize, "max-file-bytes", c.Transfer.MaxFileSize, "单文件最大字节数")
	fs.IntVar(&c.Transfer.ChunkSize, "chunk-bytes", c.Transfer.ChunkSize, "每块原始数据最大字节数")
	fs.IntVar(&c.Transfer.MaxTransfers, "max-transfers", c.Transfer.MaxTransfers, "最大并发传输数")
	fs.DurationVar(&c.Transfer.IdleTimeout, "transfer-idle", c.Transfer.IdleTimeout, "传输空闲回收时间")
	fs.DurationVar(&c.Transfer.Retention, "transfer-retention", c.Transfer.Retention, "已结束传输记录保留时间")
	fs.IntVar(&c.Transfer.MaxRecords, "max-transfer-records", c.Transfer.MaxRecords, "最大传输记录数")
	fs.DurationVar(&c.Execution.DefaultTimeout, "command-timeout", c.Execution.DefaultTimeout, "普通命令默认超时")
	fs.DurationVar(&c.Execution.TerminalIdleTimeout, "terminal-idle", c.Execution.TerminalIdleTimeout, "终端无调用活动回收时间")
	fs.DurationVar(&c.Execution.Retention, "process-retention", c.Execution.Retention, "已结束进程和终端保留时间")
	fs.DurationVar(&c.Execution.SweepInterval, "sweep-interval", c.Execution.SweepInterval, "执行资源回收检查间隔")
	fs.IntVar(&c.Execution.MaxProcesses, "max-processes", c.Execution.MaxProcesses, "最大并发进程数")
	fs.IntVar(&c.Execution.MaxTerminals, "max-terminals", c.Execution.MaxTerminals, "最大并发终端数")
	fs.IntVar(&c.Execution.OutputBytes, "output-bytes", c.Execution.OutputBytes, "每个进程或终端的输出缓冲字节数")
	fs.IntVar(&c.Execution.ReadBytes, "read-bytes", c.Execution.ReadBytes, "单次输出读取最大字节数")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "用法：remote-mcp [选项]；启动后每个 IP 输出一份备选 MCP JSON。")
		fs.SetOutput(stderr)
		fs.PrintDefaults()
		fs.SetOutput(io.Discard)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return c, err
		}
		return c, errors.New("命令行参数无效，请使用 --help 查看用法")
	}
	if fs.NArg() != 0 {
		return c, errors.New("不接受位置参数")
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
		return errors.New("监听地址必须为 IP:端口")
	}
	if _, err = netip.ParseAddr(host); err != nil {
		return errors.New("监听地址须使用明确的 IPv4 或 IPv6 地址")
	}
	if _, err = net.LookupPort("tcp", port); err != nil {
		return errors.New("监听端口无效")
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return errors.New("TLS 证书和私钥必须同时提供")
	}
	if c.MaxBodyBytes <= 0 || c.Transfer.ChunkSize <= 0 || int64(c.Transfer.ChunkSize) > (c.MaxBodyBytes-4096)/4*3 {
		return errors.New("请求体上限须容纳 Base64 分块及至少 4 KiB 元数据")
	}
	for _, origin := range c.AllowedOrigins {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("Origin 须为不含路径的 http(s) 源地址")
		}
	}
	return nil
}
