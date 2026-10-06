package config

import (
	"errors"
	"io"
	"os"
	"runtime"
	"strings"
)

// LoadToken 从权限受控文件或环境变量读取可选凭据；空环境变量表示匿名访问。
// 显式凭据文件必须有效且非空，错误信息不包含凭据和文件内容。
func LoadToken(file string) (string, error) {
	token := os.Getenv("REMOTE_MCP_TOKEN")
	if file != "" {
		f, err := os.Open(file)
		if err != nil {
			return "", errors.New("Unable to open token file")
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 8192 {
			return "", errors.New("Token file must be a regular file no larger than 8 KiB")
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
			return "", errors.New("Token file must be accessible only to the service account; set permissions to 0600")
		}
		data, err := io.ReadAll(io.LimitReader(f, 8193))
		if err != nil || len(data) > 8192 {
			return "", errors.New("Unable to read token file")
		}
		token = strings.TrimSpace(string(data))
		if token == "" {
			return "", errors.New("Token file must not be empty")
		}
	}
	if err := ValidateToken(token); err != nil {
		return "", err
	}
	return token, nil
}

func ValidateToken(token string) error {
	if len(token) > 8192 {
		return errors.New("Token must not exceed 8 KiB")
	}
	for _, b := range []byte(token) {
		if b <= 32 || b >= 127 {
			return errors.New("Token must contain printable ASCII characters without whitespace")
		}
	}
	return nil
}
