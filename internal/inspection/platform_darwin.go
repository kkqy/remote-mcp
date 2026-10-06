package inspection

import (
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"os/exec"
)

func platformKernel() (string, error) { return unix.Sysctl("kern.osrelease") }
func platformProcesses(ctx context.Context, cfg Config, out *ProcessesResult) []Process {
	b, stderr, truncated, commandErr := runCommandStreams(ctx, cfg.MaxCommandBytes, "/bin/ps", "-axo", "pid=,ppid=,comm=")
	if commandErr != nil && !truncated {
		out.fail(fixedCommandError(ctx, commandErr, stderr))
	}
	items, partial, err := parsePS(ctx, b, cfg.MaxProcesses)
	out.Scanned = len(items)
	out.Truncated = truncated || partial
	if err != nil || len(stderr) > 0 {
		out.warn("io_error", "The process command output could not be completely parsed")
	}
	if out.Truncated {
		out.warn("resource_limit", "The process command output or scan limit was reached")
	}
	out.Visibility = "best_effort"
	return items
}
func platformListeners(ctx context.Context, cfg Config, out *ListenersResult) []Listener {
	b, stderr, truncated, commandErr := runCommandStreams(ctx, cfg.MaxCommandBytes, "/usr/sbin/lsof", "-nP", "-a", "-iTCP", "-sTCP:LISTEN", "-F0pctfn")
	var exit *exec.ExitError
	// 退出码 1 仅在两条流都为空时代表没有匹配，诊断输出不能当空结果。
	if commandErr != nil && !truncated && !(errors.As(commandErr, &exit) && exit.ExitCode() == 1 && len(b) == 0 && len(stderr) == 0) {
		out.fail(fixedCommandError(ctx, commandErr, stderr))
	}
	items, partial, parseErr := parseLsof(ctx, b, 4096)
	out.Truncated = truncated || partial
	out.PIDVisibility = "best_effort"
	out.warn("visibility_limited", "Listener visibility is limited to the current account and system permissions")
	if parseErr != nil || len(stderr) > 0 {
		out.warn("io_error", "The listener command output could not be completely parsed")
	}
	if out.Truncated {
		out.warn("resource_limit", "The listener command output or scan limit was reached")
	}
	return items
}
