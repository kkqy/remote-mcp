package fileops

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"
)

func (m *Manager) Search(ctx context.Context, in SearchInput) (out SearchResult, err error) {
	ctx, done, err := m.begin(ctx)
	if err != nil {
		return out, err
	}
	defer done()
	depth := m.cfg.MaxDepth
	if in.MaxDepth != nil {
		depth = *in.MaxDepth
	}
	if in.MaxEntries == 0 {
		in.MaxEntries = m.cfg.MaxScanEntries
	}
	if in.MaxScanBytes == 0 {
		in.MaxScanBytes = m.cfg.MaxScanBytes
	}
	if in.MaxFileBytes == 0 {
		in.MaxFileBytes = m.cfg.MaxFileBytes
	}
	if in.MaxMatches == 0 {
		in.MaxMatches = m.cfg.MaxMatches
	}
	if in.Query == "" || !utf8.ValidString(in.Query) || len(in.Query) > m.cfg.MaxResultBytes || depth < 0 || depth > m.cfg.MaxDepth || in.MaxEntries < 1 || in.MaxEntries > m.cfg.MaxScanEntries || in.MaxScanBytes < 1 || in.MaxScanBytes > m.cfg.MaxScanBytes || in.MaxFileBytes < 1 || in.MaxFileBytes > m.cfg.MaxFileBytes || in.MaxMatches < 1 || in.MaxMatches > m.cfg.MaxMatches {
		return out, failure("invalid_argument", "Invalid search query or scan limits")
	}
	if err = contextError(ctx); err != nil {
		return out, err
	}
	root, _, err := openPath(in.Path, true)
	if err != nil {
		return out, err
	}
	root.Close()
	out.Matches = []Match{}
	out.Issues = []Issue{}
	resultBytes := 512
	stop := false
	truncate := func(reason string) {
		out.Truncated = true
		if out.Reason == "" {
			out.Reason = reason
		}
	}
	issue := func(path string, err error) {
		var typed *Error
		if !errors.As(err, &typed) {
			typed = &Error{"io_error", "File system operation failed"}
		}
		value := Issue{path, typed.Code, typed.Message}
		encoded, _ := json.Marshal(value)
		cost := len(encoded) + 1
		truncate("partial_scan")
		if resultBytes+cost > m.cfg.MaxResultBytes {
			truncate("byte_limit")
			stop = true
			return
		}
		out.Issues = append(out.Issues, value)
		resultBytes += cost
	}
	var walk func(string, int) error
	walk = func(dir string, level int) error {
		f, _, openErr := openPath(dir, true)
		if openErr != nil {
			if level == 0 {
				return openErr
			}
			issue(dir, openErr)
			return nil
		}
		defer f.Close()
		for !stop {
			if err := contextError(ctx); err != nil {
				return err
			}
			if out.ScannedEntries >= in.MaxEntries {
				truncate("entry_limit")
				stop = true
				return nil
			}
			entries, readErr := f.ReadDir(1)
			if readErr != nil && readErr != io.EOF {
				issue(dir, ioError(readErr))
				return nil
			}
			if len(entries) == 0 {
				return nil
			}
			out.ScannedEntries++
			path := filepath.Join(dir, entries[0].Name())
			info, statErr := entries[0].Info()
			if statErr != nil {
				issue(path, ioError(statErr))
				continue
			}
			if info.Mode()&os.ModeSymlink != 0 {
				issue(path, failure("symlink", "Symbolic link targets are not supported"))
				continue
			}
			if info.IsDir() {
				if level >= depth {
					truncate("depth_limit")
					continue
				}
				if err := walk(path, level+1); err != nil {
					return err
				}
				continue
			}
			if !info.Mode().IsRegular() {
				issue(path, failure("not_regular", "Target is not a regular file"))
				continue
			}
			if info.Size() > in.MaxFileBytes {
				issue(path, failure("file_too_large", "Text exceeds the file byte limit"))
				continue
			}
			remaining := in.MaxScanBytes - out.ScannedBytes
			if remaining <= 0 || info.Size() > remaining {
				truncate("scan_byte_limit")
				stop = true
				return nil
			}
			file, before, openErr := openPath(path, false)
			if openErr != nil {
				issue(path, openErr)
				continue
			}
			out.ScannedFiles++
			pending := []Match{}
			pendingBytes := 0
			stats, scanErr := scanTextMatch(ctx, file, min(in.MaxFileBytes, remaining), m.cfg.MaxResultBytes, []byte(in.Query), func(line int, text []byte, overflow, matched bool) error {
				if stop || !matched {
					return nil
				}
				if len(out.Matches)+len(pending) >= in.MaxMatches {
					truncate("match_limit")
					stop = true
					return nil
				}
				value := Match{path, line, string(text), overflow}
				encoded, _ := json.Marshal(value)
				available := m.cfg.MaxResultBytes - resultBytes - pendingBytes
				for len(encoded)+1 > available && len(value.Text) > 0 {
					// JSON 转义及路径也消耗预算，缩短预览时保持 UTF-8 边界。
					value.Text = string(utf8Prefix([]byte(value.Text), len(value.Text)/2))
					value.Truncated = true
					encoded, _ = json.Marshal(value)
				}
				if len(encoded)+1 > available {
					truncate("byte_limit")
					stop = true
					return nil
				}
				pending = append(pending, value)
				pendingBytes += len(encoded) + 1
				if value.Truncated {
					truncate("preview_limit")
				}
				return nil
			})
			out.ScannedBytes += min(stats.scanned, remaining)
			if stats.scanned > remaining {
				truncate("scan_byte_limit")
				stop = true
			}
			after, statErr := file.Stat()
			file.Close()
			if scanErr != nil {
				var typed *Error
				if errors.As(scanErr, &typed) && (typed.Code == "cancelled" || typed.Code == "timeout") {
					return scanErr
				}
				issue(path, scanErr)
				continue
			}
			if statErr != nil {
				issue(path, ioError(statErr))
				continue
			}
			if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
				issue(path, failure("conflict", "File changed while it was being read"))
				continue
			}
			out.Matches = append(out.Matches, pending...)
			resultBytes += pendingBytes
		}
		return nil
	}
	if err = walk(in.Path, 0); err != nil {
		return SearchResult{}, err
	}
	return out, nil
}
