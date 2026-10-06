package execution

import (
	"context"
	"sync"
	"testing"
	"time"
)

// 使用可控资源准确验证订阅锁序，不依赖启动调度时间。
func readResource(t *testing.T) (*Manager, *resource) {
	t.Helper()
	m := manager(t)
	r := &resource{status: Status{ID: "read-test", Kind: "process", State: "running"}, out: &outputBuffer{capacity: 128}, err: &outputBuffer{capacity: 128}, done: make(chan struct{}), lastCall: time.Now()}
	m.mu.Lock()
	m.resources[r.status.ID] = r
	m.mu.Unlock()
	t.Cleanup(func() { m.mu.Lock(); delete(m.resources, r.status.ID); m.mu.Unlock() })
	return m, r
}
func TestReadWaitImmediateTimeoutCancelAndBounds(t *testing.T) {
	m, r := readResource(t)
	for _, in := range []ReadInput{{ID: r.status.ID, WaitMS: -1}, {ID: r.status.ID, WaitMS: 30001}} {
		if _, err := m.ReadContext(context.Background(), in); err == nil {
			t.Fatal("等待边界未拒绝")
		}
	}
	start := time.Now()
	out, err := m.ReadContext(context.Background(), ReadInput{ID: r.status.ID})
	if err != nil || out.Reason != "immediate" || time.Since(start) > time.Second {
		t.Fatalf("立即读取: %+v %v", out, err)
	}
	out, err = m.ReadContext(context.Background(), ReadInput{ID: r.status.ID, WaitMS: 20})
	if err != nil || out.Reason != "timeout" || out.NextCursor != 0 {
		t.Fatalf("超时: %+v %v", out, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err = m.ReadContext(ctx, ReadInput{ID: r.status.ID, WaitMS: 30000})
	if err != nil || out.Reason != "cancelled" || out.ID != r.status.ID || out.NextCursor != 0 {
		t.Fatalf("取消: %+v %v", out, err)
	}
}
func TestReadWaitOutputAndExit(t *testing.T) {
	for _, exit := range []bool{false, true} {
		t.Run(map[bool]string{false: "output", true: "exit"}[exit], func(t *testing.T) {
			m, r := readResource(t)
			done := make(chan ReadResult, 1)
			go func() {
				out, err := m.ReadContext(context.Background(), ReadInput{ID: r.status.ID, WaitMS: 30000})
				if err != nil {
					t.Error(err)
				}
				done <- out
			}()
			// 锁内订阅完成后才发布事件，直接覆盖检查与等待的切换。
			deadline := time.Now().Add(time.Second)
			for {
				r.out.mu.Lock()
				subscribed := r.out.changed != nil
				r.out.mu.Unlock()
				if subscribed {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("没有订阅输出")
				}
				time.Sleep(time.Millisecond)
			}
			if exit {
				r.mu.Lock()
				r.status.State = "exited"
				r.mu.Unlock()
				close(r.done)
			} else {
				r.out.Write([]byte("中文"))
			}
			select {
			case out := <-done:
				if exit && out.Reason != "exit" {
					t.Fatalf("退出: %+v", out)
				}
				if !exit && (out.Reason != "output" || out.Text != "中文" || out.NextCursor != 6) {
					t.Fatalf("新输出: %+v", out)
				}
			case <-time.After(time.Second):
				t.Fatal("等待未唤醒")
			}
		})
	}
}
func TestReadWaitNoLostNotification(t *testing.T) {
	m, r := readResource(t)
	cursor := int64(0)
	for i := 0; i < 200; i++ {
		var wg sync.WaitGroup
		wg.Add(1)
		go func() { defer wg.Done(); r.out.Write([]byte("x")) }()
		out, err := m.ReadContext(context.Background(), ReadInput{ID: r.status.ID, Cursor: cursor, WaitMS: 1000})
		wg.Wait()
		if err != nil || out.Reason != "output" || out.NextCursor != cursor+1 {
			t.Fatalf("丢失唤醒: %+v %v", out, err)
		}
		cursor = out.NextCursor
	}
}
func TestProcessDelayedReadAndExit(t *testing.T) {
	m := manager(t)
	in := helperInput("delayed", "delayed-output")
	in.WaitMS = 0
	s, err := m.Start(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := m.ReadContext(context.Background(), ReadInput{ID: s.ID, WaitMS: 5000})
	if err != nil || out.Reason != "output" || out.Text != "稍后输出" {
		t.Fatalf("真实延迟输出: %+v %v", out, err)
	}
	out, err = m.ReadContext(context.Background(), ReadInput{ID: s.ID, Cursor: out.NextCursor, WaitMS: 5000})
	if err != nil || out.Reason != "exit" || out.State != "exited" {
		t.Fatalf("真实退出: %+v %v", out, err)
	}
}

func TestReadWaitServerClose(t *testing.T) {
	m := manager(t)
	in := helperInput("close-wait", "sleep")
	in.WaitMS = 0
	s, err := m.Start(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan ReadResult, 1)
	go func() {
		out, err := m.ReadContext(context.Background(), ReadInput{ID: s.ID, WaitMS: 30000})
		if err != nil {
			t.Error(err)
		}
		done <- out
	}()
	m.mu.Lock()
	r := m.resources[s.ID]
	m.mu.Unlock()
	deadline := time.Now().Add(time.Second)
	for {
		r.out.mu.Lock()
		subscribed := r.out.changed != nil
		r.out.mu.Unlock()
		if subscribed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("没有订阅输出")
		}
		time.Sleep(time.Millisecond)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case out := <-done:
		if out.Reason != "cancelled" {
			t.Fatalf("服务关闭: %+v", out)
		}
	case <-time.After(time.Second):
		t.Fatal("服务关闭未唤醒")
	}
}
func TestReadWaitExitRetainsTail(t *testing.T) {
	m, r := readResource(t)
	r.out.Write([]byte("尾部"))
	r.mu.Lock()
	r.status.State = "exited"
	r.mu.Unlock()
	close(r.done)
	out, err := m.ReadContext(context.Background(), ReadInput{ID: r.status.ID, WaitMS: 30000})
	if err != nil || out.Reason != "output" || out.Text != "尾部" {
		t.Fatalf("末尾输出: %+v %v", out, err)
	}
	out, err = m.ReadContext(context.Background(), ReadInput{ID: r.status.ID, Cursor: out.NextCursor, WaitMS: 30000})
	if err != nil || out.Reason != "exit" || out.NextCursor != 6 {
		t.Fatalf("尾部游标: %+v %v", out, err)
	}
}
