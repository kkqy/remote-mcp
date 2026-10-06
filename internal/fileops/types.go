// Package fileops 提供有界文本巡查和带原内容校验的行补丁。
package fileops

import "encoding/json"

type Config struct {
	MaxFileBytes   int64
	MaxResultBytes int
	MaxListEntries int
	MaxScanEntries int
	MaxScanBytes   int64
	MaxDepth       int
	MaxMatches     int
}

func DefaultConfig() Config {
	return Config{8 << 20, 64 << 10, 1000, 10000, 64 << 20, 16, 1000}
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string           { b, _ := json.Marshal(e); return string(b) }
func failure(code, message string) error { return &Error{code, message} }

type ListInput struct {
	Path   string `json:"path" jsonschema:"Directory path; symbolic link targets are rejected"`
	Offset int    `json:"offset,omitempty" jsonschema:"Directory enumeration offset; entries may change between calls"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum entries; defaults to the configured maximum"`
}
type Entry struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Size int64  `json:"size"`
}
type ListResult struct {
	Entries        []Entry `json:"entries"`
	NextOffset     int     `json:"next_offset"`
	ScannedEntries int     `json:"scanned_entries"`
	Truncated      bool    `json:"truncated"`
	Reason         string  `json:"reason,omitempty"`
}
type ReadInput struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line,omitempty" jsonschema:"First line, starting at 1; defaults to 1"`
	LineCount int    `json:"line_count,omitempty" jsonschema:"Maximum lines; defaults to the smaller of 1000 and the configured scan entry limit"`
	MaxBytes  int    `json:"max_bytes,omitempty" jsonschema:"Maximum returned UTF-8 bytes; defaults to the configured maximum, never above 65536. Increase within this limit to reread a byte-limited partial line; longer lines only provide a preview"`
}
type ReadResult struct {
	Text          string `json:"text"`
	SHA256        string `json:"sha256"`
	Size          int64  `json:"size"`
	StartLine     int    `json:"start_line"`
	EndLine       int    `json:"end_line"`
	NextLine      int    `json:"next_line" jsonschema:"First line not fully returned; a partial line keeps this coordinate unchanged. Do not blindly concatenate repeated prefixes. To skip a partial line, use end_line+1; lines longer than 65536 bytes cannot be fully returned"`
	TotalLines    int    `json:"total_lines"`
	ReturnedBytes int    `json:"returned_bytes"`
	Truncated     bool   `json:"truncated"`
	Reason        string `json:"reason,omitempty"`
}
type SearchInput struct {
	Path         string `json:"path"`
	Query        string `json:"query" jsonschema:"Non-empty literal UTF-8 substring; matching is case-sensitive within each original line, including its line ending. Queries spanning multiple lines are not supported"`
	MaxDepth     *int   `json:"max_depth,omitempty" jsonschema:"Depth below the root directory; defaults to the configured maximum"`
	MaxEntries   int    `json:"max_entries,omitempty"`
	MaxScanBytes int64  `json:"max_scan_bytes,omitempty"`
	MaxFileBytes int64  `json:"max_file_bytes,omitempty"`
	MaxMatches   int    `json:"max_matches,omitempty"`
}
type Match struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}
type Issue struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
type SearchResult struct {
	Matches        []Match `json:"matches"`
	Issues         []Issue `json:"issues"`
	ScannedEntries int     `json:"scanned_entries"`
	ScannedFiles   int     `json:"scanned_files"`
	ScannedBytes   int64   `json:"scanned_bytes"`
	Truncated      bool    `json:"truncated"`
	Reason         string  `json:"reason,omitempty"`
}
type Edit struct {
	StartLine   int    `json:"start_line" jsonschema:"Original line coordinate, starting at 1; line_count+1 appends"`
	DeleteCount int    `json:"delete_count" jsonschema:"Number of original lines to remove"`
	Text        string `json:"text" jsonschema:"Exact UTF-8 replacement bytes; include desired LF or CRLF explicitly"`
}
type PatchInput struct {
	Path           string `json:"path"`
	ExpectedSHA256 string `json:"expected_sha256" jsonschema:"SHA-256 of the original complete file"`
	Edits          []Edit `json:"edits" jsonschema:"Non-overlapping edits ordered by original line coordinate"`
}
type PatchResult struct {
	SHA256         string `json:"sha256"`
	PreviousSHA256 string `json:"previous_sha256"`
	Size           int64  `json:"size"`
	AppliedEdits   int    `json:"applied_edits"`
}
