package inspection

import (
	"context"
	"net"
	"os"
	"testing"
)

func TestWrappedTCPPlatformErrnoClassification(t *testing.T) {
	for _, test := range []struct {
		err    error
		code   string
		reason string
	}{
		{connectionRefusedErrno, "tcp_failed", "refused"},
		{networkUnreachableErrno, "tcp_failed", "unreachable"},
		{hostUnreachableErrno, "tcp_failed", "unreachable"},
		{connectionTimedOutErrno, "timeout", "timeout"},
		{os.ErrInvalid, "tcp_failed", "connection_error"},
	} {
		err := &net.OpError{Op: "dial", Net: "tcp", Err: &os.SyscallError{Syscall: "connect", Err: test.err}}
		out, reason := probeError(context.Background(), "tcp", err)
		if out.Code != test.code || reason != test.reason {
			t.Fatalf("Wrapped TCP error classification: %+v %s", out, reason)
		}
	}
}
