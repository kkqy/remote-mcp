//go:build linux

package fileops

import (
	"os"

	"golang.org/x/sys/unix"
)

func statPath(path string) (os.FileInfo, error) { return os.Lstat(path) }

// 非阻塞且拒绝最终路径链接，防止检查后被替换成管道或符号链接。
func openFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
