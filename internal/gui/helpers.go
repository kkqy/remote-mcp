package gui

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

func (c Config) Validate() error {
	if c.IdleTimeout <= 0 || c.Retention <= 0 || c.AuthorizationTimeout <= 0 || c.OperationTimeout <= 0 || c.SweepInterval <= 0 || c.CaptureRetention <= 0 {
		return failure("invalid_argument", "GUI 时间限制必须大于零")
	}
	if c.AuthorizationTimeout > 10*time.Minute || c.OperationTimeout > 2*time.Minute {
		return failure("invalid_argument", "GUI 授权或操作期限过长")
	}
	if c.MaxRecords < 1 || c.MaxRecords > 4096 || c.MaxCaptures < 1 || c.MaxCaptures > 1024 || c.MaxPixels < 1 || c.MaxPixels > 134217728 || c.MaxPNGBytes < 1 || c.MaxPNGBytes > 128<<20 || c.MaxTextBytes < 1 || c.MaxTextBytes > 1<<20 || c.MaxClipboardBytes < 1 || c.MaxClipboardBytes > 16<<20 {
		return failure("invalid_argument", "GUI 资源大小或数量限制无效")
	}
	return nil
}
func checkPixels(w, h, max int) error {
	if w <= 0 || h <= 0 {
		return failure("capture_failed", "截图尺寸无效")
	}
	if w > max/h {
		return failure("limit_exceeded", "截图像素超过限制")
	}
	return nil
}
func normalizeKeys(keys []string) ([]string, error) {
	if len(keys) < 1 || len(keys) > 8 {
		return nil, failure("invalid_argument", "按键数量必须为1到8")
	}
	names := map[string]string{"ctrl": "Ctrl", "control": "Ctrl", "shift": "Shift", "alt": "Alt", "meta": "Meta", "super": "Meta", "cmd": "Meta", "command": "Meta", "enter": "Enter", "return": "Enter", "escape": "Escape", "esc": "Escape", "tab": "Tab", "space": "Space", "backspace": "Backspace", "delete": "Delete", "insert": "Insert", "home": "Home", "end": "End", "pageup": "PageUp", "pagedown": "PageDown", "up": "Up", "down": "Down", "left": "Left", "right": "Right"}
	out := make([]string, len(keys))
	seen := map[string]bool{}
	for i, k := range keys {
		n := names[strings.ToLower(k)]
		if n == "" {
			u := strings.ToUpper(k)
			if len(u) == 1 && ((u[0] >= 'A' && u[0] <= 'Z') || (u[0] >= '0' && u[0] <= '9')) {
				n = u
			}
			for f := 1; f <= 24; f++ {
				if u == fmt.Sprintf("F%d", f) {
					n = u
				}
			}
		}
		if n == "" || seen[n] {
			return nil, failure("invalid_argument", "包含未知或重复按键")
		}
		seen[n] = true
		out[i] = n
	}
	return out, nil
}
func pause(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func partialError(err error) error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		copy := *e
		copy.InputMayHaveApplied = true
		return &copy
	}
	return &Error{Code: "input_failed", Message: "输入未完整结束，部分事件可能已经生效", InputMayHaveApplied: true}
}
func contextError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return failure("timeout", "GUI 操作等待超时")
	}
	return failure("authorization_cancelled", "GUI 调用已取消")
}
func performKeys(ctx context.Context, keys []string, emit func(context.Context, string, bool) error) (err error) {
	var held []string
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		for i := len(held) - 1; i >= 0; i-- {
			if e := emit(cleanup, held[i], false); e != nil && err == nil {
				err = e
			}
		}
		if err != nil && len(held) > 0 {
			err = partialError(err)
		}
	}()
	for _, k := range keys {
		if e := ctx.Err(); e != nil {
			return e
		}
		held = append(held, k)
		if e := emit(ctx, k, true); e != nil {
			return e
		}
	}
	return nil
}
func performMouse(ctx context.Context, ev MouseEvent, move func(context.Context, Point) error, button func(context.Context, string, bool) error, scroll func(context.Context, int, int) error) (err error) {
	if e := ctx.Err(); e != nil {
		return e
	}
	if e := move(ctx, ev.Start); e != nil {
		return partialError(e)
	}
	held := false
	defer func() {
		if held {
			cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if e := button(cleanup, ev.Button, false); e != nil && err == nil {
				err = e
			}
		}
		if err != nil {
			err = partialError(err)
		}
	}()
	press := func() error { held = true; return button(ctx, ev.Button, true) }
	release := func() error {
		if e := button(ctx, ev.Button, false); e != nil {
			return e
		}
		held = false
		return nil
	}
	switch ev.Action {
	case "move":
		return nil
	case "scroll":
		return scroll(ctx, ev.ScrollX, ev.ScrollY)
	case "click", "double_click":
		count := 1
		if ev.Action == "double_click" {
			count = 2
		}
		for i := 0; i < count; i++ {
			if e := press(); e != nil {
				return e
			}
			if e := pause(ctx, 20*time.Millisecond); e != nil {
				return e
			}
			if e := release(); e != nil {
				return e
			}
			if i+1 < count {
				if e := pause(ctx, 80*time.Millisecond); e != nil {
					return e
				}
			}
		}
		return nil
	case "drag":
		if e := press(); e != nil {
			return e
		}
		steps := int(math.Ceil(float64(ev.Duration) / (float64(20 * time.Millisecond))))
		if steps < 1 {
			steps = 1
		}
		for i := 1; i <= steps; i++ {
			if e := pause(ctx, ev.Duration/time.Duration(steps)); e != nil {
				return e
			}
			r := float64(i) / float64(steps)
			if e := move(ctx, Point{ev.Start.X + (ev.End.X-ev.Start.X)*r, ev.Start.Y + (ev.End.Y-ev.Start.Y)*r}); e != nil {
				return e
			}
		}
		return release()
	default:
		return failure("invalid_argument", "未知鼠标操作")
	}
}
