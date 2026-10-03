package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"remote-mcp/internal/config"
	"remote-mcp/internal/transfer"
)

type caller interface {
	CallTool(context.Context, *mcp.CallToolParams) (*mcp.CallToolResult, error)
}
type summary struct {
	OK        bool   `json:"ok"`
	Operation string `json:"operation,omitempty"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256,omitempty"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
}

type bearerTransport struct {
	base  http.RoundTripper
	token string
}

func (b bearerTransport) CloseIdleConnections() {
	if closer, ok := b.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}
func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	clone.Header = r.Header.Clone()
	if b.token != "" {
		clone.Header.Set("Authorization", "Bearer "+b.token)
	} else {
		clone.Header.Del("Authorization")
	}
	return b.base.RoundTrip(clone)
}
func clientFor(endpoint, token, caFile string) (*http.Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("服务 URL 必须是无凭据、查询及片段的 HTTP(S) 地址")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, errors.New("无法读取 CA 证书")
		}
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("CA 证书无效")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	return &http.Client{Transport: bearerTransport{transport, token}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("不允许 MCP 服务重定向") }}, nil
}
func invoke(ctx context.Context, c caller, name string, input any) (transfer.Result, error) {
	res, err := c.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: input})
	if err != nil {
		return transfer.Result{}, errors.New("MCP 调用失败，请检查连接、鉴权及服务状态")
	}
	var raw []byte
	if res.StructuredContent != nil {
		raw, err = json.Marshal(res.StructuredContent)
	} else {
		for _, item := range res.Content {
			if text, ok := item.(*mcp.TextContent); ok {
				raw = []byte(text.Text)
				break
			}
		}
	}
	var result transfer.Result
	if err != nil || json.Unmarshal(raw, &result) != nil {
		return result, errors.New("MCP 返回的文件响应格式无效")
	}
	if res.IsError || !result.OK {
		return result, result
	}
	return result, nil
}
func cleanup(c caller, name, id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = invoke(ctx, c, name, transfer.IDInput{ID: id})
}
func progress(w io.Writer, n, total int64, last *time.Time) {
	if time.Since(*last) >= time.Second || n == total {
		fmt.Fprintf(w, "已传输 %d / %d 字节\n", n, total)
		*last = time.Now()
	}
}
func upload(ctx context.Context, c caller, source, target string, overwrite bool, stderr io.Writer) (summary, error) {
	info, err := os.Stat(source)
	if err != nil || !info.Mode().IsRegular() {
		return summary{}, errors.New("本地源文件必须是普通文件")
	}
	file, err := os.Open(source)
	if err != nil {
		return summary{}, errors.New("无法打开本地源文件")
	}
	defer file.Close()
	if !transfer.Unchanged(file, source, info) {
		return summary{}, errors.New("本地源文件已变化")
	}
	digest, err := transfer.HashFile(ctx, file)
	if err != nil {
		return summary{}, errors.New("本地文件哈希失败")
	}
	if !transfer.Unchanged(file, source, info) {
		return summary{}, errors.New("本地源文件在哈希期间发生变化")
	}
	requestID := make([]byte, 16)
	if _, err = rand.Read(requestID); err != nil {
		return summary{}, errors.New("生成请求标识失败")
	}
	created, err := invoke(ctx, c, "upload_create", transfer.UploadInput{RequestID: hex.EncodeToString(requestID), Path: target, Size: info.Size(), SHA256: digest, Overwrite: overwrite})
	if err != nil {
		return summary{}, err
	}
	defer cleanup(c, "upload_cancel", created.ID)
	if created.ID == "" || created.State != "active" || created.ChunkSize <= 0 || created.ChunkSize > 16<<20 {
		return summary{}, errors.New("服务返回的上传配置无效")
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return summary{}, errors.New("无法重置本地文件偏移")
	}
	buffer := make([]byte, created.ChunkSize)
	var offset int64
	var last time.Time
	for offset < info.Size() {
		count := int(min(int64(len(buffer)), info.Size()-offset))
		n, err := io.ReadFull(file, buffer[:count])
		if err != nil {
			return summary{}, errors.New("读取本地文件失败或源文件发生变化")
		}
		result, err := invoke(ctx, c, "upload_write", transfer.WriteInput{ID: created.ID, Offset: offset, Data: base64.StdEncoding.EncodeToString(buffer[:n])})
		if err != nil {
			return summary{}, err
		}
		offset += int64(n)
		if result.Offset != offset {
			return summary{}, errors.New("服务返回的上传偏移无效")
		}
		progress(stderr, offset, info.Size(), &last)
	}
	if !transfer.Unchanged(file, source, info) {
		return summary{}, errors.New("本地源文件在上传期间发生变化")
	}
	result, err := invoke(ctx, c, "upload_finish", transfer.IDInput{ID: created.ID})
	if err != nil {
		return summary{}, err
	}
	if result.Size != info.Size() || result.SHA256 != digest || result.State != "completed" {
		return summary{}, errors.New("服务返回的完成校验无效")
	}
	return summary{OK: true, Operation: "upload", Size: result.Size, SHA256: result.SHA256}, nil
}
func download(ctx context.Context, c caller, source, target string, overwrite bool, stderr io.Writer) (summary, error) {
	target, err := filepath.Abs(target)
	if err != nil {
		return summary{}, errors.New("本地目标路径无效")
	}
	if !overwrite {
		if _, err = os.Lstat(target); err == nil {
			return summary{}, errors.New("本地目标已存在，请显式指定 --overwrite")
		} else if !errors.Is(err, os.ErrNotExist) {
			return summary{}, errors.New("无法访问本地目标")
		}
	}
	opened, err := invoke(ctx, c, "download_open", transfer.PathInput{Path: source})
	if err != nil {
		return summary{}, err
	}
	closed := false
	defer func() {
		if !closed {
			cleanup(c, "download_close", opened.ID)
		}
	}()
	hashBytes, hashErr := hex.DecodeString(opened.SHA256)
	if opened.ID == "" || opened.Size < 0 || opened.ChunkSize <= 0 || opened.ChunkSize > 16<<20 || hashErr != nil || len(hashBytes) != sha256.Size {
		return summary{}, errors.New("服务返回的下载配置无效")
	}
	file, err := os.CreateTemp(filepath.Dir(target), ".remote-mcp-download-*")
	if err != nil {
		return summary{}, errors.New("无法创建本地临时文件")
	}
	defer os.Remove(file.Name())
	defer file.Close()
	h := sha256.New()
	var offset int64
	var last time.Time
	for offset < opened.Size {
		result, err := invoke(ctx, c, "download_read", transfer.ReadInput{ID: opened.ID, Offset: offset, Length: opened.ChunkSize})
		if err != nil {
			return summary{}, err
		}
		if len(result.Data) > base64.StdEncoding.EncodedLen(opened.ChunkSize) {
			return summary{}, errors.New("服务返回的下载分块过大")
		}
		data, err := base64.StdEncoding.Strict().DecodeString(result.Data)
		if err != nil || len(data) == 0 || len(data) > opened.ChunkSize || int64(len(data)) > opened.Size-offset || result.Offset != offset+int64(len(data)) {
			return summary{}, errors.New("服务返回的下载分块无效")
		}
		if _, err = file.Write(data); err != nil {
			return summary{}, errors.New("本地文件写入失败")
		}
		_, _ = h.Write(data)
		offset += int64(len(data))
		progress(stderr, offset, opened.Size, &last)
	}
	digest := hex.EncodeToString(h.Sum(nil))
	if digest != opened.SHA256 {
		return summary{}, errors.New("下载 SHA-256 校验不一致")
	}
	result, err := invoke(ctx, c, "download_close", transfer.IDInput{ID: opened.ID})
	if err != nil {
		return summary{}, err
	}
	closed = true
	if result.Size != opened.Size || result.SHA256 != digest {
		return summary{}, errors.New("下载结束时文件信息不一致")
	}
	if err = file.Sync(); err != nil {
		return summary{}, errors.New("本地文件同步失败")
	}
	if err = file.Close(); err != nil {
		return summary{}, errors.New("本地文件关闭失败")
	}
	if err = transfer.Publish(file.Name(), target, overwrite); err != nil {
		return summary{}, errors.New("本地文件发布失败，目标可能已存在或被占用")
	}
	return summary{OK: true, Operation: "download", Size: opened.Size, SHA256: digest}, nil
}
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fail := func(err error) int {
		message := err.Error()
		fmt.Fprintln(stderr, message)
		_ = json.NewEncoder(stdout).Encode(summary{Code: "TRANSFER_FAILED", Message: message})
		return 1
	}
	if len(args) == 0 || (args[0] != "upload" && args[0] != "download") {
		return fail(errors.New("用法：remote-mcp-transfer upload|download [选项] 源路径 目标路径"))
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	// 解析错误可能回显原始参数；帮助仅显示固定默认值，不展示环境中的 URL。
	flags.SetOutput(io.Discard)
	endpoint := flags.String("url", "", "MCP HTTP(S) 地址，也可设置 REMOTE_MCP_URL")
	tokenFile := flags.String("token-file", "", "可选 Token 文件，否则读取 REMOTE_MCP_TOKEN；未配置时不发送凭据")
	overwrite := flags.Bool("overwrite", false, "允许替换目标文件")
	caFile := flags.String("ca-file", "", "额外信任的 PEM CA 证书")
	timeout := flags.Duration("timeout", 30*time.Minute, "整个传输的最大时长")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "用法：remote-mcp-transfer upload|download [选项] 源路径 目标路径")
		flags.SetOutput(stderr)
		flags.PrintDefaults()
		flags.SetOutput(io.Discard)
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return fail(errors.New("命令参数无效"))
	}
	if *endpoint == "" {
		*endpoint = os.Getenv("REMOTE_MCP_URL")
	}
	if flags.NArg() != 2 || *timeout <= 0 {
		return fail(errors.New("必须提供源路径、目标路径和正数超时"))
	}
	tokenFileSet := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "token-file" {
			tokenFileSet = true
		}
	})
	if tokenFileSet && *tokenFile == "" {
		return fail(errors.New("Token 文件路径不能为空"))
	}
	token, err := config.LoadToken(*tokenFile)
	if err != nil {
		return fail(err)
	}
	httpClient, err := clientFor(*endpoint, token, *caFile)
	if err != nil {
		return fail(err)
	}
	defer httpClient.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "remote-mcp-transfer", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: *endpoint, HTTPClient: httpClient, MaxRetries: -1, DisableStandaloneSSE: true}, nil)
	if err != nil {
		return fail(errors.New("MCP 连接失败，请检查 URL、Token、证书及网络"))
	}
	defer session.Close()
	var result summary
	if args[0] == "upload" {
		result, err = upload(ctx, session, flags.Arg(0), flags.Arg(1), *overwrite, stderr)
	} else {
		result, err = download(ctx, session, flags.Arg(0), flags.Arg(1), *overwrite, stderr)
	}
	if err != nil {
		return fail(err)
	}
	if err = json.NewEncoder(stdout).Encode(result); err != nil {
		return 1
	}
	return 0
}
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
