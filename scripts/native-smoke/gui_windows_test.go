//go:build windows

package main

import (
	"testing"
	"time"
	"unsafe"
)

// 架构 ABI 回归仅检查结构布局，不创建窗口或控制桌面。
func TestWinFixtureABILayout(t *testing.T) {
	if unsafe.Sizeof(nativeClass{}) != 80 || unsafe.Offsetof(nativeClass{}.Instance) != 24 || unsafe.Sizeof(nativeMessage{}) != 48 || unsafe.Offsetof(nativeMessage{}.WParam) != 16 || unsafe.Sizeof(nativePaint{}) != 72 {
		t.Fatal("Windows 64 位原生结构对齐不符合 Win32 ABI")
	}
}

func TestFixtureCancelledStartupReapsThreadWithoutWindow(t *testing.T) {
	f := &winFixture{done: make(chan struct{}), ready: make(chan bool, 1), stop: make(chan struct{})}
	f.stopOnce.Do(func() { close(f.stop) })
	go f.run(object{})
	f.stopAndWait()
	select {
	case <-f.done:
	case <-time.After(time.Second):
		t.Fatal("取消的启动没有回收 UI 线程")
	}
	if f.window != 0 || f.failed {
		t.Fatal("预取消的启动不应创建窗口或访问输入")
	}
}
