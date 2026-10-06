package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"remote-mcp/internal/config"
	"remote-mcp/internal/gui"
)

type protocolGUIBackend struct{ desktop *protocolGUIDesktop }

func (b protocolGUIBackend) Probe(context.Context) (gui.Status, error) {
	return gui.Status{Backend: "protocol-fixture", State: "available", Capabilities: b.desktop.Capabilities()}, nil
}
func (b protocolGUIBackend) Open(context.Context) (gui.Desktop, error) { return b.desktop, nil }

type protocolGUIDesktop struct{ closes atomic.Int32 }

func (*protocolGUIDesktop) Capabilities() gui.Capabilities {
	return gui.Capabilities{Screenshot: true, Mouse: true, Keyboard: true, Text: true, DirectText: true}
}
func (*protocolGUIDesktop) Displays(context.Context) ([]gui.Display, error) {
	return []gui.Display{{ID: "fixture-display", Primary: true, PixelWidth: 8, PixelHeight: 6, LogicalBounds: gui.Bounds{X: -4, Y: 3, Width: 4, Height: 3}, AbsoluteInput: true}}, nil
}
func (*protocolGUIDesktop) Capture(context.Context, gui.Display) (gui.Frame, error) {
	frame := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for y := range 6 {
		for x := range 8 {
			frame.SetRGBA(x, y, color.RGBA{R: uint8(x * 20), G: uint8(y * 20), B: 7, A: 255})
		}
	}
	return gui.Frame{Image: frame, CapturedAt: time.Now(), Freshness: "new_frame"}, nil
}
func (*protocolGUIDesktop) Mouse(context.Context, gui.MouseEvent) error { return nil }
func (*protocolGUIDesktop) Key(context.Context, []string) error         { return nil }
func (*protocolGUIDesktop) Text(context.Context, gui.TextInput) (gui.InputResult, error) {
	return gui.InputResult{Submitted: true, Mode: "direct"}, nil
}
func (d *protocolGUIDesktop) Close() error { d.closes.Add(1); return nil }

// 本测试验证传输和清理契约；模拟后端不能代替真实桌面验收。
func TestGUIIndependentHTTP(t *testing.T) {
	for _, token := range []string{"", testToken} {
		t.Run(map[bool]string{true: "token", false: "anonymous"}[token != ""], func(t *testing.T) {
			c := config.Default()
			c.Token = token
			var logs bytes.Buffer
			app, err := New(c, &logs)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { app.Close() })
			if err := closeGUI(app.gui); err != nil {
				t.Fatal(err)
			}
			desktop := &protocolGUIDesktop{}
			app.gui, err = gui.NewWithBackend(c.GUI, protocolGUIBackend{desktop})
			if err != nil {
				t.Fatal(err)
			}
			app.gui.Register(app.MCP)
			ts := httptest.NewServer(app.Handler)
			defer ts.Close()
			client := ts.Client()
			client.Timeout = 5 * time.Second
			response, _ := rpcWithToken(t, client, ts.URL+"/mcp", "", initialize, token)
			session := response.Header.Get("Mcp-Session-Id")
			if response.StatusCode != 200 || session == "" {
				t.Fatal("GUI 服务未成功初始化")
			}
			rpcWithToken(t, client, ts.URL+"/mcp", session, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, token)
			_, listed := rpcWithToken(t, client, ts.URL+"/mcp", session, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, token)
			names := map[string]bool{}
			for _, item := range listed["result"].(map[string]any)["tools"].([]any) {
				tool := item.(map[string]any)
				name := tool["name"].(string)
				names[name] = true
				if name == "gui_text" && (!strings.Contains(tool["description"].(string), "native Unicode") || !strings.Contains(tool["description"].(string), "clipboard mode is unsupported")) {
					t.Fatal("独立协议发现中的文本说明不符合 Windows 契约")
				}
			}
			for _, name := range []string{"gui_status", "gui_open", "gui_close", "gui_screenshot", "gui_mouse", "gui_key", "gui_text", "file_stat", "process_start", "terminal_open", "port_forward_create"} {
				if !names[name] {
					t.Fatal("工具未注册", name)
				}
			}
			call := func(name string, arguments map[string]any) map[string]any {
				t.Helper()
				request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": name, "arguments": arguments}})
				_, envelope := rpcWithToken(t, client, ts.URL+"/mcp", session, string(request), token)
				if envelope["error"] != nil {
					t.Fatal("协议调用失败", envelope)
				}
				return envelope["result"].(map[string]any)
			}
			opened := call("gui_open", map[string]any{"request_id": "protocol-gui", "wait_ms": 1000})["structuredContent"].(map[string]any)
			id := opened["id"].(string)
			shot := call("gui_screenshot", map[string]any{"id": id, "display_id": "fixture-display", "region": map[string]any{"x": 2, "y": 1, "width": 3, "height": 2}})
			if shot["isError"] == true {
				t.Fatal("截图工具失败", shot)
			}
			metadata := shot["structuredContent"].(map[string]any)
			if metadata["width"] != float64(3) || metadata["height"] != float64(2) || metadata["display_id"] != "fixture-display" || metadata["capture_id"] == "" || metadata["layout_generation"] == nil || metadata["captured_at"] == nil || metadata["captured_at_source"] != "acquired_at" || metadata["frame_sequence"] == nil || metadata["freshness"] == nil {
				t.Fatal("截图结构化元数据缺失", metadata)
			}
			region := metadata["region"].(map[string]any)
			if region["x"] != float64(2) || region["y"] != float64(1) {
				t.Fatal("区域截图丢失偏移", region)
			}
			images := 0
			for _, item := range shot["content"].([]any) {
				block := item.(map[string]any)
				if block["type"] != "image" {
					continue
				}
				images++
				if block["mimeType"] != "image/png" {
					t.Fatal("标准图片 MIME 不符")
				}
				data, err := base64.StdEncoding.DecodeString(block["data"].(string))
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := png.Decode(bytes.NewReader(data))
				if err != nil || decoded.Bounds() != image.Rect(0, 0, 3, 2) {
					t.Fatal("PNG 无法解码或尺寸不符", err)
				}
				r, g, b, _ := decoded.At(0, 0).RGBA()
				if r != 40*257 || g != 20*257 || b != 7*257 {
					t.Fatal("截图裁剪内容错误")
				}
			}
			if images != 1 {
				t.Fatal("未返回唯一标准图片块")
			}
			const sensitiveText = "测试中文不应出现在普通日志中"
			for range 5 {
				input := call("gui_text", map[string]any{"id": id, "text": sensitiveText})
				if input["isError"] == true || input["structuredContent"].(map[string]any)["submitted"] != true {
					t.Fatal("连续成功输入被误报失败", input)
				}
				key := call("gui_key", map[string]any{"id": id, "keys": []string{"Ctrl", "A"}})
				if key["isError"] == true || key["structuredContent"].(map[string]any)["submitted"] != true {
					t.Fatal("连续成功按键被误报失败", key)
				}
			}
			failed := call("gui_mouse", map[string]any{"id": id, "capture_id": "invalid-capture", "action": "click", "x": 0, "y": 0})
			if failed["isError"] != true || failed["structuredContent"].(map[string]any)["code"] != "stale_capture" {
				t.Fatal("失效截图应为稳定工具失败", failed)
			}
			call("file_stat", map[string]any{"path": t.TempDir()})
			if err := app.Close(); err != nil {
				t.Fatal(err)
			}
			if desktop.closes.Load() != 1 {
				t.Fatal("服务关闭没有释放图形桌面")
			}
			if strings.Contains(logs.String(), sensitiveText) || token != "" && strings.Contains(logs.String(), token) {
				t.Fatal("普通日志泄漏输入或凭据")
			}
			if !strings.Contains(logs.String(), "gui_screenshot") {
				t.Fatal("缺少 GUI 调用元数据日志")
			}
		})
	}
}

func TestServerCreationDoesNotRequireDesktop(t *testing.T) {
	app, err := New(config.Default(), io.Discard)
	if err != nil {
		t.Fatal("无桌面不应阻止服务创建", err)
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPlatformGUIDiscovery(t *testing.T) {
	for _, token := range []string{"", testToken} {
		t.Run(map[bool]string{true: "token", false: "anonymous"}[token != ""], func(t *testing.T) {
			c := config.Default()
			c.Token = token
			if !gui.Supported {
				// 无 GUI 的产品启动不能被未使用的 GUI 配置或桌面环境阻止。
				c.GUI = gui.Config{}
				t.Setenv("DISPLAY", "unreachable-test-display")
				t.Setenv("WAYLAND_DISPLAY", "unreachable-test-wayland")
				t.Setenv("DBUS_SESSION_BUS_ADDRESS", "invalid-test-bus")
			}
			var logs bytes.Buffer
			app, err := New(c, &logs)
			if err != nil {
				t.Fatal(err)
			}
			defer app.Close()
			if (app.gui != nil) != gui.Supported {
				t.Fatal("服务创建了错误平台的 GUI 管理器")
			}
			ts := httptest.NewServer(app.Handler)
			defer ts.Close()
			client := ts.Client()
			client.Timeout = 5 * time.Second
			response, _ := rpcWithToken(t, client, ts.URL+"/mcp", "", initialize, token)
			session := response.Header.Get("Mcp-Session-Id")
			rpcWithToken(t, client, ts.URL+"/mcp", session, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, token)
			_, envelope := rpcWithToken(t, client, ts.URL+"/mcp", session, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, token)
			tools := envelope["result"].(map[string]any)["tools"].([]any)
			wantCount := 33
			if gui.Supported {
				wantCount += 7
			}
			if len(tools) != wantCount {
				t.Fatalf("产品工具数不符：got=%d want=%d", len(tools), wantCount)
			}
			names := map[string]bool{}
			for _, item := range tools {
				tool := item.(map[string]any)
				name := tool["name"].(string)
				names[name] = true
				if !gui.Supported && strings.HasPrefix(name, "gui_") {
					t.Fatal("非 Windows 工具发现包含 GUI", name)
				}
				if name == "gui_text" && (!strings.Contains(tool["description"].(string), "native Unicode") || !strings.Contains(tool["description"].(string), "clipboard mode is unsupported")) {
					t.Fatal("Windows 文本工具说明必须匹配直接输入与不支持剪贴板的契约")
				}
				if name == "gui_open" && strings.Contains(tool["description"].(string), "Wayland") {
					t.Fatal("Windows 工具说明残留 Linux 授权语义")
				}
			}
			for _, name := range []string{"gui_status", "gui_open", "gui_close", "gui_screenshot", "gui_mouse", "gui_key", "gui_text"} {
				if names[name] != gui.Supported {
					t.Fatal("GUI 工具发现与平台条件不符", name)
				}
				if !gui.Supported {
					request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": name, "arguments": map[string]any{}}})
					_, call := rpcWithToken(t, client, ts.URL+"/mcp", session, string(request), token)
					if call["error"] == nil {
						t.Fatal("移除的 GUI 工具必须走 SDK 未注册协议错误", name, call)
					}
				}
			}
			if !gui.Supported && (!strings.Contains(logs.String(), "error_code=unknown_tool") || !strings.Contains(logs.String(), "Requested tool is not registered")) {
				t.Fatal("缺少移除 GUI 的明确受控日志诊断")
			}
			if err := app.Close(); err != nil {
				t.Fatal("无 GUI 管理器关闭失败", err)
			}
			if err := app.Close(); err != nil {
				t.Fatal("重复关闭失败", err)
			}
		})
	}
}
