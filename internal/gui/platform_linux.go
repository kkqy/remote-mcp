package gui

import (
	"context"
	"os"
)

type linuxBackend struct{ cfg Config }

func newPlatformBackend(cfg Config) Backend { return &linuxBackend{cfg} }
func (b *linuxBackend) selected() Backend {
	// Wayland 桌面绝不退到 XWayland 伪报原生完整控制。
	if os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("XDG_SESSION_TYPE") == "wayland" {
		return &waylandBackend{cfg: b.cfg}
	}
	return &x11Backend{cfg: b.cfg}
}
func (b *linuxBackend) Probe(ctx context.Context) (Status, error) { return b.selected().Probe(ctx) }
func (b *linuxBackend) Open(ctx context.Context) (Desktop, error) { return b.selected().Open(ctx) }
