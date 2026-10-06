package fileops

import (
	"os"

	"golang.org/x/sys/windows"
)

// 打开最终重解析点本身；目录需要 BACKUP_SEMANTICS，随后统一核对类型和身份。
func openFile(path string) (*os.File, error) {
	return openFileAccess(path, windows.GENERIC_READ)
}

// 路径 Stat 在 Windows 上会延迟查询身份；句柄 Stat 立即冻结 volume/file index。
// 只请求元数据，避免为初始类型和身份检查要求文件内容读取权限。
func statPath(path string) (os.FileInfo, error) {
	initial, err := os.Lstat(path)
	if err != nil || (!initial.Mode().IsRegular() && !initial.IsDir()) {
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
