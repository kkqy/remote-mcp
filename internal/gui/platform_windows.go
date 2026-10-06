//go:build windows

package gui

import (
	"context"
	"image"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var guiUser32 = windows.NewLazySystemDLL("user32.dll")
var guiGDI32 = windows.NewLazySystemDLL("gdi32.dll")
var guiEnumMonitors = guiUser32.NewProc("EnumDisplayMonitors")
var guiMonitorInfo = guiUser32.NewProc("GetMonitorInfoW")
var guiGetDC = guiUser32.NewProc("GetDC")
var guiReleaseDC = guiUser32.NewProc("ReleaseDC")
var guiCreateDC = guiGDI32.NewProc("CreateCompatibleDC")
var guiDeleteDC = guiGDI32.NewProc("DeleteDC")
var guiDIB = guiGDI32.NewProc("CreateDIBSection")
var guiSelectObject = guiGDI32.NewProc("SelectObject")
var guiDeleteObject = guiGDI32.NewProc("DeleteObject")
var guiBitBlt = guiGDI32.NewProc("BitBlt")
var guiGDIFlush = guiGDI32.NewProc("GdiFlush")
var guiSendInput = guiUser32.NewProc("SendInput")
var guiSetDPI = guiUser32.NewProc("SetThreadDpiAwarenessContext")
var guiMetrics = guiUser32.NewProc("GetSystemMetrics")
var guiOpenInputDesktop = guiUser32.NewProc("OpenInputDesktop")
var guiCloseDesktop = guiUser32.NewProc("CloseDesktop")
var guiGetThreadDesktop = guiUser32.NewProc("GetThreadDesktop")
var guiObjectInfo = guiUser32.NewProc("GetUserObjectInformationW")

// 原生 INPUT 在两种 64 位架构均为 40 字节；联合体从第 8 字节开始。
type winInput struct {
	Kind uint32
	Pad  uint32
	Data [32]byte
}
type winRect struct{ Left, Top, Right, Bottom int32 }
type winMonitor struct {
	Size          uint32
	Monitor, Work winRect
	Flags         uint32
	Device        [32]uint16
}
type winBitmapInfo struct {
	Size                   uint32
	Width, Height          int32
	Planes, BitCount       uint16
	Compression, SizeImage uint32
	XPels, YPels           int32
	Colors, Important      uint32
}
type windowsGUI struct {
	cfg      Config
	closed   atomic.Bool
	nativeMu sync.Mutex
}

func newPlatformBackend(cfg Config) Backend { return &windowsGUI{cfg: cfg} }
func (w *windowsGUI) Capabilities() Capabilities {
	return Capabilities{Screenshot: true, Mouse: true, Keyboard: true, Text: true, DirectText: true, Reasons: map[string]string{"clipboard": "原生 Unicode 输入可用；未提供剪贴板模式"}}
}
func (w *windowsGUI) Probe(ctx context.Context) (Status, error) {
	ds, err := w.Displays(ctx)
	st := Status{Backend: "windows", State: "available", Displays: ds, Capabilities: w.Capabilities()}
	if err != nil {
		st.State = "unavailable"
		st.Capabilities = Capabilities{}
	}
	return st, err
}
func (w *windowsGUI) Open(ctx context.Context) (Desktop, error) {
	if _, err := w.Displays(ctx); err != nil {
		return nil, err
	}
	return &windowsGUI{cfg: w.cfg}, nil
}
func (w *windowsGUI) Close() error {
	w.nativeMu.Lock()
	defer w.nativeMu.Unlock()
	w.closed.Store(true)
	return nil
}
func (w *windowsGUI) check(ctx context.Context) error {
	if w.closed.Load() {
		return failure("session_closed", "图形会话已关闭")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

// 每次调用在固定线程上启用 PMv2，避免影响进程内其他模块的 DPI 设置。
func winThread() (func(), error) {
	runtime.LockOSThread()
	if err := guiSetDPI.Find(); err != nil {
		runtime.UnlockOSThread()
		return nil, failure("unsupported", "系统缺少线程 DPI 感知接口，需要 Windows 10 1703 或更新版本")
	}
	old, _, _ := guiSetDPI.Call(^uintptr(3))
	if old == 0 {
		runtime.UnlockOSThread()
		return nil, failure("input_failed", "设置线程 DPI 感知失败")
	}
	return func() { guiSetDPI.Call(old); runtime.UnlockOSThread() }, nil
}
func winDesktopName(h uintptr) string {
	var name [256]uint16
	var needed uint32
	ok, _, _ := guiObjectInfo.Call(h, 2, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)*2), uintptr(unsafe.Pointer(&needed)))
	if ok == 0 {
		return ""
	}
	return windows.UTF16ToString(name[:])
}
func winInteractive() error {
	h, _, _ := guiOpenInputDesktop.Call(0, 0, 1)
	if h == 0 {
		return failure("no_gui", "无法访问当前交互桌面，锁屏或服务会话不支持 GUI")
	}
	defer guiCloseDesktop.Call(h)
	current, _, _ := guiGetThreadDesktop.Call(uintptr(windows.GetCurrentThreadId()))
	if winDesktopName(h) != "Default" || winDesktopName(current) != "Default" {
		return failure("permission_denied", "当前线程与可输入桌面不一致，或正在安全桌面")
	}
	return nil
}

// 枚举回调同步执行；串行保管结果，避免将 Go 指针转换成回调 LPARAM。
var winEnumMu sync.Mutex
var winEnumDisplays []Display
var winEnumCallback = windows.NewCallback(func(h, dc, r, p uintptr) uintptr {
	if len(winEnumDisplays) >= 128 {
		return 0
	}
	info := winMonitor{Size: uint32(unsafe.Sizeof(winMonitor{}))}
	ok, _, _ := guiMonitorInfo.Call(h, uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		return 0
	}
	b := info.Monitor
	winEnumDisplays = append(winEnumDisplays, Display{ID: strconv.FormatUint(uint64(h), 10), Name: windows.UTF16ToString(info.Device[:]), Primary: info.Flags&1 != 0, PixelWidth: int(b.Right - b.Left), PixelHeight: int(b.Bottom - b.Top), LogicalBounds: Bounds{float64(b.Left), float64(b.Top), float64(b.Right - b.Left), float64(b.Bottom - b.Top)}, AbsoluteInput: true})
	return 1
})

func (w *windowsGUI) Displays(ctx context.Context) ([]Display, error) {
	if err := w.check(ctx); err != nil {
		return nil, err
	}
	restore, err := winThread()
	if err != nil {
		return nil, err
	}
	defer restore()
	if err = winInteractive(); err != nil {
		return nil, err
	}
	winEnumMu.Lock()
	defer winEnumMu.Unlock()
	winEnumDisplays = nil
	defer func() { winEnumDisplays = nil }()
	ok, _, _ := guiEnumMonitors.Call(0, 0, winEnumCallback, 0)
	ds := winEnumDisplays
	if ok == 0 || len(ds) == 0 {
		return nil, failure("no_gui", "未找到可用的交互显示器")
	}
	return ds, nil
}
func (w *windowsGUI) Capture(ctx context.Context, d Display) (Frame, error) {
	if err := w.check(ctx); err != nil {
		return Frame{}, err
	}
	if d.PixelWidth <= 0 || d.PixelHeight <= 0 || d.PixelWidth > w.cfg.MaxPixels/d.PixelHeight {
		return Frame{}, failure("limit_exceeded", "显示器截图超过像素上限")
	}
	restore, err := winThread()
	if err != nil {
		return Frame{}, err
	}
	defer restore()
	if err = winInteractive(); err != nil {
		return Frame{}, err
	}
	screen, _, _ := guiGetDC.Call(0)
	if screen == 0 {
		return Frame{}, failure("capture_failed", "无法取得桌面绘图上下文")
	}
	defer guiReleaseDC.Call(0, screen)
	mem, _, _ := guiCreateDC.Call(screen)
	if mem == 0 {
		return Frame{}, failure("capture_failed", "无法创建截图绘图上下文")
	}
	defer guiDeleteDC.Call(mem)
	info := winBitmapInfo{Size: 40, Width: int32(d.PixelWidth), Height: -int32(d.PixelHeight), Planes: 1, BitCount: 32}
	var pixels unsafe.Pointer
	bitmap, _, _ := guiDIB.Call(screen, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&pixels)), 0, 0)
	if bitmap == 0 || pixels == nil {
		return Frame{}, failure("capture_failed", "创建截图位图失败")
	}
	defer guiDeleteObject.Call(bitmap)
	old, _, _ := guiSelectObject.Call(mem, bitmap)
	if old == 0 || old == ^uintptr(0) {
		return Frame{}, failure("capture_failed", "选择截图位图失败")
	}
	defer guiSelectObject.Call(mem, old)
	ok, _, _ := guiBitBlt.Call(mem, 0, 0, uintptr(d.PixelWidth), uintptr(d.PixelHeight), screen, uintptr(int32(d.LogicalBounds.X)), uintptr(int32(d.LogicalBounds.Y)), 0x40cc0020)
	flushed, _, _ := guiGDIFlush.Call()
	if flushed == 0 {
		return Frame{}, failure("capture_failed", "同步截图位图失败")
	}
	at := time.Now()
	if ok == 0 {
		return Frame{}, failure("capture_failed", "桌面截图失败")
	}
	if err = w.check(ctx); err != nil {
		return Frame{}, err
	}
	img := image.NewRGBA(image.Rect(0, 0, d.PixelWidth, d.PixelHeight))
	src := unsafe.Slice((*byte)(pixels), len(img.Pix))
	for i := 0; i < len(src); i += 4 {
		img.Pix[i] = src[i+2]
		img.Pix[i+1] = src[i+1]
		img.Pix[i+2] = src[i]
		img.Pix[i+3] = 255
	}
	return Frame{img, at, "fresh"}, nil
}
func winMouseInput(dx, dy int32, data, flags uint32) winInput {
	i := winInput{}
	*(*int32)(unsafe.Pointer(&i.Data[0])) = dx
	*(*int32)(unsafe.Pointer(&i.Data[4])) = dy
	*(*uint32)(unsafe.Pointer(&i.Data[8])) = data
	*(*uint32)(unsafe.Pointer(&i.Data[12])) = flags
	return i
}
func winKeyInput(vk, scan uint16, flags uint32) winInput {
	i := winInput{Kind: 1}
	*(*uint16)(unsafe.Pointer(&i.Data[0])) = vk
	*(*uint16)(unsafe.Pointer(&i.Data[2])) = scan
	*(*uint32)(unsafe.Pointer(&i.Data[4])) = flags
	return i
}
func (w *windowsGUI) send(ctx context.Context, i winInput) error { return w.sendNative(ctx, i, false) }
func (w *windowsGUI) sendNative(ctx context.Context, i winInput, release bool) error {
	w.nativeMu.Lock()
	defer w.nativeMu.Unlock()
	if !release {
		if err := w.check(ctx); err != nil {
			return err
		}
	} else if ctx.Err() != nil {
		return ctx.Err()
	}
	restore, err := winThread()
	if err != nil {
		return err
	}
	defer restore()
	if err = winInteractive(); err != nil {
		return err
	}
	n, _, _ := guiSendInput.Call(1, uintptr(unsafe.Pointer(&i)), unsafe.Sizeof(i))
	if n != 1 {
		return &Error{Code: "input_failed", Message: "输入事件未完全提交，检查目标应用权限与当前桌面", InputMayHaveApplied: true}
	}
	return nil
}
func winAbsolute(x, y float64, left, top, width, height int32) (int32, int32) {
	return int32((x - float64(left)) * 65535 / float64(width-1)), int32((y - float64(top)) * 65535 / float64(height-1))
}
func (w *windowsGUI) Mouse(ctx context.Context, e MouseEvent) error {
	move := func(c context.Context, p Point) error {
		restore, err := winThread()
		if err != nil {
			return err
		}
		defer restore()
		a, _, _ := guiMetrics.Call(76)
		b, _, _ := guiMetrics.Call(77)
		ww, _, _ := guiMetrics.Call(78)
		hh, _, _ := guiMetrics.Call(79)
		if int32(ww) <= 1 || int32(hh) <= 1 {
			return failure("no_gui", "虚拟桌面尺寸无效")
		}
		x, y := winAbsolute(p.X+e.Display.LogicalBounds.X, p.Y+e.Display.LogicalBounds.Y, int32(a), int32(b), int32(ww), int32(hh))
		return w.send(c, winMouseInput(x, y, 0, 0xc001))
	}
	button := func(c context.Context, b string, down bool) error {
		f := uint32(2)
		switch b {
		case "right":
			f = 8
		case "middle":
			f = 32
		}
		if !down {
			f *= 2
		}
		return w.sendNative(c, winMouseInput(0, 0, 0, f), !down)
	}
	scroll := func(c context.Context, x, y int) error {
		if y != 0 {
			if err := w.send(c, winMouseInput(0, 0, uint32(int32(-y*120)), 0x800)); err != nil {
				return err
			}
		}
		if x != 0 {
			return w.send(c, winMouseInput(0, 0, uint32(int32(x*120)), 0x1000))
		}
		return nil
	}
	return performMouse(ctx, e, move, button, scroll)
}
func winVK(key string) (uint16, bool) {
	if len(key) == 1 && ((key[0] >= 'A' && key[0] <= 'Z') || (key[0] >= '0' && key[0] <= '9')) {
		return uint16(key[0]), true
	}
	if len(key) > 1 && key[0] == 'F' {
		n, e := strconv.Atoi(key[1:])
		if e == nil && n >= 1 && n <= 24 {
			return uint16(0x6f + n), true
		}
	}
	v, ok := map[string]uint16{"Ctrl": 0x11, "Alt": 0x12, "Shift": 0x10, "Meta": 0x5b, "Enter": 13, "Tab": 9, "Escape": 27, "Space": 32, "Backspace": 8, "Delete": 46, "Insert": 45, "Home": 36, "End": 35, "PageUp": 33, "PageDown": 34, "Left": 37, "Up": 38, "Right": 39, "Down": 40}[key]
	return v, ok
}
func (w *windowsGUI) Key(ctx context.Context, keys []string) error {
	if err := w.check(ctx); err != nil {
		return err
	}
	for _, k := range keys {
		if _, ok := winVK(k); !ok {
			return failure("unsupported", "当前平台不支持指定按键")
		}
	}
	return performKeys(ctx, keys, func(c context.Context, k string, down bool) error {
		vk, _ := winVK(k)
		flags := uint32(0)
		if !down {
			flags = 2
		}
		if vk >= 33 && vk <= 46 || vk == 0x5b {
			flags |= 1
		}
		return w.sendNative(c, winKeyInput(vk, 0, flags), !down)
	})
}
func (w *windowsGUI) Text(ctx context.Context, in TextInput) (InputResult, error) {
	if in.Mode == "clipboard" {
		return InputResult{}, failure("unsupported", "Windows 后端使用原生 Unicode 文本输入，不支持显式剪贴板模式")
	}
	applied := false
	for _, u := range utf16.Encode([]rune(in.Text)) {
		if err := w.send(ctx, winKeyInput(0, u, 4)); err != nil {
			if applied {
				err = partialError(err)
			}
			return InputResult{InputMayHaveApplied: applied}, err
		}
		applied = true
		err := w.sendNative(context.Background(), winKeyInput(0, u, 6), true)
		if err != nil {
			return InputResult{InputMayHaveApplied: true}, partialError(err)
		}
	}
	return InputResult{Submitted: true, Mode: "direct"}, nil
}

var _ Backend = (*windowsGUI)(nil)
var _ Desktop = (*windowsGUI)(nil)
