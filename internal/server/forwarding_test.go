package server

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"remote-mcp/internal/config"
	"strings"
	"testing"
	"time"
)

func TestForwardingProtocol(t *testing.T) {
	for _, token := range []string{"", testToken} {
		t.Run("token="+token, func(t *testing.T) {
			c := config.Default()
			c.Token = token
			c.Listen = "127.0.0.1:0"
			app, e := New(c, io.Discard)
			if e != nil {
				t.Fatal(e)
			}
			defer app.Close()
			ts := httptest.NewServer(app.Handler)
			defer ts.Close()
			client := ts.Client()
			client.Timeout = 5 * time.Second
			initSession := func() string {
				r, _ := rpcWithToken(t, client, ts.URL+"/mcp", "", initialize, token)
				sid := r.Header.Get("Mcp-Session-Id")
				rpcWithToken(t, client, ts.URL+"/mcp", sid, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, token)
				return sid
			}
			sid := initSession()
			_, body := rpcWithToken(t, client, ts.URL+"/mcp", sid, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, token)
			names := map[string]bool{}
			for _, v := range body["result"].(map[string]any)["tools"].([]any) {
				names[v.(map[string]any)["name"].(string)] = true
			}
			for _, name := range []string{"create", "list", "status", "stop"} {
				if !names["port_forward_"+name] {
					t.Fatal(name)
				}
			}
			call := func(name string, args map[string]any) map[string]any {
				p, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "port_forward_" + name, "arguments": args}})
				_, b := rpcWithToken(t, client, ts.URL+"/mcp", sid, string(p), token)
				return b["result"].(map[string]any)
			}
			l, e := net.Listen("tcp", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			defer l.Close()
			go func() {
				c, e := l.Accept()
				if e == nil {
					defer c.Close()
					c.SetDeadline(time.Now().Add(3 * time.Second))
					io.Copy(c, c)
				}
			}()
			args := map[string]any{"request_id": "rpc", "target_host": "127.0.0.1", "target_port": l.Addr().(*net.TCPAddr).Port}
			r := call("create", args)
			if r["isError"] == true {
				t.Fatal(r)
			}
			s := r["structuredContent"].(map[string]any)
			id := map[string]any{"id": s["id"]}
			addr := s["listen_address"].(string)
			if !strings.HasPrefix(addr, "127.0.0.1:") {
				t.Fatal(addr)
			}
			if token != "" {
				for _, bad := range []string{"", "wrong"} {
					req, _ := http.NewRequest("POST", ts.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"port_forward_stop","arguments":{}}}`))
					if bad != "" {
						req.Header.Set("Authorization", "Bearer "+bad)
					}
					res, e := client.Do(req)
					if e != nil {
						t.Fatal(e)
					}
					res.Body.Close()
					if res.StatusCode != 401 {
						t.Fatal(res.StatusCode)
					}
				}
			}
			// 主动结束协议会话，再用新会话访问同一资源。
			req, _ := http.NewRequest("DELETE", ts.URL+"/mcp", nil)
			req.Header.Set("Mcp-Session-Id", sid)
			req.Header.Set("MCP-Protocol-Version", ProtocolVersion)
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			res, e := client.Do(req)
			if e != nil {
				t.Fatal(e)
			}
			res.Body.Close()
			if res.StatusCode != http.StatusNoContent {
				t.Fatal(res.StatusCode)
			}
			sid = initSession()
			conn, e := net.Dial("tcp", addr)
			if e != nil {
				t.Fatal(e)
			}
			conn.SetDeadline(time.Now().Add(time.Second))
			conn.Write([]byte{0, 255, 42})
			b := make([]byte, 3)
			_, e = io.ReadFull(conn, b)
			conn.Close()
			if e != nil || string(b) != string([]byte{0, 255, 42}) {
				t.Fatal(b, e)
			}
			if call("status", id)["structuredContent"].(map[string]any)["id"] != s["id"] {
				t.Fatal("查询错误")
			}
			if len(call("list", map[string]any{})["structuredContent"].(map[string]any)["forwards"].([]any)) != 1 {
				t.Fatal("列表错误")
			}
			args["target_port"] = 0
			if call("create", args)["isError"] != true {
				t.Fatal("业务失败未标记")
			}
			if call("stop", id)["structuredContent"].(map[string]any)["state"] != "stopped" {
				t.Fatal("未停止")
			}
			call("stop", id)
			bound, e := net.Listen("tcp", addr)
			if e != nil {
				t.Fatal(e)
			}
			bound.Close()
		})
	}
}
