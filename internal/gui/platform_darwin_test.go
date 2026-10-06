//go:build darwin

package gui

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"
	"unsafe"
)

func TestMacNativeGeometryABI(t *testing.T) {
	if unsafe.Sizeof(macPoint{}) != 16 || unsafe.Sizeof(macRect{}) != 32 || unsafe.Offsetof(macRect{}.Size) != 16 {
		t.Fatal("CoreGraphics 结构体布局不一致")
	}
}
func TestMacUnicodeAndCancellationRelease(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var units [][]uint16
	var posted, released []uintptr
	id := uintptr(10)
	a := &macAPI{trusted: func() bool { return true }, keyEvent: func(_ uintptr, _ uint16, _ bool) uintptr { id++; return id }, unicode: func(_ uintptr, n uintptr, p *uint16) {
		units = append(units, append([]uint16(nil), unsafe.Slice(p, int(n))...))
	}, post: func(_ uint32, p uintptr) {
		posted = append(posted, p)
		if len(posted) == 1 {
			cancel()
		}
	}, release: func(p uintptr) { released = append(released, p) }}
	m := &macGUI{api: a, cfg: DefaultConfig()}
	result, err := m.Text(ctx, TextInput{Text: "😀中文", Mode: "direct"})
	if err == nil || !result.InputMayHaveApplied {
		t.Fatalf("取消没有报告部分输入：%+v %v", result, err)
	}
	if !reflect.DeepEqual(units[:2], [][]uint16{{0xd83d, 0xde00}, {0xd83d, 0xde00}}) {
		t.Fatalf("Unicode 补充平面编码错误：%v", units)
	}
	if len(posted) != 2 || len(released) != 3 {
		t.Fatalf("取消后未释放按键与事件对象：%v %v", posted, released)
	}
}
func TestMacCloseStillReleasesHeldModifier(t *testing.T) {
	var posted int
	var eventFlags []uint64
	m := &macGUI{cfg: DefaultConfig()}
	m.api = &macAPI{trusted: func() bool { return true }, keyEvent: func(_ uintptr, _ uint16, _ bool) uintptr { return 1 }, flags: func(_ uintptr, f uint64) { eventFlags = append(eventFlags, f) }, post: func(_ uint32, _ uintptr) {
		posted++
		if posted == 1 {
			m.closed.Store(true)
		}
	}, release: func(uintptr) {}}
	err := m.Key(context.Background(), []string{"Ctrl", "V"})
	var e *Error
	if !errors.As(err, &e) || !e.InputMayHaveApplied {
		t.Fatalf("关闭后没有报告部分输入：%v", err)
	}
	if posted != 3 || eventFlags[len(eventFlags)-1] != 0 || m.mods.Load() != 0 {
		t.Fatalf("关闭后修饰键未被释放：%d %v", posted, eventFlags)
	}
}
func TestMacNativeCapture(t *testing.T) {
	if os.Getenv("REMOTE_MCP_GUI_NATIVE_TEST") != "1" {
		t.Skip("需要 macOS 桌面、屏幕录制权限及显式 REMOTE_MCP_GUI_NATIVE_TEST=1")
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
