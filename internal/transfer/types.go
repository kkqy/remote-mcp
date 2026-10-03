package transfer

import "time"

// Config 限制活跃句柄、内存及幂等记录的生命周期。
type Config struct {
	MaxFileSize  int64
	ChunkSize    int
	MaxTransfers int
	IdleTimeout  time.Duration
	Retention    time.Duration
	MaxRecords   int
}

func DefaultConfig() Config {
	return Config{4 << 30, 256 << 10, 8, 10 * time.Minute, 10 * time.Minute, 1024}
}

type PathInput struct {
	Path string `json:"path"`
}
type IDInput struct {
	ID string `json:"id"`
}
type UploadInput struct {
	RequestID string `json:"request_id"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Overwrite bool   `json:"overwrite,omitempty"`
}
type WriteInput struct {
	ID     string `json:"id"`
	Offset int64  `json:"offset"`
	Data   string `json:"data"`
}
type ReadInput struct {
	ID     string `json:"id"`
	Offset int64  `json:"offset"`
	Length int    `json:"length"`
}

// Result 是 MCP 工具和辅助命令共同使用的唯一响应契约。
type Result struct {
	OK        bool   `json:"ok"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
	ID        string `json:"id,omitempty"`
	Path      string `json:"path,omitempty"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256,omitempty"`
	ChunkSize int    `json:"chunk_size,omitempty"`
	Offset    int64  `json:"offset"`
	Data      string `json:"data,omitempty"`
	EOF       bool   `json:"eof,omitempty"`
	State     string `json:"state,omitempty"`
}
