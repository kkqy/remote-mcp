//go:build windows

package execution

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf16"
	"unsafe"

	"github.com/charmbracelet/x/conpty"
	"golang.org/x/sys/windows"
)

type windowsProcess struct {
	mu          sync.Mutex
	handle, job windows.Handle
	pid         int
	terminal    *conpty.ConPty
	readers     []*os.File
	copies      chan struct{}
	readerCount int
	closed      bool
}

func defaultShell() string {
	if shell := os.Getenv("COMSPEC"); shell != "" {
		return shell
	}
	return "cmd.exe"
}
func newJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}
func newConsole(columns, rows int) (*conpty.ConPty, error) {
	var ir, iw, or, ow windows.Handle
	if err := windows.CreatePipe(&ir, &iw, nil, 0); err != nil {
		return nil, err
	}
	if err := windows.CreatePipe(&or, &ow, nil, 0); err != nil {
		windows.CloseHandle(ir)
		windows.CloseHandle(iw)
		return nil, err
	}
	console, err := conpty.NewWithPipes(uintptr(ir), uintptr(iw), uintptr(or), uintptr(ow), columns, rows, 0)
	if err != nil {
		for _, h := range []windows.Handle{ir, iw, or, ow} {
			windows.CloseHandle(h)
		}
	}
	return console, err
}
func startProcess(cmd *exec.Cmd, out, stderr io.Writer) (running, error) {
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return nil, failure("io_error", "创建输出管道失败")
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		stdoutR.Close()
		stdoutW.Close()
		return nil, failure("io_error", "创建错误管道失败")
	}
	stdin, err := os.Open(os.DevNull)
	if err != nil {
		stdoutR.Close()
		stdoutW.Close()
		stderrR.Close()
		stderrW.Close()
		return nil, failure("io_error", "打开空输入失败")
	}
	defer stdin.Close()
	defer stdoutW.Close()
	defer stderrW.Close()
	p := &windowsProcess{readers: []*os.File{stdoutR, stderrR}, copies: make(chan struct{}, 2), readerCount: 2}
	err = p.spawn(cmd, []windows.Handle{windows.Handle(stdin.Fd()), windows.Handle(stdoutW.Fd()), windows.Handle(stderrW.Fd())})
	if err != nil {
		stdoutR.Close()
		stderrR.Close()
		return nil, failure("start_failed", "进程或 Job Object 启动失败，请检查程序、目录和权限")
	}
	go copyOutput(out, stdoutR, p.copies)
	go copyOutput(stderr, stderrR, p.copies)
	return p, nil
}
func startTerminal(cmd *exec.Cmd, columns, rows int, out io.Writer) (running, error) {
	console, err := newConsole(columns, rows)
	if err != nil {
		return nil, failure("start_failed", "创建 ConPTY 失败，要求 Windows 10 1809 或更新系统")
	}
	p := &windowsProcess{terminal: console, copies: make(chan struct{}, 1), readerCount: 1}
	// ClosePseudoConsole 可能等待输出排空，读循环必须先于任何关闭路径启动。
	go copyOutput(out, console, p.copies)
	if err = p.spawn(cmd, nil); err != nil {
		console.Close()
		<-p.copies
		return nil, failure("start_failed", "终端进程或 Job Object 启动失败，请检查程序、目录和权限")
	}
	return p, nil
}
func (p *windowsProcess) spawn(cmd *exec.Cmd, handles []windows.Handle) error {
	if cmd.Err != nil {
		return cmd.Err
	}
	job, err := newJob()
	if err != nil {
		return err
	}
	success := false
	defer func() {
		if !success {
			windows.CloseHandle(job)
		}
	}()
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return err
	}
	defer attrs.Delete()
	si := windows.StartupInfoEx{}
	si.Cb = uint32(unsafe.Sizeof(si))
	si.ProcThreadAttributeList = attrs.List()
	if p.terminal != nil {
		if err = setConsoleAttribute(attrs, p.terminal.Fd()); err != nil {
			return err
		}
	} else {
		for _, h := range handles {
			if err = windows.SetHandleInformation(h, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
				return err
			}
		}
		if err = attrs.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&handles[0]), uintptr(len(handles))*unsafe.Sizeof(handles[0])); err != nil {
			return err
		}
		si.Flags = windows.STARTF_USESTDHANDLES
		si.StdInput = handles[0]
		si.StdOutput = handles[1]
		si.StdErr = handles[2]
	}
	path := cmd.Path
	if !filepath.IsAbs(path) {
		path, err = filepath.Abs(filepath.Join(cmd.Dir, path))
		if err != nil {
			return err
		}
	}
	application, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	line, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(cmd.Args))
	if err != nil {
		return err
	}
	var directory *uint16
	if cmd.Dir != "" {
		directory, err = windows.UTF16PtrFromString(cmd.Dir)
		if err != nil {
			return err
		}
	}
	env := utf16.Encode([]rune(strings.Join(cmd.Env, "\x00") + "\x00\x00"))
	pi := windows.ProcessInformation{}
	flags := uint32(windows.CREATE_SUSPENDED | windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT)
	if p.terminal == nil {
		flags |= windows.CREATE_NO_WINDOW
	}
	if err = windows.CreateProcess(application, line, nil, nil, p.terminal == nil, flags, &env[0], directory, &si.StartupInfo, &pi); err != nil {
		return err
	}
	defer windows.CloseHandle(pi.Thread)
	// 先暂停创建，再加入 Job，最后恢复；避免快速子进程逃过作业绑定。
	if err = windows.AssignProcessToJobObject(job, pi.Process); err == nil {
		_, err = windows.ResumeThread(pi.Thread)
	}
	if err != nil {
		windows.TerminateProcess(pi.Process, 1)
		windows.WaitForSingleObject(pi.Process, windows.INFINITE)
		windows.CloseHandle(pi.Process)
		return err
	}
	p.handle = pi.Process
	p.job = job
	p.pid = int(pi.ProcessId)
	success = true
	return nil
}
func (p *windowsProcess) PID() int { return p.pid }
func (p *windowsProcess) Wait() (int, error) {
	_, err := windows.WaitForSingleObject(p.handle, windows.INFINITE)
	if err != nil {
		return -1, err
	}
	var code uint32
	if err = windows.GetExitCodeProcess(p.handle, &code); err != nil {
		return -1, err
	}
	return int(code), nil
}
func (p *windowsProcess) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	return windows.TerminateJobObject(p.job, 1)
}
func (p *windowsProcess) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	job, handle := p.job, p.handle
	p.mu.Unlock()
	var errs []error
	errs = append(errs, windows.CloseHandle(job))
	if p.terminal != nil {
		errs = append(errs, p.terminal.Close())
	}
	for i := 0; i < p.readerCount; i++ {
		<-p.copies
	}
	for _, f := range p.readers {
		errs = append(errs, f.Close())
	}
	errs = append(errs, windows.CloseHandle(handle))
	return errors.Join(errs...)
}
func (p *windowsProcess) Write(b []byte) (int, error) {
	if p.terminal == nil {
		return 0, os.ErrInvalid
	}
	return p.terminal.Write(b)
}
func (p *windowsProcess) Resize(columns, rows int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.terminal == nil {
		return os.ErrClosed
	}
	return p.terminal.Resize(columns, rows)
}

// 此属性接收不透明 HPCON 值而非 Go 指针，直接调用避免把句柄加入 GC 指针列表。
var updateConsoleAttribute = windows.NewLazySystemDLL("kernel32.dll").NewProc("UpdateProcThreadAttribute")

func setConsoleAttribute(attrs *windows.ProcThreadAttributeListContainer, console uintptr) error {
	ok, _, err := updateConsoleAttribute.Call(uintptr(unsafe.Pointer(attrs.List())), 0, windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, console, unsafe.Sizeof(windows.Handle(0)), 0, 0)
	if ok == 0 {
		return err
	}
	return nil
}
