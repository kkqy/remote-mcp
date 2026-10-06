package fileops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func digest(data []byte) string { value := sha256.Sum256(data); return hex.EncodeToString(value[:]) }

// patchText 只处理有界文件，按原行坐标扫描，不为每行创建额外索引。
func patchText(data []byte, edits []Edit, maxBytes int64) ([]byte, error) {
	lineCount := bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		lineCount++
	}
	previousStart, previousEnd := 0, 0
	inserted := int64(0)
	for _, edit := range edits {
		if edit.StartLine < 1 || edit.StartLine > lineCount+1 || edit.DeleteCount < 0 || edit.DeleteCount > lineCount-edit.StartLine+1 || edit.StartLine <= previousStart || edit.StartLine < previousEnd || !utf8.ValidString(edit.Text) {
			return nil, failure("invalid_patch", "Edits must be valid UTF-8, ordered, non-overlapping, and within the original line range")
		}
		inserted += int64(len(edit.Text))
		if inserted > maxBytes {
			return nil, failure("file_too_large", "Patch text exceeds the file byte limit")
		}
		previousStart = edit.StartLine
		previousEnd = edit.StartLine + edit.DeleteCount
	}
	var out bytes.Buffer
	position, line, copied := 0, 1, 0
	advance := func(target int) int {
		for line < target {
			n := bytes.IndexByte(data[position:], '\n')
			if n < 0 {
				position = len(data)
			} else {
				position += n + 1
			}
			line++
		}
		return position
	}
	for _, edit := range edits {
		start := advance(edit.StartLine)
		if int64(out.Len())+int64(start-copied)+int64(len(edit.Text)) > maxBytes {
			return nil, failure("file_too_large", "Patched text exceeds the file byte limit")
		}
		out.Write(data[copied:start])
		out.WriteString(edit.Text)
		copied = advance(edit.StartLine + edit.DeleteCount)
	}
	if int64(out.Len())+int64(len(data)-copied) > maxBytes {
		return nil, failure("file_too_large", "Patched text exceeds the file byte limit")
	}
	out.Write(data[copied:])
	return out.Bytes(), nil
}

func loadPatchText(ctx context.Context, f *os.File, maxBytes int64) ([]byte, string, error) {
	var out bytes.Buffer
	h := sha256.New()
	buffer := make([]byte, 32<<10)
	r := io.LimitReader(f, maxBytes+1)
	for {
		if err := contextError(ctx); err != nil {
			return nil, "", err
		}
		n, err := r.Read(buffer)
		if int64(out.Len())+int64(n) > maxBytes {
			return nil, "", failure("file_too_large", "Text exceeds the file byte limit")
		}
		out.Write(buffer[:n])
		h.Write(buffer[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", ioError(err)
		}
	}
	if !utf8.Valid(out.Bytes()) {
		return nil, "", failure("invalid_utf8", "File content is not valid UTF-8")
	}
	return out.Bytes(), hex.EncodeToString(h.Sum(nil)), nil
}

func writeTemporary(ctx context.Context, temp *os.File, data []byte) error {
	for offset := 0; offset < len(data); {
		if err := contextError(ctx); err != nil {
			return err
		}
		n, err := temp.Write(data[offset:min(offset+(32<<10), len(data))])
		if err != nil || n == 0 {
			return failure("write_failed", "Failed to write the patch temporary file")
		}
		offset += n
	}
	if err := temp.Sync(); err != nil {
		return failure("write_failed", "Failed to synchronize the patch temporary file")
	}
	if err := temp.Close(); err != nil {
		return failure("write_failed", "Failed to close the patch temporary file")
	}
	return nil
}

func verifyTarget(ctx context.Context, path string, before os.FileInfo, originalHash string, cfg Config) error {
	current, named, err := openPath(path, false)
	if err != nil {
		return failure("conflict", "Target changed before patch publication")
	}
	stats, scanErr := scanText(ctx, current, cfg.MaxFileBytes, cfg.MaxResultBytes, nil)
	closeErr := current.Close()
	if scanErr != nil {
		if contextErr := contextError(ctx); contextErr != nil {
			return contextErr
		}
		return failure("conflict", "Target changed before patch publication")
	}
	if closeErr != nil {
		return ioError(closeErr)
	}
	latest, statErr := os.Lstat(path)
	if statErr != nil || !latest.Mode().IsRegular() || !os.SameFile(before, named) || !os.SameFile(before, latest) || stats.digest != originalHash || latest.Size() != before.Size() || !latest.ModTime().Equal(before.ModTime()) {
		return failure("conflict", "Target identity or content changed before patch publication")
	}
	return nil
}

func (m *Manager) Patch(ctx context.Context, in PatchInput) (out PatchResult, err error) {
	ctx, done, err := m.begin(ctx)
	if err != nil {
		return out, err
	}
	defer done()
	expected, decodeErr := hex.DecodeString(in.ExpectedSHA256)
	if decodeErr != nil || len(expected) != sha256.Size || len(in.Edits) == 0 || len(in.Edits) > m.cfg.MaxMatches {
		return out, failure("invalid_argument", "A complete SHA-256 and a bounded non-empty edit list are required")
	}
	select {
	case m.patchGate <- struct{}{}:
		defer func() { <-m.patchGate }()
	case <-ctx.Done():
		return out, contextError(ctx)
	}
	if err = contextError(ctx); err != nil {
		return out, err
	}
	f, before, err := openPath(in.Path, false)
	if err != nil {
		return out, err
	}
	data, originalHash, readErr := loadPatchText(ctx, f, m.cfg.MaxFileBytes)
	after, statErr := f.Stat()
	closeErr := f.Close()
	if readErr != nil {
		return out, readErr
	}
	if statErr != nil {
		return out, ioError(statErr)
	}
	if closeErr != nil {
		return out, ioError(closeErr)
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || originalHash != strings.ToLower(in.ExpectedSHA256) {
		return out, failure("conflict", "File content does not match the expected SHA-256 or changed during reading")
	}
	patched, err := patchText(data, in.Edits, m.cfg.MaxFileBytes)
	if err != nil {
		return out, err
	}
	if err = contextError(ctx); err != nil {
		return out, err
	}
	temp, err := os.CreateTemp(filepath.Dir(in.Path), ".remote-mcp-patch-*")
	if err != nil {
		return out, ioError(err)
	}
	tempName := temp.Name()
	defer func() {
		temp.Close()
		if cleanupErr := os.Remove(tempName); cleanupErr != nil && !os.IsNotExist(cleanupErr) && err == nil {
			err = failure("cleanup_failed", "Failed to remove the patch temporary file")
		}
	}()
	if err = temp.Chmod(before.Mode()); err != nil {
		return out, ioError(err)
	}
	if err = writeTemporary(ctx, temp, patched); err != nil {
		return out, err
	}
	if err = contextError(ctx); err != nil {
		return out, err
	}
	if err = verifyTarget(ctx, in.Path, before, originalHash, m.cfg); err != nil {
		return out, err
	}
	if err = contextError(ctx); err != nil {
		return out, err
	}
	// 最终复核与平台替换之间无法对非合作外部写入者提供文件系统 CAS。
	if publishErr := m.publish(tempName, in.Path, true); publishErr != nil {
		return out, failure("publish_failed", "Failed to atomically publish the patch; the existing target was preserved")
	}
	return PatchResult{digest(patched), originalHash, int64(len(patched)), len(in.Edits)}, nil
}
