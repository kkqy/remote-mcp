package gui

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jezek/xgb"
)

// XGB 的 NewConnNet 没有显示编号参数。此传输适配仅替换首个认证报文，
// 让可取消的 DialContext 与正确显示器的 Xauthority 同时生效，不改全局环境。
type x11AuthConn struct {
	net.Conn
	once     sync.Once
	auth     []byte
	writeErr error
}

func (c *x11AuthConn) Write(data []byte) (int, error) {
	first := false
	c.once.Do(func() { first = true; _, c.writeErr = writeAll(c.Conn, c.auth) })
	if first {
		if c.writeErr != nil {
			return 0, c.writeErr
		}
		return len(data), nil
	}
	return c.Conn.Write(data)
}
func writeAll(w io.Writer, data []byte) (int, error) {
	total := 0
	for len(data) > 0 {
		n, err := w.Write(data)
		total += n
		data = data[n:]
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}
func x11Address(display string) (network, address, host, number string, screen int, err error) {
	colon := strings.LastIndex(display, ":")
	if colon < 0 {
		err = errors.New("无效显示器")
		return
	}
	host = display[:colon]
	suffix := strings.Split(display[colon+1:], ".")
	number = suffix[0]
	n, e := strconv.Atoi(number)
	if e != nil || n < 0 || n > 59535 {
		err = errors.New("无效显示器")
		return
	}
	if len(suffix) > 2 {
		err = errors.New("无效屏幕编号")
		return
	}
	if len(suffix) == 2 {
		screen, e = strconv.Atoi(suffix[1])
		if e != nil || screen < 0 || screen > 255 {
			err = errors.New("无效屏幕编号")
			return
		}
	}
	network = "unix"
	if strings.HasPrefix(host, "/") {
		address = host + ":" + number
		host = ""
		return
	}
	host = strings.TrimPrefix(host, "unix/")
	if host == "" || host == "unix" {
		host = ""
		address = "/tmp/.X11-unix/X" + number
		return
	}
	host = strings.TrimPrefix(host, "tcp/")
	network = "tcp"
	address = net.JoinHostPort(host, strconv.Itoa(6000+n))
	return
}
func x11Cookie(host, number string) ([]byte, error) {
	path := os.Getenv("XAUTHORITY")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, ".Xauthority")
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, errors.New("认证文件不可读取")
	}
	reader := io.LimitReader(file, 1<<20)
	hostname, _ := os.Hostname()
	readField := func() ([]byte, error) {
		var length uint16
		if err := binary.Read(reader, binary.BigEndian, &length); err != nil {
			return nil, err
		}
		data := make([]byte, int(length))
		_, err := io.ReadFull(reader, data)
		return data, err
	}
	for {
		var family uint16
		if err := binary.Read(reader, binary.BigEndian, &family); err != nil {
			if err == io.EOF {
				return nil, nil
			}
			return nil, err
		}
		addr, err := readField()
		if err != nil {
			return nil, err
		}
		disp, err := readField()
		if err != nil {
			return nil, err
		}
		name, err := readField()
		if err != nil {
			return nil, err
		}
		data, err := readField()
		if err != nil {
			return nil, err
		}
		addrMatch := family == 65535 || family == 256 && (string(addr) == hostname || string(addr) == host || host == "localhost")
		if family == 0 || family == 6 {
			ip := net.ParseIP(host)
			addrMatch = ip != nil && ip.Equal(net.IP(addr))
		}
		if addrMatch && (string(disp) == number || len(disp) == 0) && string(name) == "MIT-MAGIC-COOKIE-1" && len(data) == 16 {
			return data, nil
		}
	}
}
func x11Handshake(cookie []byte) []byte {
	name := []byte{}
	if len(cookie) > 0 {
		name = []byte("MIT-MAGIC-COOKIE-1")
	}
	pad := func(n int) int { return (n + 3) &^ 3 }
	data := make([]byte, 12+pad(len(name))+pad(len(cookie)))
	data[0] = 'l'
	binary.LittleEndian.PutUint16(data[2:], 11)
	binary.LittleEndian.PutUint16(data[6:], uint16(len(name)))
	binary.LittleEndian.PutUint16(data[8:], uint16(len(cookie)))
	copy(data[12:], name)
	copy(data[12+pad(len(name)):], cookie)
	return data
}
func x11ConnectTransport(ctx context.Context, display string) (*xgb.Conn, net.Conn, error) {
	network, address, host, number, screen, err := x11Address(display)
	if err != nil {
		return nil, nil, failure("no_gui", "X11 显示器地址无效")
	}
	raw, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		return nil, nil, failure("permission_denied", "无法连接当前 X11 显示服务器")
	}
	cookie, err := x11Cookie(host, number)
	if err == nil && len(cookie) == 0 && network == "tcp" {
		if remote, ok := raw.RemoteAddr().(*net.TCPAddr); ok {
			cookie, err = x11Cookie(remote.IP.String(), number)
		}
	}
	if err != nil {
		_ = raw.Close()
		return nil, nil, failure("permission_denied", "当前显示器的 Xauthority 无法读取")
	}

	if deadline, ok := ctx.Deadline(); ok {
		_ = raw.SetDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()
	conn, err := safeX11Connection(&x11AuthConn{Conn: raw, auth: x11Handshake(cookie)})
	if err != nil {
		_ = raw.Close()
		return nil, nil, failure("permission_denied", "X11 显示服务器认证失败")
	}
	conn.DefaultScreen = screen
	conn.DisplayNumber, _ = strconv.Atoi(number)
	if len(conn.SetupBytes) < 40 || screen >= int(conn.SetupBytes[28]) {
		conn.Close()
		_ = raw.Close()
		return nil, nil, failure("no_gui", "X11 屏幕编号不存在")
	}
	if ctx.Err() != nil {
		conn.Close()
		_ = raw.Close()
		return nil, nil, ctx.Err()
	}
	return conn, raw, nil
}
func (d *x11Desktop) protocolDeadline(ctx context.Context) func() {
	deadline := time.Now().Add(d.cfg.OperationTimeout)
	if until, ok := ctx.Deadline(); ok && until.Before(deadline) {
		deadline = until
	}
	_ = d.raw.SetDeadline(deadline)
	completed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = d.raw.Close(); close(completed) })
	return func() {
		if !stop() {
			<-completed
		}
		if ctx.Err() == nil {
			_ = d.raw.SetDeadline(time.Time{})
		}
	}
}

func safeX11Connection(transport net.Conn) (conn *xgb.Conn, err error) {
	defer func() {
		if recover() != nil {
			_ = transport.Close()
			conn = nil
			err = failure("no_gui", "X11 握手响应格式无效")
		}
	}()
	return xgb.NewConnNet(transport)
}
