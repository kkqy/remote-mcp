package config

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestToken(t *testing.T) {
	t.Setenv("REMOTE_MCP_TOKEN", "fictional-test-secret")
	got, err := LoadToken("")
	if err != nil || got != "fictional-test-secret" {
		t.Fatal(got, err)
	}
	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte("file-test-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err = LoadToken(file)
	if err != nil || got != "file-test-token" {
		t.Fatal(got, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(file, 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadToken(file); err == nil {
			t.Fatal("应拒绝开放权限的凭据文件")
		}
	}
	for _, bad := range []string{"a\nb", "a b", "中文"} {
		if err := ValidateToken(bad); err == nil {
			t.Fatalf("应拒绝不合法Token")
		}
	}
}

func TestParseAndValidation(t *testing.T) {
	t.Setenv("REMOTE_MCP_TOKEN", "fictional-test-secret")
	var diagnostic bytes.Buffer
	c, err := Parse(nil, &diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "0.0.0.0:8080" || c.MaxBodyBytes != 1<<20 {
		t.Fatal(c.Listen, c.MaxBodyBytes)
	}
	if _, err := Parse([]string{"--max-body-bytes", "fictional-test-secret"}, &diagnostic); err == nil || strings.Contains(err.Error(), "fictional-test-secret") || strings.Contains(diagnostic.String(), "fictional-test-secret") {
		t.Fatal("解析错误未脱敏")
	}
	c.MaxBodyBytes = 1
	if c.Validate() == nil {
		t.Fatal("请求体与块限制应兼容")
	}
	c = Default()
	c.Token = "test"
	c.AllowedOrigins = []string{"https://agent.example/path"}
	if c.Validate() == nil {
		t.Fatal("错误Origin应拒绝")
	}
}

func TestOptionalToken(t *testing.T) {
	for _, value := range []string{"", "fictional-token", " ", "a\nb", "中文", strings.Repeat("a", 8193)} {
		t.Run(value[:min(len(value), 20)], func(t *testing.T) {
			t.Setenv("REMOTE_MCP_TOKEN", value)
			c, err := Parse(nil, &bytes.Buffer{})
			valid := value == "" || value == "fictional-token"
			if valid && (err != nil || c.Token != value) || !valid && err == nil {
				t.Fatal("可选环境 Token 校验结果不符")
			}
		})
	}
	t.Setenv("REMOTE_MCP_TOKEN", "")
	if err := os.Unsetenv("REMOTE_MCP_TOKEN"); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(nil, &bytes.Buffer{}); err != nil {
		t.Fatal("未设置环境变量应允许启动", err)
	}
	t.Setenv("REMOTE_MCP_TOKEN", "fictional-token")
	file := filepath.Join(t.TempDir(), "token")
	for _, contents := range []string{"", " \n\t", "bad token", "中文"} {
		if err := os.WriteFile(file, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Parse([]string{"--token-file", file}, &bytes.Buffer{}); err == nil {
			t.Fatal("显式无效文件不能退回环境凭据或匿名")
		}
	}
	for _, path := range []string{"", filepath.Join(t.TempDir(), "missing"), t.TempDir()} {
		if _, err := Parse([]string{"--token-file", path}, &bytes.Buffer{}); err == nil {
			t.Fatal("无效凭据文件路径应报错")
		}
	}
}

func TestForwardingLimits(t *testing.T) {
	t.Setenv("REMOTE_MCP_TOKEN", "")
	for _, name := range []string{"max-forwards", "max-forward-connections", "max-forward-records", "forward-dial-timeout", "forward-retention", "forward-sweep-interval"} {
		if _, err := Parse([]string{"--" + name, "0"}, io.Discard); err == nil {
			t.Fatalf("%s 未拒绝零值", name)
		}
	}
}
