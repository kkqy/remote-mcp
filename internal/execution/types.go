// Package execution 管理命令和真实终端，生命周期独立于单次 MCP 请求。
package execution

import (
	"encoding/json"
	"time"
)

type Config struct {
	DefaultTimeout      time.Duration
	TerminalIdleTimeout time.Duration
	Retention           time.Duration
	MaxProcesses        int
	MaxTerminals        int
	OutputBytes         int
	ReadBytes           int
	SweepInterval       time.Duration
}

func DefaultConfig() Config {
	return Config{5 * time.Minute, 30 * time.Minute, 10 * time.Minute, 16, 8, 4 << 20, 64 << 10, 30 * time.Second}
}

type StartInput struct {
	RequestID  string            `json:"request_id" jsonschema:"创建去重键，同一键只能对应同一参数"`
	Command    string            `json:"command" jsonschema:"可执行文件，管道和重定向需显式使用 shell"`
	Args       []string          `json:"args,omitempty"`
	Dir        string            `json:"dir,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Background bool              `json:"background,omitempty"`
	TimeoutMS  *int64            `json:"timeout_ms,omitempty" jsonschema:"省略使用默认超时，只有后台模式可设置为零"`
	WaitMS     int               `json:"wait_ms,omitempty" jsonschema:"等待结束的毫秒数，范围0到10000"`
}
type TerminalInput struct {
	RequestID string            `json:"request_id"`
	Command   string            `json:"command,omitempty" jsonschema:"省略时使用平台默认 shell"`
	Args      []string          `json:"args,omitempty"`
	Dir       string            `json:"dir,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Columns   int               `json:"columns,omitempty"`
	Rows      int               `json:"rows,omitempty"`
}
type IDInput struct {
	ID string `json:"id"`
}
type ReadInput struct {
	ID     string `json:"id"`
	Stream string `json:"stream,omitempty" jsonschema:"普通进程选 stdout 或 stderr，终端固定 terminal"`
	Cursor int64  `json:"cursor,omitempty" jsonschema:"原始字节的绝对游标"`
	Limit  int    `json:"limit,omitempty"`
}
type WriteInput struct {
	ID         string `json:"id"`
	DataBase64 string `json:"data_base64" jsonschema:"原始终端输入；Ctrl+C 为单字节03的Base64 Aw=="`
}
type ResizeInput struct {
	ID      string `json:"id"`
	Columns int    `json:"columns"`
	Rows    int    `json:"rows"`
}
type Status struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	State     string     `json:"state"`
	PID       int        `json:"pid"`
	ExitCode  *int       `json:"exit_code,omitempty"`
	Reason    string     `json:"reason,omitempty"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}
type ReadResult struct {
	ID          string `json:"id"`
	Stream      string `json:"stream"`
	StartCursor int64  `json:"start_cursor"`
	NextCursor  int64  `json:"next_cursor"`
	EndCursor   int64  `json:"end_cursor"`
	Truncated   bool   `json:"truncated"`
	DataBase64  string `json:"data_base64"`
	Text        string `json:"text"`
	ValidUTF8   bool   `json:"valid_utf8"`
	State       string `json:"state"`
}
type WriteResult struct {
	Written int `json:"written"`
}
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string           { b, _ := json.Marshal(e); return string(b) }
func failure(code, message string) error { return &Error{code, message} }
