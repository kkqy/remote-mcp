// Package forwarding 管理独立于 MCP 会话的 TCP 转发。
package forwarding

import (
	"encoding/json"
	"net/netip"
	"time"
)

type Config struct {
	ListenHost     string
	MaxRules       int
	MaxConnections int
	DialTimeout    time.Duration
	Retention      time.Duration
	MaxRecords     int
	SweepInterval  time.Duration
}

func DefaultConfig() Config {
	return Config{"0.0.0.0", 16, 256, 10 * time.Second, 10 * time.Minute, 1024, 30 * time.Second}
}
func (c Config) Validate() error {
	if _, err := netip.ParseAddr(c.ListenHost); err != nil {
		return failure("invalid_argument", "The default listen address must be an IP address")
	}
	if c.MaxRules <= 0 || c.MaxConnections <= 0 || c.DialTimeout <= 0 || c.Retention <= 0 || c.MaxRecords <= 0 || c.SweepInterval <= 0 {
		return failure("invalid_argument", "Forwarding limits and timeouts must be positive")
	}
	return nil
}

type CreateInput struct {
	RequestID  string `json:"request_id" jsonschema:"Creation deduplication key; reuse requires identical parameters"`
	TargetHost string `json:"target_host" jsonschema:"IP address or hostname reachable from the machine running remote-mcp; omit the protocol and port"`
	TargetPort int    `json:"target_port"`
	ListenHost string `json:"listen_host,omitempty" jsonschema:"Explicit listen IP address; defaults to the service listen IP"`
	ListenPort int    `json:"listen_port,omitempty" jsonschema:"Use 0 or omit to allocate a port automatically"`
}
type IDInput struct {
	ID string `json:"id"`
}
type ListResult struct {
	Forwards []Status `json:"forwards"`
}
type Status struct {
	ID                string     `json:"id"`
	State             string     `json:"state"`
	ListenAddress     string     `json:"listen_address"`
	TargetAddress     string     `json:"target_address"`
	ActiveConnections int        `json:"active_connections"`
	TotalConnections  uint64     `json:"total_connections"`
	FailedConnections uint64     `json:"failed_connections"`
	CreatedAt         time.Time  `json:"created_at"`
	EndedAt           *time.Time `json:"ended_at,omitempty"`
	LastErrorCode     string     `json:"last_error_code,omitempty"`
}
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string           { b, _ := json.Marshal(e); return string(b) }
func failure(code, message string) error { return &Error{code, message} }
