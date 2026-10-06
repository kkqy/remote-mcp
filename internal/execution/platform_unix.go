//go:build linux

package execution

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/creack/pty"
)

type unixProcess struct {
	mu          sync.Mutex
	cmd         *exec.Cmd
	terminal    *os.File
	readers     []*os.File
	copies      chan struct{}
	readerCount int
	closed      bool
	session     string
	killed      bool
}

func defaultShell() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	return "/bin/sh"
}
func startProcess(cmd *exec.Cmd, out, stderr io.Writer) (running, error) {
	p := &unixProcess{cmd: cmd, copies: make(chan struct{}, 2), readerCount: 2}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return nil, failure("io_error", "Failed to create the stdout pipe")
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		stdoutR.Close()
		stdoutW.Close()
		return nil, failure("io_error", "Failed to create the stderr pipe")
	}
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	err = cmd.Start()
	stdoutW.Close()
	stderrW.Close()
	if err != nil {
		stdoutR.Close()
		stderrR.Close()
		return nil, failure("start_failed", "Failed to start the process; check the program, working directory, and account permissions")
	}
	p.readers = []*os.File{stdoutR, stderrR}
	go copyOutput(out, stdoutR, p.copies)
	go copyOutput(stderr, stderrR, p.copies)
	return p, nil
}
func startTerminal(cmd *exec.Cmd, columns, rows int, out io.Writer) (running, error) {
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(columns), Rows: uint16(rows)})
	if err != nil {
		return nil, failure("start_failed", "Failed to start the terminal; check the PTY, program, and working directory")
	}
	// 非阻塞描述符由 Go 轮询器管理，关闭能够解除等待读写。
	if err = syscall.SetNonblock(int(f.Fd()), true); err != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		f.Close()
		cmd.Wait()
		return nil, failure("io_error", "Failed to enable nonblocking terminal I/O")
	}
	p := &unixProcess{cmd: cmd, terminal: f, readers: []*os.File{f}, copies: make(chan struct{}, 1), readerCount: 1}
	go copyOutput(out, f, p.copies)
	// Linux 的会话编号为组长 PID，用于回收已经脱离父子关系的终端作业。
	p.session = strconv.Itoa(cmd.Process.Pid)
	return p, nil
}
func (p *unixProcess) PID() int { return p.cmd.Process.Pid }
func (p *unixProcess) Wait() (int, error) {
	err := p.cmd.Wait()
	return p.cmd.ProcessState.ExitCode(), err
}
func (p *unixProcess) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.killed {
		return nil
	}
	pid := p.PID()
	// 交互 shell 会把作业放入独立进程组，需要连同后代组一起回收。
	groups := map[int]bool{pid: true}
	if p.terminal != nil {
		var foreground int32
		if err := terminalIoctl(p.terminal, syscall.TIOCGPGRP, unsafe.Pointer(&foreground)); err == nil && foreground > 0 {
			groups[int(foreground)] = true
		}
	}
	data, err := processTable("-axo", "pid=,ppid=,pgid=,sess=")
	if err == nil {
		type row struct{ pid, parent, group int }
		var rows []row
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) != 4 {
				continue
			}
			a, _ := strconv.Atoi(fields[0])
			b, _ := strconv.Atoi(fields[1])
			c, _ := strconv.Atoi(fields[2])
			rows = append(rows, row{a, b, c})
			if p.session != "" && fields[3] == p.session {
				groups[c] = true
			}
		}
		descendants := map[int]bool{pid: true}
		for changed := true; changed; {
			changed = false
			for _, r := range rows {
				if descendants[r.parent] && !descendants[r.pid] {
					descendants[r.pid] = true
					groups[r.group] = true
					changed = true
				}
			}
		}
	}
	var errs []error
	for group := range groups {
		if group <= 0 || group == syscall.Getpgrp() {
			continue
		}
		if err := syscall.Kill(-group, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		p.killed = true
	}
	return errors.Join(errs...)
}
func (p *unixProcess) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()
	// 先排空已产生输出，再强制释放仍被外部后代持有的描述符。
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	remaining := p.readerCount
	for remaining > 0 {
		select {
		case <-p.copies:
			remaining--
		case <-timer.C:
			for _, f := range p.readers {
				_ = f.Close()
			}
			for remaining > 0 {
				<-p.copies
				remaining--
			}
		}
	}
	for _, f := range p.readers {
		_ = f.Close()
	}
	return nil
}
func (p *unixProcess) Write(b []byte) (int, error) {
	if p.terminal == nil {
		return 0, os.ErrInvalid
	}
	return p.terminal.Write(b)
}
func (p *unixProcess) Resize(columns, rows int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.terminal == nil {
		return os.ErrClosed
	}
	size := pty.Winsize{Cols: uint16(columns), Rows: uint16(rows)}
	return terminalIoctl(p.terminal, syscall.TIOCSWINSZ, unsafe.Pointer(&size))
}

// 使用 RawConn 避免 Fd() 将终端切回阻塞模式。
func terminalIoctl(f *os.File, request uintptr, arg unsafe.Pointer) error {
	raw, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var syscallErr syscall.Errno
	err = raw.Control(func(fd uintptr) { _, _, syscallErr = syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(arg)) })
	if err != nil {
		return err
	}
	if syscallErr != 0 {
		return syscallErr
	}
	return nil
}

func processTable(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "/bin/ps", args...).Output()
}
