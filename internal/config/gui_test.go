package config

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"remote-mcp/internal/gui"
	"strings"
	"testing"
	"time"
)

func TestGUIConfigFlags(t *testing.T) {
	if !gui.Supported {
		t.Skip("GUI 参数只适用于 Windows")
	}
	t.Setenv("REMOTE_MCP_TOKEN", "")
	c, err := Parse([]string{"--gui-idle", "1m", "--gui-authorize-timeout", "45s", "--gui-operation-timeout", "5s", "--gui-max-pixels", "123456", "--gui-max-png-bytes", "65536"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.GUI.IdleTimeout != time.Minute || c.GUI.AuthorizationTimeout != 45*time.Second || c.GUI.OperationTimeout != 5*time.Second || c.GUI.MaxPixels != 123456 || c.GUI.MaxPNGBytes != 65536 {
		t.Fatal("GUI 参数未传递到管理器配置")
	}
	for _, name := range []string{"gui-idle", "gui-authorize-timeout", "gui-operation-timeout", "gui-max-pixels", "gui-max-png-bytes"} {
		for _, value := range []string{"0", "-1"} {
			if _, err := Parse([]string{"--" + name, value}, io.Discard); err == nil {
				t.Fatalf("%s 未拒绝非正值", name)
			}
		}
	}
}

func TestGUIConfigProgrammaticValidation(t *testing.T) {
	c := Default()
	c.GUI.MaxPixels = 0
	if err := c.Validate(); (err != nil) != gui.Supported {
		t.Fatalf("GUI 配置校验必须遵循平台支持条件：supported=%v err=%v", gui.Supported, err)
	}
}

func TestGUIHelpAndUnsupportedFlags(t *testing.T) {
	t.Setenv("REMOTE_MCP_TOKEN", "")
	var help bytes.Buffer
	if _, err := Parse([]string{"--help"}, &help); !errors.Is(err, flag.ErrHelp) {
		t.Fatal(err)
	}
	for _, name := range []string{"gui-idle", "gui-authorize-timeout", "gui-operation-timeout", "gui-max-pixels", "gui-max-png-bytes"} {
		if strings.Contains(help.String(), "-"+name) != gui.Supported {
			t.Fatalf("帮助中的 GUI 参数与平台不符：%s", name)
		}
		if !gui.Supported {
			for _, value := range []string{"1", "fictional-secret-value"} {
				var diagnostics bytes.Buffer
				_, err := Parse([]string{"--" + name, value}, &diagnostics)
				if err == nil || err.Error() != "Invalid command-line arguments; use --help for usage" || value == "fictional-secret-value" && strings.Contains(diagnostics.String(), value) {
					t.Fatalf("不支持的 GUI 参数必须安全拒绝：%s err=%v", name, err)
				}
			}
		}
	}
}
