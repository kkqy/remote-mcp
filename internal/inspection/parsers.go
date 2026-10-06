package inspection

import (
	"context"
	"encoding/binary"
	"net"
)

// DWORD 字段四字节对齐；IPv4 row 为 24 字节，IPv6 row 为 56 字节。
// 字段布局依据微软 tcpmib.h 的 MIB_TCPROW_OWNER_PID / MIB_TCP6ROW_OWNER_PID。
func parseWindowsTCP(ctx context.Context, b []byte, ipv6 bool, max int) ([]Listener, bool, error) {
	items := []Listener{}
	size := 24
	if ipv6 {
		size = 56
	}
	if len(b) < 4 {
		return items, false, failure("io_error", "The TCP owner table is malformed")
	}
	n := uint64(binary.LittleEndian.Uint32(b))
	if n > uint64((len(b)-4)/size) {
		return items, false, failure("io_error", "The TCP owner table length is invalid")
	}
	for i := uint64(0); i < n; i++ {
		if ctx.Err() != nil {
			return items, true, contextError(ctx.Err())
		}
		row := b[4+int(i)*size : 4+(int(i)+1)*size]
		var address net.IP
		var state, pid, scope uint32
		port := 0
		if ipv6 {
			address = net.IP(row[:16])
			scope = binary.BigEndian.Uint32(row[16:20])
			port = int(binary.BigEndian.Uint16(row[20:22]))
			state = binary.LittleEndian.Uint32(row[48:52])
			pid = binary.LittleEndian.Uint32(row[52:56])
		} else {
			state = binary.LittleEndian.Uint32(row[:4])
			address = net.IP(row[4:8])
			port = int(binary.BigEndian.Uint16(row[8:10]))
			pid = binary.LittleEndian.Uint32(row[20:24])
		}
		if state != 2 {
			continue
		}
		if len(items) >= max {
			return items, true, nil
		}
		items = append(items, Listener{Protocol: "tcp", Address: address.String(), Port: port, PIDs: []int{int(pid)}, PIDVisibility: "complete", ScopeID: scope})
	}
	return items, false, nil
}

// 调用状态直接使用返回 DWORD，不能读取 LazyProc 的 last-error 当作成功判定。
func queryTCPTable(ctx context.Context, maxBytes int, call func([]byte, *uint32) uint32) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, contextError(ctx.Err())
	}
	var size uint32
	status := call(nil, &size)
	if status != 122 && status != 0 {
		return nil, tcpTableError(status)
	}
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil {
			return nil, contextError(ctx.Err())
		}
		if size < 4 || uint64(size) > uint64(maxBytes) {
			return nil, failure("resource_limit", "The TCP owner table exceeded the byte limit")
		}
		b := make([]byte, int(size))
		capacity := size
		status = call(b, &size)
		if status == 0 {
			if size > capacity || size < 4 {
				return nil, failure("io_error", "The TCP owner table length is invalid")
			}
			return b[:size], nil
		}
		if status != 122 {
			return nil, tcpTableError(status)
		}
	}
	return nil, failure("resource_limit", "The TCP owner table changed beyond the bounded retry limit")
}
func tcpTableError(status uint32) *Error {
	switch status {
	case 5:
		return failure("permission_denied", "The TCP owner table is not accessible with the current permissions")
	case 50:
		return failure("unsupported", "The TCP owner table API is not supported")
	default:
		return failure("io_error", "The TCP owner table API failed")
	}
}
