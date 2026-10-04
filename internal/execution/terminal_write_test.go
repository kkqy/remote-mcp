package execution

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"testing"
	"time"
)

// 只替换底层写入，以独立验证管理器的完成通知与互斥语义。
type controlledTerminalWriter struct {
	running
	write func([]byte) (int, error)
}

func (p controlledTerminalWriter) Write(data []byte) (int, error) { return p.write(data) }

func terminalWriterManager(write func([]byte) (int, error)) (*Manager, *resource, WriteInput) {
	r := &resource{
		status:    Status{ID: "write-test", Kind: "terminal", State: "running"},
		process:   controlledTerminalWriter{write: write},
		inputBusy: make(chan struct{}, 1),
	}
	m := &Manager{cfg: DefaultConfig(), resources: map[string]*resource{r.status.ID: r}}
	return m, r, WriteInput{ID: r.status.ID, DataBase64: base64.StdEncoding.EncodeToString([]byte("输入"))}
}

func requireWriteCode(t *testing.T, err error, code string) {
	t.Helper()
	var detail *Error
	if !errors.As(err, &detail) || detail.Code != code {
		t.Fatalf("错误码应为 %s，实际为 %v", code, err)
	}
}

func TestTerminalWriteSequentialCompletion(t *testing.T) {
	for _, writeErr := range []error{nil, io.ErrClosedPipe} {
		name := "成功"
		if writeErr != nil {
			name = "部分写入失败"
		}
		t.Run(name, func(t *testing.T) {
			written := len("输入")
			if writeErr != nil {
				written--
			}
			m, _, in := terminalWriterManager(func(data []byte) (int, error) {
				return written, writeErr
			})
			// 结果返回后立即继续输入；不得靠重试 busy 掩盖完成通知顺序。
			for i := 0; i < 10000; i++ {
				result, err := m.Write(context.Background(), in)
				if writeErr == nil {
					if err != nil {
						t.Fatalf("第 %d 次顺序输入失败: %v", i, err)
					}
				} else {
					requireWriteCode(t, err, "io_error")
				}
				if result.Written != written {
					t.Fatalf("实际写入字节数未保留: %+v", result)
				}
			}
		})
	}
}

func TestTerminalWriteCancellationKeepsBusy(t *testing.T) {
	for _, writeErr := range []error{nil, io.ErrClosedPipe} {
		name := "成功"
		if writeErr != nil {
			name = "失败"
		}
		t.Run(name, func(t *testing.T) {
			entered := make(chan struct{}, 1)
			release := make(chan struct{}, 1)
			defer close(release)
			m, r, in := terminalWriterManager(func(data []byte) (int, error) {
				entered <- struct{}{}
				<-release
				return len(data), writeErr
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			finished := make(chan error, 1)
			go func() { _, err := m.Write(ctx, in); finished <- err }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("底层写入未开始")
			}
			_, err := m.Write(context.Background(), in)
			requireWriteCode(t, err, "busy")
			cancel()
			select {
			case err := <-finished:
				requireWriteCode(t, err, "interrupted")
			case <-time.After(time.Second):
				t.Fatal("取消未结束等待")
			}
			_, err = m.Write(context.Background(), in)
			requireWriteCode(t, err, "busy")
			// 允许底层返回，再等待占用释放，避免用 sleep 猜测 goroutine 进度。
			release <- struct{}{}
			select {
			case r.inputBusy <- struct{}{}:
				<-r.inputBusy
			case <-time.After(time.Second):
				t.Fatal("底层写入完成后未释放占用")
			}
			release <- struct{}{}
			result, err := m.Write(context.Background(), in)
			if writeErr != nil {
				requireWriteCode(t, err, "io_error")
			} else if err != nil {
				t.Fatalf("取消后的下一次输入失败: %v", err)
			}
			if result.Written != len("输入") {
				t.Fatalf("取消后的下一次输入字节数错误: %+v", result)
			}
		})
	}
}
