package logstream

import (
	"golang.org/x/sys/windows"
	"os"
)

// 允许其他进程轮转打开的文件，且不追随检查后的重解析点替换。
func openFile(path string) (*os.File, error) {
	return openFileAccess(path, windows.GENERIC_READ)
}

// Windows 路径 Stat 延迟读取身份，必须在下一次打开前以句柄保存稳定快照。
func statPath(path string) (os.FileInfo, error) {
	initial, err := os.Lstat(path)
	if err != nil || !initial.Mode().IsRegular() {
		return initial, err
	}
	f, err := openFileAccess(path, 0)
	if err != nil {
		return nil, err
	}
	info, statErr := f.Stat()
	closeErr := f.Close()
	if statErr != nil {
		return nil, statErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return info, nil
}

func openFileAccess(path string, access uint32) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(name, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}
