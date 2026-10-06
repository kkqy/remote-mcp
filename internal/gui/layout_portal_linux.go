package gui

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/godbus/dbus/v5"
)

type layoutEndpoint struct {
	service             string
	path                dbus.ObjectPath
	iface, signal, read string
}

var gnomeLayout = layoutEndpoint{"org.gnome.Mutter.DisplayConfig", "/org/gnome/Mutter/DisplayConfig", "org.gnome.Mutter.DisplayConfig", "MonitorsChanged", "GetCurrentState"}
var kdeLayout = layoutEndpoint{"org.kde.KScreen", "/backend", "org.kde.kscreen.Backend", "configChanged", "getConfig"}

func layoutDigest(value any) ([32]byte, error) {
	nodes := 0
	var normalize func(reflect.Value, int) (any, error)
	normalize = func(v reflect.Value, depth int) (any, error) {
		nodes++
		if nodes > 8192 || depth > 24 {
			return nil, errors.New("布局数据过大")
		}
		if !v.IsValid() {
			return nil, nil
		}
		if v.Kind() == reflect.Interface {
			return normalize(v.Elem(), depth+1)
		}
		if variant, ok := v.Interface().(dbus.Variant); ok {
			return normalize(reflect.ValueOf(variant.Value()), depth+1)
		}
		switch v.Kind() {
		case reflect.Map:
			out := map[string]any{}
			if v.Len() > 256 || v.Type().Key().Kind() != reflect.String {
				return nil, errors.New("布局字典无效")
			}
			for _, key := range v.MapKeys() {
				data, err := normalize(v.MapIndex(key), depth+1)
				if err != nil {
					return nil, err
				}
				out[key.String()] = data
			}
			return out, nil
		case reflect.Slice, reflect.Array:
			if v.Len() > 4096 {
				return nil, errors.New("布局数组过大")
			}
			out := make([]any, v.Len())
			for i := range out {
				data, err := normalize(v.Index(i), depth+1)
				if err != nil {
					return nil, err
				}
				out[i] = data
			}
			return out, nil
		case reflect.String:
			if v.Len() > 4096 {
				return nil, errors.New("布局字符串过大")
			}
			return v.String(), nil
		default:
			return v.Interface(), nil
		}
	}
	data, err := normalize(reflect.ValueOf(value), 0)
	if err != nil {
		return [32]byte{}, err
	}
	output := &limitedBuffer{limit: 256 << 10}
	if err := json.NewEncoder(output).Encode(data); err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(output.Bytes()), nil
}
func (d *waylandDesktop) readLayout(ctx context.Context, e layoutEndpoint) ([32]byte, string, error) {
	call := d.conn.Object(e.service, e.path).CallWithContext(ctx, e.iface+"."+e.read, 0)
	if call.Err != nil {
		return [32]byte{}, "", call.Err
	}
	if len(call.Body) == 0 {
		return [32]byte{}, "", errors.New("布局响应为空")
	}
	var value any
	if e == gnomeLayout {
		serial, ok := call.Body[0].(uint32)
		if !ok {
			return [32]byte{}, "", errors.New("布局序号无效")
		}
		value = serial
	} else {
		config, ok := call.Body[0].(map[string]dbus.Variant)
		if !ok || len(config) == 0 {
			return [32]byte{}, "", errors.New("布局字典无效")
		}
		value = config
	}
	digest, err := layoutDigest(value)
	if err != nil {
		return [32]byte{}, "", err
	}
	var owner string
	if err := d.conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, e.service).Store(&owner); err != nil || owner == "" {
		return [32]byte{}, "", errors.New("布局服务不可确认")
	}
	return digest, owner, nil
}

// 先订阅再读取及核验基线；接口名称存在本身不表示监视已经可用。
func (d *waylandDesktop) monitorLayout(ctx context.Context) bool {
	for _, e := range []layoutEndpoint{gnomeLayout, kdeLayout} {
		change := []dbus.MatchOption{dbus.WithMatchSender(e.service), dbus.WithMatchObjectPath(e.path), dbus.WithMatchInterface(e.iface), dbus.WithMatchMember(e.signal)}
		ownerMatch := []dbus.MatchOption{dbus.WithMatchSender("org.freedesktop.DBus"), dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchArg(0, e.service)}
		if d.conn.AddMatchSignalContext(ctx, change...) != nil {
			continue
		}
		if d.conn.AddMatchSignalContext(ctx, ownerMatch...) != nil {
			_ = d.conn.RemoveMatchSignalContext(ctx, change...)
			continue
		}
		digest, owner, err := d.readLayout(ctx, e)
		if err != nil && e == kdeLayout {
			var initialized bool
			if d.conn.Object(e.service, "/").CallWithContext(ctx, "org.kde.KScreen.requestBackend", 0, "KWayland", map[string]dbus.Variant{}).Store(&initialized) == nil && initialized {
				digest, owner, err = d.readLayout(ctx, e)
			}
		}
		if err == nil {
			// 第二次读取确认建立基线期间没有配置/owner漂移；随后授权期间任何漂移都会失效。
			again, againOwner, readErr := d.readLayout(ctx, e)
			if readErr == nil && again == digest && againOwner == owner {
				d.mu.Lock()
				d.monitorEndpoint = e
				d.layoutDigest = digest
				d.layoutOwner = owner
				d.mu.Unlock()
				return true
			}
		}
		_ = d.conn.RemoveMatchSignalContext(ctx, change...)
		_ = d.conn.RemoveMatchSignalContext(ctx, ownerMatch...)
	}
	return false
}
func (d *waylandDesktop) markLayoutInvalid() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.layoutInvalidated = true
	for i := range d.displays {
		d.displays[i].AbsoluteInput = false
	}
	if len(d.displays) > 0 {
		if d.caps.Reasons == nil {
			d.caps.Reasons = map[string]string{}
		}
		d.caps.Reasons["absolute_input"] = "显示器布局、缩放或监视服务发生改变；需要重新打开授权会话"
	}
}

// 主动读取补充异步信号，避免信号尚未处理时使用旧截图坐标。
func (d *waylandDesktop) verifyLayout(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	d.mu.Lock()
	e, baseline, owner := d.monitorEndpoint, d.layoutDigest, d.layoutOwner
	invalid := d.layoutInvalidated
	d.mu.Unlock()
	if e.service == "" || invalid {
		return
	}
	current, currentOwner, err := d.readLayout(ctx, e)
	if err != nil || current != baseline || currentOwner != owner {
		d.markLayoutInvalid()
	}
}
func (d *waylandDesktop) layoutSignal(signal *dbus.Signal) bool {
	d.mu.Lock()
	e, baseline, owner := d.monitorEndpoint, d.layoutDigest, d.layoutOwner
	d.mu.Unlock()
	if e.service == "" {
		return false
	}
	if signal.Name == "org.freedesktop.DBus.NameOwnerChanged" && len(signal.Body) == 3 {
		name, ok := signal.Body[0].(string)
		old, oldOK := signal.Body[1].(string)
		next, nextOK := signal.Body[2].(string)
		if ok && oldOK && nextOK && name == e.service {
			if old != "" || next != owner {
				d.markLayoutInvalid()
			}
			return true
		}
	}
	if signal.Path != e.path || signal.Name != e.iface+"."+e.signal {
		return false
	}
	if e == kdeLayout {
		if len(signal.Body) != 1 {
			return false
		}
		config, ok := signal.Body[0].(map[string]dbus.Variant)
		if !ok {
			return false
		}
		digest, err := layoutDigest(config)
		if err != nil || digest != baseline {
			d.markLayoutInvalid()
		}
		return true
	}
	if len(signal.Body) != 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(d.life, 2*time.Second)
	defer cancel()
	d.verifyLayout(ctx)
	return true
}
