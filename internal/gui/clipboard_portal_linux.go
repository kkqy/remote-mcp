package gui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

type portalClipboard struct {
	desktop      *waylandDesktop
	mu           sync.Mutex
	opMu         sync.Mutex
	abort        context.CancelFunc
	pending      bool
	unconfirmed  map[string][]byte
	closing      bool
	recovery     map[string][]byte
	known, owner bool
	mimes        []string
	epoch        uint64
	data         map[string][]byte
	changed      chan struct{}
	transferred  chan struct{}
	workers      chan struct{}
}

func newPortalClipboard(d *waylandDesktop) *portalClipboard {
	return &portalClipboard{desktop: d, changed: make(chan struct{}, 1), transferred: make(chan struct{}, 1), workers: make(chan struct{}, 8)}
}
func (c *portalClipboard) call(ctx context.Context, method string, args ...any) *dbus.Call {
	return c.desktop.conn.Object(portalName, portalPath).CallWithContext(ctx, clipboardInterface+"."+method, 0, append([]any{c.desktop.path}, args...)...)
}
func (c *portalClipboard) signal(sig *dbus.Signal) {
	if len(sig.Body) < 1 {
		return
	}
	path, ok := sig.Body[0].(dbus.ObjectPath)
	if !ok || path != c.desktop.path {
		return
	}
	switch sig.Name {
	case clipboardInterface + ".SelectionOwnerChanged":
		var mimes []string
		var own, valid bool
		if len(sig.Body) == 2 {
			if opts, ok := sig.Body[1].(map[string]dbus.Variant); ok {
				var mimeOK, ownerOK bool
				mimes, mimeOK = opts["mime_types"].Value().([]string)
				own, ownerOK = opts["session_is_owner"].Value().(bool)
				valid = mimeOK && ownerOK && len(mimes) <= 64
				for _, mime := range mimes {
					if mime == "" || len(mime) > 4096 {
						valid = false
					}
				}
			}
		}
		c.mu.Lock()
		// 缺失或非法可选信息不等于已知空剪贴板，旧快照也必须失效。
		c.known = valid
		c.owner = valid && own
		c.mimes = nil
		if valid {
			c.mimes = append([]string{}, mimes...)
		}
		c.epoch++
		if c.owner && c.unconfirmed != nil && sameMIMEs(c.mimes, c.unconfirmed) {
			// 设置确认可能晚于请求取消；仍只为本会话实际拥有的目标格式提供数据。
			c.data = c.unconfirmed
			c.unconfirmed = nil
		}
		if !c.owner && !c.pending {
			c.data = nil
		}
		c.mu.Unlock()
		select {
		case c.changed <- struct{}{}:
		default:
		}
	case clipboardInterface + ".SelectionTransfer":
		if len(sig.Body) != 3 {
			return
		}
		mime, ok := sig.Body[1].(string)
		serial, valid := sig.Body[2].(uint32)
		if !ok || !valid {
			return
		}
		select {
		case c.workers <- struct{}{}:
			c.desktop.wg.Add(1)
			go func() { defer c.desktop.wg.Done(); defer func() { <-c.workers }(); c.transfer(mime, serial) }()
		default:
			ctx, cancel := context.WithTimeout(c.desktop.life, 100*time.Millisecond)
			_ = c.call(ctx, "SelectionWriteDone", serial, false).Err
			cancel()
		}
	}
}
func (c *portalClipboard) transfer(mime string, serial uint32) {
	ctx, cancel := context.WithTimeout(c.desktop.life, 5*time.Second)
	defer cancel()
	c.mu.Lock()
	data, ok := c.data[mime]
	c.mu.Unlock()
	success := false
	if ok {
		var fd dbus.UnixFD
		if err := c.call(ctx, "SelectionWrite", serial).Store(&fd); err == nil {
			file := os.NewFile(uintptr(fd), "portal-clipboard-write")
			err = fdWrite(ctx, file, data)
			_ = file.Close()
			success = err == nil
		}
	}
	_ = c.call(ctx, "SelectionWriteDone", serial, success).Err
	if success {
		select {
		case c.transferred <- struct{}{}:
		default:
		}
	}
}
func cloneClipboard(data map[string][]byte) map[string][]byte {
	out := map[string][]byte{}
	for k, v := range data {
		out[k] = append([]byte{}, v...)
	}
	return out
}

func sameMIMEs(mimes []string, data map[string][]byte) bool {
	if len(mimes) != len(data) {
		return false
	}
	seen := map[string]bool{}
	for _, mime := range mimes {
		if _, exists := data[mime]; !exists || seen[mime] {
			return false
		}
		seen[mime] = true
	}
	return true
}

// 仅提供固定原因和计数，不拼接 D-Bus Body、MIME 名称或剪贴板字节。
func clipboardCause(err error) string {
	var business *Error
	if errors.As(err, &business) {
		return business.Code + ": " + business.Message
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "transport_error"
}

func (c *portalClipboard) prepareClose() error {
	c.mu.Lock()
	c.closing = true
	abort := c.abort
	c.mu.Unlock()
	if abort != nil {
		abort()
	}
	// 独立清理仍需watch和FD传输，不能先取消桌面生命周期。
	c.opMu.Lock()
	defer c.opMu.Unlock()
	c.mu.Lock()
	old, known, own, epoch := c.recovery, c.known, c.owner, c.epoch
	c.mu.Unlock()
	if old == nil {
		return nil
	}
	if !known || !own {
		return failure("clipboard_restore_failed", "关闭前保留了原快照，但不能确认仍有恢复权限；未覆盖其他应用")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := c.set(ctx, old, epoch); err != nil {
		return failure("clipboard_restore_failed", "关闭前再次恢复失败（"+clipboardCause(err)+"）")
	}
	c.mu.Lock()
	c.recovery = nil
	c.mu.Unlock()
	return nil
}
func (c *portalClipboard) snapshot(ctx context.Context) (map[string][]byte, uint64, error) {
	c.mu.Lock()
	known, own, epoch := c.known, c.owner, c.epoch
	mimes := append([]string{}, c.mimes...)
	for _, mime := range mimes {
		if mime == "application/x-kde-onlyReplaceEmpty" {
			c.mu.Unlock()
			return nil, epoch, failure("clipboard_preservation_unavailable", "剪贴板包含 KDE onlyReplaceEmpty 所有权控制格式，不能在非空 selection 上原样恢复")
		}
	}
	if own && c.data != nil {
		data := cloneClipboard(c.data)
		c.mu.Unlock()
		return data, epoch, nil
	}
	c.mu.Unlock()
	if !known {
		return nil, epoch, failure("clipboard_preservation_unavailable", "Portal 尚未提供可靠的剪贴板所有权及格式快照")
	}
	if len(mimes) > 64 {
		return nil, epoch, failure("clipboard_preservation_unavailable", "剪贴板格式数量超过可保存上限")
	}
	out := map[string][]byte{}
	remaining := c.desktop.cfg.MaxClipboardBytes
	for _, mime := range mimes {
		if mime == "application/vnd.portal.filetransfer" || mime == "x-special/gnome-copied-files" {
			return nil, epoch, failure("clipboard_preservation_unavailable", "剪贴板包含不能可靠恢复的文件传输格式")
		}
		var fd dbus.UnixFD
		if err := c.call(ctx, "SelectionRead", mime).Store(&fd); err != nil {
			return nil, epoch, failure("clipboard_preservation_unavailable", "原剪贴板格式无法读取")
		}
		file := os.NewFile(uintptr(fd), "portal-clipboard-read")
		data, err := fdRead(ctx, file, remaining)
		_ = file.Close()
		if err != nil {
			return nil, epoch, failure("clipboard_preservation_unavailable", "原剪贴板读取失败或超过限制")
		}
		remaining -= len(data)
		out[mime] = data
	}
	c.mu.Lock()
	same := c.known && c.epoch == epoch
	c.mu.Unlock()
	if !same {
		return nil, epoch, failure("clipboard_preservation_unavailable", "保存过程中剪贴板所有者发生改变")
	}
	return out, epoch, nil
}

type selectionUpdate struct {
	epoch                                  uint64
	applied, confirmed, providerRolledBack bool
}

func (c *portalClipboard) set(ctx context.Context, data map[string][]byte, expected uint64) (update selectionUpdate, err error) {
	mimes := make([]string, 0, len(data))
	for mime := range data {
		mimes = append(mimes, mime)
	}
	sort.Strings(mimes)
	c.mu.Lock()
	if c.epoch != expected {
		c.mu.Unlock()
		return update, failure("clipboard_preservation_unavailable", "替换前剪贴板所有者发生改变")
	}
	previous := c.data
	previousOwner := c.owner
	c.data = data
	c.unconfirmed = nil
	c.pending = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.pending = false
		if update.applied && !update.confirmed {
			c.unconfirmed = data
		}
		if !c.owner {
			c.data = nil
		}
		c.mu.Unlock()
	}()
	callErr := c.call(ctx, "SetSelection", map[string]dbus.Variant{"mime_types": dbus.MakeVariant(mimes)}).Err
	if callErr != nil {
		c.mu.Lock()
		if c.epoch == expected {
			c.data = previous
			update.providerRolledBack = previousOwner && previous != nil
		}
		update.epoch = c.epoch
		c.mu.Unlock()
		var remote dbus.Error
		update.applied = !errors.As(callErr, &remote)
		cause := "transport_error"
		if errors.As(callErr, &remote) {
			// 错误名称属于协议类型，不使用可能包含任意内容的remote.Body。
			cause = "dbus_error"
			switch remote.Name {
			case "org.freedesktop.DBus.Error.AccessDenied", "org.freedesktop.DBus.Error.InvalidArgs", "org.freedesktop.portal.Error.Failed", "org.freedesktop.DBus.Error.Failed":
				cause = remote.Name
			}
		} else if errors.Is(callErr, context.Canceled) || errors.Is(callErr, context.DeadlineExceeded) {
			cause = clipboardCause(callErr)
		}
		return update, failure("input_failed", "Portal 剪贴板设置失败：method_error/"+cause)
	}
	update.applied = true
	for {
		c.mu.Lock()
		own := c.known && c.owner
		cleared := c.known && !c.owner && len(c.mimes) == 0 && len(data) == 0
		matching := c.known && sameMIMEs(c.mimes, data)
		known := c.known
		mimeCount := len(c.mimes)
		update.epoch = c.epoch
		c.mu.Unlock()
		// KDE 清空selection后明确通知空格式且非session owner；这也是成功恢复空快照。
		if (own && matching || cleared) && update.epoch > expected {
			update.confirmed = true
			return update, nil
		}
		select {
		case <-c.changed:
		case <-ctx.Done():
			if update.epoch > expected {
				reason := "owner_unknown"
				if known && !own {
					reason = "owner_false"
				} else if own && !matching {
					reason = "mimetype_mismatch"
				}
				return update, failure("clipboard_preservation_unavailable", fmt.Sprintf("设置未获目标格式及所有权确认：%s/%s（代次%d→%d，格式数%d）", reason, clipboardCause(ctx.Err()), expected, update.epoch, mimeCount))
			}
			return update, ctx.Err()
		}
	}
}

func (c *portalClipboard) text(ctx context.Context, in TextInput) (out InputResult, err error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	c.mu.Lock()
	if c.closing {
		c.mu.Unlock()
		return out, failure("session_closed", "桌面授权会话正在关闭")
	}
	if c.recovery != nil {
		c.mu.Unlock()
		return out, failure("clipboard_restore_failed", "前次原剪贴板仍待恢复，拒绝再次替换")
	}
	c.abort = cancel
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.abort = nil; c.mu.Unlock() }()
	out.Mode = "clipboard"
	out.ClipboardRestore = "not_attempted"
	if _, err := keysyms(in.PasteKeys); err != nil {
		return out, err
	}
	old, epoch, saveErr := c.snapshot(ctx)
	preserve := saveErr == nil
	if !preserve && !in.AllowClipboardReplace {
		return out, saveErr
	}
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	if preserve {
		c.mu.Lock()
		c.recovery = old
		c.mu.Unlock()
	}
	if !preserve {
		c.mu.Lock()
		epoch = c.epoch
		c.mu.Unlock()
		out.ClipboardReplaced = true
		out.ClipboardRestore = "not_requested"
	}
	for {
		select {
		case <-c.transferred:
			continue
		default:
			goto drained
		}
	}
drained:
	var update selectionUpdate
	defer func() {
		if !preserve {
			return
		}
		if update.providerRolledBack {
			out.ClipboardRestore = "restored"
			c.mu.Lock()
			c.recovery = nil
			c.mu.Unlock()
			return
		}
		if !update.applied {
			out.ClipboardRestore = "not_attempted"
			c.mu.Lock()
			c.recovery = nil
			c.mu.Unlock()
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var ownedEpoch uint64
		for {
			c.mu.Lock()
			owner, known, epochNow := c.owner, c.known, c.epoch
			c.mu.Unlock()
			if known && owner && epochNow > epoch {
				ownedEpoch = epochNow
				break
			}
			if known && !owner && epochNow > epoch {
				if !update.confirmed {
					out.ClipboardRestore = "unknown"
					err = &Error{Code: "clipboard_restore_failed", Message: "设置尚未确认，非owner通知不能证明第三方最终接管；原快照保留到安全恢复或关闭（" + clipboardCause(err) + "）", InputMayHaveApplied: out.Submitted, ClipboardRestore: "unknown"}
					return
				}
				out.ClipboardRestore = "skipped_new_owner"
				c.mu.Lock()
				c.recovery = nil
				c.mu.Unlock()
				return
			}
			select {
			case <-c.changed:
			case <-cleanup.Done():
				out.ClipboardRestore = "unknown"
				err = &Error{Code: "clipboard_restore_failed", Message: "剪贴板替换可能已生效，但无法确认安全恢复所需的所有权", InputMayHaveApplied: out.Submitted, ClipboardRestore: "unknown"}
				return
			}
		}
		if _, restoreErr := c.set(cleanup, old, ownedEpoch); restoreErr != nil {
			out.ClipboardRestore = "failed"
			err = &Error{Code: "clipboard_restore_failed", Message: "文本事件可能已经生效，但原剪贴板恢复失败（" + clipboardCause(restoreErr) + "）", InputMayHaveApplied: out.Submitted, ClipboardRestore: "failed"}
		} else {
			out.ClipboardRestore = "restored"
			c.mu.Lock()
			c.recovery = nil
			c.mu.Unlock()
		}
		var typed *Error
		if errors.As(err, &typed) {
			copy := *typed
			copy.ClipboardRestore = out.ClipboardRestore
			err = &copy
		}
	}()
	update, err = c.set(ctx, map[string][]byte{"text/plain;charset=utf-8": []byte(in.Text), "text/plain": []byte(in.Text)}, epoch)
	if err != nil {
		return out, err
	}
	if err := c.desktop.Key(ctx, in.PasteKeys); err != nil {
		return out, partialError(err)
	}
	out.Submitted = true
	select {
	case <-c.transferred:
	case <-ctx.Done():
		return out, partialError(contextError(ctx))
	}
	// 接收方取得内容不证明控件已渲染；留出有界读取完成间隔后再恢复。
	if err := pause(ctx, 100*time.Millisecond); err != nil {
		return out, partialError(err)
	}
	return out, nil
}
