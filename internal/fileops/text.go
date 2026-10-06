package fileops

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"unicode/utf8"
)

type textStats struct {
	size    int64
	scanned int64
	lines   int
	digest  string
}

type countedReader struct {
	reader io.Reader
	n      int64
}

func (r *countedReader) Read(buffer []byte) (int, error) {
	n, err := r.reader.Read(buffer)
	r.n += int64(n)
	return n, err
}

// scanText 逐块校验完整文本并计算哈希；单行只保留有限预览，超长部分继续扫描。
func scanText(ctx context.Context, f *os.File, fileLimit int64, lineLimit int, visit func(int, []byte, bool) error) (stats textStats, err error) {
	return scanTextMatch(ctx, f, fileLimit, lineLimit, nil, func(line int, text []byte, overflow, matched bool) error {
		if visit == nil {
			return nil
		}
		return visit(line, text, overflow)
	})
}

// 字面匹配保留 query 长度减一的跨块尾部；整行匹配无需保存无限长行。
func scanTextMatch(ctx context.Context, f *os.File, fileLimit int64, lineLimit int, query []byte, visit func(int, []byte, bool, bool) error) (stats textStats, err error) {
	counted := &countedReader{reader: io.LimitReader(f, fileLimit+1)}
	defer func() { stats.scanned = counted.n }()
	r := bufio.NewReaderSize(counted, 32<<10)
	h := sha256.New()
	line := make([]byte, 0, min(lineLimit, 32<<10))
	var carry []byte
	var lineBytes int64
	var overlap []byte
	matched := false
	for {
		if err = contextError(ctx); err != nil {
			return stats, err
		}
		fragment, readErr := r.ReadSlice('\n')
		stats.size += int64(len(fragment))
		if stats.size > fileLimit {
			return stats, failure("file_too_large", "Text exceeds the file byte limit")
		}
		h.Write(fragment)
		check := fragment
		if len(carry) != 0 {
			check = append(carry, fragment...)
			carry = nil
		}
		for len(check) != 0 {
			if !utf8.FullRune(check) {
				carry = append([]byte(nil), check...)
				break
			}
			runeValue, n := utf8.DecodeRune(check)
			if runeValue == utf8.RuneError && n == 1 {
				return stats, failure("invalid_utf8", "File content is not valid UTF-8")
			}
			check = check[n:]
		}
		lineBytes += int64(len(fragment))
		if len(query) > 0 && !matched {
			window := fragment
			if len(overlap) > 0 {
				window = append(overlap, fragment...)
			}
			matched = bytes.Contains(window, query)
			keep := min(len(window), len(query)-1)
			overlap = append([]byte(nil), window[len(window)-keep:]...)
		}
		if remaining := lineLimit - len(line); remaining > 0 {
			line = append(line, fragment[:min(remaining, len(fragment))]...)
		}
		if readErr == bufio.ErrBufferFull {
			continue
		}
		if readErr != nil && readErr != io.EOF {
			return stats, ioError(readErr)
		}
		if lineBytes != 0 {
			stats.lines++
			preview := utf8Prefix(line, len(line))
			if visit != nil {
				if err = visit(stats.lines, preview, lineBytes > int64(len(preview)), matched); err != nil {
					return stats, err
				}
			}
			line = line[:0]
			lineBytes = 0
			overlap = nil
			matched = false
		}
		if readErr == io.EOF {
			break
		}
	}
	if len(carry) != 0 {
		return stats, failure("invalid_utf8", "File content is not valid UTF-8")
	}
	stats.digest = hex.EncodeToString(h.Sum(nil))
	return stats, nil
}

func utf8Prefix(data []byte, limit int) []byte {
	n := min(len(data), limit)
	for n > 0 && !utf8.Valid(data[:n]) {
		n--
	}
	return data[:n]
}

func (m *Manager) Read(ctx context.Context, in ReadInput) (out ReadResult, err error) {
	ctx, done, err := m.begin(ctx)
	if err != nil {
		return out, err
	}
	defer done()
	if in.StartLine == 0 {
		in.StartLine = 1
	}
	if in.LineCount == 0 {
		in.LineCount = min(1000, m.cfg.MaxScanEntries)
	}
	if in.MaxBytes == 0 {
		in.MaxBytes = m.cfg.MaxResultBytes
	}
	if in.StartLine < 1 || in.LineCount < 1 || in.LineCount > m.cfg.MaxScanEntries || in.MaxBytes < 1 || in.MaxBytes > m.cfg.MaxResultBytes {
		return out, failure("invalid_argument", "Invalid line range or returned byte limit")
	}
	if err = contextError(ctx); err != nil {
		return out, err
	}
	f, before, err := openPath(in.Path, false)
	if err != nil {
		return out, err
	}
	defer f.Close()
	if before.Size() > m.cfg.MaxFileBytes {
		return out, failure("file_too_large", "Text exceeds the file byte limit")
	}
	var data bytes.Buffer
	out.StartLine = in.StartLine
	out.NextLine = in.StartLine
	stats, err := scanText(ctx, f, m.cfg.MaxFileBytes, in.MaxBytes, func(line int, text []byte, overflow bool) error {
		if line < in.StartLine {
			return nil
		}
		if line-in.StartLine >= in.LineCount {
			out.Truncated = true
			if out.Reason == "" {
				out.Reason = "line_limit"
			}
			return nil
		}
		if out.Reason == "byte_limit" || out.Reason == "line_too_long" {
			return nil
		}
		part := utf8Prefix(text, in.MaxBytes-data.Len())
		data.Write(part)
		if len(part) != len(text) || overflow {
			out.Truncated = true
			out.Reason = "byte_limit"
			if overflow {
				out.Reason = "line_too_long"
			}
			out.EndLine = line
			out.NextLine = line
			return nil
		}
		out.EndLine = line
		out.NextLine = line + 1
		return nil
	})
	if err != nil {
		return ReadResult{}, err
	}
	after, err := f.Stat()
	if err != nil {
		return ReadResult{}, ioError(err)
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return ReadResult{}, failure("conflict", "File changed while it was being read")
	}
	out.Text = data.String()
	out.ReturnedBytes = data.Len()
	out.SHA256 = stats.digest
	out.Size = stats.size
	out.TotalLines = stats.lines
	return out, nil
}
