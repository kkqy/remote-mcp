package execution

import (
	"encoding/base64"
	"strings"
	"sync"
	"unicode/utf8"
)

// 环形缓冲只保留最近字节；每个流有独立的绝对游标。
type outputBuffer struct {
	mu       sync.Mutex
	data     []byte
	capacity int
	end      int64
	changed  chan struct{}
}

func (b *outputBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if n > 0 && b.changed != nil {
		close(b.changed)
		b.changed = nil
	}
	b.end += int64(n)
	if n >= b.capacity {
		b.data = append(b.data[:0], p[n-b.capacity:]...)
		return n, nil
	}
	if excess := len(b.data) + n - b.capacity; excess > 0 {
		copy(b.data, b.data[excess:])
		b.data = b.data[:len(b.data)-excess]
	}
	b.data = append(b.data, p...)
	return n, nil
}
func (b *outputBuffer) read(cursor int64, limit int) (ReadResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.readLocked(cursor, limit)
}

// 同一把锁中读取并订阅，防止写入发生在检查与订阅之间。
func (b *outputBuffer) readSubscribe(cursor int64, limit int) (ReadResult, <-chan struct{}, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.changed == nil {
		b.changed = make(chan struct{})
	}
	out, err := b.readLocked(cursor, limit)
	return out, b.changed, err
}
func (b *outputBuffer) readLocked(cursor int64, limit int) (ReadResult, error) {
	if cursor < 0 || cursor > b.end {
		return ReadResult{}, failure("invalid_argument", "The cursor is outside the output range")
	}
	start := b.end - int64(len(b.data))
	truncated := cursor < start
	if cursor < start {
		cursor = start
	}
	n := min(int64(limit), b.end-cursor)
	data := b.data[cursor-start : cursor-start+n]
	return ReadResult{StartCursor: cursor, NextCursor: cursor + n, EndCursor: b.end, Truncated: truncated, DataBase64: base64.StdEncoding.EncodeToString(data), Text: strings.ToValidUTF8(string(data), "�"), ValidUTF8: utf8.Valid(data)}, nil
}
