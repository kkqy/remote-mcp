package gui

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"runtime"
	"sync"
	"testing"
	"time"
)

type fakeBackend struct {
	d    *fakeDesktop
	open func(context.Context) (Desktop, error)
}

func (b *fakeBackend) Probe(context.Context) (Status, error) {
	return Status{Backend: "fake", State: "available", Capabilities: b.d.Capabilities()}, nil
}
func (b *fakeBackend) Open(ctx context.Context) (Desktop, error) {
	if b.open != nil {
		return b.open(ctx)
	}
	return b.d, nil
}

type fakeDesktop struct {
	mu                                 sync.Mutex
	displays                           []Display
	caps                               Capabilities
	captureCalls, keyCalls, closeCalls int
	mouseEvents                        []MouseEvent
	key                                func(context.Context, []string) error
	capture                            func(context.Context, Display) (Frame, error)
	displaysHook                       func(context.Context) error
	closed                             chan struct{}
	closeHook                          func() error
}

func newFakeDesktop() *fakeDesktop {
	return &fakeDesktop{displays: []Display{{ID: "left", PixelWidth: 100, PixelHeight: 80, LogicalBounds: Bounds{-50, -20, 50, 40}, Primary: true, AbsoluteInput: true}}, caps: Capabilities{Screenshot: true, Mouse: true, Keyboard: true, Text: true, DirectText: true, Reasons: map[string]string{}}, closed: make(chan struct{})}
}
func (d *fakeDesktop) Capabilities() Capabilities {
	d.mu.Lock()
	defer d.mu.Unlock()
	return copyStatus(Status{Capabilities: d.caps}).Capabilities
}
func (d *fakeDesktop) Displays(ctx context.Context) ([]Display, error) {
	d.mu.Lock()
	displays, hook := append([]Display{}, d.displays...), d.displaysHook
	d.mu.Unlock()
	if hook != nil {
		if err := hook(ctx); err != nil {
			return nil, err
		}
	}
	return displays, nil
}
func (d *fakeDesktop) Capture(ctx context.Context, display Display) (Frame, error) {
	d.mu.Lock()
	d.captureCalls++
	fn := d.capture
	d.mu.Unlock()
	if fn != nil {
		return fn(ctx, display)
	}
	img := image.NewRGBA(image.Rect(0, 0, display.PixelWidth, display.PixelHeight))
	img.SetRGBA(20, 10, color.RGBA{255, 0, 0, 255})
	return Frame{img, time.Now(), "new_capture"}, nil
}
func (d *fakeDesktop) Mouse(_ context.Context, ev MouseEvent) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.mouseEvents = append(d.mouseEvents, ev)
	return nil
}
func (d *fakeDesktop) Key(ctx context.Context, keys []string) error {
	d.mu.Lock()
	d.keyCalls++
	fn := d.key
	d.mu.Unlock()
	if fn != nil {
		return fn(ctx, keys)
	}
	return nil
}
func (d *fakeDesktop) Text(context.Context, TextInput) (InputResult, error) {
	return InputResult{Submitted: true, Mode: "direct"}, nil
}
func (d *fakeDesktop) Close() error {
	d.mu.Lock()
	d.closeCalls++
	select {
	case <-d.closed:
	default:
		close(d.closed)
	}
	hook := d.closeHook
	d.mu.Unlock()
	if hook != nil {
		return hook()
	}
	return nil
}

func TestScreenshotDoesNotCertifyRemovedDisplay(t *testing.T) {
	d := newFakeDesktop()
	d.capture = func(_ context.Context, selected Display) (Frame, error) {
		d.mu.Lock()
		d.displays = []Display{{ID: "remaining", PixelWidth: 100, PixelHeight: 80, LogicalBounds: Bounds{100, 0, 100, 80}, AbsoluteInput: true}}
		d.mu.Unlock()
		return Frame{image.NewRGBA(image.Rect(0, 0, selected.PixelWidth, selected.PixelHeight)), time.Now(), "new_capture"}, nil
	}
	m, id := openFake(t, DefaultConfig(), d)
	result, err := m.Screenshot(context.Background(), ScreenshotInput{ID: id, DisplayID: "left"})
	assertCode(t, err, "capture_failed")
	if result.CaptureID != "" || len(result.PNG) != 0 {
		t.Fatal("已移除显示器生成了可用于输入的截图")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sessions[id].captures) != 0 {
		t.Fatal("已移除显示器被登记为最新代次坐标")
	}
}

func openFake(t *testing.T, cfg Config, d *fakeDesktop) (*Manager, string) {
	t.Helper()
	m, err := NewWithBackend(cfg, &fakeBackend{d: d})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	status, err := m.Open(context.Background(), OpenInput{RequestID: "test", WaitMS: 1000})
	if err != nil || status.State != "ready" {
		t.Fatalf("open: %+v %v", status, err)
	}
	return m, status.ID
}
func assertCode(t *testing.T, err error, code string) {
	t.Helper()
	var failure *Error
	if !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("错误码期待%s，实际%v", code, err)
	}
}
func TestCoordinatesCropLayoutAndInvalidPoint(t *testing.T) {
	d := newFakeDesktop()
	m, id := openFake(t, DefaultConfig(), d)
	s, err := m.Screenshot(context.Background(), ScreenshotInput{ID: id, Region: &Rect{20, 10, 40, 30}})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(s.PNG))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 40 || color.RGBAModel.Convert(decoded.At(0, 0)).(color.RGBA).R != 255 {
		t.Fatal("区域像素偏移错误")
	}
	if _, err := m.Mouse(context.Background(), MouseInput{ID: id, CaptureID: s.CaptureID, Action: "click", X: 10, Y: 6}); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	event := d.mouseEvents[0]
	d.mu.Unlock()
	if event.Start != (Point{15, 8}) || event.Display.LogicalBounds.X != -50 {
		t.Fatalf("混合缩放及负原点映射错误: %+v", event)
	}
	for _, point := range []Point{{math.NaN(), 0}, {math.Inf(1), 0}, {0, math.Inf(-1)}, {40, 0}, {-1, 0}, {0, 30}} {
		_, err := m.Mouse(context.Background(), MouseInput{ID: id, CaptureID: s.CaptureID, Action: "move", X: point.X, Y: point.Y})
		assertCode(t, err, "invalid_argument")
	}
	d.mu.Lock()
	if len(d.mouseEvents) != 1 {
		t.Fatal("无效输入不应执行")
	}
	d.displays[0].LogicalBounds.X = -100
	d.mu.Unlock()
	_, err = m.Mouse(context.Background(), MouseInput{ID: id, CaptureID: s.CaptureID, Action: "move", X: 1, Y: 1})
	assertCode(t, err, "stale_capture")
}
func TestScreenshotLimitsBeforeCaptureAndOverflow(t *testing.T) {
	d := newFakeDesktop()
	cfg := DefaultConfig()
	cfg.MaxPixels = 100
	m, id := openFake(t, cfg, d)
	_, err := m.Screenshot(context.Background(), ScreenshotInput{ID: id})
	assertCode(t, err, "limit_exceeded")
	d.mu.Lock()
	if d.captureCalls != 0 {
		t.Fatal("像素超限应在实际截图前拒绝")
	}
	d.displays[0].PixelWidth = math.MaxInt
	d.displays[0].PixelHeight = math.MaxInt
	d.mu.Unlock()
	_, err = m.Screenshot(context.Background(), ScreenshotInput{ID: id})
	assertCode(t, err, "limit_exceeded")
	d.mu.Lock()
	if d.captureCalls != 0 {
		t.Fatal("尺寸溢出不应调用后端")
	}
	d.mu.Unlock()
}
func TestCancelRetainsInputBusyUntilUnderlyingCompletion(t *testing.T) {
	d := newFakeDesktop()
	entered := make(chan struct{})
	release := make(chan struct{})
	d.key = func(context.Context, []string) error { close(entered); <-release; return nil }
	m, id := openFake(t, DefaultConfig(), d)
	ctx, cancel := context.WithCancel(context.Background())
	completed := make(chan error, 1)
	go func() { _, err := m.Key(ctx, KeyInput{id, []string{"Enter"}}); completed <- err }()
	<-entered
	cancel()
	err := <-completed
	var failure *Error
	if !errors.As(err, &failure) || !failure.InputMayHaveApplied {
		t.Fatalf("取消需要声明可能已提交: %v", err)
	}
	_, err = m.Key(context.Background(), KeyInput{id, []string{"Enter"}})
	assertCode(t, err, "busy")
	close(release)
	deadline := time.Now().Add(time.Second)
	for {
		m.mu.Lock()
		busy := m.sessions[id].busy
		m.mu.Unlock()
		if !busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("底层完成未释放busy")
		}
		time.Sleep(time.Millisecond)
	}
	d.mu.Lock()
	d.key = nil
	d.mu.Unlock()
	if _, err := m.Key(context.Background(), KeyInput{id, []string{"Enter"}}); err != nil {
		t.Fatal(err)
	}
}
func TestNormalResultPublicationAndSequentialCalls(t *testing.T) {
	d := newFakeDesktop()
	m, id := openFake(t, DefaultConfig(), d)
	for i := 0; i < 200; i++ {
		if _, err := m.Key(context.Background(), KeyInput{id, []string{"Ctrl", "V"}}); err != nil {
			t.Fatalf("正常输入被错误取消: %v", err)
		}
		if i < 30 {
			if _, err := m.Screenshot(context.Background(), ScreenshotInput{ID: id}); err != nil {
				t.Fatalf("正常截图被错误取消: %v", err)
			}
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.keyCalls != 200 {
		t.Fatal("顺序按键调用数量错误")
	}
}
func TestCloseInterruptsInputAndDeduplicatesClose(t *testing.T) {
	d := newFakeDesktop()
	entered := make(chan struct{})
	d.key = func(ctx context.Context, _ []string) error { close(entered); <-d.closed; return ctx.Err() }
	m, id := openFake(t, DefaultConfig(), d)
	ended := make(chan struct{})
	go func() { _, _ = m.Key(context.Background(), KeyInput{id, []string{"Enter"}}); close(ended) }()
	<-entered
	for i := 0; i < 2; i++ {
		status, err := m.CloseSession(context.Background(), IDInput{id})
		if err != nil || status.State != "closed" {
			t.Fatalf("重复关闭: %+v %v", status, err)
		}
	}
	<-ended
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closeCalls != 1 {
		t.Fatalf("后端应只关闭一次，实际%d", d.closeCalls)
	}
}
func TestOpenDedupConflictAndBoundedMetadata(t *testing.T) {
	d := newFakeDesktop()
	cfg := DefaultConfig()
	cfg.MaxCaptures = 2
	cfg.MaxRecords = 2
	m, id := openFake(t, cfg, d)
	again, err := m.Open(context.Background(), OpenInput{"test", 1000})
	if err != nil || again.ID != id {
		t.Fatal("同一请求创建了重复会话")
	}
	_, err = m.Open(context.Background(), OpenInput{"test", 1})
	assertCode(t, err, "conflict")
	_, err = m.Open(context.Background(), OpenInput{"other", 0})
	assertCode(t, err, "busy")
	first, err := m.Screenshot(context.Background(), ScreenshotInput{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := m.Screenshot(context.Background(), ScreenshotInput{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	_, err = m.Mouse(context.Background(), MouseInput{ID: id, CaptureID: first.CaptureID, Action: "move"})
	assertCode(t, err, "stale_capture")
	m.mu.Lock()
	if len(m.sessions[id].captures) != 2 {
		t.Fatal("坐标记录无界")
	}
	m.mu.Unlock()
}
func TestAuthorizationTimeoutAndRequestCancel(t *testing.T) {
	for _, cancelRequest := range []bool{false, true} {
		t.Run(map[bool]string{false: "timeout", true: "cancel"}[cancelRequest], func(t *testing.T) {
			d := newFakeDesktop()
			cfg := DefaultConfig()
			cfg.AuthorizationTimeout = 20 * time.Millisecond
			b := &fakeBackend{d: d, open: func(ctx context.Context) (Desktop, error) { <-ctx.Done(); return nil, ctx.Err() }}
			m, err := NewWithBackend(cfg, b)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelRequest {
				cancel()
			}
			status, err := m.Open(ctx, OpenInput{"auth", 1000})
			if cancelRequest {
				assertCode(t, err, "authorization_cancelled")
				m.mu.Lock()
				s := m.sessions[m.requests["auth"]]
				m.mu.Unlock()
				<-s.opened
				status, _ = m.Status(context.Background(), StatusInput{ID: s.status.ID})
			}
			expected := "authorization_timeout"
			if cancelRequest {
				expected = "authorization_cancelled"
			}
			if status.State != "failed" || status.Code != expected {
				t.Fatalf("授权结果: %+v %v", status, err)
			}
		})
	}
}
func TestInputHelpersReleaseAfterCancelAndFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var events []string
	err := performKeys(ctx, []string{"Ctrl", "V"}, func(ctx context.Context, key string, down bool) error {
		if ctx.Err() != nil {
			t.Fatal("释放操作错误沿用已取消上下文")
		}
		if down {
			events = append(events, key+"+")
			if key == "V" {
				cancel()
				return failure("input_failed", "Simulated failure")
			}
		} else {
			events = append(events, key+"-")
		}
		return nil
	})
	assertCode(t, err, "input_failed")
	if got := events; len(got) != 4 || got[2] != "V-" || got[3] != "Ctrl-" {
		t.Fatalf("组合键未逆序清理: %v", got)
	}
	ctx, cancel = context.WithCancel(context.Background())
	var buttons []bool
	err = performMouse(ctx, MouseEvent{Action: "drag", Button: "left", Duration: time.Second}, func(context.Context, Point) error { return nil }, func(ctx context.Context, _ string, down bool) error {
		if ctx.Err() != nil {
			t.Fatal("按钮释放上下文已取消")
		}
		buttons = append(buttons, down)
		if down {
			cancel()
		}
		return nil
	}, func(context.Context, int, int) error { return nil })
	if err == nil || len(buttons) != 2 || buttons[1] {
		t.Fatalf("拖拽取消后按钮未释放: %v %v", buttons, err)
	}
}
func TestStatusConcurrentLayoutAndCapabilityChanges(t *testing.T) {
	d := newFakeDesktop()
	m, id := openFake(t, DefaultConfig(), d)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	// 状态刷新与截图共用占用；闸门确定覆盖 busy，而不依赖调度碰巧重叠。
	entered, release := make(chan struct{}), make(chan struct{})
	d.displaysHook = func(ctx context.Context) error {
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	refreshDone := make(chan struct{})
	refreshResult := make(chan error, 1)
	t.Cleanup(func() {
		cancel()
		select {
		case <-refreshDone:
		case <-time.After(time.Second):
			t.Error("状态刷新后台调用未在取消后退出")
		}
	})
	go func() {
		defer close(refreshDone)
		_, err := m.Status(ctx, StatusInput{ID: id})
		refreshResult <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("状态刷新未进入受控闸门")
	}
	_, err := m.Screenshot(ctx, ScreenshotInput{ID: id})
	assertCode(t, err, "busy")
	close(release)
	select {
	case err := <-refreshResult:
		if err != nil {
			t.Fatalf("释放闸门后状态刷新失败：%v", err)
		}
	case <-ctx.Done():
		t.Fatal("释放闸门后状态刷新未完成")
	}
	d.mu.Lock()
	d.displaysHook = nil
	d.mu.Unlock()
	initial, err := m.Status(ctx, StatusInput{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	statusResult := make(chan error, 1)
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("并发状态查询未在取消后退出")
		}
	})
	go func() {
		defer close(done)
		for i := 0; i < 300; i++ {
			if _, err := m.Status(ctx, StatusInput{ID: id}); err != nil {
				statusResult <- err
				return
			}
		}
		statusResult <- nil
	}()
	for i := 0; i < 20; i++ {
		d.mu.Lock()
		d.displays[0].LogicalBounds.X = float64(i)
		d.caps.Reasons["mouse"] = "Layout changed"
		d.mu.Unlock()
		capture, err := m.Screenshot(ctx, ScreenshotInput{ID: id})
		if err != nil {
			assertCode(t, err, "busy")
		} else if capture.CaptureID == "" || len(capture.PNG) == 0 || capture.LogicalBounds.X != float64(i) {
			t.Fatalf("并发截图未返回当前布局及有效图片：%+v", capture)
		}
	}
	select {
	case <-done:
		if err := <-statusResult; err != nil {
			t.Fatalf("并发状态查询失败：%v", err)
		}
	case <-ctx.Done():
		t.Fatal("并发状态查询未在期限内结束")
	}
	// 并发阶段允许合法 busy，但必须实际恢复截图并读到最新布局及能力。
	capture, err := m.Screenshot(ctx, ScreenshotInput{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	status, err := m.Status(ctx, StatusInput{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	if capture.CaptureID == "" || len(capture.PNG) == 0 || capture.LogicalBounds.X != 19 || capture.LayoutGeneration <= initial.LayoutGeneration {
		t.Fatalf("占用释放后未完成最新布局截图：%+v", capture)
	}
	if len(status.Displays) != 1 || status.Displays[0].LogicalBounds.X != 19 || status.Capabilities.Reasons["mouse"] != "Layout changed" || status.LayoutGeneration != capture.LayoutGeneration {
		t.Fatalf("并发更新后的状态未刷新：%+v", status)
	}
}
func TestInvalidTextAndKeysNeverExecute(t *testing.T) {
	d := newFakeDesktop()
	cfg := DefaultConfig()
	cfg.MaxTextBytes = 4
	m, id := openFake(t, cfg, d)
	for _, keys := range [][]string{nil, {"Ctrl", "Control"}, {"unknown"}} {
		_, err := m.Key(context.Background(), KeyInput{id, keys})
		assertCode(t, err, "invalid_argument")
	}
	for _, text := range []string{"", string([]byte{0xff})} {
		_, err := m.Text(context.Background(), TextInput{ID: id, Text: text})
		assertCode(t, err, "invalid_argument")
	}
	_, err := m.Text(context.Background(), TextInput{ID: id, Text: "超限"})
	assertCode(t, err, "limit_exceeded")
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.keyCalls != 0 {
		t.Fatal("校验失败执行了输入")
	}
}

func TestStatusRefreshesCapabilitiesAndLayout(t *testing.T) {
	d := newFakeDesktop()
	m, id := openFake(t, DefaultConfig(), d)
	capture, err := m.Screenshot(context.Background(), ScreenshotInput{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	d.caps.Keyboard = false
	d.displays[0].LogicalBounds.X = -200
	d.mu.Unlock()
	status, err := m.Status(context.Background(), StatusInput{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	if status.Capabilities.Keyboard || status.Displays[0].LogicalBounds.X != -200 || status.LayoutGeneration <= capture.LayoutGeneration {
		t.Fatalf("状态未刷新实际权限或拓扑: %+v", status)
	}
	_, err = m.Mouse(context.Background(), MouseInput{ID: id, CaptureID: capture.CaptureID, Action: "click"})
	assertCode(t, err, "stale_capture")
}
func TestAlreadyCancelledInputDoesNotReachBackend(t *testing.T) {
	d := newFakeDesktop()
	m, id := openFake(t, DefaultConfig(), d)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := m.Key(ctx, KeyInput{id, []string{"Enter"}})
	assertCode(t, err, "authorization_cancelled")
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.keyCalls != 0 {
		t.Fatal("取消发生于调用前仍执行了输入")
	}
}

func TestBackendContextErrorsAlwaysHaveStableCodes(t *testing.T) {
	for _, test := range []struct {
		err  error
		code string
	}{{context.Canceled, "authorization_cancelled"}, {context.DeadlineExceeded, "timeout"}} {
		d := newFakeDesktop()
		d.key = func(context.Context, []string) error { return test.err }
		m, id := openFake(t, DefaultConfig(), d)
		_, err := m.Key(context.Background(), KeyInput{id, []string{"Enter"}})
		assertCode(t, err, test.code)
		var business *Error
		if !errors.As(err, &business) || !business.InputMayHaveApplied {
			t.Fatal("底层取消丢失部分输入标记")
		}
	}
}

func TestRepeatedClosePreservesCleanupError(t *testing.T) {
	d := newFakeDesktop()
	d.closeHook = func() error { return errors.New("不可泄漏的系统原始错误") }
	m, err := NewWithBackend(DefaultConfig(), &fakeBackend{d: d})
	if err != nil {
		t.Fatal(err)
	}
	status, err := m.Open(context.Background(), OpenInput{"close-error", 1000})
	if err != nil || status.State != "ready" {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		assertCode(t, m.Close(), "session_closed")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closeCalls != 1 {
		t.Fatal("重复close重复清理")
	}
}
func TestCancelledCloseRequestsDoNotAccumulateWorkers(t *testing.T) {
	d := newFakeDesktop()
	started := make(chan struct{})
	release := make(chan struct{})
	d.closeHook = func() error { close(started); <-release; return nil }
	m, id := openFake(t, DefaultConfig(), d)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = m.CloseSession(ctx, IDInput{id})
	<-started
	baseline := runtime.NumGoroutine()
	for i := 0; i < 200; i++ {
		_, err := m.CloseSession(ctx, IDInput{id})
		assertCode(t, err, "authorization_cancelled")
	}
	time.Sleep(10 * time.Millisecond)
	if runtime.NumGoroutine() > baseline+20 {
		close(release)
		t.Fatal("已取消重复close积累等待goroutine")
	}
	close(release)
	if _, err := m.CloseSession(context.Background(), IDInput{id}); err != nil {
		t.Fatal(err)
	}
}
