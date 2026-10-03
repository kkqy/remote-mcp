package config

import (
	"errors"
	"io"
	"os"
	"runtime"
	"strings"
)

// LoadToken 从权限受控文件或环境变量读取凭据，错误信息不包含凭据和文件内容。
func LoadToken(file string) (string, error) {
	token := os.Getenv("REMOTE_MCP_TOKEN")
	if file != "" {
		f, err := os.Open(file)
		if err != nil {
			return "", errors.New("无法打开 Token 文件")
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 8192 {
			return "", errors.New("Token 文件必须是小于 8 KiB 的普通文件")
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
			return "", errors.New("Token 文件仅允许运行账号访问，请设置权限 0600")
		}
		data, err := io.ReadAll(io.LimitReader(f, 8193))
		if err != nil || len(data) > 8192 {
			return "", errors.New("无法读取 Token 文件")
		}
		token = strings.TrimSpace(string(data))
	}
	if err := ValidateToken(token); err != nil {
		return "", err
	}
	return token, nil
}

func ValidateToken(token string) error {
	if token == "" || len(token) > 8192 {
		return errors.New("请通过 REMOTE_MCP_TOKEN 或 --token-file 提供非空 Token（最多 8 KiB）")
	}
	for _, b := range []byte(token) {
		if b <= 32 || b >= 127 {
			return errors.New("Token 必须由不含空白的可打印 ASCII 字符组成")
		}
	}
	return nil
}
