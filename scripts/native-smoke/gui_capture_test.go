package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func captureFixture() (object, fixtureRect) {
	region := fixtureRect{70, 80, 640, 420}
	img := image.NewRGBA(image.Rect(0, 0, region.Width, region.Height))
	points := []image.Point{{20, 20}, {620, 20}, {20, 400}, {620, 400}}
	for index, point := range points {
		draw.Draw(img, image.Rect(point.X-8, point.Y-8, point.X+9, point.Y+9), &image.Uniform{fixtureColors[index]}, image.Point{}, draw.Src)
	}
	var output bytes.Buffer
	if err := png.Encode(&output, img); err != nil {
		panic(err)
	}
	return object{"structuredContent": object{"capture_id": "owned", "display_id": "display", "width": float64(640), "height": float64(420), "region": object{"x": float64(70), "y": float64(80), "width": float64(640), "height": float64(420)}, "captured_at": "2026-10-06T10:00:00Z", "captured_at_source": "acquired_at", "freshness": "fresh", "frame_sequence": float64(1), "layout_generation": float64(1)}, "content": []any{object{"type": "image", "mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(output.Bytes())}}}, region
}
func expectPanic(t *testing.T, run func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Error("无效截图或协议响应应被拒绝")
		}
	}()
	run()
}

// 在实际像素校验中取消，覆盖拿到好帧后、发布成功前的取消窗口。
type cancelDuringPixelValidation struct {
	image.Image
	cancel context.CancelFunc
}

func (img cancelDuringPixelValidation) At(x, y int) color.Color {
	img.cancel()
	return img.Image.At(x, y)
}
func TestGUICaptureDecodesPixelsAndRejectsWrongRegionImageAndFreshness(t *testing.T) {
	result, region := captureFixture()
	_, img, _ := decodeGUICapture(result, region)
	verifyFixtureMarkers(img)
	for _, mutate := range []func(object){
		func(o object) { obj(o["structuredContent"])["freshness"] = "latest_available" },
		func(o object) { obj(obj(o["structuredContent"])["region"])["x"] = float64(0) },
		func(o object) { obj(rows(o["content"])[0])["mimeType"] = "image/jpeg" },
		func(o object) { obj(rows(o["content"])[0])["data"] = "invalid!" },
		func(o object) { o["isError"] = true },
	} {
		bad, _ := captureFixture()
		mutate(bad)
		expectPanic(t, func() { decodeGUICapture(bad, region) })
	}
	changed := image.NewRGBA(img.Bounds())
	draw.Draw(changed, changed.Bounds(), img, image.Point{}, draw.Src)
	changed.Set(20, 20, fixtureColors[1])
	expectPanic(t, func() { verifyFixtureMarkers(changed) })
}
func TestProtocolGUIPayloadLimitIsExplicitAndLegacyLimitRemains(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"jsonrpc":"2.0","result":{"padding":"`))
		w.Write(bytes.Repeat([]byte{'x'}, 2<<20))
		w.Write([]byte(`"}}`))
	}))
	defer server.Close()
	legacy := &protocol{url: server.URL, client: server.Client()}
	expectPanic(t, func() { legacy.rpc("test", object{}, false) })
	gui := &protocol{url: server.URL, client: server.Client(), responseLimit: 24 << 20}
	ensure(gui.rpc("test", object{}, false)["padding"] != nil, "Explicit GUI response limit failed")
}

// 只读等待允许延迟呈现，不改变精确断言，也不能接受超时后得到的好图。
func TestFixtureMarkerWaitUsesLatestFrameAndRejectsPersistentOrLatePixels(t *testing.T) {
	result, region := captureFixture()
	metadata, good, raw := decodeGUICapture(result, region)
	bad := image.NewRGBA(good.Bounds())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	calls := 0
	frame := awaitFixtureCapture(ctx, time.Nanosecond, func(context.Context, int) fixtureCapture {
		calls++
		if calls == 1 {
			return fixtureCapture{img: bad}
		}
		return fixtureCapture{metadata, good, raw}
	})
	if calls != 2 || frame.metadata["capture_id"] != "owned" || !bytes.Equal(frame.raw, raw) {
		t.Fatal("必须使用最后实际核验成功的截图")
	}
	var failure any
	calls = 0
	func() {
		defer func() { failure = recover() }()
		awaitFixtureCapture(ctx, time.Nanosecond, func(context.Context, int) fixtureCapture { calls++; return fixtureCapture{img: bad} })
	}()
	if calls != 61 || !strings.Contains(fmt.Sprint(failure), "Fixture markers were obscured or misplaced") {
		t.Fatal("持续不匹配必须在61次上限拒绝", calls, failure)
	}
	late, stop := context.WithCancel(context.Background())
	expectPanic(t, func() {
		awaitFixtureCapture(late, time.Nanosecond, func(context.Context, int) fixtureCapture { stop(); return fixtureCapture{img: good} })
	})
	validating, stopValidation := context.WithCancel(context.Background())
	defer stopValidation()
	expectPanic(t, func() {
		awaitFixtureCapture(validating, time.Nanosecond, func(context.Context, int) fixtureCapture {
			return fixtureCapture{img: cancelDuringPixelValidation{good, stopValidation}}
		})
	})
	calls = 0
	expectPanic(t, func() {
		awaitFixtureCapture(late, time.Nanosecond, func(context.Context, int) fixtureCapture { calls++; return fixtureCapture{img: good} })
	})
	if calls != 0 {
		t.Fatal("已取消不能再次获取截图")
	}
}

func TestFixtureMarkerWaitDeadlineAndGuardFailureStopReadAttempts(t *testing.T) {
	result, region := captureFixture()
	_, img, _ := decodeGUICapture(result, region)
	bad := image.NewRGBA(img.Bounds())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	calls := 0
	started := time.Now()
	expectPanic(t, func() {
		awaitFixtureCapture(ctx, time.Millisecond, func(context.Context, int) fixtureCapture { calls++; return fixtureCapture{img: bad} })
	})
	if calls == 0 || calls > 61 || time.Since(started) > time.Second {
		t.Fatal("读图总等待必须有界")
	}
	calls = 0
	expectPanic(t, func() {
		awaitFixtureCapture(context.Background(), time.Nanosecond, func(context.Context, int) fixtureCapture { calls++; panic("Fixture ownership or geometry changed") })
	})
	if calls != 1 {
		t.Fatal("守卫失败不能重试读取或输入")
	}
}

func TestProtocolGUICaptureDeadlineCancelsActualHTTPRead(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	p := &protocol{url: server.URL, client: server.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	expectPanic(t, func() { p.rpcContext(ctx, "tools/call", object{}, false) })
	if ctx.Err() == nil || time.Since(started) > time.Second {
		t.Fatal("GUI截图必须使用共享context截止时间")
	}
}
