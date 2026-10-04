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
		return failure("invalid_argument", "默认监听地址必须为 IP")
	}
	if c.MaxRules <= 0 || c.MaxConnections <= 0 || c.DialTimeout <= 0 || c.Retention <= 0 || c.MaxRecords <= 0 || c.SweepInterval <= 0 {
		return failure("invalid_argument", "转发限额和时间必须为正数")
	}
	return nil
}

type CreateInput struct {
	RequestID  string `json:"request_id" jsonschema:"创建去重键，同一键必须使用相同参数"`
	TargetHost string `json:"target_host" jsonschema:"运行 remote-mcp 的机器可达的 IP 或主机名，不含协议和端口"`
	TargetPort int    `json:"target_port"`
	ListenHost string `json:"listen_host,omitempty" jsonschema:"明确的监听 IP，省略沿用服务监听 IP"`
	ListenPort int    `json:"listen_port,omitempty" jsonschema:"0 或省略时自动分配端口"`
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
