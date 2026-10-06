//go:build linux || darwin

package logstream

import (
	"golang.org/x/sys/unix"
	"os"
)

// 非阻塞且拒绝符号链接，避免路径检查后被替换为管道或链接而挂起。
func openFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
