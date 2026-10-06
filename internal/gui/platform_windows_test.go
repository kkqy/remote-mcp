//go:build windows

package gui

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
	"unsafe"
)

func TestWindowsInputABI(t *testing.T) {
	if unsafe.Sizeof(winInput{}) != 40 || unsafe.Offsetof(winInput{}.Data) != 8 || unsafe.Sizeof(winBitmapInfo{}) != 40 || unsafe.Sizeof(winMonitor{}) != 104 {
		t.Fatal("Win32 结构体布局与 64 位 API 契约不一致")
	}
	i := winKeyInput(0, 0x4e2d, 6)
	if i.Kind != 1 || *(*uint16)(unsafe.Pointer(&i.Data[2])) != 0x4e2d || *(*uint32)(unsafe.Pointer(&i.Data[4])) != 6 {
		t.Fatal("Unicode 按键字段位置错误")
	}
}
func TestWindowsNegativeVirtualDesktop(t *testing.T) {
	x, y := winAbsolute(-1920, -1080, -1920, -1080, 3840, 2160)
	if x != 0 || y != 0 {
		t.Fatalf("负坐标起点映射错误：%d,%d", x, y)
	}
	x, y = winAbsolute(1919, 1079, -1920, -1080, 3840, 2160)
	if x != 65535 || y != 65535 {
		t.Fatalf("虚拟桌面终点映射错误：%d,%d", x, y)
	}
}
func TestWindowsClosedInputDoesNotSubmit(t *testing.T) {
	w := &windowsGUI{cfg: DefaultConfig()}
	_ = w.Close()
	err := w.Key(context.Background(), []string{"Ctrl", "V"})
	var e *Error
	if !errors.As(err, &e) || e.Code != "session_closed" {
		t.Fatalf("已关闭会话没有拒绝按键：%v", err)
	}
}

// 显式开启后在当前桌面进行截图；普通 CI 不把跳过当作原生验收。
func TestWindowsNativeCapture(t *testing.T) {
	if os.Getenv("REMOTE_MCP_GUI_NATIVE_TEST") != "1" {
		t.Skip("需要 Windows 交互桌面及显式 REMOTE_MCP_GUI_NATIVE_TEST=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	d, e := newPlatformBackend(DefaultConfig()).Open(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	ds, e := d.Displays(ctx)
	if e != nil || len(ds) == 0 {
		t.Fatalf("显示器读取失败：%v", e)
	}
	for _, display := range ds {
		f, e := d.Capture(ctx, display)
		if e != nil {
			t.Fatal(e)
		}
		if f.Image.Bounds().Dx() != display.PixelWidth || f.Image.Bounds().Dy() != display.PixelHeight || f.CapturedAt.IsZero() || f.Freshness != "fresh" {
			t.Fatal("截图尺寸或采集元数据错误")
		}
	}
}
