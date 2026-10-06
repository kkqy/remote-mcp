package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"time"
)

const guiTestText = "\u4f60\u597d\uff0cWindows GUI \U0001f642"

type fixtureRect struct{ X, Y, Width, Height int }

func (r fixtureRect) object() object {
	return object{"x": r.X, "y": r.Y, "width": r.Width, "height": r.Height}
}

var fixtureColors = []color.RGBA{{240, 20, 80, 255}, {20, 200, 90, 255}, {30, 100, 240, 255}, {220, 170, 20, 255}}

// 先读 PNG 尺寸和预算，再完整解码；图片及元数据必须相互一致。
func decodeGUICapture(result object, expected fixtureRect) (object, image.Image, []byte) {
	metadata := decodeToolResult(result, "gui_screenshot", false)
	ensure(number(metadata["width"]) == int64(expected.Width) && number(metadata["height"]) == int64(expected.Height), "GUI capture dimensions mismatch")
	region := obj(metadata["region"])
	for name, value := range expected.object() {
		ensure(number(region[name]) == int64(value.(int)), "GUI capture region mismatch")
	}
	captureID, captureOK := metadata["capture_id"].(string)
	displayID, displayOK := metadata["display_id"].(string)
	ensure(captureOK && displayOK && captureID != "" && displayID != "", "GUI capture identifiers were missing")
	ensure(metadata["captured_at_source"] == "acquired_at" && metadata["freshness"] == "fresh", "GUI capture freshness mismatch")
	stamp, ok := metadata["captured_at"].(string)
	ensure(ok, "GUI capture timestamp was missing")
	_, err := time.Parse(time.RFC3339Nano, stamp)
	check(err)
	ensure(number(metadata["frame_sequence"]) > 0 && number(metadata["layout_generation"]) > 0, "GUI capture generation was missing")
	var raw []byte
	for _, entry := range rows(result["content"]) {
		block := obj(entry)
		if block["type"] == "image" {
			ensure(raw == nil && block["mimeType"] == "image/png", "GUI capture image block mismatch")
			encoded, ok := block["data"].(string)
			ensure(ok && len(encoded) <= 4*((16<<20)+2)/3, "GUI image exceeded the limit")
			raw, err = base64.StdEncoding.DecodeString(encoded)
			check(err)
		}
	}
	ensure(len(raw) > 0 && len(raw) <= 16<<20, "GUI capture lacked a bounded PNG")
	config, err := png.DecodeConfig(bytes.NewReader(raw))
	check(err)
	ensure(config.Width == expected.Width && config.Height == expected.Height && config.Width > 0 && config.Height > 0 && config.Width <= 16777216/config.Height, "GUI PNG dimensions exceeded bounds")
	decoded, err := png.Decode(bytes.NewReader(raw))
	check(err)
	return metadata, decoded, raw
}

// 标记必须在实际返回的像素中出现；后续操作点相对该图，不发送桌面绝对坐标。
func verifyFixtureMarkers(img image.Image) {
	ensure(fixtureMarkersPresent(img), "Fixture markers were obscured or misplaced")
}

func fixtureMarkersPresent(img image.Image) bool {
	bounds := img.Bounds()
	points := []image.Point{{20, 20}, {bounds.Dx() - 20, 20}, {20, bounds.Dy() - 20}, {bounds.Dx() - 20, bounds.Dy() - 20}}
	for index, point := range points {
		for y := point.Y - 3; y <= point.Y+3; y++ {
			for x := point.X - 3; x <= point.X+3; x++ {
				if color.RGBAModel.Convert(img.At(x, y)) != fixtureColors[index] {
					return false
				}
			}
		}
	}
	return true
}

type fixtureCapture struct {
	metadata object
	img      image.Image
	raw      []byte
}

// 最多 61 次只读截图；末尾仍执行原精确断言，绝不重试输入或接受旧图。
func awaitFixtureCapture(ctx context.Context, interval time.Duration, capture func(context.Context, int) fixtureCapture) fixtureCapture {
	var latest fixtureCapture
	for attempt := 1; attempt <= 61 && ctx.Err() == nil; attempt++ {
		latest = capture(ctx, attempt)
		// 校验像素期间也可能取消，成功发布前最后检查共享期限。
		if fixtureMarkersPresent(latest.img) && ctx.Err() == nil {
			return latest
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	ensure(latest.img != nil, "Fixture capture deadline elapsed before a frame was received")
	verifyFixtureMarkers(latest.img)
	ensure(ctx.Err() == nil, "Fixture marker presentation exceeded the deadline")
	return latest
}

// 仅扫描专用客户区，诊断固定四种夹具颜色；不保留画面或任意用户像素。
func fixtureMarkerDiagnostic(img image.Image) []object {
	area := img.Bounds()
	ensure(area.Dx() > 40 && area.Dy() > 40 && area.Dx() <= 1000000/area.Dy(), "Fixture diagnostic crop exceeded its limit")
	points := []image.Point{{20, 20}, {area.Dx() - 20, 20}, {20, area.Dy() - 20}, {area.Dx() - 20, area.Dy() - 20}}
	result := make([]object, 4)
	counts := [4]int{}
	for y := 0; y < area.Dy(); y++ {
		for x := 0; x < area.Dx(); x++ {
			pixel := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			for index, expected := range fixtureColors {
				if pixel == expected {
					counts[index]++
				}
			}
		}
	}
	for index, point := range points {
		actual := color.RGBAModel.Convert(img.At(point.X, point.Y)).(color.RGBA)
		expected := fixtureColors[index]
		mismatch := 0
		for y := point.Y - 3; y <= point.Y+3; y++ {
			for x := point.X - 3; x <= point.X+3; x++ {
				if color.RGBAModel.Convert(img.At(x, y)) != expected {
					mismatch++
				}
			}
		}
		result[index] = object{"index": index, "expected_rgb": []int{int(expected.R), int(expected.G), int(expected.B)}, "actual_rgb": []int{int(actual.R), int(actual.G), int(actual.B)}, "mismatched_pixels": mismatch, "matching_color_pixels": counts[index]}
	}
	return result
}
