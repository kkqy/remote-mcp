package transfer

import "golang.org/x/sys/windows"

// Publish 不先删除目标；同卷 MoveFileEx 在冲突或共享锁失败时保留旧文件。
func Publish(temp, target string, overwrite bool) error {
	from, err := windows.UTF16PtrFromString(temp)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	flags := uint32(windows.MOVEFILE_WRITE_THROUGH)
	if overwrite {
		flags |= windows.MOVEFILE_REPLACE_EXISTING
	}
	return windows.MoveFileEx(from, to, flags)
}
