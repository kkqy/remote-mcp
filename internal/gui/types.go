// Package gui 管理当前用户桌面的截图与坐标式输入。
package gui

import (
	"context"
	"encoding/json"
	"image"
	"time"
)

type Config struct {
	IdleTimeout          time.Duration
	Retention            time.Duration
	AuthorizationTimeout time.Duration
	OperationTimeout     time.Duration
	SweepInterval        time.Duration
	MaxRecords           int
	MaxPixels            int
	MaxPNGBytes          int
	MaxCaptures          int
	CaptureRetention     time.Duration
	MaxTextBytes         int
	MaxClipboardBytes    int
}

func DefaultConfig() Config {
	return Config{30 * time.Minute, 10 * time.Minute, 2 * time.Minute, 30 * time.Second, 30 * time.Second, 256, 16777216, 16 << 20, 64, 5 * time.Minute, 64 << 10, 1 << 20}
}

type Rect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}
type Bounds struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}
type Display struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	Primary       bool   `json:"primary"`
	PixelWidth    int    `json:"pixel_width"`
	PixelHeight   int    `json:"pixel_height"`
	LogicalBounds Bounds `json:"logical_bounds"`
	// AbsoluteInput 表示后端拥有可靠的截图到逻辑坐标映射。坐标为已旋转的可见画面方向。
	AbsoluteInput bool `json:"absolute_input"`
}
type Capabilities struct {
	Screenshot bool              `json:"screenshot"`
	Mouse      bool              `json:"mouse"`
	Keyboard   bool              `json:"keyboard"`
	Text       bool              `json:"text"`
	DirectText bool              `json:"direct_text"`
	Clipboard  bool              `json:"clipboard"`
	Reasons    map[string]string `json:"reasons,omitempty"`
}
type Status struct {
	ID               string       `json:"id,omitempty"`
	Backend          string       `json:"backend"`
	State            string       `json:"state"`
	Capabilities     Capabilities `json:"capabilities"`
	Displays         []Display    `json:"displays"`
	LayoutGeneration uint64       `json:"layout_generation"`
	Code             string       `json:"code,omitempty"`
	Message          string       `json:"message,omitempty"`
}
type StatusInput struct {
	ID string `json:"id,omitempty"`
}
type IDInput struct {
	ID string `json:"id"`
}
type OpenInput struct {
	RequestID string `json:"request_id"`
	WaitMS    int    `json:"wait_ms,omitempty" jsonschema:"Milliseconds to wait for authorization, from 0 to 10000"`
}
type ScreenshotInput struct {
	ID        string `json:"id"`
	DisplayID string `json:"display_id,omitempty"`
	Region    *Rect  `json:"region,omitempty"`
}
type CaptureMetadata struct {
	ID               string    `json:"id"`
	CaptureID        string    `json:"capture_id"`
	DisplayID        string    `json:"display_id"`
	Width            int       `json:"width"`
	Height           int       `json:"height"`
	Region           Rect      `json:"region"`
	LogicalBounds    Bounds    `json:"logical_bounds"`
	LayoutGeneration uint64    `json:"layout_generation"`
	CapturedAt       time.Time `json:"captured_at"`
	CapturedAtSource string    `json:"captured_at_source"`
	FrameSequence    uint64    `json:"frame_sequence"`
	Freshness        string    `json:"freshness"`
}
type ScreenshotResult struct {
	CaptureMetadata
	PNG []byte `json:"-"`
}
type MouseInput struct {
	ID         string  `json:"id"`
	CaptureID  string  `json:"capture_id"`
	Action     string  `json:"action" jsonschema:"move/click/double_click/drag/scroll"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	EndX       float64 `json:"end_x,omitempty"`
	EndY       float64 `json:"end_y,omitempty"`
	Button     string  `json:"button,omitempty" jsonschema:"left/middle/right; defaults to left"`
	ScrollX    int     `json:"scroll_x,omitempty"`
	ScrollY    int     `json:"scroll_y,omitempty"`
	DurationMS int     `json:"duration_ms,omitempty" jsonschema:"Drag duration in milliseconds, from 0 to 10000"`
}
type KeyInput struct {
	ID   string   `json:"id"`
	Keys []string `json:"keys" jsonschema:"A single key or key combination, up to 8 keys; for Ctrl+V, use the array Ctrl,V"`
}
type TextInput struct {
	ID                    string   `json:"id"`
	Text                  string   `json:"text"`
	Mode                  string   `json:"mode,omitempty" jsonschema:"auto/direct/clipboard; defaults to auto"`
	PasteKeys             []string `json:"paste_keys,omitempty"`
	AllowClipboardReplace bool     `json:"allow_clipboard_replace,omitempty"`
}
type InputResult struct {
	Submitted           bool   `json:"submitted"`
	Mode                string `json:"mode,omitempty"`
	ClipboardRestore    string `json:"clipboard_restore,omitempty"`
	ClipboardReplaced   bool   `json:"clipboard_replaced,omitempty"`
	InputMayHaveApplied bool   `json:"input_may_have_applied"`
}
type Error struct {
	Code                string `json:"code"`
	Message             string `json:"message"`
	InputMayHaveApplied bool   `json:"input_may_have_applied,omitempty"`
	ClipboardRestore    string `json:"clipboard_restore,omitempty"`
}

func (e *Error) Error() string           { b, _ := json.Marshal(e); return string(b) }
func failure(code, message string) error { return &Error{Code: code, Message: message} }

// Backend 的 Probe 不发起授权；Open 接收管理器的授权期限，建立独立于请求的桌面会话。
type Backend interface {
	Probe(context.Context) (Status, error)
	Open(context.Context) (Desktop, error)
}

// Desktop 的方法可与 Close 并发；实现必须响应取消并释放本次按下的按钮及按键。
// Capture 在分配图像前核对 factory 传入的 Config.MaxPixels；返回原始显示器可见方向图像。
type Desktop interface {
	Capabilities() Capabilities
	Displays(context.Context) ([]Display, error)
	Capture(context.Context, Display) (Frame, error)
	Mouse(context.Context, MouseEvent) error
	Key(context.Context, []string) error
	Text(context.Context, TextInput) (InputResult, error)
	Close() error
}
type Frame struct {
	Image      image.Image
	CapturedAt time.Time
	Freshness  string
}
type Point struct{ X, Y float64 }

// MouseEvent.X/Y 为显示器内的逻辑坐标；原生系统后端按需加 LogicalBounds 的桌面偏移。
// Wayland 直接向授权流提交局部坐标。End 是拖拽终点。
type MouseEvent struct {
	Display          Display
	Action, Button   string
	Start, End       Point
	ScrollX, ScrollY int
	Duration         time.Duration
}

// 实现可选健康接口，以便管理器主动响应会话撤销及桌面连接断开。
type desktopHealth interface {
	Done() <-chan struct{}
	Err() error
}
