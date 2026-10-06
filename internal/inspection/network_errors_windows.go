package inspection

import "golang.org/x/sys/windows"

// Windows 网络返回 Winsock 错误，不能与 syscall 的模拟 POSIX 编号比较。
const (
	connectionRefusedErrno  = windows.WSAECONNREFUSED
	networkUnreachableErrno = windows.WSAENETUNREACH
	hostUnreachableErrno    = windows.WSAEHOSTUNREACH
	connectionTimedOutErrno = windows.WSAETIMEDOUT
)
