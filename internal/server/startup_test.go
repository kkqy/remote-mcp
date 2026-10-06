package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

func TestStartupCandidates(t *testing.T) {
	parse := netip.MustParseAddr
	interfaces := []Interface{{true, []netip.Addr{parse("192.168.2.4"), parse("10.8.0.2"), parse("192.168.2.4"), parse("127.0.0.1"), parse("0.0.0.0"), parse("224.0.0.1"), parse("2001:db8::2"), parse("fe80::1")}}, {false, []netip.Addr{parse("10.0.0.8")}}}
	enum := func() ([]Interface, error) { return interfaces, nil }
	for _, tt := range []struct {
		bind string
		want []netip.Addr
	}{{"0.0.0.0", []netip.Addr{parse("10.8.0.2"), parse("192.168.2.4")}}, {"::", []netip.Addr{parse("2001:db8::2")}}, {"127.0.0.1", []netip.Addr{parse("127.0.0.1")}}, {"192.168.2.4", []netip.Addr{parse("192.168.2.4")}}} {
		t.Run(tt.bind, func(t *testing.T) {
			got, err := CandidateAddresses(&net.TCPAddr{IP: net.ParseIP(tt.bind), Port: 1234}, enum)
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("地址不符: %v %v", got, err)
			}
		})
	}
	var out, diag bytes.Buffer
	token := "test-quote-\"-slash-\\"
	if err := WriteStartup(&out, &diag, &net.TCPAddr{IP: net.IPv4zero, Port: 1234}, false, token, enum); err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(&out)
	for _, want := range []string{"http://10.8.0.2:1234/mcp", "http://192.168.2.4:1234/mcp"} {
		entry := decodeOpenCodeEntry(t, dec)
		headers, ok := entry["headers"].(map[string]any)
		if entry["url"] != want || !ok || headers["Authorization"] != "Bearer "+token {
			t.Fatalf("错误配置: %#v", entry)
		}
	}
	if err := dec.Decode(new(map[string]any)); !errors.Is(err, io.EOF) {
		t.Fatal("存在多余配置")
	}
	if strings.Contains(diag.String(), token) {
		t.Fatal("日志泄露Token")
	}
	out.Reset()
	if err := WriteStartup(&out, &diag, &net.TCPAddr{IP: net.ParseIP("2001:db8::2"), Port: 443}, true, token, enum); err != nil {
		t.Fatal(err)
	}
	entry := decodeOpenCodeEntry(t, json.NewDecoder(&out))
	if entry["url"] != "https://[2001:db8::2]:443/mcp" {
		t.Fatal(out.String())
	}
}

func TestStartupEnumerationFailure(t *testing.T) {
	var out, diag bytes.Buffer
	err := WriteStartup(&out, &diag, &net.TCPAddr{IP: net.IPv4zero}, false, "test", func() ([]Interface, error) { return nil, errors.New("内部枚举错误") })
	if err != nil || out.Len() != 0 || diag.Len() == 0 {
		t.Fatal("枚举失败行为错误")
	}
}

func TestExplicitUnusableAddressDoesNotProduceConfig(t *testing.T) {
	for _, address := range []string{"fe80::1", "ff02::1", "224.0.0.1"} {
		t.Run(address, func(t *testing.T) {
			var out, diag bytes.Buffer
			bound := &net.TCPAddr{IP: net.ParseIP(address), Port: 8080}
			if strings.Contains(address, ":") {
				bound.Zone = "server-interface"
			}
			err := WriteStartup(&out, &diag, bound, false, "test", func() ([]Interface, error) {
				t.Fatal("单地址不应枚举网卡")
				return nil, nil
			})
			if err != nil || out.Len() != 0 || diag.Len() == 0 {
				t.Fatal("不可展示的单地址不应生成配置，应提供诊断")
			}
		})
	}
}

func TestAnonymousStartupConfigurations(t *testing.T) {
	var out, diag bytes.Buffer
	enum := func() ([]Interface, error) {
		return []Interface{{Up: true, Addresses: []netip.Addr{netip.MustParseAddr("192.168.1.10"), netip.MustParseAddr("10.8.0.2")}}}, nil
	}
	if err := WriteStartup(&out, &diag, &net.TCPAddr{IP: net.IPv4zero, Port: 8080}, false, "", enum); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&out)
	for _, want := range []string{"http://10.8.0.2:8080/mcp", "http://192.168.1.10:8080/mcp"} {
		entry := decodeOpenCodeEntry(t, decoder)
		if _, exists := entry["headers"]; exists {
			t.Fatal("匿名配置必须完全省略 headers")
		}
		if entry["url"] != want {
			t.Fatal(entry)
		}
	}
	if err := decoder.Decode(new(map[string]any)); !errors.Is(err, io.EOF) {
		t.Fatal("配置数量错误")
	}
	if !strings.Contains(diag.String(), "anonymous access is allowed") || strings.Contains(diag.String(), "contains the current token") {
		t.Fatal(diag.String())
	}
}

// 独立解码外部配置契约，避免使用生产结构体掩盖 JSON 字段拼写错误。
func decodeOpenCodeEntry(t *testing.T, decoder *json.Decoder) map[string]any {
	t.Helper()
	var conf map[string]any
	if err := decoder.Decode(&conf); err != nil {
		t.Fatal(err)
	}
	if _, exists := conf["mcpServers"]; exists {
		t.Fatal("不应输出旧的 mcpServers 配置")
	}
	if conf["$schema"] != "https://opencode.ai/config.json" {
		t.Fatal("OpenCode 配置 schema 错误")
	}
	servers, ok := conf["mcp"].(map[string]any)
	if !ok || len(servers) != 1 {
		t.Fatal("配置应在 mcp 下提供一个服务")
	}
	entry, ok := servers["remote-mcp"].(map[string]any)
	if !ok || entry["type"] != "remote" || entry["enabled"] != true {
		t.Fatal("必须启用 remote 类型的服务")
	}
	if oauth, exists := entry["oauth"]; !exists || oauth != false {
		t.Fatal("必须显式设置 oauth 为 false")
	}
	return entry
}
