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
		var c ClientConfig
		if err := dec.Decode(&c); err != nil {
			t.Fatal(err)
		}
		entry := c.Servers["remote-mcp"]
		if entry.URL != want || entry.Type != "http" || entry.Headers["Authorization"] != "Bearer "+token {
			t.Fatalf("错误配置: %#v", c)
		}
	}
	if err := dec.Decode(new(ClientConfig)); !errors.Is(err, io.EOF) {
		t.Fatal("存在多余配置")
	}
	if strings.Contains(diag.String(), token) {
		t.Fatal("日志泄露Token")
	}
	out.Reset()
	if err := WriteStartup(&out, &diag, &net.TCPAddr{IP: net.ParseIP("2001:db8::2"), Port: 443}, true, token, enum); err != nil {
		t.Fatal(err)
	}
	var c ClientConfig
	if err := json.Unmarshal(out.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	if c.Servers["remote-mcp"].URL != "https://[2001:db8::2]:443/mcp" {
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
