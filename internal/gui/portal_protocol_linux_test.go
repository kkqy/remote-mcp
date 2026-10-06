//go:build linux

package gui

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// 这些测试只连接临时私有总线，不访问宿主桌面，也不触发实际授权。
func privateBus(t *testing.T) (*dbus.Conn, *dbus.Conn) {
	t.Helper()
	if os.Getenv("REMOTE_MCP_GUI_PORTAL_TEST") != "1" {
		t.Skip("设置 REMOTE_MCP_GUI_PORTAL_TEST=1 运行隔离 D-Bus 协议测试")
	}
	daemon := exec.Command("dbus-daemon", "--session", "--nofork", "--print-address=1")
	stdout, err := daemon.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	daemon.Stderr = io.Discard
	if err := daemon.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Process.Kill(); _ = daemon.Wait() })
	address, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	server, err := dbus.Connect(strings.TrimSpace(address))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	client, err := dbus.Connect(strings.TrimSpace(address))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if _, err := server.RequestName(portalName, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	return server, client
}
func responsePath(sender dbus.Sender, options map[string]dbus.Variant) dbus.ObjectPath {
	name := strings.ReplaceAll(strings.TrimPrefix(string(sender), ":"), ".", "_")
	token, _ := options["handle_token"].Value().(string)
	return dbus.ObjectPath("/org/freedesktop/portal/desktop/request/" + name + "/" + token)
}
func TestPortalRequestImmediateResponseAndCancel(t *testing.T) {
	server, client := privateBus(t)
	closed := make(chan struct{}, 1)
	err := server.ExportMethodTable(map[string]any{"CreateSession": func(sender dbus.Sender, options map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
		path := responsePath(sender, options)
		_ = server.Emit(path, "org.freedesktop.portal.Request.Response", uint32(0), map[string]dbus.Variant{"session_handle": dbus.MakeVariant("/org/freedesktop/portal/desktop/session/test")})
		return path, nil
	}, "Start": func(sender dbus.Sender, _ dbus.ObjectPath, _ string, options map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
		path := responsePath(sender, options)
		_ = server.ExportMethodTable(map[string]any{"Close": func() *dbus.Error { closed <- struct{}{}; return nil }}, path, "org.freedesktop.portal.Request")
		return path, nil
	}}, portalPath, remoteInterface)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := portalRequest(ctx, client, remoteInterface, "CreateSession", nil, map[string]dbus.Variant{})
	if err != nil || out["session_handle"].Value() == nil {
		t.Fatalf("先信号后方法返回的授权响应丢失: %v %v", out, err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel2()
	_, err = portalRequest(ctx2, client, remoteInterface, "Start", []any{dbus.ObjectPath("/session"), ""}, map[string]dbus.Variant{})
	assertCode(t, err, "authorization_timeout")
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("取消授权未关闭Request")
	}
}

type clipboardFixture struct {
	mu                  sync.Mutex
	conn                *dbus.Conn
	path                dbus.ObjectPath
	mime                string
	original            []byte
	fds                 []*os.File
	received            chan string
	changeOwner         bool
	failSet             bool
	noOwnerSignal       bool
	intermediateRestore bool
	duplicateOwner      bool
	sets                int
}

func (f *clipboardFixture) owner(own bool, mimes []string) {
	_ = f.conn.Emit(portalPath, clipboardInterface+".SelectionOwnerChanged", f.path, map[string]dbus.Variant{"mime_types": dbus.MakeVariant(mimes), "session_is_owner": dbus.MakeVariant(own)})
}
func (f *clipboardFixture) install(t *testing.T) {
	t.Helper()
	err := f.conn.ExportMethodTable(map[string]any{
		"SelectionRead": func(_ dbus.ObjectPath, _ string) (dbus.UnixFD, *dbus.Error) {
			read, write, err := os.Pipe()
			if err != nil {
				return 0, dbus.MakeFailedError(err)
			}
			_, _ = write.Write(f.original)
			_ = write.Close()
			f.mu.Lock()
			f.fds = append(f.fds, read)
			f.mu.Unlock()
			return dbus.UnixFD(read.Fd()), nil
		},
		"SetSelection": func(_ dbus.ObjectPath, options map[string]dbus.Variant) *dbus.Error {
			f.mu.Lock()
			fail := f.failSet
			noSignal := f.noOwnerSignal
			f.sets++
			intermediate := f.intermediateRestore && f.sets > 1
			f.mu.Unlock()
			if fail {
				return dbus.NewError("org.freedesktop.portal.Error.Failed", nil)
			}
			mimes, _ := options["mime_types"].Value().([]string)
			if !noSignal {
				if intermediate {
					f.owner(false, []string{"text/plain;charset=utf-8", "text/plain"})
					// 模拟旧source取消与新source取得selection之间的非owner窗口。
					time.Sleep(10 * time.Millisecond)
				}
				f.owner(len(mimes) > 0, mimes)
			}
			return nil
		},
		"SelectionWrite": func(_ dbus.ObjectPath, _ uint32) (dbus.UnixFD, *dbus.Error) {
			read, write, err := os.Pipe()
			if err != nil {
				return 0, dbus.MakeFailedError(err)
			}
			f.mu.Lock()
			f.fds = append(f.fds, write)
			f.mu.Unlock()
			go func() {
				data, _ := io.ReadAll(read)
				_ = read.Close()
				f.received <- string(data)
				f.mu.Lock()
				change := f.changeOwner
				duplicate := f.duplicateOwner
				f.mu.Unlock()
				if change {
					f.owner(false, []string{"text/plain"})
				} else if duplicate {
					f.owner(true, []string{"text/plain;charset=utf-8", "text/plain"})
				}
			}()
			return dbus.UnixFD(write.Fd()), nil
		},
		"SelectionWriteDone": func(_ dbus.ObjectPath, _ uint32, _ bool) *dbus.Error {
			f.mu.Lock()
			for _, fd := range f.fds {
				_ = fd.Close()
			}
			f.fds = nil
			f.mu.Unlock()
			return nil
		},
	}, portalPath, clipboardInterface)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, fd := range f.fds {
			_ = fd.Close()
		}
	})
	if err := f.conn.ExportMethodTable(map[string]any{"NotifyKeyboardKeysym": func(_ dbus.ObjectPath, _ map[string]dbus.Variant, key int32, state uint32) *dbus.Error {
		if key == int32('v') && state == 1 {
			_ = f.conn.Emit(portalPath, clipboardInterface+".SelectionTransfer", f.path, "text/plain;charset=utf-8", uint32(1))
		}
		return nil
	}}, portalPath, remoteInterface); err != nil {
		t.Fatal(err)
	}
}
func clipboardDesktop(t *testing.T, server, client *dbus.Conn) *waylandDesktop {
	t.Helper()
	life, cancel := context.WithCancel(context.Background())
	d := &waylandDesktop{cfg: DefaultConfig(), conn: client, path: "/org/freedesktop/portal/desktop/session/test", life: life, cancel: cancel, done: make(chan struct{}), signals: make(chan *dbus.Signal, 64)}
	d.clipboard = newPortalClipboard(d)
	client.Signal(d.signals)
	if err := client.AddMatchSignal(dbus.WithMatchSender(portalName), dbus.WithMatchInterface(clipboardInterface), dbus.WithMatchObjectPath(portalPath)); err != nil {
		t.Fatal(err)
	}
	d.wg.Add(1)
	go d.watch()
	t.Cleanup(func() { _ = d.Close() })
	return d
}
func waitClipboardKnown(t *testing.T, c *portalClipboard) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		c.mu.Lock()
		known := c.known
		c.mu.Unlock()
		if known {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("未收到剪贴板所有权信号")
		}
		time.Sleep(time.Millisecond)
	}
}
func TestPortalClipboardPreserveNewOwnerAndRestoreFailure(t *testing.T) {
	for _, scenario := range []string{"restore", "new-owner", "restore-failure", "intermediate-restore", "duplicate-own"} {
		t.Run(scenario, func(t *testing.T) {
			server, client := privateBus(t)
			d := clipboardDesktop(t, server, client)
			f := &clipboardFixture{conn: server, path: d.path, original: []byte("原内容"), received: make(chan string, 4), changeOwner: scenario == "new-owner", intermediateRestore: scenario == "intermediate-restore", duplicateOwner: scenario == "duplicate-own"}
			f.install(t)
			f.owner(false, []string{"text/plain;charset=utf-8"})
			waitClipboardKnown(t, d.clipboard)
			if scenario == "restore-failure" {
				go func() { <-f.received; f.mu.Lock(); f.failSet = true; f.mu.Unlock() }()
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			out, err := d.Text(ctx, TextInput{Mode: "clipboard", Text: "中文", PasteKeys: []string{"Ctrl", "V"}})
			if scenario == "restore-failure" {
				assertCode(t, err, "clipboard_restore_failed")
				if !strings.Contains(clipboardCause(err), "method_error") {
					t.Fatal("恢复失败没有保留固定底层原因")
				}
				c := d.clipboard
				c.mu.Lock()
				if string(c.recovery["text/plain;charset=utf-8"]) != "原内容" {
					c.mu.Unlock()
					t.Fatal("恢复失败丢弃了有界原快照")
				}
				c.mu.Unlock()
				f.mu.Lock()
				f.failSet = false
				f.mu.Unlock()
				if err := c.prepareClose(); err != nil {
					t.Fatalf("关闭前有权重试没有恢复: %v", err)
				}
				c.mu.Lock()
				if c.recovery != nil || string(c.data["text/plain;charset=utf-8"]) != "原内容" {
					c.mu.Unlock()
					t.Fatal("重试后的原快照没有恢复")
				}
				c.mu.Unlock()
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			expected := "restored"
			if scenario == "new-owner" {
				expected = "skipped_new_owner"
			}
			if out.ClipboardRestore != expected || !out.Submitted {
				t.Fatalf("剪贴板结果: %+v", out)
			}
			if value := <-f.received; value != "中文" {
				t.Fatal("接收方未拿到UTF-8文本")
			}
			if scenario == "restore" || scenario == "intermediate-restore" || scenario == "duplicate-own" {
				_ = server.Emit(portalPath, clipboardInterface+".SelectionTransfer", d.path, "text/plain;charset=utf-8", uint32(2))
				select {
				case value := <-f.received:
					if value != "原内容" {
						t.Fatal("原剪贴板MIME字节未恢复")
					}
				case <-time.After(time.Second):
					t.Fatal("恢复内容未提供给实际读取方")
				}
			}
		})
	}
}

func TestPortalClipboardUnconfirmedReplacementReportsRestoreRisk(t *testing.T) {
	server, client := privateBus(t)
	d := clipboardDesktop(t, server, client)
	f := &clipboardFixture{conn: server, path: d.path, original: []byte("原内容"), received: make(chan string, 4), noOwnerSignal: true}
	f.install(t)
	f.owner(false, []string{"text/plain;charset=utf-8"})
	waitClipboardKnown(t, d.clipboard)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	out, err := d.Text(ctx, TextInput{Mode: "clipboard", Text: "新内容", PasteKeys: []string{"Ctrl", "V"}})
	assertCode(t, err, "clipboard_restore_failed")
	if out.Submitted || out.ClipboardRestore != "unknown" {
		t.Fatalf("未知所有权不能声称恢复成功: %+v", out)
	}
}

func assertClipboardRawCleared(t *testing.T, c *portalClipboard) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.data != nil || c.recovery != nil || c.unconfirmed != nil || c.mimes != nil || c.known || c.owner || c.pending || c.abort != nil {
		t.Fatal("关闭后终态仍保留原始剪贴板或provider状态")
	}
}

func TestPortalClipboardLateOwnerAfterUnconfirmedTimeout(t *testing.T) {
	server, client := privateBus(t)
	d := clipboardDesktop(t, server, client)
	c := d.clipboard
	c.known, c.owner, c.epoch = true, true, 2
	c.data = map[string][]byte{"text/plain;charset=utf-8": []byte("原内容")}
	var mu sync.Mutex
	sets := 0
	if err := server.ExportMethodTable(map[string]any{"SetSelection": func(_ dbus.ObjectPath, opts map[string]dbus.Variant) *dbus.Error {
		mu.Lock()
		sets++
		first := sets == 1
		mu.Unlock()
		mimes, _ := opts["mime_types"].Value().([]string)
		if first {
			_ = server.Emit(portalPath, clipboardInterface+".SelectionOwnerChanged", d.path, map[string]dbus.Variant{"mime_types": dbus.MakeVariant([]string{"text/plain"}), "session_is_owner": dbus.MakeVariant(false)})
		} else {
			_ = server.Emit(portalPath, clipboardInterface+".SelectionOwnerChanged", d.path, map[string]dbus.Variant{"mime_types": dbus.MakeVariant(mimes), "session_is_owner": dbus.MakeVariant(true)})
		}
		return nil
	}}, portalPath, clipboardInterface); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	out, err := d.Text(ctx, TextInput{Mode: "clipboard", Text: "中文", PasteKeys: []string{"Ctrl", "V"}})
	assertCode(t, err, "clipboard_restore_failed")
	if out.ClipboardRestore != "unknown" || out.Submitted || !strings.Contains(clipboardCause(err), "owner_false") {
		t.Fatalf("未确认设置被错误视为外部接管: %+v %v", out, err)
	}
	c.mu.Lock()
	backup := string(c.recovery["text/plain;charset=utf-8"])
	c.mu.Unlock()
	if backup != "原内容" {
		t.Fatal("未确认设置丢失原备份")
	}
	_ = server.Emit(portalPath, clipboardInterface+".SelectionOwnerChanged", d.path, map[string]dbus.Variant{"mime_types": dbus.MakeVariant([]string{"text/plain", "text/plain;charset=utf-8"}), "session_is_owner": dbus.MakeVariant(true)})
	deadline := time.Now().Add(time.Second)
	for {
		c.mu.Lock()
		ready := c.owner && string(c.data["text/plain"]) == "中文"
		c.mu.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("晚到的合法owner确认未恢复有界provider数据")
		}
		time.Sleep(time.Millisecond)
	}
	if err := c.prepareClose(); err != nil {
		t.Fatalf("晚到owner后不能安全恢复原快照: %v", err)
	}
	c.mu.Lock()
	if string(c.data["text/plain;charset=utf-8"]) != "原内容" || c.recovery != nil {
		c.mu.Unlock()
		t.Fatal("晚到确认后的原快照恢复不完整")
	}
	c.mu.Unlock()
}

func TestPortalClipboardMIMEMismatchHasFixedFinalReason(t *testing.T) {
	server, client := privateBus(t)
	d := clipboardDesktop(t, server, client)
	c := d.clipboard
	if err := server.ExportMethodTable(map[string]any{"SetSelection": func(_ dbus.ObjectPath, _ map[string]dbus.Variant) *dbus.Error {
		_ = server.Emit(portalPath, clipboardInterface+".SelectionOwnerChanged", d.path, map[string]dbus.Variant{"mime_types": dbus.MakeVariant([]string{"application/octet-stream"}), "session_is_owner": dbus.MakeVariant(true)})
		return nil
	}}, portalPath, clipboardInterface); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	update, err := c.set(ctx, map[string][]byte{"text/plain": []byte("内容")}, 0)
	if err == nil || !strings.Contains(clipboardCause(err), "mimetype_mismatch/timeout") || !update.applied || update.confirmed {
		t.Fatalf("格式未匹配被误确认或缺少固定诊断: %+v %v", update, err)
	}
}

func TestPortalClipboardConcurrentCloseRestoresWithWatchAlive(t *testing.T) {
	server, client := privateBus(t)
	d := clipboardDesktop(t, server, client)
	f := &clipboardFixture{conn: server, path: d.path, received: make(chan string, 4)}
	f.install(t)
	c := d.clipboard
	c.known, c.owner, c.epoch = true, true, 2
	c.data = map[string][]byte{"text/plain;charset=utf-8": []byte("原内容")}
	pasted := make(chan struct{}, 1)
	if err := server.ExportMethodTable(map[string]any{"NotifyKeyboardKeysym": func(_ dbus.ObjectPath, _ map[string]dbus.Variant, key int32, state uint32) *dbus.Error {
		if key == int32('v') && state == 1 {
			pasted <- struct{}{}
		}
		return nil
	}}, portalPath, remoteInterface); err != nil {
		t.Fatal(err)
	}
	providerAtClose := make(chan bool, 1)
	allowSessionClose := make(chan struct{})
	var allowOnce sync.Once
	allowClose := func() { allowOnce.Do(func() { close(allowSessionClose) }) }
	t.Cleanup(allowClose)
	if err := server.ExportMethodTable(map[string]any{"Close": func() *dbus.Error {
		c.mu.Lock()
		providerAtClose <- c.owner && string(c.data["text/plain;charset=utf-8"]) == "原内容" && c.recovery == nil
		c.mu.Unlock()
		<-allowSessionClose
		return nil
	}}, d.path, "org.freedesktop.portal.Session"); err != nil {
		t.Fatal(err)
	}
	type textResult struct {
		out InputResult
		err error
	}
	finished := make(chan textResult, 1)
	go func() {
		out, err := d.Text(context.Background(), TextInput{Mode: "clipboard", Text: "中文", PasteKeys: []string{"Ctrl", "V"}})
		finished <- textResult{out, err}
	}()
	select {
	case <-pasted:
	case <-time.After(time.Second):
		t.Fatal("文本未进入有意阻塞的接收等待")
	}
	closed := make(chan error, 1)
	go func() { closed <- d.Close() }()
	select {
	case ready := <-providerAtClose:
		if !ready {
			t.Fatal("Session.Close早于仍活watch确认的原provider恢复")
		}
	case <-time.After(time.Second):
		t.Fatal("并发关闭未到达授权关闭步骤")
	}
	// 授权关闭方法尚在执行，总线仍连接，也必须拒绝新的文本替换。
	_, err := d.Text(context.Background(), TextInput{Mode: "clipboard", Text: "再次输入", PasteKeys: []string{"Ctrl", "V"}})
	assertCode(t, err, "session_closed")
	allowClose()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("并发关闭未完成恢复: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("并发关闭不能取消文本并完成独立恢复")
	}
	result := <-finished
	if result.err == nil || result.out.ClipboardRestore != "restored" {
		t.Fatalf("并发关闭时文本没有独立恢复结果: %+v", result)
	}
	assertClipboardRawCleared(t, c)
	_, err = d.Text(context.Background(), TextInput{Mode: "clipboard", Text: "再次输入", PasteKeys: []string{"Ctrl", "V"}})
	assertCode(t, err, "session_closed")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sets != 2 {
		t.Fatalf("closing门控没有拒绝新替换: %d", f.sets)
	}
}

func TestPortalClipboardFailedRecoveryCloseClearsRaw(t *testing.T) {
	server, client := privateBus(t)
	d := clipboardDesktop(t, server, client)
	f := &clipboardFixture{conn: server, path: d.path, received: make(chan string, 4), failSet: true}
	f.install(t)
	c := d.clipboard
	c.known, c.owner, c.epoch = true, true, 2
	c.mimes = []string{"text/plain"}
	c.data = map[string][]byte{"text/plain": []byte("新内容")}
	c.recovery = map[string][]byte{"text/plain": []byte("原内容")}
	assertCode(t, d.Close(), "clipboard_restore_failed")
	assertClipboardRawCleared(t, c)
}

func TestPortalClipboardRestoresExplicitEmptySelection(t *testing.T) {
	server, client := privateBus(t)
	d := clipboardDesktop(t, server, client)
	f := &clipboardFixture{conn: server, path: d.path, received: make(chan string, 4)}
	f.install(t)
	f.owner(false, []string{})
	waitClipboardKnown(t, d.clipboard)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := d.Text(ctx, TextInput{Mode: "clipboard", Text: "中文文本", PasteKeys: []string{"Ctrl", "V"}})
	if err != nil || !out.Submitted || out.ClipboardRestore != "restored" || out.ClipboardReplaced {
		t.Fatalf("明确空selection未被安全恢复: %+v %v", out, err)
	}
	c := d.clipboard
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.known || c.owner || len(c.mimes) != 0 || c.data != nil {
		t.Fatal("清空确认后仍保留本工具的文本provider")
	}
}
func TestPortalClipboardRejectedUpdateKeepsExistingProvider(t *testing.T) {
	server, client := privateBus(t)
	d := clipboardDesktop(t, server, client)
	f := &clipboardFixture{conn: server, path: d.path, received: make(chan string, 4), failSet: true}
	f.install(t)
	c := d.clipboard
	c.mu.Lock()
	c.known = true
	c.owner = true
	c.epoch = 2
	c.data = map[string][]byte{"text/plain": []byte("原内容")}
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := c.text(ctx, TextInput{Text: "新内容", PasteKeys: []string{"Ctrl", "V"}})
	assertCode(t, err, "input_failed")
	c.mu.Lock()
	data := string(c.data["text/plain"])
	c.mu.Unlock()
	if data != "原内容" || out.ClipboardRestore != "restored" || out.Submitted {
		t.Fatalf("失败的所有权更新改变了既有provider: %+v %s", out, data)
	}
}
func TestPortalClipboardSaturationRejectsWithoutNewWorker(t *testing.T) {
	server, client := privateBus(t)
	d := clipboardDesktop(t, server, client)
	rejected := make(chan bool, 1)
	if err := server.ExportMethodTable(map[string]any{"SelectionWriteDone": func(_ dbus.ObjectPath, _ uint32, success bool) *dbus.Error { rejected <- success; return nil }}, portalPath, clipboardInterface); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < cap(d.clipboard.workers); i++ {
		d.clipboard.workers <- struct{}{}
	}
	d.clipboard.signal(&dbus.Signal{Name: clipboardInterface + ".SelectionTransfer", Body: []any{d.path, "text/plain", uint32(7)}})
	select {
	case success := <-rejected:
		if success {
			t.Fatal("饱和transfer应明确失败")
		}
	case <-time.After(time.Second):
		t.Fatal("饱和未拒绝")
	}
	if len(d.clipboard.workers) != cap(d.clipboard.workers) {
		t.Fatal("饱和额外创建了worker")
	}
}

func TestKDELayoutMonitorRequiresBaselineAndTracksChanges(t *testing.T) {
	server, client := privateBus(t)
	if _, err := server.RequestName(kdeLayout.service, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	d := clipboardDesktop(t, server, client)
	if err := server.ExportMethodTable(map[string]any{"requestBackend": func(_ string, _ map[string]dbus.Variant) (bool, *dbus.Error) {
		_ = server.ExportMethodTable(map[string]any{"getConfig": func() (map[string]dbus.Variant, *dbus.Error) {
			return map[string]dbus.Variant{"valid": dbus.MakeVariant(true)}, nil
		}}, kdeLayout.path, kdeLayout.iface)
		return true, nil
	}}, "/", "org.kde.KScreen"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if !d.monitorLayout(ctx) {
		t.Fatal("未探测真实导出的KScreen只读基线")
	}
	d.mu.Lock()
	d.displays = []Display{{AbsoluteInput: true, PixelWidth: 2880, PixelHeight: 1800}}
	d.caps.Reasons = map[string]string{}
	d.mu.Unlock()
	_ = server.Emit(kdeLayout.path, kdeLayout.iface+"."+kdeLayout.signal, map[string]dbus.Variant{"scale": dbus.MakeVariant(2.0)})
	deadline := time.Now().Add(time.Second)
	for {
		d.mu.Lock()
		invalid := d.layoutInvalidated
		d.mu.Unlock()
		if invalid {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("KScreen实际信号未使映射失效")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestLayoutActiveReadRejectsAuthorizationDriftAndLostOwner(t *testing.T) {
	for _, scenario := range []string{"authorization-drift", "owner-replaced", "disconnected"} {
		t.Run(scenario, func(t *testing.T) {
			server, client := privateBus(t)
			if _, err := server.RequestName(kdeLayout.service, dbus.NameFlagDoNotQueue); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			scale := 1.6
			if err := server.ExportMethodTable(map[string]any{"getConfig": func() (map[string]dbus.Variant, *dbus.Error) {
				mu.Lock()
				defer mu.Unlock()
				return map[string]dbus.Variant{"scale": dbus.MakeVariant(scale)}, nil
			}}, kdeLayout.path, kdeLayout.iface); err != nil {
				t.Fatal(err)
			}
			life, stop := context.WithCancel(context.Background())
			defer stop()
			// 不运行signal消费者，证明授权后主动读取本身能发现变化。
			d := &waylandDesktop{conn: client, life: life, caps: Capabilities{Reasons: map[string]string{}}}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if !d.monitorLayout(ctx) {
				t.Fatal("不能建立稳定基线")
			}
			d.displays = []Display{{AbsoluteInput: true, PixelWidth: 2880, PixelHeight: 1800}}
			switch scenario {
			case "authorization-drift":
				mu.Lock()
				scale = 2
				mu.Unlock()
			case "owner-replaced":
				if _, err := server.ReleaseName(kdeLayout.service); err != nil {
					t.Fatal(err)
				}
				if _, err := client.RequestName(kdeLayout.service, dbus.NameFlagDoNotQueue); err != nil {
					t.Fatal(err)
				}
				if err := client.ExportMethodTable(map[string]any{"getConfig": func() (map[string]dbus.Variant, *dbus.Error) {
					return map[string]dbus.Variant{"scale": dbus.MakeVariant(1.6)}, nil
				}}, kdeLayout.path, kdeLayout.iface); err != nil {
					t.Fatal(err)
				}
			case "disconnected":
				_ = client.Close()
			}
			d.verifyLayout(ctx)
			if !d.layoutInvalidated || d.displays[0].AbsoluteInput || d.caps.Reasons["absolute_input"] == "" {
				t.Fatal("授权后的配置/服务变化未禁用旧映射")
			}
			// 失效标志不能因随后回到相同配置而重置。
			d.verifyLayout(ctx)
			if !d.layoutInvalidated {
				t.Fatal("旧会话映射被意外恢复")
			}
		})
	}
}

func TestWaylandCloseReportsCleanupFailures(t *testing.T) {
	for _, scenario := range []string{"release", "session"} {
		t.Run(scenario, func(t *testing.T) {
			server, client := privateBus(t)
			d := clipboardDesktop(t, server, client)
			if err := server.ExportMethodTable(map[string]any{"Close": func() *dbus.Error {
				if scenario == "session" {
					return dbus.NewError("org.freedesktop.portal.Error.Failed", nil)
				}
				return nil
			}}, d.path, "org.freedesktop.portal.Session"); err != nil {
				t.Fatal(err)
			}
			if scenario == "release" {
				d.held = map[string]int32{"NotifyKeyboardKeysym:65": 65}
				if err := server.ExportMethodTable(map[string]any{"NotifyKeyboardKeysym": func(_ dbus.ObjectPath, _ map[string]dbus.Variant, _ int32, _ uint32) *dbus.Error {
					return dbus.NewError("org.freedesktop.portal.Error.Failed", nil)
				}}, portalPath, remoteInterface); err != nil {
					t.Fatal(err)
				}
				assertCode(t, d.Close(), "input_failed")
			} else {
				assertCode(t, d.Close(), "session_closed")
			}
		})
	}
}
