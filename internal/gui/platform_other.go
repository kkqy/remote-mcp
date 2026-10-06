//go:build !windows

package gui

import "context"

// Supported 仅表示产品是否在此编译目标启用 GUI，不探测当前桌面。
const Supported = false

type unavailableBackend struct{}

func newPlatformBackend(Config) Backend { return unavailableBackend{} }
func (unavailableBackend) Probe(context.Context) (Status, error) {
	return Status{State: "unavailable", Displays: []Display{}}, failure("unsupported", "GUI control is supported only on Windows")
}
func (unavailableBackend) Open(context.Context) (Desktop, error) {
	return nil, failure("unsupported", "GUI control is supported only on Windows")
}
