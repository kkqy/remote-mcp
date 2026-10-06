// Package logstream 管理具有代际标识的有界日志跟踪。
package logstream

import (
	"encoding/json"
	"time"
)

type Config struct {
	MaxOpen       int
	ReadBytes     int
	IdleTimeout   time.Duration
	PollInterval  time.Duration
	SweepInterval time.Duration
}

func DefaultConfig() Config {
	return Config{32, 64 << 10, 10 * time.Minute, 100 * time.Millisecond, 30 * time.Second}
}

type OpenInput struct {
	Path string `json:"path" jsonschema:"Path to an existing regular log file; symbolic links are rejected"`
}
type IDInput struct {
	ID string `json:"id"`
}
type ReadInput struct {
	ID         string `json:"id"`
	Generation uint64 `json:"generation,omitempty" jsonschema:"Generation from the previous response; zero uses the current generation"`
	Cursor     int64  `json:"cursor,omitempty" jsonschema:"Raw byte offset within the supplied generation"`
	Limit      int    `json:"limit,omitempty" jsonschema:"Maximum bytes to return, default and maximum 65536"`
	WaitMS     int    `json:"wait_ms,omitempty" jsonschema:"Milliseconds to wait for output or a file change, from 0 to 30000; default 0 returns immediately"`
}
type OpenResult struct {
	ID         string `json:"id"`
	Generation uint64 `json:"generation"`
	EndCursor  int64  `json:"end_cursor"`
}
type ReadResult struct {
	ID          string `json:"id"`
	Generation  uint64 `json:"generation"`
	StartCursor int64  `json:"start_cursor"`
	NextCursor  int64  `json:"next_cursor"`
	EndCursor   int64  `json:"end_cursor"`
	DataBase64  string `json:"data_base64"`
	Text        string `json:"text"`
	ValidUTF8   bool   `json:"valid_utf8"`
	Truncated   bool   `json:"truncated"`
	Rotated     bool   `json:"rotated"`
	Reason      string `json:"reason"`
}
type CloseResult struct {
	ID     string `json:"id"`
	Closed bool   `json:"closed"`
}
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string           { b, _ := json.Marshal(e); return string(b) }
func failure(code, message string) error { return &Error{code, message} }
