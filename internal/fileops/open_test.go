package fileops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenRejectsSameMetadataReplacement(t *testing.T) {
	for _, directory := range []bool{false, true} {
		name := "file"
		if directory {
			name = "directory"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			create := func() {
				t.Helper()
				if directory {
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
				} else {
					writeText(t, path, "same content")
				}
			}
			create()
			before, err := statPath(path)
			if err != nil {
				t.Fatal(err)
			}
			// 在身份快照与真实打开之间替换目标，内容和 mtime 不足以识别变化。
			f, _, err := openPathWith(path, directory, func(name string) (*os.File, error) {
				if err := os.Rename(name, name+".original"); err != nil {
					return nil, err
				}
				create()
				if err := os.Chtimes(name, before.ModTime(), before.ModTime()); err != nil {
					return nil, err
				}
				return openFile(name)
			})
			if f != nil {
				f.Close()
			}
			requireCode(t, err, "conflict")
			if !directory {
				for _, target := range []string{path, path + ".original"} {
					data, readErr := os.ReadFile(target)
					if readErr != nil || string(data) != "same content" {
						t.Fatal("Identity validation damaged a file", readErr)
					}
				}
			}
		})
	}
}
