//go:build darwin

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

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

type macPoint struct{ X, Y float64 }
type macSize struct{ Width, Height float64 }
type macRect struct {
	Origin macPoint
	Size   macSize
}
type macAPI struct {
	list                     func(uint32, *uint32, *uint32) int32
	bounds                   func(uint32) macRect
	main                     func() uint32
	mode                     func(uint32) uintptr
	pixelWidth, pixelHeight  func(uintptr) uintptr
	rotation                 func(uint32) float64
	releaseMode              func(uintptr)
	preflight, requestScreen func() bool
	trusted                  func() bool
	event                    func(uintptr, uint32, macPoint, uint32) uintptr
	keyEvent                 func(uintptr, uint16, bool) uintptr
	unicode                  func(uintptr, uintptr, *uint16)
	flags                    func(uintptr, uint64)
	field                    func(uintptr, uint32, int64)
	scroll                   func(uintptr, uint32, uint32, int32, int32, int32) uintptr
	post                     func(uint32, uintptr)
	release                  func(uintptr)
	imageWidth, imageHeight  func(uintptr) uintptr
	imageRelease             func(uintptr)
	imageRetain              func(uintptr) uintptr
	legacyCapture            func(uint32) uintptr
	colorSpace               func() uintptr
	colorRelease             func(uintptr)
	bitmap                   func(unsafe.Pointer, uintptr, uintptr, uintptr, uintptr, uintptr, uint32) uintptr
	contextRelease           func(uintptr)
	translate                func(uintptr, float64, float64)
	scale                    func(uintptr, float64, float64)
	draw                     func(uintptr, macRect, uintptr)
}

var macScreenSlot = make(chan struct{}, 1)
var macPermissionOnce sync.Once
var macPermissionDone = make(chan struct{})
var macLoadOnce sync.Once
var macLoaded *macAPI
var macLoadErr error

// 框架句柄在进程期间保持加载；失败不影响其他服务模块。
func loadMacAPI() (*macAPI, error) {
	macLoadOnce.Do(func() {
		a := &macAPI{}
		cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			macLoadErr = failure("dependency_missing", "无法加载 CoreGraphics")
			return
		}
		ax, err := purego.Dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			macLoadErr = failure("dependency_missing", "无法加载 ApplicationServices")
			return
		}
		if _, err = purego.Dlopen("/System/Library/Frameworks/Foundation.framework/Foundation", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
			macLoadErr = failure("dependency_missing", "无法加载 Foundation")
			return
		}
		bind := func(dst any, h uintptr, name string) bool {
			p, e := purego.Dlsym(h, name)
			if e != nil {
				macLoadErr = failure("unsupported", "当前系统缺少必要的图形接口")
				return false
			}
			purego.RegisterFunc(dst, p)
			return true
		}
		bindings := []struct {
			dst  any
			name string
		}{
			{&a.list, "CGGetActiveDisplayList"}, {&a.bounds, "CGDisplayBounds"}, {&a.main, "CGMainDisplayID"}, {&a.mode, "CGDisplayCopyDisplayMode"}, {&a.pixelWidth, "CGDisplayModeGetPixelWidth"}, {&a.pixelHeight, "CGDisplayModeGetPixelHeight"}, {&a.rotation, "CGDisplayRotation"}, {&a.releaseMode, "CGDisplayModeRelease"},
			{&a.preflight, "CGPreflightScreenCaptureAccess"}, {&a.requestScreen, "CGRequestScreenCaptureAccess"}, {&a.event, "CGEventCreateMouseEvent"}, {&a.keyEvent, "CGEventCreateKeyboardEvent"}, {&a.unicode, "CGEventKeyboardSetUnicodeString"}, {&a.flags, "CGEventSetFlags"}, {&a.field, "CGEventSetIntegerValueField"}, {&a.scroll, "CGEventCreateScrollWheelEvent2"}, {&a.post, "CGEventPost"},
			{&a.imageWidth, "CGImageGetWidth"}, {&a.imageHeight, "CGImageGetHeight"}, {&a.imageRelease, "CGImageRelease"}, {&a.imageRetain, "CGImageRetain"}, {&a.colorSpace, "CGColorSpaceCreateDeviceRGB"}, {&a.colorRelease, "CGColorSpaceRelease"}, {&a.bitmap, "CGBitmapContextCreate"}, {&a.contextRelease, "CGContextRelease"}, {&a.translate, "CGContextTranslateCTM"}, {&a.scale, "CGContextScaleCTM"}, {&a.draw, "CGContextDrawImage"},
		}
		for _, b := range bindings {
			if !bind(b.dst, cg, b.name) {
				return
			}
		}
		if !bind(&a.release, ax, "CFRelease") || !bind(&a.trusted, ax, "AXIsProcessTrusted") {
			return
		}
		// macOS 14 及以后选 SCScreenshotManager；旧系统保留原生截图接口。
		_, _ = purego.Dlopen("/System/Library/Frameworks/ScreenCaptureKit.framework/ScreenCaptureKit", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if objc.GetClass("SCScreenshotManager") == 0 {
			p, e := purego.Dlsym(cg, "CGDisplayCreateImage")
			if e == nil {
				purego.RegisterFunc(&a.legacyCapture, p)
			}
		}
		if objc.GetClass("SCScreenshotManager") == 0 && a.legacyCapture == nil {
			macLoadErr = failure("unsupported", "系统没有可用的原生屏幕采集接口")
			return
		}
		macLoaded = a
	})
	return macLoaded, macLoadErr
}

type macGUI struct {
	cfg         Config
	api         *macAPI
	closed      atomic.Bool
	captureBusy atomic.Bool
	mods        atomic.Uint64
	nativeMu    sync.Mutex
}

func newPlatformBackend(cfg Config) Backend { return &macGUI{cfg: cfg} }
func (m *macGUI) check(ctx context.Context) error {
	if m.closed.Load() {
		return failure("session_closed", "图形会话已关闭")
	}
	return ctx.Err()
}
func (m *macGUI) Capabilities() Capabilities {
	a, e := loadMacAPI()
	if e != nil {
		return Capabilities{Reasons: map[string]string{"backend": "原生图形框架不可用"}}
	}
	screen, input := a.preflight(), a.trusted()
	reasons := map[string]string{"clipboard": "原生 Unicode 输入可用；未提供剪贴板模式"}
	if !screen {
		reasons["screenshot"] = "需要系统屏幕录制权限"
	}
	if !input {
		reasons["input"] = "需要系统辅助功能权限"
	}
	return Capabilities{Screenshot: screen, Mouse: input, Keyboard: input, Text: input, DirectText: input, Reasons: reasons}
}
func (m *macGUI) Probe(ctx context.Context) (Status, error) {
	_, e := loadMacAPI()
	if e != nil {
		return Status{Backend: "macos", State: "unavailable"}, e
	}
	ds, e := m.Displays(ctx)
	st := Status{Backend: "macos", State: "available", Capabilities: m.Capabilities(), Displays: ds}
	if e != nil {
		st.State = "unavailable"
	}
	return st, e
}
func (m *macGUI) Open(ctx context.Context) (Desktop, error) {
	a, e := loadMacAPI()
	if e != nil {
		return nil, e
	}
	d := &macGUI{cfg: m.cfg, api: a}
	if _, e = d.Displays(ctx); e != nil {
		return nil, e
	}
	if !a.preflight() {
		// 权限请求仅发生在显式打开。系统 UI 不可被 Go 取消；等待仍由管理器有界处理。
		macPermissionOnce.Do(func() { go func() { a.requestScreen(); close(macPermissionDone) }() })
		select {
		case <-macPermissionDone:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if e = d.check(ctx); e != nil {
		return nil, e
	}
	if !a.preflight() && !a.trusted() {
		return nil, failure("permission_denied", "需要在系统设置授予屏幕录制或辅助功能权限")
	}
	return d, nil
}
func (m *macGUI) Close() error {
	m.nativeMu.Lock()
	defer m.nativeMu.Unlock()
	m.closed.Store(true)
	return nil
}
func (m *macGUI) Displays(ctx context.Context) ([]Display, error) {
	if e := m.check(ctx); e != nil {
		return nil, e
	}
	a, e := loadMacAPI()
	if e != nil {
		return nil, e
	}
	var count uint32
	if a.list(0, nil, &count) != 0 || count == 0 {
		return nil, failure("no_gui", "未找到当前用户可访问的显示器")
	}
	if count > 128 {
		return nil, failure("limit_exceeded", "显示器数量超过上限")
	}
	ids := make([]uint32, count)
	if a.list(count, &ids[0], &count) != 0 {
		return nil, failure("no_gui", "读取显示器布局失败")
	}
	out := make([]Display, 0, count)
	for _, id := range ids[:count] {
		b := a.bounds(id)
		mode := a.mode(id)
		if mode == 0 {
			return nil, failure("capture_failed", "读取显示器像素模式失败")
		}
		w, h := int(a.pixelWidth(mode)), int(a.pixelHeight(mode))
		a.releaseMode(mode)
		r := int(a.rotation(id)+0.5) % 360
		if r == 90 || r == 270 {
			w, h = h, w
		}
		if w <= 0 || h <= 0 || b.Size.Width <= 0 || b.Size.Height <= 0 {
			return nil, failure("capture_failed", "显示器尺寸无效")
		}
		out = append(out, Display{ID: strconv.FormatUint(uint64(id), 10), Primary: id == a.main(), PixelWidth: w, PixelHeight: h, LogicalBounds: Bounds{b.Origin.X, b.Origin.Y, b.Size.Width, b.Size.Height}, AbsoluteInput: true})
	}
	return out, nil
}

// 回调参数在退出回调后失效，必须在回调中 retain；等待取消仍等待回调到达后释放。
type macAsyncResult struct {
	object uintptr
	failed bool
	at     time.Time
}

func (m *macGUI) captureModern(ctx context.Context, id uint32, d Display) (uintptr, time.Time, error) {
	select {
	case macScreenSlot <- struct{}{}:
	case <-ctx.Done():
		return 0, time.Time{}, ctx.Err()
	}
	handoff := false
	defer func() {
		if !handoff {
			<-macScreenSlot
		}
	}()
	// SCShareableContent 是 Objective-C 对象，与 CGImage 使用不同释放规则。
	ch := make(chan objc.ID, 1)
	block := objc.NewBlock(func(_ objc.Block, content objc.ID, err objc.ID) {
		if err != 0 {
			ch <- 0
			return
		}
		content.Send(objc.RegisterName("retain"))
		ch <- content
	})
	objc.ID(objc.GetClass("SCShareableContent")).Send(objc.RegisterName("getShareableContentExcludingDesktopWindows:onScreenWindowsOnly:completionHandler:"), false, true, block)
	block.Release()
	var content objc.ID
	select {
	case content = <-ch:
	case <-ctx.Done():
		handoff = true
		go func() {
			v := <-ch
			if v != 0 {
				v.Send(objc.RegisterName("release"))
			}
			<-macScreenSlot
		}()
		return 0, time.Time{}, ctx.Err()
	}
	if content == 0 {
		return 0, time.Time{}, failure("capture_failed", "获取可采集显示器失败，检查屏幕录制权限")
	}
	defer content.Send(objc.RegisterName("release"))
	displays := content.Send(objc.RegisterName("displays"))
	n := objc.Send[uintptr](displays, objc.RegisterName("count"))
	var target objc.ID
	for i := uintptr(0); i < n; i++ {
		candidate := displays.Send(objc.RegisterName("objectAtIndex:"), i)
		if objc.Send[uint32](candidate, objc.RegisterName("displayID")) == id {
			target = candidate
			break
		}
	}
	if target == 0 {
		return 0, time.Time{}, failure("stale_capture", "指定显示器当前不可采集")
	}
	empty := objc.ID(objc.GetClass("NSArray")).Send(objc.RegisterName("array"))
	filter := objc.ID(objc.GetClass("SCContentFilter")).Send(objc.RegisterName("alloc")).Send(objc.RegisterName("initWithDisplay:excludingWindows:"), target, empty)
	if filter == 0 {
		return 0, time.Time{}, failure("capture_failed", "创建显示器采集过滤器失败")
	}
	cfg := objc.ID(objc.GetClass("SCStreamConfiguration")).Send(objc.RegisterName("new"))
	if cfg == 0 {
		filter.Send(objc.RegisterName("release"))
		return 0, time.Time{}, failure("capture_failed", "创建屏幕采集配置失败")
	}
	cfg.Send(objc.RegisterName("setWidth:"), uintptr(d.PixelWidth))
	cfg.Send(objc.RegisterName("setHeight:"), uintptr(d.PixelHeight))
	cfg.Send(objc.RegisterName("setShowsCursor:"), false)
	cfg.Send(objc.RegisterName("setPixelFormat:"), uint32(0x42475241)) // kCVPixelFormatType_32BGRA
	images := make(chan macAsyncResult, 1)
	captureBlock := objc.NewBlock(func(_ objc.Block, obj uintptr, err objc.ID) {
		if obj != 0 && err == 0 {
			m.api.imageRetain(obj)
		}
		filter.Send(objc.RegisterName("release"))
		cfg.Send(objc.RegisterName("release"))
		images <- macAsyncResult{obj, err != 0, time.Now()}
	})
	objc.ID(objc.GetClass("SCScreenshotManager")).Send(objc.RegisterName("captureImageWithFilter:configuration:completionHandler:"), filter, cfg, captureBlock)
	captureBlock.Release()
	select {
	case r := <-images:
		if r.failed || r.object == 0 {
			return 0, time.Time{}, failure("capture_failed", "系统屏幕采集失败，检查屏幕录制授权")
		}
		return r.object, r.at, nil
	case <-ctx.Done():
		handoff = true
		go func() {
			r := <-images
			if r.object != 0 && !r.failed {
				m.api.imageRelease(r.object)
			}
			<-macScreenSlot
		}()
		return 0, time.Time{}, ctx.Err()
	}

}
func (m *macGUI) Capture(ctx context.Context, d Display) (Frame, error) {
	if e := m.check(ctx); e != nil {
		return Frame{}, e
	}
	if !m.api.preflight() {
		return Frame{}, failure("permission_denied", "未获系统屏幕录制权限")
	}
	if d.PixelWidth <= 0 || d.PixelHeight <= 0 || d.PixelWidth > m.cfg.MaxPixels/d.PixelHeight {
		return Frame{}, failure("limit_exceeded", "显示器截图超过像素上限")
	}
	if !m.captureBusy.CompareAndSwap(false, true) {
		return Frame{}, failure("busy", "屏幕采集正在进行")
	}
	defer m.captureBusy.Store(false)
	id, e := strconv.ParseUint(d.ID, 10, 32)
	if e != nil {
		return Frame{}, failure("invalid_argument", "显示器标识无效")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(objc.RegisterName("new"))
	defer pool.Send(objc.RegisterName("drain"))
	var cg uintptr
	var at time.Time
	if objc.GetClass("SCScreenshotManager") != 0 {
		cg, at, e = m.captureModern(ctx, uint32(id), d)
		if e != nil {
			return Frame{}, e
		}
	} else {
		cg = m.api.legacyCapture(uint32(id))
		at = time.Now()
	}
	if cg == 0 {
		return Frame{}, failure("capture_failed", "原生屏幕采集没有返回图像")
	}
	defer m.api.imageRelease(cg)
	w, h := int(m.api.imageWidth(cg)), int(m.api.imageHeight(cg))
	if w <= 0 || h <= 0 || w > m.cfg.MaxPixels/h {
		return Frame{}, failure("limit_exceeded", "原生截图超过像素上限")
	}
	if w != d.PixelWidth || h != d.PixelHeight {
		return Frame{}, failure("stale_capture", "显示器像素尺寸已变化，请重新读取布局")
	}
	if e = m.check(ctx); e != nil {
		return Frame{}, e
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	color := m.api.colorSpace()
	if color == 0 {
		return Frame{}, failure("capture_failed", "创建截图颜色空间失败")
	}
	defer m.api.colorRelease(color)
	bitmap := m.api.bitmap(unsafe.Pointer(&img.Pix[0]), uintptr(w), uintptr(h), 8, uintptr(img.Stride), color, 0x4001)
	if bitmap == 0 {
		return Frame{}, failure("capture_failed", "创建截图位图上下文失败")
	}
	m.api.translate(bitmap, 0, float64(h))
	m.api.scale(bitmap, 1, -1)
	m.api.draw(bitmap, macRect{Size: macSize{float64(w), float64(h)}}, cg)
	m.api.contextRelease(bitmap)
	runtime.KeepAlive(img)
	return Frame{img, at, "fresh"}, nil
}
func (m *macGUI) inputCheck(ctx context.Context) error {
	if e := m.check(ctx); e != nil {
		return e
	}
	if !m.api.trusted() {
		return failure("permission_denied", "未获系统辅助功能权限")
	}
	return nil
}
func (m *macGUI) post(ctx context.Context, e uintptr) error { return m.postNative(ctx, e, false) }
func (m *macGUI) postNative(ctx context.Context, e uintptr, release bool) error {
	m.nativeMu.Lock()
	defer m.nativeMu.Unlock()
	if e == 0 {
		return failure("input_failed", "创建原生输入事件失败")
	}
	defer m.api.release(e)
	if !release {
		if err := m.inputCheck(ctx); err != nil {
			return err
		}
	} else if ctx.Err() != nil {
		return ctx.Err()
	}
	m.api.post(0, e)
	return nil
}
func (m *macGUI) Mouse(ctx context.Context, e MouseEvent) error {
	if err := m.inputCheck(ctx); err != nil {
		return err
	}
	var held bool
	var point macPoint
	button := uint32(0)
	if e.Button == "right" {
		button = 1
	} else if e.Button == "middle" {
		button = 2
	}
	clicks := int64(1)
	presses := 0
	move := func(c context.Context, p Point) error {
		point = macPoint{p.X + e.Display.LogicalBounds.X, p.Y + e.Display.LogicalBounds.Y}
		kind := uint32(5)
		if held {
			kind = 6
			if button == 1 {
				kind = 7
			} else if button == 2 {
				kind = 27
			}
		}
		return m.post(c, m.api.event(0, kind, point, button))
	}
	press := func(c context.Context, _ string, down bool) error {
		kind := uint32(1)
		if button == 1 {
			kind = 3
		} else if button == 2 {
			kind = 25
		}
		if !down {
			kind++
			held = false
		} else {
			held = true
			presses++
			if e.Action == "double_click" && presses == 2 {
				clicks = 2
			}
		}
		event := m.api.event(0, kind, point, button)
		if event != 0 {
			m.api.field(event, 1, clicks)
		}
		return m.postNative(c, event, !down)
	}
	scroll := func(c context.Context, x, y int) error {
		return m.post(c, m.api.scroll(0, 1, 2, int32(-y), int32(-x), 0))
	}
	return performMouse(ctx, e, move, press, scroll)
}
func macKeyCode(k string) (uint16, bool) {
	v, ok := map[string]uint16{"A": 0, "S": 1, "D": 2, "F": 3, "H": 4, "G": 5, "Z": 6, "X": 7, "C": 8, "V": 9, "B": 11, "Q": 12, "W": 13, "E": 14, "R": 15, "Y": 16, "T": 17, "1": 18, "2": 19, "3": 20, "4": 21, "6": 22, "5": 23, "9": 25, "7": 26, "8": 28, "0": 29, "O": 31, "U": 32, "I": 34, "P": 35, "Enter": 36, "L": 37, "J": 38, "K": 40, "N": 45, "M": 46, "Tab": 48, "Space": 49, "Backspace": 51, "Escape": 53, "Meta": 55, "Shift": 56, "Alt": 58, "Ctrl": 59, "F17": 64, "F18": 79, "F19": 80, "F20": 90, "F5": 96, "F6": 97, "F7": 98, "F3": 99, "F8": 100, "F9": 101, "F11": 103, "F13": 105, "F16": 106, "F14": 107, "F10": 109, "F12": 111, "F15": 113, "Insert": 114, "Home": 115, "PageUp": 116, "Delete": 117, "F4": 118, "End": 119, "F2": 120, "PageDown": 121, "F1": 122, "Left": 123, "Right": 124, "Down": 125, "Up": 126}[k]
	return v, ok
}
func macModifier(k string) uint64 {
	switch k {
	case "Shift":
		return 1 << 17
	case "Ctrl":
		return 1 << 18
	case "Alt":
		return 1 << 19
	case "Meta":
		return 1 << 20
	}
	return 0
}
func (m *macGUI) Key(ctx context.Context, keys []string) error {
	if e := m.inputCheck(ctx); e != nil {
		return e
	}
	for _, k := range keys {
		if _, ok := macKeyCode(k); !ok {
			return failure("unsupported", "当前平台不支持指定按键")
		}
	}
	return performKeys(ctx, keys, func(c context.Context, k string, down bool) error {
		vk, _ := macKeyCode(k)
		f := m.mods.Load()
		if down {
			f |= macModifier(k)
		} else {
			f &^= macModifier(k)
		}
		m.mods.Store(f)
		event := m.api.keyEvent(0, vk, down)
		if event != 0 {
			m.api.flags(event, f)
		}
		return m.postNative(c, event, !down)
	})
}
func (m *macGUI) Text(ctx context.Context, in TextInput) (InputResult, error) {
	if in.Mode == "clipboard" {
		return InputResult{}, failure("unsupported", "macOS 后端使用原生 Unicode 输入，不支持显式剪贴板模式")
	}
	if e := m.inputCheck(ctx); e != nil {
		return InputResult{}, e
	}
	applied := false
	for _, r := range in.Text {
		u := utf16.Encode([]rune{r})
		event := m.api.keyEvent(0, 0, true)
		if event == 0 {
			err := failure("input_failed", "创建文本输入事件失败")
			if applied {
				err = partialError(err)
			}
			return InputResult{InputMayHaveApplied: applied}, err
		}
		m.api.unicode(event, uintptr(len(u)), &u[0])
		if e := m.post(ctx, event); e != nil {
			if applied {
				e = partialError(e)
			}
			return InputResult{InputMayHaveApplied: applied}, e
		}
		applied = true
		up := m.api.keyEvent(0, 0, false)
		if up != 0 {
			m.api.unicode(up, uintptr(len(u)), &u[0])
		}
		if e := m.postNative(context.Background(), up, true); e != nil {
			return InputResult{InputMayHaveApplied: true}, partialError(e)
		}
		runtime.KeepAlive(u)
	}
	return InputResult{Submitted: true, Mode: "direct"}, nil
}

var _ Backend = (*macGUI)(nil)
var _ Desktop = (*macGUI)(nil)
