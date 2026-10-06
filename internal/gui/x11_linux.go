package gui

import (
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"math/bits"
	"net"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/randr"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

type x11Backend struct{ cfg Config }
type x11Desktop struct {
	raw       net.Conn
	cfg       Config
	conn      *xgb.Conn
	setup     *xproto.SetupInfo
	screen    *xproto.ScreenInfo
	caps      Capabilities
	randr     bool
	mu        sync.Mutex
	closed    bool
	healthErr error
	done      chan struct{}
	once      sync.Once
	window    xproto.Window
	atoms     map[string]xproto.Atom
	notify    chan xproto.SelectionNotifyEvent
	data      map[xproto.Atom]x11Selection
	served    chan struct{}
	emitMu    sync.Mutex
	held      map[uint16]bool
}
type x11Selection struct {
	kind   xproto.Atom
	format byte
	data   []byte
}

func (b *x11Backend) connect(ctx context.Context) (*x11Desktop, error) {
	if os.Getenv("DISPLAY") == "" {
		return nil, failure("no_gui", "The current user has no accessible graphical display")
	}
	conn, raw, err := x11ConnectTransport(ctx, os.Getenv("DISPLAY"))
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()
	defer raw.SetDeadline(time.Time{})
	setup, err := safeX11Setup(conn)
	if err != nil {
		conn.Close()
		_ = raw.Close()
		return nil, err
	}
	screen := setup.DefaultScreen(conn)
	d := &x11Desktop{cfg: b.cfg, conn: conn, raw: raw, setup: setup, screen: screen, done: make(chan struct{}), atoms: map[string]xproto.Atom{}, notify: make(chan xproto.SelectionNotifyEvent, 8), served: make(chan struct{}, 1)}
	d.caps = Capabilities{Screenshot: true, Reasons: map[string]string{}}
	if err := xtest.Init(conn); err == nil {
		if _, err := xtest.GetVersion(conn, 2, 2).Reply(); err == nil {
			d.caps.Mouse = true
			d.caps.Keyboard = true
		}
	}
	d.randr = randr.Init(conn) == nil
	if d.randr {
		_, _ = randr.QueryVersion(conn, 1, 5).Reply()
	}
	// X11 的常用输入通过现有键图；任意 UTF-8 使用标准剪贴板选择机制。
	d.caps.Clipboard = d.caps.Keyboard
	d.caps.Text = d.caps.Keyboard
	if !d.caps.Keyboard {
		d.caps.Reasons["keyboard"] = "The XTEST extension is unavailable"
		d.caps.Reasons["mouse"] = "The XTEST extension is unavailable"
		d.caps.Reasons["text"] = "The XTEST extension is unavailable"
	}
	for _, name := range []string{"CLIPBOARD", "CLIPBOARD_MANAGER", "SAVE_TARGETS", "TARGETS", "UTF8_STRING", "TEXT", "STRING", "INCR", "REMOTE_MCP_SELECTION"} {
		r, err := xproto.InternAtom(conn, false, uint16(len(name)), name).Reply()
		if err != nil {
			conn.Close()
			return nil, failure("no_gui", "Failed to initialize the X11 clipboard")
		}
		d.atoms[name] = r.Atom
	}
	window, err := xproto.NewWindowId(conn)
	if err != nil {
		conn.Close()
		return nil, failure("no_gui", "Failed to create an X11 window handle")
	}
	d.window = window
	if err := xproto.CreateWindowChecked(conn, 0, window, screen.Root, 0, 0, 1, 1, 0, xproto.WindowClassInputOnly, 0, 0, nil).Check(); err != nil {
		conn.Close()
		return nil, failure("no_gui", "Failed to create the X11 clipboard window")
	}
	go d.events()
	return d, nil
}
func (b *x11Backend) Probe(ctx context.Context) (Status, error) {
	out := Status{Backend: "x11", State: "available", Displays: []Display{}}
	d, err := b.connect(ctx)
	if err != nil {
		return out, err
	}
	defer d.Close()
	out.Capabilities = d.Capabilities()
	out.Displays, err = d.Displays(ctx)
	return out, err
}
func (b *x11Backend) Open(ctx context.Context) (Desktop, error) { return b.connect(ctx) }
func (d *x11Desktop) Capabilities() Capabilities                { return d.caps }
func (d *x11Desktop) Done() <-chan struct{}                     { return d.done }
func (d *x11Desktop) Err() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.healthErr != nil {
		return d.healthErr
	}
	return failure("session_closed", "X11 display connection is closed")
}
func (d *x11Desktop) check(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return failure("session_closed", "X11 display connection is closed")
	}
	return nil
}
func (d *x11Desktop) events() {
	for {
		event, err := d.conn.WaitForEvent()
		if err != nil || event == nil {
			d.mu.Lock()
			d.closed = true
			d.healthErr = failure("session_closed", "X11 display connection was disconnected")
			d.mu.Unlock()
			d.once.Do(func() { close(d.done) })
			return
		}
		switch ev := event.(type) {
		case xproto.SelectionNotifyEvent:
			select {
			case d.notify <- ev:
			default:
			}
		case xproto.SelectionRequestEvent:
			d.serve(ev)
		case xproto.SelectionClearEvent:
			d.mu.Lock()
			d.data = nil
			d.mu.Unlock()
		}
	}
}
func (d *x11Desktop) Displays(ctx context.Context) ([]Display, error) {
	deadlineDone := d.protocolDeadline(ctx)
	defer deadlineDone()
	if err := d.check(ctx); err != nil {
		return nil, err
	}
	makeDisplay := func(id string, x, y, w, h int, primary bool) Display {
		return Display{ID: id, Name: "X11 display", Primary: primary, PixelWidth: w, PixelHeight: h, LogicalBounds: Bounds{float64(x), float64(y), float64(w), float64(h)}, AbsoluteInput: true}
	}
	var displays []Display
	if d.randr {
		r, err := randr.GetMonitors(d.conn, d.screen.Root, true).Reply()
		if err == nil {
			for _, monitor := range r.Monitors {
				if monitor.Width > 0 && monitor.Height > 0 {
					displays = append(displays, makeDisplay(strconv.FormatUint(uint64(monitor.Name), 10), int(monitor.X), int(monitor.Y), int(monitor.Width), int(monitor.Height), monitor.Primary))
				}
			}
		} else {
			resources, e := randr.GetScreenResourcesCurrent(d.conn, d.screen.Root).Reply()
			if e == nil {
				for _, crtc := range resources.Crtcs {
					info, e := randr.GetCrtcInfo(d.conn, crtc, resources.ConfigTimestamp).Reply()
					if e == nil && info.Width > 0 && info.Height > 0 {
						displays = append(displays, makeDisplay("crtc-"+strconv.FormatUint(uint64(crtc), 10), int(info.X), int(info.Y), int(info.Width), int(info.Height), len(displays) == 0))
					}
				}
			}
		}
	}
	if len(displays) == 0 {
		geometry, err := xproto.GetGeometry(d.conn, xproto.Drawable(d.screen.Root)).Reply()
		if err != nil {
			return nil, failure("no_gui", "Unable to read X11 display dimensions")
		}
		displays = append(displays, makeDisplay("root", 0, 0, int(geometry.Width), int(geometry.Height), true))
	}
	sort.Slice(displays, func(i, j int) bool { return displays[i].ID < displays[j].ID })
	return displays, nil
}
func x11Decode(reply *xproto.GetImageReply, setup *xproto.SetupInfo, screen *xproto.ScreenInfo, w, h int) (image.Image, error) {
	var format xproto.Format
	for _, f := range setup.PixmapFormats {
		if f.Depth == reply.Depth {
			format = f
			break
		}
	}
	if format.BitsPerPixel != 16 && format.BitsPerPixel != 24 && format.BitsPerPixel != 32 || format.ScanlinePad == 0 {
		return nil, failure("unsupported", "Unsupported X11 pixel format")
	}
	var visual xproto.VisualInfo
	for _, depth := range screen.AllowedDepths {
		for _, v := range depth.Visuals {
			if v.VisualId == reply.Visual {
				visual = v
			}
		}
	}
	if visual.RedMask == 0 || visual.GreenMask == 0 || visual.BlueMask == 0 {
		return nil, failure("unsupported", "X11 visuals other than TrueColor are unsupported")
	}
	stride := ((w*int(format.BitsPerPixel) + int(format.ScanlinePad) - 1) / int(format.ScanlinePad)) * int(format.ScanlinePad) / 8
	if len(reply.Data) < stride*h {
		return nil, failure("capture_failed", "X11 pixel data is too short")
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	channels := func(v, mask uint32) uint8 {
		shift := bits.TrailingZeros32(mask)
		max := mask >> shift
		return uint8(((v & mask) >> shift) * 255 / max)
	}
	size := int(format.BitsPerPixel) / 8
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			start := y*stride + x*size
			var value uint32
			if setup.ImageByteOrder == xproto.ImageOrderLSBFirst {
				for i := 0; i < size; i++ {
					value |= uint32(reply.Data[start+i]) << (8 * i)
				}
			} else {
				for i := 0; i < size; i++ {
					value = (value << 8) | uint32(reply.Data[start+i])
				}
			}
			img.SetRGBA(x, y, color.RGBA{channels(value, visual.RedMask), channels(value, visual.GreenMask), channels(value, visual.BlueMask), 255})
		}
	}
	return img, nil
}
func (d *x11Desktop) Capture(ctx context.Context, display Display) (Frame, error) {
	deadlineDone := d.protocolDeadline(ctx)
	defer deadlineDone()
	if err := d.check(ctx); err != nil {
		return Frame{}, err
	}
	if err := checkPixels(display.PixelWidth, display.PixelHeight, d.cfg.MaxPixels); err != nil {
		return Frame{}, err
	}
	x, y := int(display.LogicalBounds.X), int(display.LogicalBounds.Y)
	if x < -32768 || x > 32767 || y < -32768 || y > 32767 || display.PixelWidth > 65535 || display.PixelHeight > 65535 {
		return Frame{}, failure("unsupported", "Display coordinates exceed the X11 protocol range")
	}
	reply, err := xproto.GetImage(d.conn, xproto.ImageFormatZPixmap, xproto.Drawable(d.screen.Root), int16(x), int16(y), uint16(display.PixelWidth), uint16(display.PixelHeight), 0xffffffff).Reply()
	captured := time.Now()
	if err != nil {
		return Frame{}, failure("capture_failed", "X11 screenshot capture failed")
	}
	img, err := x11Decode(reply, d.setup, d.screen, display.PixelWidth, display.PixelHeight)
	if err != nil {
		return Frame{}, err
	}
	return Frame{img, captured, "new_capture"}, nil
}
func (d *x11Desktop) fake(ctx context.Context, kind, detail byte, x, y int16) error {
	d.emitMu.Lock()
	defer d.emitMu.Unlock()
	deadlineDone := d.protocolDeadline(ctx)
	defer deadlineDone()
	if err := d.check(ctx); err != nil {
		return err
	}
	if d.held == nil {
		d.held = map[uint16]bool{}
	}
	if kind == xproto.KeyPress || kind == xproto.ButtonPress {
		d.held[uint16(kind)<<8|uint16(detail)] = true
	}
	if err := xtest.FakeInputChecked(d.conn, kind, detail, 0, d.screen.Root, x, y, 0).Check(); err != nil {
		return failure("input_failed", "Failed to submit an XTEST event")
	}
	if kind == xproto.KeyRelease || kind == xproto.ButtonRelease {
		delete(d.held, uint16(kind-1)<<8|uint16(detail))
	}
	return nil
}
func (d *x11Desktop) Mouse(ctx context.Context, ev MouseEvent) error {
	return performMouse(ctx, ev, func(ctx context.Context, p Point) error {
		x, y := p.X+ev.Display.LogicalBounds.X, p.Y+ev.Display.LogicalBounds.Y
		if x < -32768 || x > 32767 || y < -32768 || y > 32767 {
			return failure("invalid_argument", "Mouse coordinates exceed the X11 protocol range")
		}
		return d.fake(ctx, xproto.MotionNotify, 0, int16(x), int16(y))
	}, func(ctx context.Context, name string, down bool) error {
		kind := byte(xproto.ButtonRelease)
		if down {
			kind = xproto.ButtonPress
		}
		return d.fake(ctx, kind, map[string]byte{"left": 1, "middle": 2, "right": 3}[name], 0, 0)
	}, func(ctx context.Context, x, y int) error {
		for _, axis := range []struct {
			steps              int
			positive, negative byte
		}{{y, 5, 4}, {x, 7, 6}} {
			steps, button := axis.steps, axis.positive
			if steps < 0 {
				steps = -steps
				button = axis.negative
			}
			for i := 0; i < steps; i++ {
				if err := d.fake(ctx, xproto.ButtonPress, button, 0, 0); err != nil {
					return err
				}
				if err := d.fake(ctx, xproto.ButtonRelease, button, 0, 0); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
func (d *x11Desktop) Key(ctx context.Context, keys []string) error {
	deadlineDone := d.protocolDeadline(ctx)
	defer deadlineDone()
	symbols, err := keysyms(keys)
	if err != nil {
		return err
	}
	mapping, err := xproto.GetKeyboardMapping(d.conn, d.setup.MinKeycode, byte(int(d.setup.MaxKeycode)-int(d.setup.MinKeycode)+1)).Reply()
	if err != nil || mapping.KeysymsPerKeycode == 0 || len(mapping.Keysyms)%int(mapping.KeysymsPerKeycode) != 0 {
		return failure("input_failed", "Unable to read a reliable X11 keyboard map")
	}
	codes := map[string]byte{}
	for name, symbol := range symbols {
		for i := 0; i < len(mapping.Keysyms); i += int(mapping.KeysymsPerKeycode) {
			if uint32(mapping.Keysyms[i]) == symbol {
				codes[name] = byte(int(d.setup.MinKeycode) + i/int(mapping.KeysymsPerKeycode))
				break
			}
		}
		if codes[name] == 0 {
			return failure("unsupported", "The current X11 keyboard map does not contain the requested key")
		}
	}
	return performKeys(ctx, keys, func(ctx context.Context, key string, down bool) error {
		kind := byte(xproto.KeyRelease)
		if down {
			kind = xproto.KeyPress
		}
		return d.fake(ctx, kind, codes[key], 0, 0)
	})
}
func (d *x11Desktop) selection(ctx context.Context, selection, target xproto.Atom) (x11Selection, error) {
	for {
		select {
		case <-d.notify:
			continue
		default:
			goto empty
		}
	}
empty:
	if err := xproto.ConvertSelectionChecked(d.conn, d.window, selection, target, d.atoms["REMOTE_MCP_SELECTION"], xproto.TimeCurrentTime).Check(); err != nil {
		return x11Selection{}, failure("clipboard_preservation_unavailable", "X11 clipboard conversion failed")
	}
	for {
		select {
		case <-ctx.Done():
			return x11Selection{}, ctx.Err()
		case ev := <-d.notify:
			if ev.Selection != selection || ev.Target != target {
				continue
			}
			if ev.Property == 0 {
				return x11Selection{}, failure("clipboard_preservation_unavailable", "The X11 clipboard format cannot be preserved")
			}
			prop, err := xproto.GetProperty(d.conn, true, d.window, ev.Property, xproto.GetPropertyTypeAny, 0, uint32((d.cfg.MaxClipboardBytes+3)/4)).Reply()
			if err != nil || prop.BytesAfter != 0 || prop.Type == d.atoms["INCR"] || len(prop.Value) > d.cfg.MaxClipboardBytes {
				return x11Selection{}, failure("clipboard_preservation_unavailable", "X11 clipboard data exceeds the limit or requires incremental transfer")
			}
			return x11Selection{prop.Type, prop.Format, prop.Value}, nil
		case <-d.done:
			return x11Selection{}, failure("session_closed", "X11 connection is closed")
		}
	}
}
func (d *x11Desktop) owner() (xproto.Window, error) {
	reply, err := xproto.GetSelectionOwner(d.conn, d.atoms["CLIPBOARD"]).Reply()
	if err != nil {
		return 0, err
	}
	return reply.Owner, nil
}
func (d *x11Desktop) serve(ev xproto.SelectionRequestEvent) {
	property := ev.Property
	if property == 0 {
		property = ev.Target
	}
	d.mu.Lock()
	data, ok := d.data[ev.Target]
	if ev.Target == d.atoms["TARGETS"] {
		list := make([]byte, 4*(len(d.data)+1))
		binary.LittleEndian.PutUint32(list, uint32(d.atoms["TARGETS"]))
		i := 4
		for atom := range d.data {
			binary.LittleEndian.PutUint32(list[i:], uint32(atom))
			i += 4
		}
		data = x11Selection{xproto.AtomAtom, 32, list}
		ok = true
	}
	d.mu.Unlock()
	if !ok || data.format != 8 && data.format != 16 && data.format != 32 || len(data.data) > int(d.setup.MaximumRequestLength)*4-32 {
		property = 0
	} else {
		if err := xproto.ChangePropertyChecked(d.conn, xproto.PropModeReplace, ev.Requestor, property, data.kind, data.format, uint32(len(data.data)*8/int(data.format)), data.data).Check(); err != nil {
			property = 0
		}
	}
	notify := xproto.SelectionNotifyEvent{Time: ev.Time, Requestor: ev.Requestor, Selection: ev.Selection, Target: ev.Target, Property: property}
	_ = xproto.SendEventChecked(d.conn, false, ev.Requestor, 0, string(notify.Bytes())).Check()
	if property != 0 && ev.Target != d.atoms["TARGETS"] {
		select {
		case d.served <- struct{}{}:
		default:
		}
	}
}
func (d *x11Desktop) snapshot(ctx context.Context) (map[xproto.Atom]x11Selection, xproto.Window, error) {
	original, err := d.owner()
	if err != nil {
		return nil, 0, failure("clipboard_preservation_unavailable", "Unable to query the X11 clipboard owner")
	}
	if original == 0 {
		return map[xproto.Atom]x11Selection{}, 0, nil
	}
	// 有内容时需要剪贴板管理器接管恢复内容，避免会话关闭后原内容丢失。
	manager, err := xproto.GetSelectionOwner(d.conn, d.atoms["CLIPBOARD_MANAGER"]).Reply()
	if err != nil || manager.Owner == 0 {
		return nil, original, failure("clipboard_preservation_unavailable", "Reliable restoration of a nonempty X11 clipboard requires CLIPBOARD_MANAGER")
	}
	targets, err := d.selection(ctx, d.atoms["CLIPBOARD"], d.atoms["TARGETS"])
	if err != nil || targets.format != 32 || len(targets.data)%4 != 0 || len(targets.data)/4 > 64 {
		return nil, original, failure("clipboard_preservation_unavailable", "The X11 clipboard format list cannot be preserved")
	}
	out := map[xproto.Atom]x11Selection{}
	remaining := d.cfg.MaxClipboardBytes
	for i := 0; i < len(targets.data); i += 4 {
		target := xproto.Atom(binary.LittleEndian.Uint32(targets.data[i:]))
		if target == d.atoms["TARGETS"] {
			continue
		}
		data, err := d.selection(ctx, d.atoms["CLIPBOARD"], target)
		if err != nil {
			return nil, original, err
		}
		remaining -= len(data.data)
		if remaining < 0 {
			return nil, original, failure("clipboard_preservation_unavailable", "The X11 clipboard snapshot exceeds the total byte limit")
		}
		out[target] = data
	}
	current, err := d.owner()
	if err != nil || current != original {
		return nil, original, failure("clipboard_preservation_unavailable", "The X11 clipboard owner changed during preservation")
	}
	return out, original, nil
}
func (d *x11Desktop) setSelection(data map[xproto.Atom]x11Selection, expected xproto.Window) (bool, error) {
	// 服务器短暂原子区只做所有者确认和替换，不读数据、不等待目标应用。
	if err := xproto.GrabServerChecked(d.conn).Check(); err != nil {
		return false, err
	}
	defer xproto.UngrabServer(d.conn)
	current, err := d.owner()
	if err != nil {
		return false, err
	}
	if current != expected {
		return false, nil
	}
	d.mu.Lock()
	previous := d.data
	d.data = data
	d.mu.Unlock()
	owner := d.window
	if len(data) == 0 {
		owner = 0
	}
	if err := xproto.SetSelectionOwnerChecked(d.conn, owner, d.atoms["CLIPBOARD"], xproto.TimeCurrentTime).Check(); err != nil {
		d.mu.Lock()
		d.data = previous
		d.mu.Unlock()
		return false, err
	}
	return true, nil
}
func (d *x11Desktop) Text(ctx context.Context, in TextInput) (out InputResult, err error) {
	deadlineDone := d.protocolDeadline(ctx)
	defer deadlineDone()
	if in.Mode != "clipboard" {
		return out, failure("unsupported", "The current X11 backend uses clipboard text input")
	}
	if len(in.PasteKeys) == 0 {
		in.PasteKeys = []string{"Ctrl", "V"}
	}
	if _, err := keysyms(in.PasteKeys); err != nil {
		return out, err
	}
	out.Mode = "clipboard"
	old, owner, saveErr := d.snapshot(ctx)
	preserve := saveErr == nil
	if !preserve && !in.AllowClipboardReplace {
		return out, saveErr
	}
	if !preserve {
		owner, err = d.owner()
		if err != nil {
			return out, failure("input_failed", "Failed to query the X11 clipboard owner")
		}
		out.ClipboardReplaced = true
		out.ClipboardRestore = "not_requested"
	}
	for {
		select {
		case <-d.served:
			continue
		default:
			goto cleared
		}
	}
cleared:
	data := map[xproto.Atom]x11Selection{d.atoms["UTF8_STRING"]: {d.atoms["UTF8_STRING"], 8, []byte(in.Text)}, d.atoms["TEXT"]: {d.atoms["UTF8_STRING"], 8, []byte(in.Text)}}
	changed, e := d.setSelection(data, owner)
	if e != nil || !changed {
		return out, failure("clipboard_preservation_unavailable", "X11 clipboard replacement failed or its owner changed")
	}
	defer func() {
		if !preserve {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		changed, e := d.setSelection(old, d.window)
		if e != nil {
			out.ClipboardRestore = "failed"
			err = &Error{Code: "clipboard_restore_failed", Message: "Failed to restore the original X11 clipboard", InputMayHaveApplied: out.Submitted, ClipboardRestore: "failed"}
			return
		}
		if !changed {
			out.ClipboardRestore = "skipped_new_owner"
			return
		}
		if len(old) > 0 {
			if _, e := d.selection(cleanup, d.atoms["CLIPBOARD_MANAGER"], d.atoms["SAVE_TARGETS"]); e != nil {
				out.ClipboardRestore = "failed"
				err = &Error{Code: "clipboard_restore_failed", Message: "The X11 clipboard manager did not take ownership of the restored content", InputMayHaveApplied: out.Submitted, ClipboardRestore: "failed"}
				return
			}
		}
		out.ClipboardRestore = "restored"
	}()
	if err := d.Key(ctx, in.PasteKeys); err != nil {
		return out, partialError(err)
	}
	out.Submitted = true
	select {
	case <-d.served:
	case <-ctx.Done():
		return out, partialError(contextError(ctx))
	case <-d.done:
		return out, partialError(failure("session_closed", "X11 connection is closed"))
	}
	if err := pause(ctx, 100*time.Millisecond); err != nil {
		return out, partialError(err)
	}
	return out, nil
}
func (d *x11Desktop) Close() error {
	// 先打断服务器未回应的协议等待，再获取提交锁，避免关闭永久卡在 Check。
	_ = d.raw.SetDeadline(time.Now())
	d.emitMu.Lock()
	defer d.emitMu.Unlock()
	d.mu.Lock()
	d.closed = true
	d.data = nil
	d.mu.Unlock()
	var cleanupErr error
	if len(d.held) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		conn, raw, err := x11ConnectTransport(ctx, os.Getenv("DISPLAY"))
		if err == nil {
			stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
			if err = xtest.Init(conn); err == nil {
				for value := range d.held {
					if e := xtest.FakeInputChecked(conn, byte(value>>8)+1, byte(value), 0, d.screen.Root, 0, 0, 0).Check(); e != nil {
						cleanupErr = e
					}
				}
			} else {
				cleanupErr = err
			}
			stop()
			conn.Close()
			_ = raw.Close()
		} else {
			cleanupErr = err
		}
		cancel()
	}
	d.held = nil
	_ = d.raw.Close()
	d.conn.Close()
	d.once.Do(func() { close(d.done) })
	if cleanupErr != nil {
		return failure("input_failed", "Unable to confirm key and button release after the X11 connection failed")
	}
	return nil
}

func safeX11Setup(conn *xgb.Conn) (setup *xproto.SetupInfo, err error) {
	defer func() {
		if recover() != nil {
			setup = nil
			err = failure("no_gui", "Invalid X11 display initialization response")
		}
	}()
	if len(conn.SetupBytes) < 40 {
		return nil, failure("no_gui", "X11 display initialization response is too short")
	}
	return xproto.Setup(conn), nil
}
