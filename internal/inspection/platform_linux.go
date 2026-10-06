package inspection

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func platformKernel() (string, error) {
	var u unix.Utsname
	if err := unix.Uname(&u); err != nil {
		return "", err
	}
	b := make([]byte, 0, len(u.Release))
	for _, c := range u.Release {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	return string(b), nil
}
func procPIDs(ctx context.Context, cfg Config, r *Result, root string) ([]int, error) {
	f, err := os.Open(root)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	pids := []int{}
	scanned := 0
	for {
		if ctx.Err() != nil {
			return pids, ctx.Err()
		}
		names, e := f.Readdirnames(128)
		for _, name := range names {
			scanned++
			if scanned > cfg.MaxProcesses*4 {
				r.warn("resource_limit", "The process directory scan limit was reached")
				r.Truncated = true
				return pids, nil
			}
			pid, e := strconv.Atoi(name)
			if e != nil || pid <= 0 {
				continue
			}
			if len(pids) >= cfg.MaxProcesses {
				r.warn("resource_limit", "The process scan limit was reached")
				r.Truncated = true
				return pids, nil
			}
			pids = append(pids, pid)
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return pids, e
		}
	}
	return pids, nil
}
func parseProcStat(b []byte) (Process, error) {
	s := strings.TrimSpace(string(b))
	left := strings.IndexByte(s, '(')
	right := strings.LastIndexByte(s, ')')
	if left < 1 || right <= left {
		return Process{}, failure("io_error", "A process status record is malformed")
	}
	pid, e := strconv.Atoi(strings.TrimSpace(s[:left]))
	fields := strings.Fields(s[right+1:])
	if e != nil || pid <= 0 || len(fields) < 2 {
		return Process{}, failure("io_error", "A process status record is malformed")
	}
	ppid, e := strconv.Atoi(fields[1])
	if e != nil || ppid < 0 {
		return Process{}, failure("io_error", "A process status record is malformed")
	}
	return Process{pid, ppid, s[left+1 : right]}, nil
}
func platformProcesses(ctx context.Context, cfg Config, out *ProcessesResult) []Process {
	return processesAt(ctx, cfg, out, "/proc")
}
func processesAt(ctx context.Context, cfg Config, out *ProcessesResult, root string) []Process {
	pids, err := procPIDs(ctx, cfg, &out.Result, root)
	if err != nil {
		if ctx.Err() != nil {
			out.fail(contextError(ctx.Err()))
		} else {
			out.fail(systemError(err))
		}
		return nil
	}
	items := []Process{}
	for _, pid := range pids {
		if ctx.Err() != nil {
			break
		}
		out.Scanned++
		b, err := readBounded(filepath.Join(root, strconv.Itoa(pid), "stat"), 16<<10)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			e := systemError(err)
			out.warn(e.Code, "Some process status records are not readable; the process snapshot is incomplete")
			continue
		}
		p, err := parseProcStat(b)
		if err != nil {
			out.warn("io_error", "Some process status records are malformed; the process snapshot is incomplete")
			continue
		}
		var truncated bool
		p.Name, truncated = boundedString(p.Name, cfg.MaxNameBytes)
		out.Truncated = out.Truncated || truncated
		items = append(items, p)
	}
	return items
}

type inodeListener struct {
	Listener
	inode string
}

func parseProcTCP(line string, ipv6 bool) (inodeListener, bool, error) {
	fields := strings.Fields(line)
	if len(fields) < 10 {
		return inodeListener{}, false, failure("io_error", "A TCP table record is malformed")
	}
	if fields[3] != "0A" {
		return inodeListener{}, false, nil
	}
	endpoint := strings.Split(fields[1], ":")
	if len(endpoint) != 2 {
		return inodeListener{}, false, failure("io_error", "A TCP endpoint record is malformed")
	}
	b, e := hex.DecodeString(endpoint[0])
	n := 4
	if ipv6 {
		n = 16
	}
	if e != nil || len(b) != n {
		return inodeListener{}, false, failure("io_error", "A TCP address record is malformed")
	}
	for i := 0; i < n; i += 4 {
		b[i], b[i+3] = b[i+3], b[i]
		b[i+1], b[i+2] = b[i+2], b[i+1]
	}
	p, e := strconv.ParseUint(endpoint[1], 16, 16)
	if e != nil {
		return inodeListener{}, false, failure("io_error", "A TCP port record is malformed")
	}
	if _, e = strconv.ParseUint(fields[9], 10, 64); e != nil {
		return inodeListener{}, false, failure("io_error", "A TCP inode record is malformed")
	}
	return inodeListener{Listener{Protocol: "tcp", Address: net.IP(b).String(), Port: int(p), PIDs: []int{}, PIDVisibility: "unknown"}, fields[9]}, true, nil
}
func platformListeners(ctx context.Context, cfg Config, out *ListenersResult) []Listener {
	return listenersAt(ctx, cfg, out, "/proc")
}
func listenersAt(ctx context.Context, cfg Config, out *ListenersResult, root string) []Listener {
	records := []inodeListener{}
	rows := 0
	for _, name := range []string{"tcp", "tcp6"} {
		f, err := os.Open(filepath.Join(root, "net", name))
		if err != nil {
			if name == "tcp" {
				out.fail(systemError(err))
				return nil
			}
			out.warn(systemError(err).Code, "The IPv6 TCP system table is not readable")
			continue
		}
		counter := &countReader{r: io.LimitReader(f, int64(cfg.MaxCommandBytes+1))}
		scanner := bufio.NewScanner(counter)
		scanner.Buffer(make([]byte, 4096), 16<<10)
		scanner.Scan()
		for scanner.Scan() {
			if ctx.Err() != nil {
				break
			}
			rows++
			if rows > 65536 {
				out.warn("resource_limit", "The TCP table scan limit was reached")
				out.Truncated = true
				break
			}
			item, ok, err := parseProcTCP(scanner.Text(), name == "tcp6")
			if err != nil {
				out.warn("io_error", "Some TCP table records are malformed")
				continue
			}
			if ok {
				if len(records) >= 4096 {
					out.warn("resource_limit", "The listener scan limit was reached")
					out.Truncated = true
					break
				}
				records = append(records, item)
			}
		}
		if counter.n > cfg.MaxCommandBytes {
			out.Truncated = true
			out.warn("resource_limit", "The TCP table byte limit was reached")
		}
		if scanner.Err() != nil {
			out.warn("io_error", "The TCP table could not be completely parsed")
		}
		f.Close()
		if rows > 65536 {
			break
		}
	}
	byInode := map[string][]int{}
	for i, r := range records {
		byInode[r.inode] = append(byInode[r.inode], i)
	}
	pids, err := procPIDs(ctx, cfg, &out.Result, root)
	if err != nil {
		out.warn(systemError(err).Code, "The process directory is not readable; listener ownership is incomplete")
	}
	ownershipPartial := out.Partial
outer:
	for _, pid := range pids {
		if ctx.Err() != nil {
			ownershipPartial = true
			break
		}
		out.ScannedProcesses++
		f, err := os.Open(filepath.Join(root, strconv.Itoa(pid), "fd"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			ownershipPartial = true
			out.warn(systemError(err).Code, "Some process file descriptors are not readable; listener PID ownership is incomplete")
			continue
		}
		for {
			if ctx.Err() != nil {
				f.Close()
				ownershipPartial = true
				break outer
			}
			names, err := f.Readdirnames(128)
			for _, name := range names {
				if ctx.Err() != nil {
					f.Close()
					ownershipPartial = true
					break outer
				}
				if out.ScannedFDs >= cfg.MaxFDs {
					f.Close()
					ownershipPartial = true
					out.warn("resource_limit", "The file descriptor scan limit was reached; listener PID ownership is incomplete")
					out.Truncated = true
					break outer
				}
				out.ScannedFDs++
				target, e := os.Readlink(filepath.Join(root, strconv.Itoa(pid), "fd", name))
				if e != nil {
					if errors.Is(e, os.ErrNotExist) {
						continue
					}
					ownershipPartial = true
					out.warn(systemError(e).Code, "Some process file descriptors are not readable; listener PID ownership is incomplete")
					continue
				}
				if strings.HasPrefix(target, "socket:[") && strings.HasSuffix(target, "]") {
					inode := target[8 : len(target)-1]
					for _, i := range byInode[inode] {
						owners := records[i].PIDs
						found := false
						for _, owner := range owners {
							if owner == pid {
								found = true
								break
							}
						}
						if !found {
							if len(owners) < 256 {
								records[i].PIDs = append(owners, pid)
							} else {
								ownershipPartial = true
								out.Truncated = true
								out.warn("resource_limit", "The listener owner PID limit was reached")
							}
						}
					}
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				ownershipPartial = true
				out.warn("io_error", "A file descriptor directory could not be completely read")
				break
			}
		}
		f.Close()
	}
	if ownershipPartial {
		out.PIDVisibility = "partial"
	} else {
		out.PIDVisibility = "best_effort"
	}
	items := make([]Listener, 0, len(records))
	for _, r := range records {
		r.PIDVisibility = out.PIDVisibility
		if len(r.PIDs) == 0 {
			r.PIDVisibility = "unknown"
		}
		items = append(items, r.Listener)
	}
	return items
}

type countReader struct {
	r io.Reader
	n int
}

func (c *countReader) Read(p []byte) (int, error) { n, err := c.r.Read(p); c.n += n; return n, err }
