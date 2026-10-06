//go:build linux

package gui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

func TestPortalStreamsLogicalGeometry(t *testing.T) {
	var streams []portalStream
	wire := [][]any{{uint32(91), map[string]dbus.Variant{"size": dbus.MakeVariant([]any{int32(1280), int32(720)}), "position": dbus.MakeVariant([]any{int32(-1280), int32(40)})}}}
	if err := dbus.Store([]any{wire}, &streams); err != nil {
		t.Fatal(err)
	}
	display := displayFromPortal(streams[0], true)
	if display.ID != "91" || display.LogicalBounds != (Bounds{-1280, 40, 1280, 720}) || !display.AbsoluteInput {
		t.Fatalf("Portal tuple解析错误: %+v", display)
	}
	display = displayFromPortal(portalStream{Node: 1, Props: map[string]dbus.Variant{}}, true)
	if display.AbsoluteInput || display.PixelWidth != 0 {
		t.Fatal("缺少可靠映射时不能猜测1:1")
	}
}
func TestX11PixelDecodeEndianStrideAndMasks(t *testing.T) {
	for _, order := range []byte{xproto.ImageOrderLSBFirst, xproto.ImageOrderMSBFirst} {
		data := []byte{0x00, 0x00, 0xff, 0x00, 0x00, 0xff, 0x00, 0x00}
		if order == xproto.ImageOrderMSBFirst {
			data = []byte{0x00, 0xff, 0x00, 0x00, 0x00, 0x00, 0xff, 0x00}
		}
		setup := &xproto.SetupInfo{ImageByteOrder: order, PixmapFormats: []xproto.Format{{Depth: 24, BitsPerPixel: 32, ScanlinePad: 32}}}
		screen := &xproto.ScreenInfo{AllowedDepths: []xproto.DepthInfo{{Visuals: []xproto.VisualInfo{{VisualId: 7, RedMask: 0xff0000, GreenMask: 0x00ff00, BlueMask: 0x0000ff}}}}}
		img, err := x11Decode(&xproto.GetImageReply{Depth: 24, Visual: 7, Data: data}, setup, screen, 2, 1)
		if err != nil {
			t.Fatal(err)
		}
		if img.At(0, 0) != (color.RGBA{255, 0, 0, 255}) || img.At(1, 0) != (color.RGBA{0, 255, 0, 255}) {
			t.Fatalf("X11像素顺序错误: %v %v", img.At(0, 0), img.At(1, 0))
		}
		_, err = x11Decode(&xproto.GetImageReply{Depth: 24, Visual: 7, Data: data[:1]}, setup, screen, 2, 1)
		assertCode(t, err, "capture_failed")
	}
}
func TestFDReadLimitAndCancel(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	_, _ = write.Write([]byte("12345"))
	_, err = fdRead(context.Background(), read, 4)
	assertCode(t, err, "limit_exceeded")
	read2, write2, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read2.Close()
	defer write2.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = fdRead(ctx, read2, 100)
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("FD等待未响应取消: %v", err)
	}
}
func TestFDWriteFullPipeCancel(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err = fdWrite(ctx, write, bytes.Repeat([]byte{'x'}, 1<<20))
	if err == nil {
		t.Fatal("不读取的目标应在有限时间内取消")
	}
}
func fakeGStreamer(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gst-launch-1.0"), []byte("#!/bin/sh\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return dir
}
func TestGStreamerExplicitFDPNGAndByteBounds(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 5, 3))
	img.SetRGBA(0, 0, color.RGBA{12, 34, 56, 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	dir := fakeGStreamer(t, "test \"$1\" = '-q' || exit 2\ntest \"$3\" = 'fd=3' || exit 3\ncat <&3 >/dev/null\ncat \"$GUI_TEST_PNG\"\n")
	fixture := filepath.Join(dir, "fixture.png")
	if err := os.WriteFile(fixture, encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GUI_TEST_PNG", fixture)
	fd, err := os.Open(fixture)
	if err != nil {
		t.Fatal(err)
	}
	defer fd.Close()
	frame, err := runGStreamer(context.Background(), fd, 71, 4096, 20)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Image.Bounds().Dx() != 5 || frame.Freshness != "latest_available" || frame.CapturedAt.IsZero() {
		t.Fatal("没有准确返回帧元数据")
	}
	_, err = runGStreamer(context.Background(), fd, 71, 5, 20)
	assertCode(t, err, "limit_exceeded")
	_, err = runGStreamer(context.Background(), fd, 71, 4096, 10)
	assertCode(t, err, "limit_exceeded")
}
func TestGStreamerTimeoutReapsProcessGroup(t *testing.T) {
	fakeGStreamer(t, "sleep 10 &\nwait\n")
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = runGStreamer(ctx, read, 1, 1024, 100)
	assertCode(t, err, "timeout")
	if time.Since(start) > 2*time.Second {
		t.Fatal("采集辅助进程未及时回收")
	}
}
func TestPortalClipboardUnknownStateDoesNotReplace(t *testing.T) {
	d := &waylandDesktop{cfg: DefaultConfig()}
	clip := newPortalClipboard(d)
	_, _, err := clip.snapshot(context.Background())
	assertCode(t, err, "clipboard_preservation_unavailable")
	out, err := clip.text(context.Background(), TextInput{Text: "中文", PasteKeys: []string{"Ctrl", "V"}})
	assertCode(t, err, "clipboard_preservation_unavailable")
	if out.Submitted || out.ClipboardReplaced || clip.data != nil {
		t.Fatal("无法保存剪贴板时执行了替换")
	}
}

func TestMalformedPortalTuplesAreRejected(t *testing.T) {
	for _, v := range []dbus.Variant{{}, dbus.MakeVariant("invalid"), dbus.MakeVariant([]int32{1}), dbus.MakeVariant([]any{dbus.MakeVariant("x"), dbus.MakeVariant("y")}), dbus.MakeVariant([]uint32{1, 2})} {
		if _, _, ok := tupleInts(v); ok {
			t.Fatalf("错误接受不可靠坐标元数据: %v", v)
		}
	}
}

func TestX11DisplayAddressAndAuthenticationHandshake(t *testing.T) {
	for _, test := range []struct {
		display, network, address, number string
		screen                            int
	}{{":3.1", "unix", "/tmp/.X11-unix/X3", "3", 1}, {"unix/:0", "unix", "/tmp/.X11-unix/X0", "0", 0}, {"localhost:10.0", "tcp", "localhost:6010", "10", 0}, {"/tmp/custom:2", "unix", "/tmp/custom:2", "2", 0}} {
		network, address, _, number, screen, err := x11Address(test.display)
		if err != nil || network != test.network || address != test.address || number != test.number || screen != test.screen {
			t.Fatalf("显示器地址解析错误: %s %s %s %d %v", network, address, number, screen, err)
		}
	}
	cookie := bytes.Repeat([]byte{'x'}, 16)
	packet := x11Handshake(cookie)
	if packet[0] != 'l' || len(packet) != 48 || string(packet[12:30]) != "MIT-MAGIC-COOKIE-1" || !bytes.Equal(packet[32:], cookie) {
		t.Fatal("认证报文未对齐或未使用正确cookie")
	}
}

func TestX11HandshakeCancellationClosesUnderlyingTransport(t *testing.T) {
	if os.Getenv("REMOTE_MCP_GUI_PORTAL_TEST") != "1" {
		t.Skip("使用隔离socket回归需设置 REMOTE_MCP_GUI_PORTAL_TEST=1")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	if port < 6000 {
		t.Skip("临时端口低于X11基准端口")
	}
	authority := filepath.Join(t.TempDir(), "authority")
	if err := os.WriteFile(authority, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XAUTHORITY", authority)
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	conn, raw, err := x11ConnectTransport(ctx, fmt.Sprintf("127.0.0.1:%d", port-6000))
	if err == nil {
		conn.Close()
		_ = raw.Close()
		t.Fatal("停滞服务器不应完成认证")
	}
	if time.Since(started) > time.Second {
		t.Fatal("X11认证未响应取消")
	}
	select {
	case server := <-accepted:
		defer server.Close()
		_ = server.SetReadDeadline(time.Now().Add(time.Second))
		_, err = io.Copy(io.Discard, server)
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatal("客户端取消未关闭transport")
		}
	case <-time.After(time.Second):
		t.Fatal("未连接到隔离服务器")
	}
}

func TestMalformedX11SetupDoesNotPanic(t *testing.T) {
	if _, err := safeX11Setup(&xgb.Conn{SetupBytes: []byte{1}}); err == nil {
		t.Fatal("错误接受过短setup")
	}
	malformed := make([]byte, 40)
	malformed[28] = 1
	if _, err := safeX11Setup(&xgb.Conn{SetupBytes: malformed}); err == nil {
		t.Fatal("错误接受不存在的root记录")
	}
	client, server := net.Pipe()
	defer server.Close()
	go func() {
		header := make([]byte, 12)
		_, _ = io.ReadFull(server, header)
		reply := make([]byte, 8)
		reply[0] = 1
		reply[2] = 11
		_, _ = server.Write(reply)
	}()
	conn, err := safeX11Connection(&x11AuthConn{Conn: client, auth: x11Handshake(nil)})
	if conn != nil || err == nil {
		t.Fatal("异常依赖握手parser未被隔离")
	}
}

func TestLogicalLayoutSignalInvalidatesSamePixelFrame(t *testing.T) {
	d := &waylandDesktop{monitorEndpoint: kdeLayout, caps: Capabilities{Reasons: map[string]string{}}, displays: []Display{{ID: "node", PixelWidth: 2880, PixelHeight: 1800, LogicalBounds: Bounds{Width: 1800, Height: 1125}, AbsoluteInput: true}}}
	if !d.layoutSignal(&dbus.Signal{Path: kdeLayout.path, Name: kdeLayout.iface + "." + kdeLayout.signal, Body: []any{map[string]dbus.Variant{"changed": dbus.MakeVariant(true)}}}) {
		t.Fatal("未识别逻辑布局变化")
	}
	if d.displays[0].PixelWidth != 2880 || d.displays[0].AbsoluteInput || !d.layoutInvalidated {
		t.Fatal("像素尺寸相同的布局变化仍允许旧映射")
	}
	d.displays[0].AbsoluteInput = true
	d.layoutInvalidated = false
	d.layoutSignal(&dbus.Signal{Name: "org.freedesktop.DBus.NameOwnerChanged", Body: []any{kdeLayout.service, ":1.1", ""}})
	if d.displays[0].AbsoluteInput {
		t.Fatal("监视服务消失仍允许绝对输入")
	}
}
func TestMalformedLayoutSignalsDoNotPanicOrInvalidate(t *testing.T) {
	d := &waylandDesktop{monitorEndpoint: kdeLayout, caps: Capabilities{Reasons: map[string]string{}}, displays: []Display{{AbsoluteInput: true}}}
	for _, body := range [][]any{nil, {nil}, {"unexpected"}} {
		d.layoutSignal(&dbus.Signal{Path: kdeLayout.path, Name: kdeLayout.iface + "." + kdeLayout.signal, Body: body})
	}
	for _, body := range [][]any{nil, {nil, nil, nil}, {kdeLayout.service, 1, 2}} {
		d.layoutSignal(&dbus.Signal{Name: "org.freedesktop.DBus.NameOwnerChanged", Body: body})
	}
	if !d.displays[0].AbsoluteInput || d.layoutInvalidated {
		t.Fatal("错误接受畸形布局信号")
	}
}

func TestMalformedClipboardOwnerInvalidatesSavedProvider(t *testing.T) {
	path := dbus.ObjectPath("/session")
	for _, body := range [][]any{
		{path}, {path, nil}, {path, map[string]dbus.Variant{}},
		{path, map[string]dbus.Variant{"mime_types": dbus.MakeVariant("text/plain"), "session_is_owner": dbus.MakeVariant(false)}},
		{path, map[string]dbus.Variant{"mime_types": dbus.MakeVariant([]string{"text/plain"})}},
		{path, map[string]dbus.Variant{"mime_types": dbus.MakeVariant([]string{}), "session_is_owner": dbus.MakeVariant("false")}},
	} {
		c := newPortalClipboard(&waylandDesktop{path: path})
		c.known, c.owner, c.epoch = true, true, 7
		c.data = map[string][]byte{"text/plain": []byte("旧内容")}
		c.signal(&dbus.Signal{Name: clipboardInterface + ".SelectionOwnerChanged", Body: body})
		if c.known || c.owner || c.data != nil || c.epoch != 8 {
			t.Fatalf("非法通知保留了可替换快照: %+v", c)
		}
		_, _, err := c.snapshot(context.Background())
		assertCode(t, err, "clipboard_preservation_unavailable")
	}
	c := newPortalClipboard(&waylandDesktop{path: path})
	c.signal(&dbus.Signal{Name: clipboardInterface + ".SelectionOwnerChanged", Body: []any{path, map[string]dbus.Variant{"mime_types": dbus.MakeVariant([]string{}), "session_is_owner": dbus.MakeVariant(false)}}})
	data, _, err := c.snapshot(context.Background())
	if err != nil || len(data) != 0 || !c.known {
		t.Fatalf("显式合法空快照应可保存: %v", err)
	}
}

func TestKDEControlMIMERejectsDefaultBeforeReplacement(t *testing.T) {
	d := &waylandDesktop{path: "/session", cfg: DefaultConfig()}
	c := newPortalClipboard(d)
	c.known = true
	c.mimes = []string{"text/plain", "application/x-kde-onlyReplaceEmpty"}
	d.clipboard = c
	// 没有D-Bus连接，若开始读取或修改selection本测试会失败。
	out, err := c.text(context.Background(), TextInput{Text: "中文", PasteKeys: []string{"Ctrl", "V"}})
	assertCode(t, err, "clipboard_preservation_unavailable")
	if out.Submitted || out.ClipboardReplaced || c.data != nil || c.recovery != nil {
		t.Fatal("不可重放的KDE控制格式导致默认路径修改了剪贴板")
	}
}
