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
	RequestID  string            `json:"request_id" jsonschema:"Creation deduplication key; each key must identify identical parameters"`
	Command    string            `json:"command" jsonschema:"Executable; pipes and redirection require an explicit shell"`
	Args       []string          `json:"args,omitempty"`
	Dir        string            `json:"dir,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Background bool              `json:"background,omitempty"`
	TimeoutMS  *int64            `json:"timeout_ms,omitempty" jsonschema:"Defaults to the configured timeout; only background mode accepts zero"`
	WaitMS     int               `json:"wait_ms,omitempty" jsonschema:"Milliseconds to wait for completion; range 0 to 10000"`
}
type TerminalInput struct {
	RequestID string            `json:"request_id"`
	Command   string            `json:"command,omitempty" jsonschema:"Defaults to the platform shell"`
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
	Stream string `json:"stream,omitempty" jsonschema:"Use stdout or stderr for a process; terminals use terminal"`
	Cursor int64  `json:"cursor,omitempty" jsonschema:"Absolute cursor in raw bytes"`
	Limit  int    `json:"limit,omitempty"`
}
type WriteInput struct {
	ID         string `json:"id"`
	DataBase64 string `json:"data_base64" jsonschema:"Raw terminal input; Ctrl+C is the single byte 03 encoded as Base64 Aw=="`
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
