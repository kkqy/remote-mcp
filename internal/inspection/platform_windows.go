package inspection

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"unsafe"
)

func platformKernel() (string, error) {
	v := windows.RtlGetVersion()
	return fmt.Sprintf("%d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber), nil
}
func platformProcesses(ctx context.Context, cfg Config, out *ProcessesResult) []Process {
	handle, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		out.fail(systemError(err))
		return nil
	}
	defer windows.CloseHandle(handle)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	err = windows.Process32First(handle, &entry)
	items := []Process{}
	for err == nil {
		if ctx.Err() != nil {
			break
		}
		out.Scanned++
		if out.Scanned > cfg.MaxProcesses {
			out.Truncated = true
			out.warn("resource_limit", "The process scan limit was reached")
			break
		}
		if entry.ProcessID != 0 {
			items = append(items, Process{int(entry.ProcessID), int(entry.ParentProcessID), windows.UTF16ToString(entry.ExeFile[:])})
		}
		err = windows.Process32Next(handle, &entry)
	}
	if err != nil && !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		e := systemError(err)
		out.warn(e.Code, "The process snapshot could not be completely read")
	}
	out.Visibility = "system_snapshot"
	return items
}

var tcpTableProc = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

func platformListeners(ctx context.Context, cfg Config, out *ListenersResult) []Listener {
	if err := tcpTableProc.Find(); err != nil {
		out.fail(failure("dependency_missing", "The TCP owner table system API was not found"))
		return nil
	}
	items := []Listener{}
	for _, family := range []uint32{windows.AF_INET, windows.AF_INET6} {
		b, err := queryTCPTable(ctx, cfg.MaxCommandBytes, func(buffer []byte, size *uint32) uint32 {
			var pointer uintptr
			if len(buffer) != 0 {
				pointer = uintptr(unsafe.Pointer(&buffer[0]))
			}
			status, _, _ := tcpTableProc.Call(pointer, uintptr(unsafe.Pointer(size)), 0, uintptr(family), 3, 0)
			return uint32(status)
		})
		if err != nil {
			if len(items) == 0 {
				out.fail(err)
			} else {
				e := err.(*Error)
				out.warn(e.Code, e.Message)
			}
			continue
		}
		parsed, truncated, err := parseWindowsTCP(ctx, b, family == windows.AF_INET6, 4096-len(items))
		if err != nil {
			out.fail(err)
			continue
		}
		items = append(items, parsed...)
		if truncated {
			out.Truncated = true
			out.warn("resource_limit", "The listener scan limit was reached")
		}
	}
	return items
}
