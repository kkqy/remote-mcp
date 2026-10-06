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
		return nil, errors.New("Service URL must be an HTTP(S) address without credentials, query, or fragment")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, errors.New("Unable to read CA certificate")
		}
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("Invalid CA certificate")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	return &http.Client{Transport: bearerTransport{transport, token}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("MCP service redirects are not allowed") }}, nil
}
func invoke(ctx context.Context, c caller, name string, input any) (transfer.Result, error) {
	res, err := c.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: input})
	if err != nil {
		return transfer.Result{}, errors.New("MCP call failed; check connection, authentication, and service status")
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
		return result, errors.New("Invalid file response from MCP")
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
		fmt.Fprintf(w, "Transferred %d / %d bytes\n", n, total)
		*last = time.Now()
	}
}
func upload(ctx context.Context, c caller, source, target string, overwrite bool, stderr io.Writer) (summary, error) {
	info, err := os.Stat(source)
	if err != nil || !info.Mode().IsRegular() {
		return summary{}, errors.New("Local source must be a regular file")
	}
	file, err := os.Open(source)
	if err != nil {
		return summary{}, errors.New("Unable to open local source file")
	}
	defer file.Close()
	if !transfer.Unchanged(file, source, info) {
		return summary{}, errors.New("Local source file has changed")
	}
	digest, err := transfer.HashFile(ctx, file)
	if err != nil {
		return summary{}, errors.New("Unable to hash local file")
	}
	if !transfer.Unchanged(file, source, info) {
		return summary{}, errors.New("Local source file changed during hashing")
	}
	requestID := make([]byte, 16)
	if _, err = rand.Read(requestID); err != nil {
		return summary{}, errors.New("Unable to generate request ID")
	}
	created, err := invoke(ctx, c, "upload_create", transfer.UploadInput{RequestID: hex.EncodeToString(requestID), Path: target, Size: info.Size(), SHA256: digest, Overwrite: overwrite})
	if err != nil {
		return summary{}, err
	}
	defer cleanup(c, "upload_cancel", created.ID)
	if created.ID == "" || created.State != "active" || created.ChunkSize <= 0 || created.ChunkSize > 16<<20 {
		return summary{}, errors.New("Invalid upload configuration returned by service")
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return summary{}, errors.New("Unable to reset local file offset")
	}
	buffer := make([]byte, created.ChunkSize)
	var offset int64
	var last time.Time
	for offset < info.Size() {
		count := int(min(int64(len(buffer)), info.Size()-offset))
		n, err := io.ReadFull(file, buffer[:count])
		if err != nil {
			return summary{}, errors.New("Unable to read local file or source file has changed")
		}
		result, err := invoke(ctx, c, "upload_write", transfer.WriteInput{ID: created.ID, Offset: offset, Data: base64.StdEncoding.EncodeToString(buffer[:n])})
		if err != nil {
			return summary{}, err
		}
		offset += int64(n)
		if result.Offset != offset {
			return summary{}, errors.New("Invalid upload offset returned by service")
		}
		progress(stderr, offset, info.Size(), &last)
	}
	if !transfer.Unchanged(file, source, info) {
		return summary{}, errors.New("Local source file changed during upload")
	}
	result, err := invoke(ctx, c, "upload_finish", transfer.IDInput{ID: created.ID})
	if err != nil {
		return summary{}, err
	}
	if result.Size != info.Size() || result.SHA256 != digest || result.State != "completed" {
		return summary{}, errors.New("Invalid completion verification returned by service")
	}
	return summary{OK: true, Operation: "upload", Size: result.Size, SHA256: result.SHA256}, nil
}
func download(ctx context.Context, c caller, source, target string, overwrite bool, stderr io.Writer) (summary, error) {
	target, err := filepath.Abs(target)
	if err != nil {
		return summary{}, errors.New("Invalid local destination path")
	}
	if !overwrite {
		if _, err = os.Lstat(target); err == nil {
			return summary{}, errors.New("Local destination already exists; specify --overwrite to replace it")
		} else if !errors.Is(err, os.ErrNotExist) {
			return summary{}, errors.New("Unable to access local destination")
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
		return summary{}, errors.New("Invalid download configuration returned by service")
	}
	file, err := os.CreateTemp(filepath.Dir(target), ".remote-mcp-download-*")
	if err != nil {
		return summary{}, errors.New("Unable to create local temporary file")
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
			return summary{}, errors.New("Download chunk returned by service exceeds the limit")
		}
		data, err := base64.StdEncoding.Strict().DecodeString(result.Data)
		if err != nil || len(data) == 0 || len(data) > opened.ChunkSize || int64(len(data)) > opened.Size-offset || result.Offset != offset+int64(len(data)) {
			return summary{}, errors.New("Invalid download chunk returned by service")
		}
		if _, err = file.Write(data); err != nil {
			return summary{}, errors.New("Unable to write local file")
		}
		_, _ = h.Write(data)
		offset += int64(len(data))
		progress(stderr, offset, opened.Size, &last)
	}
	digest := hex.EncodeToString(h.Sum(nil))
	if digest != opened.SHA256 {
		return summary{}, errors.New("Download SHA-256 verification mismatch")
	}
	result, err := invoke(ctx, c, "download_close", transfer.IDInput{ID: opened.ID})
	if err != nil {
		return summary{}, err
	}
	closed = true
	if result.Size != opened.Size || result.SHA256 != digest {
		return summary{}, errors.New("File information changed at download completion")
	}
	if err = file.Sync(); err != nil {
		return summary{}, errors.New("Unable to sync local file")
	}
	if err = file.Close(); err != nil {
		return summary{}, errors.New("Unable to close local file")
	}
	if err = transfer.Publish(file.Name(), target, overwrite); err != nil {
		return summary{}, errors.New("Unable to publish local file; destination may already exist or be in use")
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
		return fail(errors.New("Usage: remote-mcp-transfer upload|download [options] source destination"))
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	// 解析错误可能回显原始参数；帮助仅显示固定默认值，不展示环境中的 URL。
	flags.SetOutput(io.Discard)
	endpoint := flags.String("url", "", "MCP HTTP(S) URL; may also be set with REMOTE_MCP_URL")
	tokenFile := flags.String("token-file", "", "Optional token file; otherwise reads REMOTE_MCP_TOKEN; sends no credentials when unset")
	overwrite := flags.Bool("overwrite", false, "Allow replacing the destination file")
	caFile := flags.String("ca-file", "", "Additional trusted PEM CA certificate")
	timeout := flags.Duration("timeout", 30*time.Minute, "Maximum duration of the entire transfer")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: remote-mcp-transfer upload|download [options] source destination")
		flags.SetOutput(stderr)
		flags.PrintDefaults()
		flags.SetOutput(io.Discard)
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return fail(errors.New("Invalid command arguments"))
	}
	if *endpoint == "" {
		*endpoint = os.Getenv("REMOTE_MCP_URL")
	}
	if flags.NArg() != 2 || *timeout <= 0 {
		return fail(errors.New("Source path, destination path, and a positive timeout are required"))
	}
	tokenFileSet := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "token-file" {
			tokenFileSet = true
		}
	})
	if tokenFileSet && *tokenFile == "" {
		return fail(errors.New("Token file path must not be empty"))
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
		return fail(errors.New("MCP connection failed; check URL, token, certificate, and network"))
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
