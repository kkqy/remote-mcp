package execution

import (
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
)

type running interface {
	PID() int
	Wait() (int, error)
	Kill() error
	Close() error
	Write([]byte) (int, error)
	Resize(int, int) error
}

func command(name string, args []string, dir string, env map[string]string) (*exec.Cmd, error) {
	if name == "" || strings.ContainsRune(name, 0) {
		return nil, failure("invalid_argument", "可执行文件不能为空或包含零字节")
	}
	for _, arg := range args {
		if strings.ContainsRune(arg, 0) {
			return nil, failure("invalid_argument", "参数包含零字节")
		}
	}
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	values := map[string]string{}
	for _, entry := range os.Environ() {
		if i := strings.Index(entry, "="); i > 0 {
			key := entry[:i]
			if runtime.GOOS == "windows" {
				key = strings.ToUpper(key)
			}
			values[key] = entry[i+1:]
		}
	}
	for key, value := range env {
		if key == "" || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, 0) {
			return nil, failure("invalid_argument", "环境变量格式无效")
		}
		if runtime.GOOS == "windows" {
			key = strings.ToUpper(key)
		}
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		cmd.Env = append(cmd.Env, k+"="+values[k])
	}
	return cmd, nil
}
func copyOutput(dst io.Writer, src io.Reader, done chan<- struct{}) {
	_, _ = io.Copy(dst, src)
	done <- struct{}{}
}
