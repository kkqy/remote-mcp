package gui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"
)

const portalName = "org.freedesktop.portal.Desktop"
const portalPath dbus.ObjectPath = "/org/freedesktop/portal/desktop"
const remoteInterface = "org.freedesktop.portal.RemoteDesktop"
const screenInterface = "org.freedesktop.portal.ScreenCast"
const clipboardInterface = "org.freedesktop.portal.Clipboard"

type waylandBackend struct{ cfg Config }

func portalConnect(ctx context.Context) (*dbus.Conn, error) {
	// 单独连接具有独立生命周期；授权 ctx 不绑定已经就绪的会话。
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, failure("no_gui", "Unable to connect to the current user's session D-Bus")
	}
	if !conn.SupportsUnixFDs() {
		_ = conn.Close()
		return nil, failure("unsupported", "The session bus does not support Unix FD passing")
	}
	if err := ctx.Err(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}
func portalProperty(ctx context.Context, c *dbus.Conn, iface, name string) (dbus.Variant, error) {
	var v dbus.Variant
	err := c.Object(portalName, portalPath).CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, iface, name).Store(&v)
	return v, err
}
func portalUint(ctx context.Context, c *dbus.Conn, iface, name string) uint32 {
	v, err := portalProperty(ctx, c, iface, name)
	if err != nil {
		return 0
	}
	n, _ := v.Value().(uint32)
	return n
}
func gstProbe(ctx context.Context) error {
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		return failure("dependency_missing", "Wayland screenshots require gst-launch-1.0")
	}
	inspect, err := exec.LookPath("gst-inspect-1.0")
	if err != nil {
		return failure("dependency_missing", "Wayland screenshots require gst-inspect-1.0")
	}
	for _, plugin := range []string{"pipewiresrc", "videoconvert", "pngenc", "fdsink"} {
		cmd := exec.CommandContext(ctx, inspect, plugin)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		if cmd.Run() != nil {
			return failure("dependency_missing", "Required GStreamer plugins for Wayland screenshots are missing")
		}
	}
	return nil
}
func (b *waylandBackend) probe(ctx context.Context, c *dbus.Conn) (Status, error) {
	out := Status{Backend: "wayland-portal", State: "available", Displays: []Display{}, Capabilities: Capabilities{Reasons: map[string]string{}}}
	if portalUint(ctx, c, screenInterface, "version") == 0 {
		return out, failure("dependency_missing", "The ScreenCast Portal is unavailable")
	}
	sources := portalUint(ctx, c, screenInterface, "AvailableSourceTypes")
	if sources&1 == 0 {
		return out, failure("unsupported", "The current Portal does not support display capture")
	}
	if err := gstProbe(ctx); err != nil {
		out.Capabilities.Reasons["screenshot"] = "GStreamer runtime dependencies are missing"
		return out, err
	}
	out.Capabilities.Screenshot = true
	devices := portalUint(ctx, c, remoteInterface, "AvailableDeviceTypes")
	out.Capabilities.Mouse = devices&2 != 0
	out.Capabilities.Keyboard = devices&1 != 0
	out.Capabilities.Clipboard = portalUint(ctx, c, clipboardInterface, "version") > 0 && out.Capabilities.Keyboard
	out.Capabilities.Text = out.Capabilities.Clipboard
	if !out.Capabilities.Mouse {
		out.Capabilities.Reasons["mouse"] = "The RemoteDesktop Portal does not support pointer input"
	}
	if !out.Capabilities.Keyboard {
		out.Capabilities.Reasons["keyboard"] = "The RemoteDesktop Portal does not support keyboard input"
	}
	if !out.Capabilities.Text {
		out.Capabilities.Reasons["text"] = "Portal clipboard or keyboard input is unavailable"
	}
	return out, nil
}
func (b *waylandBackend) Probe(ctx context.Context) (Status, error) {
	c, err := portalConnect(ctx)
	if err != nil {
		return Status{Backend: "wayland-portal"}, err
	}
	defer c.Close()
	return b.probe(ctx, c)
}
func requestPath(c *dbus.Conn, token string) dbus.ObjectPath {
	sender := strings.ReplaceAll(strings.TrimPrefix(c.Names()[0], ":"), ".", "_")
	return dbus.ObjectPath("/org/freedesktop/portal/desktop/request/" + sender + "/" + token)
}
func portalRequest(ctx context.Context, c *dbus.Conn, iface, method string, args []any, options map[string]dbus.Variant) (map[string]dbus.Variant, error) {
	token := "mcp" + randomID()
	options["handle_token"] = dbus.MakeVariant(token)
	path := requestPath(c, token)
	ch := make(chan *dbus.Signal, 8)
	c.Signal(ch)
	defer c.RemoveSignal(ch)
	match := []dbus.MatchOption{dbus.WithMatchSender(portalName), dbus.WithMatchInterface("org.freedesktop.portal.Request"), dbus.WithMatchMember("Response"), dbus.WithMatchObjectPath(path)}
	if err := c.AddMatchSignalContext(ctx, match...); err != nil {
		return nil, failure("dependency_missing", "Unable to subscribe to desktop authorization responses")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = c.RemoveMatchSignalContext(cleanup, match...)
	}()
	args = append(args, options)
	var actual dbus.ObjectPath
	if err := c.Object(portalName, portalPath).CallWithContext(ctx, iface+"."+method, 0, args...).Store(&actual); err != nil {
		return nil, failure("permission_denied", "Desktop Portal request failed")
	}
	if actual != path {
		return nil, failure("unsupported", "The Portal returned a request path that violates the handle convention")
	}
	for {
		select {
		case <-ctx.Done():
			cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = c.Object(portalName, path).CallWithContext(cleanup, "org.freedesktop.portal.Request.Close", 0).Err
			cancel()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, failure("authorization_timeout", "Timed out waiting for desktop authorization")
			}
			return nil, failure("authorization_cancelled", "Desktop authorization was cancelled")
		case sig, ok := <-ch:
			if !ok {
				return nil, failure("session_closed", "Desktop session bus was disconnected")
			}
			if sig.Path != path || len(sig.Body) != 2 {
				continue
			}
			code, ok := sig.Body[0].(uint32)
			result, valid := sig.Body[1].(map[string]dbus.Variant)
			if !ok || !valid {
				return nil, failure("capture_failed", "Invalid desktop authorization response")
			}
			if code == 1 {
				return nil, failure("authorization_cancelled", "The local user cancelled desktop authorization")
			}
			if code != 0 {
				return nil, failure("permission_denied", "Desktop authorization was denied")
			}
			return result, nil
		}
	}
}

type portalStream struct {
	Node  uint32
	Props map[string]dbus.Variant
}
type waylandDesktop struct {
	cfg               Config
	conn              *dbus.Conn
	path              dbus.ObjectPath
	remote            bool
	caps              Capabilities
	mu                sync.Mutex
	displays          []Display
	nodes             map[string]uint32
	closed            bool
	healthErr         error
	done              chan struct{}
	life              context.Context
	cancel            context.CancelFunc
	once              sync.Once
	signals           chan *dbus.Signal
	wg                sync.WaitGroup
	clipboard         *portalClipboard
	held              map[string]int32
	emitMu            sync.Mutex
	monitorEndpoint   layoutEndpoint
	layoutInvalidated bool
	layoutDigest      [32]byte
	layoutOwner       string
}

func (b *waylandBackend) Open(ctx context.Context) (Desktop, error) {
	c, err := portalConnect(ctx)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			_ = c.Close()
		}
	}()
	probe, err := b.probe(ctx, c)
	if err != nil {
		return nil, err
	}
	remote := portalUint(ctx, c, remoteInterface, "version") > 0
	iface := screenInterface
	if remote {
		iface = remoteInterface
	}
	created, err := portalRequest(ctx, c, iface, "CreateSession", nil, map[string]dbus.Variant{"session_handle_token": dbus.MakeVariant("s" + randomID())})
	if err != nil {
		return nil, err
	}
	raw, ok := created["session_handle"].Value().(string)
	if !ok {
		return nil, failure("capture_failed", "Desktop authorization did not return a session")
	}
	path := dbus.ObjectPath(raw)
	if !path.IsValid() {
		return nil, failure("capture_failed", "Invalid desktop session path")
	}
	life, cancel := context.WithCancel(context.Background())
	d := &waylandDesktop{cfg: b.cfg, conn: c, path: path, remote: remote, caps: probe.Capabilities, nodes: map[string]uint32{}, done: make(chan struct{}), life: life, cancel: cancel, signals: make(chan *dbus.Signal, 64)}
	defer func() {
		if !success {
			_ = d.Close()
		}
	}()
	c.Signal(d.signals)
	for _, match := range [][]dbus.MatchOption{{dbus.WithMatchSender(portalName), dbus.WithMatchInterface("org.freedesktop.portal.Session"), dbus.WithMatchObjectPath(path)}, {dbus.WithMatchSender(portalName), dbus.WithMatchInterface(clipboardInterface), dbus.WithMatchObjectPath(portalPath)}} {
		if err := c.AddMatchSignalContext(ctx, match...); err != nil {
			return nil, failure("dependency_missing", "Unable to subscribe to desktop session signals")
		}
	}
	d.clipboard = newPortalClipboard(d)
	d.wg.Add(1)
	go d.watch()
	monitoring := d.monitorLayout(ctx)
	if remote {
		devices := portalUint(ctx, c, remoteInterface, "AvailableDeviceTypes") & 3
		if devices != 0 {
			if _, err := portalRequest(ctx, c, remoteInterface, "SelectDevices", []any{path}, map[string]dbus.Variant{"types": dbus.MakeVariant(devices)}); err != nil {
				return nil, err
			}
		}
		if d.caps.Clipboard {
			if err := c.Object(portalName, portalPath).CallWithContext(ctx, clipboardInterface+".RequestClipboard", 0, path, map[string]dbus.Variant{}).Err; err != nil {
				d.caps.Clipboard = false
				d.caps.Text = false
				d.caps.Reasons["text"] = "The Portal denied the clipboard request"
			}
		}
	}
	opts := map[string]dbus.Variant{"types": dbus.MakeVariant(uint32(1)), "multiple": dbus.MakeVariant(true)}
	if modes := portalUint(ctx, c, screenInterface, "AvailableCursorModes"); modes&2 != 0 {
		opts["cursor_mode"] = dbus.MakeVariant(uint32(2))
	}
	if _, err := portalRequest(ctx, c, screenInterface, "SelectSources", []any{path}, opts); err != nil {
		return nil, err
	}
	started, err := portalRequest(ctx, c, iface, "Start", []any{path, ""}, map[string]dbus.Variant{})
	if err != nil {
		return nil, err
	}
	var streams []portalStream
	if started["streams"].Value() == nil || started["streams"].Signature().String() != "a(ua{sv})" {
		return nil, failure("permission_denied", "No displays were authorized")
	}
	if err := dbus.Store([]any{started["streams"].Value()}, &streams); err != nil || len(streams) == 0 {
		return nil, failure("permission_denied", "No displays were authorized")
	}
	d.verifyLayout(ctx)
	devices, _ := started["devices"].Value().(uint32)
	clip, _ := started["clipboard_enabled"].Value().(bool)
	d.caps.Mouse = remote && devices&2 != 0
	d.caps.Keyboard = remote && devices&1 != 0
	d.caps.Clipboard = d.caps.Clipboard && clip
	d.caps.Text = d.caps.Clipboard && d.caps.Keyboard
	d.mu.Lock()
	for i, stream := range streams {
		display := displayFromPortal(stream, i == 0)
		if !monitoring || d.layoutInvalidated {
			display.AbsoluteInput = false
		}
		d.displays = append(d.displays, display)
		d.nodes[display.ID] = stream.Node
	}
	sort.Slice(d.displays, func(i, j int) bool { return d.displays[i].ID < d.displays[j].ID })
	if !d.caps.Mouse {
		d.caps.Reasons["mouse"] = "Pointer input was not authorized for this session"
	}
	if !d.caps.Keyboard {
		d.caps.Reasons["keyboard"] = "Keyboard input was not authorized for this session"
	}
	if !d.caps.Text {
		d.caps.Reasons["text"] = "Clipboard and keyboard access were not authorized for this session"
	}
	if !monitoring || d.layoutInvalidated {
		d.caps.Reasons["absolute_input"] = "Reliable logical layout monitoring is unavailable or the layout changed during authorization; absolute coordinate input is disabled"
	}
	d.mu.Unlock()
	success = true
	return d, nil
}
func tupleInts(v dbus.Variant) (int, int, bool) {
	value := reflect.ValueOf(v.Value())
	if !value.IsValid() {
		return 0, 0, false
	}
	var a, b reflect.Value
	switch value.Kind() {
	case reflect.Slice, reflect.Array:
		if value.Len() != 2 {
			return 0, 0, false
		}
		a, b = value.Index(0), value.Index(1)
	case reflect.Struct:
		if value.NumField() != 2 {
			return 0, 0, false
		}
		a, b = value.Field(0), value.Field(1)
	default:
		return 0, 0, false
	}
	for a.IsValid() && a.Kind() == reflect.Interface {
		a = a.Elem()
	}
	for b.IsValid() && b.Kind() == reflect.Interface {
		b = b.Elem()
	}
	if !a.IsValid() || !b.IsValid() || a.Kind() != reflect.Int32 || b.Kind() != reflect.Int32 {
		return 0, 0, false
	}
	return int(a.Int()), int(b.Int()), true
}

func displayFromPortal(stream portalStream, primary bool) Display {
	w, h, ok := tupleInts(stream.Props["logical_size"])
	if !ok {
		w, h, ok = tupleInts(stream.Props["size"])
	}
	x, y, _ := tupleInts(stream.Props["position"])
	d := Display{ID: strconv.FormatUint(uint64(stream.Node), 10), Name: "Portal-authorized display", Primary: primary, LogicalBounds: Bounds{float64(x), float64(y), float64(w), float64(h)}, AbsoluteInput: ok && w > 0 && h > 0}
	return d
}
func (d *waylandDesktop) Capabilities() Capabilities {
	d.mu.Lock()
	caps := copyStatus(Status{Capabilities: d.caps}).Capabilities
	clip := d.clipboard
	d.mu.Unlock()
	if caps.Clipboard && clip != nil {
		clip.mu.Lock()
		known := clip.known
		clip.mu.Unlock()
		if !known {
			if caps.Reasons == nil {
				caps.Reasons = map[string]string{}
			}
			caps.Reasons["text"] = "The Portal has not provided the current clipboard formats; default preservation requires an ownership notification from a normal copy operation"
		}
	}
	return caps
}
func (d *waylandDesktop) Done() <-chan struct{} { return d.done }
func (d *waylandDesktop) Err() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.healthErr != nil {
		return d.healthErr
	}
	return failure("session_closed", "The authorized desktop session is closed")
}
func (d *waylandDesktop) watch() {
	defer d.wg.Done()
	for {
		select {
		case <-d.life.Done():
			return
		case <-d.conn.Context().Done():
			d.invalidate(failure("session_closed", "Desktop session bus was disconnected"))
			return
		case signal, ok := <-d.signals:
			if !ok {
				d.invalidate(failure("session_closed", "Desktop session bus was disconnected"))
				return
			}
			if signal.Path == d.path && signal.Name == "org.freedesktop.portal.Session.Closed" {
				d.invalidate(failure("session_closed", "Local desktop authorization was revoked"))
				return
			}
			if d.layoutSignal(signal) {
				continue
			}
			if strings.HasPrefix(signal.Name, clipboardInterface+".") {
				d.clipboard.signal(signal)
			}
		}
	}
}
func (d *waylandDesktop) invalidate(err error) {
	d.mu.Lock()
	d.healthErr = err
	d.closed = true
	d.mu.Unlock()
	d.cancel()
	d.once.Do(func() { close(d.done) })
}
func (d *waylandDesktop) Displays(ctx context.Context) ([]Display, error) {
	d.verifyLayout(ctx)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, failure("session_closed", "Desktop authorization is no longer valid")
	}
	return append([]Display{}, d.displays...), nil
}

type frameBuffer struct {
	limitedBuffer
	received time.Time
}

func (b *frameBuffer) Write(p []byte) (int, error) {
	if b.received.IsZero() && len(p) > 0 {
		b.received = time.Now()
	}
	return b.limitedBuffer.Write(p)
}

func runGStreamer(ctx context.Context, fd *os.File, node uint32, maxBytes, maxPixels int) (Frame, error) {
	command := exec.CommandContext(ctx, "gst-launch-1.0", "-q", "pipewiresrc", "fd=3", fmt.Sprintf("path=%d", node), "do-timestamp=true", "!", "videoconvert", "!", "pngenc", "snapshot=true", "!", "fdsink", "fd=1")
	command.ExtraFiles = []*os.File{fd}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process != nil {
			return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	command.WaitDelay = time.Second
	output := &frameBuffer{limitedBuffer: limitedBuffer{limit: maxBytes}}
	command.Stdout = output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return Frame{}, contextError(ctx)
		}
		var e *Error
		if errors.As(err, &e) {
			return Frame{}, e
		}
		return Frame{}, failure("capture_failed", "PipeWire screenshot capture failed")
	}
	info, err := png.DecodeConfig(bytes.NewReader(output.Bytes()))
	if err != nil {
		return Frame{}, failure("capture_failed", "The capture process did not return a valid PNG")
	}
	if err := checkPixels(info.Width, info.Height, maxPixels); err != nil {
		return Frame{}, err
	}
	img, err := png.Decode(bytes.NewReader(output.Bytes()))
	if err != nil {
		return Frame{}, failure("capture_failed", "Failed to decode the captured PNG")
	}
	// 一次新建流的第一帧可能是合成器的最新静态帧，无法获得媒体原始时间戳，不能声称为新渲染帧。
	return Frame{Image: img, CapturedAt: output.received, Freshness: "latest_available"}, nil
}
func (d *waylandDesktop) Capture(ctx context.Context, display Display) (Frame, error) {
	d.mu.Lock()
	node, ok := d.nodes[display.ID]
	closed := d.closed
	d.mu.Unlock()
	if closed {
		return Frame{}, failure("session_closed", "Desktop authorization is no longer valid")
	}
	if !ok {
		return Frame{}, failure("not_found", "Display is not authorized")
	}
	var fd dbus.UnixFD
	if err := d.conn.Object(portalName, portalPath).CallWithContext(ctx, screenInterface+".OpenPipeWireRemote", 0, d.path, map[string]dbus.Variant{}).Store(&fd); err != nil {
		return Frame{}, failure("capture_failed", "Unable to obtain the authorized PipeWire connection")
	}
	file := os.NewFile(uintptr(fd), "portal-pipewire")
	defer file.Close()
	capturectx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(d.life, cancel)
	defer stop()
	defer cancel()
	frame, err := runGStreamer(capturectx, file, node, d.cfg.MaxPNGBytes, d.cfg.MaxPixels)
	if err != nil {
		return Frame{}, err
	}
	d.mu.Lock()
	for i := range d.displays {
		if d.displays[i].ID == display.ID {
			old := d.displays[i]
			d.displays[i].PixelWidth = frame.Image.Bounds().Dx()
			d.displays[i].PixelHeight = frame.Image.Bounds().Dy()
			if old.PixelWidth > 0 && (old.PixelWidth != d.displays[i].PixelWidth || old.PixelHeight != d.displays[i].PixelHeight) {
				d.displays[i].AbsoluteInput = false
				d.caps.Reasons["mouse"] = "The media layout changed; authorize again to establish a reliable mapping"
			}
		}
	}
	d.mu.Unlock()
	return frame, nil
}
func (d *waylandDesktop) notify(ctx context.Context, method string, args ...any) error {
	d.emitMu.Lock()
	defer d.emitMu.Unlock()
	d.mu.Lock()
	closed := d.closed
	d.mu.Unlock()
	if closed {
		return failure("session_closed", "Desktop authorization is no longer valid")
	}
	base := []any{d.path, map[string]dbus.Variant{}}
	var heldID string
	if method == "NotifyPointerButton" || method == "NotifyKeyboardKeysym" {
		heldID = fmt.Sprintf("%s:%d", method, args[0].(int32))
		if d.held == nil {
			d.held = map[string]int32{}
		}
		if args[1].(uint32) == 1 {
			d.held[heldID] = args[0].(int32)
		}
	}
	if err := d.conn.Object(portalName, portalPath).CallWithContext(ctx, remoteInterface+"."+method, 0, append(base, args...)...).Err; err != nil {
		return failure("input_failed", "Failed to submit a Portal input event")
	}
	if heldID != "" && args[1].(uint32) == 0 {
		delete(d.held, heldID)
	}
	return nil
}
func (d *waylandDesktop) Mouse(ctx context.Context, ev MouseEvent) error {
	d.mu.Lock()
	node, ok := d.nodes[ev.Display.ID]
	d.mu.Unlock()
	if !ok {
		return failure("not_found", "Display is not authorized")
	}
	return performMouse(ctx, ev, func(ctx context.Context, p Point) error {
		return d.notify(ctx, "NotifyPointerMotionAbsolute", node, p.X, p.Y)
	}, func(ctx context.Context, name string, down bool) error {
		code := map[string]int32{"left": 272, "right": 273, "middle": 274}[name]
		state := uint32(0)
		if down {
			state = 1
		}
		return d.notify(ctx, "NotifyPointerButton", code, state)
	}, func(ctx context.Context, x, y int) error {
		for _, item := range []struct {
			axis  uint32
			steps int
		}{{0, y}, {1, x}} {
			if item.steps != 0 {
				if err := d.notify(ctx, "NotifyPointerAxisDiscrete", item.axis, int32(item.steps)); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
func keysyms(keys []string) (map[string]uint32, error) {
	out := map[string]uint32{}
	named := map[string]uint32{"Ctrl": 0xffe3, "Shift": 0xffe1, "Alt": 0xffe9, "Meta": 0xffeb, "Enter": 0xff0d, "Escape": 0xff1b, "Tab": 0xff09, "Space": 0x20, "Backspace": 0xff08, "Delete": 0xffff, "Insert": 0xff63, "Home": 0xff50, "End": 0xff57, "PageUp": 0xff55, "PageDown": 0xff56, "Up": 0xff52, "Down": 0xff54, "Left": 0xff51, "Right": 0xff53}
	for _, k := range keys {
		code := named[k]
		if code == 0 && len(k) == 1 {
			code = uint32(strings.ToLower(k)[0])
		}
		if code == 0 && strings.HasPrefix(k, "F") {
			n, err := strconv.Atoi(k[1:])
			if err == nil && n >= 1 && n <= 24 {
				code = uint32(0xffbd + n)
			}
		}
		if code == 0 {
			return nil, failure("unsupported", "The backend does not support this key")
		}
		out[k] = code
	}
	return out, nil
}
func (d *waylandDesktop) Key(ctx context.Context, keys []string) error {
	symbols, err := keysyms(keys)
	if err != nil {
		return err
	}
	return performKeys(ctx, keys, func(ctx context.Context, key string, down bool) error {
		state := uint32(0)
		if down {
			state = 1
		}
		return d.notify(ctx, "NotifyKeyboardKeysym", int32(symbols[key]), state)
	})
}
func (d *waylandDesktop) Text(ctx context.Context, in TextInput) (InputResult, error) {
	if in.Mode != "clipboard" {
		return InputResult{}, failure("unsupported", "Wayland Notify does not support direct text input")
	}
	if len(in.PasteKeys) == 0 {
		in.PasteKeys = []string{"Ctrl", "V"}
	}
	return d.clipboard.text(ctx, in)
}
func (d *waylandDesktop) Close() error {
	var cleanupErr error
	if d.clipboard != nil {
		cleanupErr = d.clipboard.prepareClose()
	}
	d.cancel()
	d.emitMu.Lock()
	d.mu.Lock()
	already := d.closed
	d.closed = true
	d.mu.Unlock()
	cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	for id, code := range d.held {
		method := "NotifyKeyboardKeysym"
		if strings.HasPrefix(id, "NotifyPointerButton:") {
			method = "NotifyPointerButton"
		}
		if e := d.conn.Object(portalName, portalPath).CallWithContext(cleanup, remoteInterface+"."+method, 0, d.path, map[string]dbus.Variant{}, code, uint32(0)).Err; e != nil {
			cleanupErr = failure("input_failed", "Unable to confirm key and button release after the desktop connection failed")
		}
	}
	d.held = nil
	if !already {
		if e := d.conn.Object(portalName, d.path).CallWithContext(cleanup, "org.freedesktop.portal.Session.Close", 0).Err; e != nil {
			var remote dbus.Error
			if cleanupErr == nil && (!errors.As(e, &remote) || remote.Name != "org.freedesktop.DBus.Error.UnknownObject") {
				cleanupErr = failure("session_closed", "Closing desktop authorization was not confirmed")
			}
		}
	}
	cancel()
	d.emitMu.Unlock()
	connectionErr := d.conn.Close()
	d.wg.Wait()
	if d.clipboard != nil {
		// 终态记录可保留诊断，不得继续持有原始剪贴板内容。
		c := d.clipboard
		c.mu.Lock()
		c.data, c.recovery, c.unconfirmed = nil, nil, nil
		c.mimes = nil
		c.known, c.owner, c.pending = false, false, false
		c.abort = nil
		c.mu.Unlock()
	}
	d.once.Do(func() { close(d.done) })
	if cleanupErr != nil {
		return cleanupErr
	}
	return connectionErr
}

// Portal FD 使用非阻塞轮询，防止目标应用不读写导致取消后仍泄露后台协程。
func fdRead(ctx context.Context, file *os.File, max int) ([]byte, error) {
	fd := int(file.Fd())
	if err := unix.SetNonblock(fd, true); err != nil {
		return nil, err
	}
	result := make([]byte, 0, 4096)
	buf := make([]byte, 16384)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, err := unix.Read(fd, buf)
		if n > 0 {
			if n > max-len(result) {
				return nil, failure("limit_exceeded", "The clipboard snapshot exceeds the byte limit")
			}
			result = append(result, buf[:n]...)
		}
		if err == nil && n == 0 {
			return result, nil
		}
		if err != nil && err != unix.EAGAIN && err != unix.EINTR {
			return nil, err
		}
		if err == unix.EAGAIN {
			_, e := unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, 50)
			if e != nil && e != unix.EINTR {
				return nil, e
			}
		}
	}
}
func fdWrite(ctx context.Context, file *os.File, data []byte) error {
	fd := int(file.Fd())
	if err := unix.SetNonblock(fd, true); err != nil {
		return err
	}
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := unix.Write(fd, data)
		if n > 0 {
			data = data[n:]
		}
		if err != nil && err != unix.EAGAIN && err != unix.EINTR {
			return err
		}
		if err == unix.EAGAIN {
			_, e := unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLOUT}}, 50)
			if e != nil && e != unix.EINTR {
				return e
			}
		}
	}
	return nil
}
