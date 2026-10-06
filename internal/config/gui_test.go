package config

import (
	"io"
	"testing"
	"time"
)

func TestGUIConfigFlags(t *testing.T) {
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
	if err := c.Validate(); err == nil {
		t.Fatal("直接构造配置也必须校验 GUI 限额")
	}
}
