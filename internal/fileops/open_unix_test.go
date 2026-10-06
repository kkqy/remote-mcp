//go:build linux

package fileops

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestOpenRejectsReplacementBeforeHandleValidation(t *testing.T) {
	for _, kind := range []string{"fifo", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "file")
			original := path + ".original"
			writeText(t, path, "中文原目标\r\n")
			done := make(chan error, 1)
			go func() {
				// 此处已完成普通文件 Lstat，精确注入打开前的目标替换。
				f, _, err := openPathWith(path, false, func(name string) (*os.File, error) {
					if err := os.Rename(name, original); err != nil {
						return nil, err
					}
					if kind == "fifo" {
						if err := unix.Mkfifo(name, 0600); err != nil {
							return nil, err
						}
					} else if err := os.Symlink(original, name); err != nil {
						return nil, err
					}
					return openFile(name)
				})
				if f != nil {
					f.Close()
				}
				done <- err
			}()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("Replacement target was followed or accepted")
				}
			case <-time.After(time.Second):
				// 失败时提供写端解除旧式阻塞打开，避免回归测试遗留 goroutine。
				if kind == "fifo" {
					fd, err := unix.Open(path, unix.O_RDWR|unix.O_NONBLOCK, 0)
					if err == nil {
						unix.Close(fd)
					}
				}
				t.Fatal("Opening the replacement target blocked")
			}
			actual, err := os.ReadFile(original)
			if err != nil || string(actual) != "中文原目标\r\n" {
				t.Fatal("The original file was damaged", err)
			}
		})
	}
}
