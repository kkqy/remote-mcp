package inspection

import "syscall"

const (
	connectionRefusedErrno  = syscall.ECONNREFUSED
	networkUnreachableErrno = syscall.ENETUNREACH
	hostUnreachableErrno    = syscall.EHOSTUNREACH
	connectionTimedOutErrno = syscall.ETIMEDOUT
)
