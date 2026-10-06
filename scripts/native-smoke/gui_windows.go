//go:build windows

package main

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var fixtureUser = syscall.NewLazyDLL("user32.dll")
var fixtureGDI = syscall.NewLazyDLL("gdi32.dll")
var fixtureKernel = syscall.NewLazyDLL("kernel32.dll")
var fixtureDWM = syscall.NewLazyDLL("dwmapi.dll")

func userProc(name string) *syscall.LazyProc { return fixtureUser.NewProc(name) }

var createWindow = userProc("CreateWindowExW")
var registerClass = userProc("RegisterClassExW")
var unregisterClass = userProc("UnregisterClassW")
var defaultProc = userProc("DefWindowProcW")
var callWindowProc = userProc("CallWindowProcW")
var setWindowLong = userProc("SetWindowLongPtrW")
var sendTimeout = userProc("SendMessageTimeoutW")
var postMessage = userProc("PostMessageW")
var foregroundWindow = userProc("GetForegroundWindow")
var getFocus = userProc("GetFocus")
var getKeyState = userProc("GetKeyState")
var destroyWindow = userProc("DestroyWindow")
var isWindow = userProc("IsWindow")
var clientRect = userProc("GetClientRect")
var clientToScreen = userProc("ClientToScreen")
var invalidateRect = userProc("InvalidateRect")
var setCapture = userProc("SetCapture")
var releaseCapture = userProc("ReleaseCapture")

const wmFixtureFocus = 0x8001
const wmFixtureRedraw = 0x8002

type nativeRect struct{ Left, Top, Right, Bottom int32 }
type nativePoint struct{ X, Y int32 }
type nativeClass struct {
	Size, Style                                          uint32
	Proc                                                 uintptr
	ClassExtra, WindowExtra                              int32
	Instance, Icon, Cursor, Brush, Menu, Name, SmallIcon uintptr
}
type nativeMessage struct {
	Window         uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Point          nativePoint
	Private        uint32
}
type nativePaint struct {
	DC                   uintptr
	Erase                int32
	Rect                 nativeRect
	Restore, Incremental int32
	Reserved             [32]byte
}
type fixtureCounts struct {
	Clicks, Doubles, Drags, WheelX, WheelY, SelectAll, Backspaces int
	LastClick, LastDouble, DragStart, DragEnd, WheelPoint         nativePoint
}
type winFixture struct {
	window, edit, editOriginal uintptr
	mu                         sync.Mutex
	counts                     fixtureCounts
	down                       nativePoint
	downActive                 bool
	failed                     bool
	paints, paintFailures      int
	thread                     uintptr
	stop                       chan struct{}
	stopOnce                   sync.Once
	done                       chan struct{}
	ready                      chan bool
}

var currentFixture *winFixture

// 只为两轮复用固定回调；句柄与 Go 指针不穿过 LPARAM。
var fixtureCallback = syscall.NewCallback(fixtureWindowProc)
var editCallback = syscall.NewCallback(fixtureEditProc)

func wide(text string) *uint16 { p, err := syscall.UTF16PtrFromString(text); check(err); return p }

func fixtureWindowProc(window, message, wp, lp uintptr) uintptr {
	f := currentFixture
	if f == nil {
		r, _, _ := defaultProc.Call(window, message, wp, lp)
		return r
	}
	x, y := int32(int16(lp&0xffff)), int32(int16((lp>>16)&0xffff))
	switch message {
	case 0x000f: // WM_PAINT：只绘制专用窗口的标记和事件结果。
		failures := 0
		var paint nativePaint
		dc, _, _ := userProc("BeginPaint").Call(window, uintptr(unsafe.Pointer(&paint)))
		if dc != 0 {
			var area nativeRect
			ok, _, _ := clientRect.Call(window, uintptr(unsafe.Pointer(&area)))
			if ok == 0 {
				failures |= 2
			}
			points := []nativePoint{{20, 20}, {area.Right - 20, 20}, {20, area.Bottom - 20}, {area.Right - 20, area.Bottom - 20}}
			for index, point := range points {
				c := fixtureColors[index]
				brush, _, _ := fixtureGDI.NewProc("CreateSolidBrush").Call(uintptr(c.R) | uintptr(c.G)<<8 | uintptr(c.B)<<16)
				if brush == 0 {
					failures |= 4
				}
				r := nativeRect{point.X - 8, point.Y - 8, point.X + 9, point.Y + 9}
				filled, _, _ := userProc("FillRect").Call(dc, uintptr(unsafe.Pointer(&r)), brush)
				if filled == 0 {
					failures |= 8
				}
				removed, _, _ := fixtureGDI.NewProc("DeleteObject").Call(brush)
				if removed == 0 {
					failures |= 16
				}
			}
			f.mu.Lock()
			counts := f.counts
			f.mu.Unlock()
			label := fmt.Sprintf("Clicks %d; doubles %d; drags %d; wheel %d/%d", counts.Clicks, counts.Doubles, counts.Drags, counts.WheelX, counts.WheelY)
			text := wide(label)
			written, _, _ := fixtureGDI.NewProc("TextOutW").Call(dc, 50, 170, uintptr(unsafe.Pointer(text)), uintptr(len(label)))
			if written == 0 {
				failures |= 32
			}
			ended, _, _ := userProc("EndPaint").Call(window, uintptr(unsafe.Pointer(&paint)))
			if ended == 0 {
				failures |= 64
			}
		} else {
			failures |= 1
		}
		f.mu.Lock()
		f.paints++
		f.paintFailures |= failures
		f.mu.Unlock()
		return 0
	case 0x0201: // WM_LBUTTONDOWN
		f.mu.Lock()
		f.down, f.downActive = nativePoint{x, y}, true
		f.mu.Unlock()
		setCapture.Call(window)
	case 0x0203: // WM_LBUTTONDBLCLK，只有 CS_DBLCLKS 类会收到。
		f.mu.Lock()
		f.counts.Doubles++
		f.counts.LastDouble = nativePoint{x, y}
		f.down, f.downActive = nativePoint{x, y}, true
		f.mu.Unlock()
		setCapture.Call(window)
	case 0x0202: // WM_LBUTTONUP：核对真实位移，不以工具 submitted 判定拖拽。
		f.mu.Lock()
		if f.downActive {
			if (x-f.down.X)*(x-f.down.X)+(y-f.down.Y)*(y-f.down.Y) >= 80*80 {
				f.counts.Drags++
				f.counts.DragStart, f.counts.DragEnd = f.down, nativePoint{x, y}
			} else {
				f.counts.Clicks++
				f.counts.LastClick = nativePoint{x, y}
			}
		}
		f.downActive = false
		f.mu.Unlock()
		releaseCapture.Call()
	case 0x020a, 0x020e:
		amount := int(int16((wp>>16)&0xffff)) / 120
		point := nativePoint{x, y}
		userProc("ScreenToClient").Call(window, uintptr(unsafe.Pointer(&point)))
		f.mu.Lock()
		f.counts.WheelPoint = point
		if message == 0x020a {
			f.counts.WheelY += amount
		} else {
			f.counts.WheelX += amount
		}
		f.mu.Unlock()
	case wmFixtureFocus:
		focused, _, _ := getFocus.Call()
		if focused == f.edit {
			return 1
		}
		return 0
	case wmFixtureRedraw:
		result, _, _ := userProc("RedrawWindow").Call(window, 0, 0, 0x185)
		return result
	case 0x0010: // WM_CLOSE
		destroyWindow.Call(window)
		return 0
	case 0x0002: // WM_DESTROY
		userProc("PostQuitMessage").Call(0)
		return 0
	}
	if message >= 0x0201 && message <= 0x020e {
		invalidateRect.Call(window, 0, 1)
	}
	r, _, _ := defaultProc.Call(window, message, wp, lp)
	return r
}

func fixtureEditProc(window, message, wp, lp uintptr) uintptr {
	f := currentFixture
	if message == 0x0100 { // WM_KEYDOWN：EDIT 的兼容全选，其余仍由原控件处理。
		state, _, _ := getKeyState.Call(0x11)
		if wp == 0x41 && int16(state&0xffff) < 0 {
			f.mu.Lock()
			f.counts.SelectAll++
			f.mu.Unlock()
			userProc("SendMessageW").Call(window, 0xb1, 0, ^uintptr(0))
			return 0
		}
		if wp == 8 {
			f.mu.Lock()
			f.counts.Backspaces++
			f.mu.Unlock()
		}
	}
	r, _, _ := callWindowProc.Call(f.editOriginal, window, message, wp, lp)
	return r
}

func newWinFixture(display object) *winFixture {
	f := &winFixture{done: make(chan struct{}), ready: make(chan bool, 1), stop: make(chan struct{})}
	go f.run(display)
	select {
	case success := <-f.ready:
		if !success {
			failFixtureStartup("Native fixture startup failed", f.stopAndWait)
		}
	case <-time.After(10 * time.Second):
		// 已持有夹具对象，即使尚未取得 HWND 也必须中止并等待窗口线程。
		failFixtureStartup("Native fixture startup timed out", f.stopAndWait)
	}
	return f
}
func (f *winFixture) run(display object) {
	runtime.LockOSThread()
	defer close(f.done)
	defer runtime.UnlockOSThread()
	// 最后执行的恢复守卫也覆盖队列清理失败，避免 UI goroutine 的 panic 逃逸。
	defer func() {
		if recover() != nil {
			f.mu.Lock()
			f.failed = true
			f.mu.Unlock()
			select {
			case f.ready <- false:
			default:
			}
		}
	}()
	// 销毁会发布 WM_QUIT；清空专用 UI 线程残留，避免 Go 复用线程时下一轮误退出。
	defer func() {
		var message nativeMessage
		for count := 0; count < 256; count++ {
			result, _, _ := userProc("PeekMessageW").Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0, 1)
			if result == 0 {
				return
			}
		}
		panic("Fixture message queue cleanup exceeded the limit")
	}()
	thread, _, _ := fixtureKernel.NewProc("GetCurrentThreadId").Call()
	f.mu.Lock()
	f.thread = thread
	f.mu.Unlock()
	if f.cancelled() {
		return
	}
	old, _, _ := userProc("SetThreadDpiAwarenessContext").Call(^uintptr(3))
	ensure(old != 0, "Fixture DPI setup failed")
	defer userProc("SetThreadDpiAwarenessContext").Call(old)
	currentFixture = f
	defer func() { currentFixture = nil }()
	instance, _, _ := fixtureKernel.NewProc("GetModuleHandleW").Call(0)
	className, title := wide("RemoteMCPNativeGUIFixture"), wide("Remote MCP GUI validation")
	cursor, _, _ := userProc("LoadCursorW").Call(0, 32512)
	class := nativeClass{Size: uint32(unsafe.Sizeof(nativeClass{})), Style: 8, Proc: fixtureCallback, Instance: instance, Cursor: cursor, Brush: 6, Name: uintptr(unsafe.Pointer(className))}
	atom, _, _ := registerClass.Call(uintptr(unsafe.Pointer(&class)))
	ensure(atom != 0, "Fixture class registration failed")
	defer func() {
		removed, _, _ := unregisterClass.Call(uintptr(unsafe.Pointer(className)), instance)
		ensure(removed != 0, "Fixture class cleanup failed")
	}()
	if f.cancelled() {
		return
	}
	bounds := obj(display["logical_bounds"])
	ensure(number(bounds["width"]) >= 720 && number(bounds["height"]) >= 540, "Display is too small for the fixture")
	window, _, _ := createWindow.Call(8, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)), 0x10c80000, uintptr(int32(number(bounds["x"])+40)), uintptr(int32(number(bounds["y"])+40)), 680, 480, 0, 0, instance, 0)
	ensure(window != 0, "Fixture window creation failed")
	f.mu.Lock()
	f.window = window
	f.mu.Unlock()
	defer func() {
		exists, _, _ := isWindow.Call(window)
		if exists != 0 {
			destroyWindow.Call(window)
		}
	}()
	if f.cancelled() {
		return
	}
	edit, _, _ := createWindow.Call(0x200, uintptr(unsafe.Pointer(wide("EDIT"))), uintptr(unsafe.Pointer(wide("sample"))), 0x50010080, 50, 220, 540, 40, window, 1, instance, 0)
	ensure(edit != 0, "Fixture edit creation failed")
	f.edit = edit
	original, _, _ := setWindowLong.Call(edit, ^uintptr(3), editCallback)
	ensure(original != 0, "Fixture edit subclass failed")
	f.editOriginal = original
	if f.cancelled() {
		return
	}
	userProc("ShowWindow").Call(window, 5)
	userProc("UpdateWindow").Call(window)
	userProc("SetForegroundWindow").Call(window)
	userProc("SetFocus").Call(window)
	f.ready <- true
	var message nativeMessage
	for {
		if f.cancelled() {
			break
		}
		result, _, _ := userProc("GetMessageW").Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if result == 0 {
			break
		}
		ensure(int32(result) != -1, "Fixture message loop failed")
		userProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&message)))
		userProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&message)))
	}
}
func (f *winFixture) owned(edit bool) bool {
	foreground, _, _ := foregroundWindow.Call()
	if foreground != f.window {
		return false
	}
	if edit {
		return f.send(f.window, wmFixtureFocus, 0, 0) == 1
	}
	return true
}
func (f *winFixture) send(window, message, wp, lp uintptr) uintptr {
	return f.sendWithin(context.Background(), window, message, wp, lp)
}

func (f *winFixture) sendWithin(ctx context.Context, window, message, wp, lp uintptr) uintptr {
	timeout := time.Second
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < timeout {
		timeout = time.Until(deadline)
	}
	ensure(ctx.Err() == nil && timeout >= time.Millisecond, "Fixture presentation deadline elapsed")
	var output uintptr
	success, _, _ := sendTimeout.Call(window, message, wp, lp, 2, uintptr(timeout/time.Millisecond), uintptr(unsafe.Pointer(&output)))
	ensure(success != 0, "Fixture message response timed out")
	return output
}

// 只读取专用客户区四个标记中心；两个 DC 必须在取得它们的同一 OS 线程释放。
func (f *winFixture) pixelDiagnostic(native fixtureRect) object {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	old, _, _ := userProc("SetThreadDpiAwarenessContext").Call(^uintptr(3))
	ensure(old != 0, "Fixture pixel diagnostic DPI setup failed")
	defer userProc("SetThreadDpiAwarenessContext").Call(old)
	windowDC, _, _ := userProc("GetDC").Call(f.window)
	if windowDC != 0 {
		defer func() {
			r, _, _ := userProc("ReleaseDC").Call(f.window, windowDC)
			ensure(r != 0, "Fixture window diagnostic DC cleanup failed")
		}()
	}
	desktopDC, _, _ := userProc("GetDC").Call(0)
	if desktopDC != 0 {
		defer func() {
			r, _, _ := userProc("ReleaseDC").Call(0, desktopDC)
			ensure(r != 0, "Fixture desktop diagnostic DC cleanup failed")
		}()
	}
	sample := func(dc uintptr, x, y int) ([]int, bool) {
		if dc == 0 {
			return []int{0, 0, 0}, false
		}
		pixel, _, _ := fixtureGDI.NewProc("GetPixel").Call(dc, uintptr(int32(x)), uintptr(int32(y)))
		if uint32(pixel) == 0xffffffff {
			return []int{0, 0, 0}, false
		}
		return []int{int(pixel & 255), int((pixel >> 8) & 255), int((pixel >> 16) & 255)}, true
	}
	points := []nativePoint{{20, 20}, {int32(native.Width - 20), 20}, {20, int32(native.Height - 20)}, {int32(native.Width - 20), int32(native.Height - 20)}}
	markers := make([]object, 4)
	for index, point := range points {
		windowRGB, windowValid := sample(windowDC, int(point.X), int(point.Y))
		desktopRGB, desktopValid := sample(desktopDC, native.X+int(point.X), native.Y+int(point.Y))
		markers[index] = object{"index": index, "window_rgb": windowRGB, "window_valid": windowValid, "desktop_rgb": desktopRGB, "desktop_valid": desktopValid}
	}
	visible, _, _ := userProc("IsWindowVisible").Call(f.window)
	iconic, _, _ := userProc("IsIconic").Call(f.window)
	var cloaked uint32
	result, _, _ := fixtureDWM.NewProc("DwmGetWindowAttribute").Call(f.window, 14, uintptr(unsafe.Pointer(&cloaked)), 4)
	ensure(result != 0 || cloaked <= 7, "Unexpected fixture DWM cloak flags")
	if result != 0 {
		cloaked = 0
	}
	return object{"visible": visible != 0, "iconic": iconic != 0, "cloaked_valid": result == 0, "cloaked": cloaked, "markers": markers}
}
func (f *winFixture) region() fixtureRect {
	// 调用方 goroutine 不属于 UI 线程；几何查询也必须在 PMv2 下使用物理像素。
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	old, _, _ := userProc("SetThreadDpiAwarenessContext").Call(^uintptr(3))
	ensure(old != 0, "Fixture geometry DPI setup failed")
	defer userProc("SetThreadDpiAwarenessContext").Call(old)
	var bounds nativeRect
	var point nativePoint
	ok, _, _ := clientRect.Call(f.window, uintptr(unsafe.Pointer(&bounds)))
	ensure(ok != 0, "Fixture bounds unavailable")
	ok, _, _ = clientToScreen.Call(f.window, uintptr(unsafe.Pointer(&point)))
	ensure(ok != 0, "Fixture origin unavailable")
	return fixtureRect{int(point.X), int(point.Y), int(bounds.Right), int(bounds.Bottom)}
}
func (f *winFixture) text() string {
	var buffer [256]uint16
	var length uintptr
	// 指针在原生调用实参中直接转换，让 Go 保持缓冲存活并按 syscall 规则固定。
	success, _, _ := sendTimeout.Call(f.edit, 0x000d, uintptr(len(buffer)), uintptr(unsafe.Pointer(&buffer[0])), 2, 1000, uintptr(unsafe.Pointer(&length)))
	ensure(success != 0, "Fixture text response timed out")
	ensure(length < uintptr(len(buffer)-1), "Fixture text exceeded the limit")
	return syscall.UTF16ToString(buffer[:])
}
func (f *winFixture) snapshot() fixtureCounts { f.mu.Lock(); defer f.mu.Unlock(); return f.counts }
func (f *winFixture) cancelled() bool {
	select {
	case <-f.stop:
		return true
	default:
		return false
	}
}
func (f *winFixture) stopAndWait() {
	f.stopOnce.Do(func() { close(f.stop) })
	f.mu.Lock()
	window, thread := f.window, f.thread
	f.mu.Unlock()
	if window != 0 {
		postMessage.Call(window, 0x0010, 0, 0)
	} else if thread != 0 {
		userProc("PostThreadMessageW").Call(thread, 0x0012, 0, 0)
	}
	select {
	case <-f.done:
	case <-time.After(5 * time.Second):
		panic("Fixture window cleanup timed out")
	}
}
func (f *winFixture) close() {
	f.stopAndWait()
	exists, _, _ := isWindow.Call(f.window)
	ensure(exists == 0, "Fixture window survived cleanup")
	f.mu.Lock()
	failed := f.failed
	f.mu.Unlock()
	ensure(!failed, "Fixture thread or resource cleanup failed")
}

func waitGUI(condition func() bool, reason string) {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	panic(reason)
}
func guiSmoke(p *protocol) object {
	guiStage = "tool_discovery"
	tools := map[string]bool{}
	for _, entry := range rows(p.rpc("tools/list", object{}, false)["tools"]) {
		name, ok := obj(entry)["name"].(string)
		ensure(ok, "Tool discovery contained an invalid name")
		tools[name] = true
	}
	for _, name := range []string{"gui_status", "gui_open", "gui_close", "gui_screenshot", "gui_mouse", "gui_key", "gui_text"} {
		ensure(tools[name], "A required GUI tool was missing")
	}
	status := p.tool("gui_status", object{}, false)
	ensure(status["backend"] == "windows" && status["state"] == "available", "Windows GUI desktop is not available")
	guiStage = "session_open"
	opened := p.tool("gui_open", object{"request_id": "native-gui", "wait_ms": 10000}, false)
	id, ok := opened["id"].(string)
	ensure(ok && id != "" && opened["state"] == "ready", "Native GUI session was not ready")
	closed := false
	defer cleanupWithCause("session_cleanup", func() {
		if !closed {
			p.tool("gui_close", object{"id": id}, false)
		}
	})
	capabilities := obj(opened["capabilities"])
	for _, name := range []string{"screenshot", "mouse", "keyboard", "text", "direct_text"} {
		ensure(capabilities[name] == true, "Native GUI capability unavailable")
	}
	displays := rows(opened["displays"])
	ensure(len(displays) > 0, "No native displays were found")
	display := obj(displays[0])
	for _, value := range displays {
		candidate := obj(value)
		if candidate["primary"] == true {
			display = candidate
			break
		}
	}
	ensure(display["absolute_input"] == true, "Native display mapping unavailable")
	guiStage = "fixture_start"
	f := newWinFixture(display)
	fixtureClosed := false
	defer cleanupWithCause("window_cleanup", func() {
		if !fixtureClosed {
			f.close()
		}
	})
	waitGUI(func() bool { return f.owned(false) }, "Fixture did not obtain foreground ownership")
	bounds := obj(display["logical_bounds"])
	ensure(number(bounds["width"]) == number(display["pixel_width"]) && number(bounds["height"]) == number(display["pixel_height"]), "Unexpected native DPI mapping")
	native := f.region()
	region := fixtureRect{native.X - int(number(bounds["x"])), native.Y - int(number(bounds["y"])), native.Width, native.Height}
	ensure(region.X >= 0 && region.Y >= 0 && region.X+region.Width <= int(number(display["pixel_width"])) && region.Y+region.Height <= int(number(display["pixel_height"])), "Fixture crossed display bounds")
	capture := func() (object, []byte) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		frame := awaitFixtureCapture(ctx, 50*time.Millisecond, func(ctx context.Context, attempt int) fixtureCapture {
			ensure(f.owned(false) && f.region() == native, "Fixture ownership or geometry changed")
			ensure(f.sendWithin(ctx, f.window, wmFixtureRedraw, 0, 0) != 0, "Fixture synchronous redraw failed")
			f.mu.Lock()
			paints, failures := f.paints, f.paintFailures
			f.mu.Unlock()
			guiDiagnostic = object{"region": region.object(), "paint_count": paints, "paint_failure_bits": failures, "attempt": attempt}
			ensure(paints > 0 && failures == 0, "Fixture native painting failed")
			ensure(f.owned(false) && f.region() == native, "Fixture ownership or geometry changed before pixel sampling")
			guiDiagnostic["dc"] = f.pixelDiagnostic(native)
			ensure(f.owned(false) && f.region() == native, "Fixture ownership or geometry changed after pixel sampling")
			result := p.rpcContext(ctx, "tools/call", object{"name": "gui_screenshot", "arguments": object{"id": id, "display_id": display["id"], "region": region.object()}}, false)
			ensure(f.owned(false) && f.region() == native, "Fixture ownership or geometry changed after capture")
			metadata, img, raw := decodeGUICapture(result, region)
			ensure(metadata["display_id"] == display["id"], "Capture display mismatch")
			guiDiagnostic["markers"] = fixtureMarkerDiagnostic(img)
			return fixtureCapture{metadata, img, raw}
		})
		return frame.metadata, frame.raw
	}
	guiStage = "initial_capture"
	_, before := capture()
	mouse := func(action string, x, y int, extra object) {
		guiStage = "before_" + action
		metadata, _ := capture()
		args := object{"id": id, "capture_id": metadata["capture_id"], "action": action, "x": x, "y": y}
		for key, value := range extra {
			args[key] = value
		}
		ensure(f.owned(false) && f.region() == native, "Fixture lost ownership before mouse input")
		r := p.tool("gui_mouse", args, false)
		ensure(r["submitted"] == true, "Mouse event was not submitted")
	}
	baseline := f.snapshot()
	mouse("click", 100, 100, nil)
	waitGUI(func() bool { c := f.snapshot(); return c.Clicks > baseline.Clicks && pointNear(c.LastClick, 100, 100) }, "Fixture did not receive the click at its expected coordinates")
	baseline = f.snapshot()
	mouse("double_click", 180, 100, nil)
	waitGUI(func() bool {
		c := f.snapshot()
		return c.Doubles > baseline.Doubles && pointNear(c.LastDouble, 180, 100)
	}, "Fixture did not receive the double click at its expected coordinates")
	baseline = f.snapshot()
	mouse("drag", 100, 100, object{"end_x": 260, "end_y": 130, "duration_ms": 250})
	waitGUI(func() bool {
		c := f.snapshot()
		return c.Drags > baseline.Drags && pointNear(c.DragStart, 100, 100) && pointNear(c.DragEnd, 260, 130)
	}, "Fixture did not receive the drag at its expected coordinates")
	baseline = f.snapshot()
	mouse("scroll", 100, 100, object{"scroll_y": 2})
	waitGUI(func() bool {
		c := f.snapshot()
		// MCP 正值向下，Win32 垂直 wheel 正值向上，方向按公开工具契约核对。
		return c.WheelY-baseline.WheelY == -2 && pointNear(c.WheelPoint, 100, 100)
	}, "Fixture did not receive the expected vertical scrolling")
	baseline = f.snapshot()
	mouse("scroll", 100, 100, object{"scroll_x": 2})
	waitGUI(func() bool {
		c := f.snapshot()
		return c.WheelX-baseline.WheelX == 2 && pointNear(c.WheelPoint, 100, 100)
	}, "Fixture did not receive the expected horizontal scrolling")
	mouse("click", 250, 240, nil)
	waitGUI(func() bool { return f.owned(true) }, "Fixture editor did not obtain focus")
	key := func(keys []string) {
		guiStage = "key_input"
		ensure(f.owned(true) && f.region() == native, "Fixture editor lost ownership before key input")
		r := p.tool("gui_key", object{"id": id, "keys": keys}, false)
		ensure(r["submitted"] == true, "Key event was not submitted")
	}
	key([]string{"Ctrl", "A"})
	waitGUI(func() bool { return f.snapshot().SelectAll > 0 }, "Fixture did not receive Ctrl+A")
	key([]string{"Backspace"})
	waitGUI(func() bool { return f.text() == "" && f.snapshot().Backspaces > 0 }, "Fixture editor was not cleared")
	guiStage = "direct_text"
	ensure(f.owned(true) && f.region() == native, "Fixture editor lost ownership before text input")
	text := p.tool("gui_text", object{"id": id, "text": guiTestText, "mode": "direct"}, false)
	ensure(text["submitted"] == true && text["mode"] == "direct" && text["clipboard_replaced"] != true, "Direct text result mismatch")
	waitGUI(func() bool { return f.text() == guiTestText }, "Fixture UTF-8 text did not match")
	guiStage = "final_capture"
	_, after := capture()
	ensure(hash(before) != hash(after), "Fixture screenshot did not reflect changed state")
	guiStage = "session_close"
	first := p.tool("gui_close", object{"id": id}, false)
	second := p.tool("gui_close", object{"id": id}, false)
	ensure(first["state"] == "closed" && second["state"] == "closed" && first["id"] == id && second["id"] == id, "GUI close was not idempotent")
	closed = true
	c := f.snapshot()
	guiStage = "window_close"
	f.close()
	fixtureClosed = true
	return object{"display_count": len(displays), "width": region.Width, "height": region.Height, "png_before_sha256": hash(before), "png_after_sha256": hash(after), "clicks": c.Clicks, "double_clicks": c.Doubles, "drags": c.Drags, "vertical_scroll": c.WheelY, "horizontal_scroll": c.WheelX, "select_all": c.SelectAll, "backspaces": c.Backspaces, "utf8_text_verified": true, "region_verified": true, "markers_verified": true, "coordinate_events_verified": true, "foreground_guard": true, "repeated_close": true, "clipboard_accessed": false, "window_cleaned": true}
}

func pointNear(point nativePoint, x, y int32) bool {
	return point.X >= x-2 && point.X <= x+2 && point.Y >= y-2 && point.Y <= y+2
}
