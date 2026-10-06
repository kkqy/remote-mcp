package logstream

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenRejectsSameMetadataReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	original := []byte("same log content")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	before, err := statPath(path)
	if err != nil {
		t.Fatal(err)
	}
	f, _, err := openRegularWith(path, func(name string) (*os.File, error) {
		// 复制内容和 mtime，身份变化仍须被初始打开检查识别。
		if err := os.Rename(name, name+".original"); err != nil {
			return nil, err
		}
		if err := os.WriteFile(name, original, 0600); err != nil {
			return nil, err
		}
		if err := os.Chtimes(name, before.ModTime(), before.ModTime()); err != nil {
			return nil, err
		}
		return openFile(name)
	})
	if f != nil {
		f.Close()
	}
	requireCode(t, err, "not_regular")
	for _, target := range []string{path, path + ".original"} {
		data, readErr := os.ReadFile(target)
		if readErr != nil || string(data) != string(original) {
			t.Fatal("Identity validation damaged a log file", readErr)
		}
	}
}
