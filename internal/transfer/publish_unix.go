//go:build linux

package transfer

import "os"

// Publish 在同一目录发布临时文件；禁止覆盖通过原子硬链接保证提交时仍无冲突。
func Publish(temp, target string, overwrite bool) error {
	if overwrite {
		return os.Rename(temp, target)
	}
	if err := os.Link(temp, target); err != nil {
		return err
	}
	// 发布后由调用方统一清理临时名称。
	return nil
}
