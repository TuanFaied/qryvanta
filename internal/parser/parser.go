// Package parser defines how Qryvanta parses T-SQL (architecture decision D2).
//
// The Parser interface hides the implementation: today a .NET ScriptDom sidecar
// (internal/parser/sidecar), later possibly a pure-Go parser. Callers never see ScriptDom
// types. A missing or failing parser is reported as ErrUnavailable and never guessed around.
package parser

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"unicode/utf8"
)

// ErrUnavailable is returned (wrapped, with the reason) when the parser cannot be used: the
// sidecar is missing, does not start, or keeps failing. Query execution still works.
var ErrUnavailable = errors.New("parser unavailable")

// Parser parses T-SQL scripts. Implementations are safe for concurrent use.
type Parser interface {
	// Parse splits sql into batches and reports syntax errors.
	Parse(ctx context.Context, sql string, opts Options) (*Result, error)
	// Check starts the parser if needed and reports whether it works.
	Check(ctx context.Context) Status
	// Close stops the parser and releases its resources.
	Close() error
}

// Options adjust one parse.
type Options struct {
	// CompatLevel is the SQL Server compatibility level (80-180). Zero uses the default.
	CompatLevel int
}

// Result is a parsed script.
type Result struct {
	CompatLevel int          `json:"compatLevel"`
	Batches     []Batch      `json:"batches"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Batch is one batch of a script, as separated by GO lines.
//
// Positions are given twice: UTF-16 code units (Offset, Length), which the editor (Monaco,
// JavaScript strings) uses, and bytes (ByteOffset, ByteLength) for slicing Go strings.
type Batch struct {
	Index      int    `json:"index"`
	Text       string `json:"text"`
	Offset     int    `json:"offset"`
	Length     int    `json:"length"`
	ByteOffset int    `json:"byteOffset"`
	ByteLength int    `json:"byteLength"`
	Line       int    `json:"line"`
	Column     int    `json:"column"`
	// Repeat is n for "GO n" after the batch, otherwise 1.
	Repeat int `json:"repeat"`
}

// Diagnostic is a syntax error. Line and Column are 1-based; Column counts UTF-16 code units,
// as editors do.
type Diagnostic struct {
	Number     int    `json:"number"`
	Message    string `json:"message"`
	Offset     int    `json:"offset"`
	ByteOffset int    `json:"byteOffset"`
	Line       int    `json:"line"`
	Column     int    `json:"column"`
}

// Status describes the parser for the UI and logs.
type Status struct {
	Available bool `json:"available"`
	// Reason explains why the parser is unavailable.
	Reason    string `json:"reason"`
	Version   string `json:"version"`
	ScriptDom string `json:"scriptDom"`
	Protocol  int    `json:"protocol"`
	Path      string `json:"path"`
}

// ExecutableName is the sidecar's file name on this platform.
func ExecutableName() string {
	if runtime.GOOS == "windows" {
		return "qryvanta-tsqlparser.exe"
	}
	return "qryvanta-tsqlparser"
}

// DefaultPath returns where the sidecar is expected: QRYVANTA_PARSER_PATH when set (for
// development), otherwise next to the running executable, where the build puts it.
func DefaultPath() string {
	if p := os.Getenv("QRYVANTA_PARSER_PATH"); p != "" {
		return p
	}
	exe, err := os.Executable()
	if err != nil {
		return ExecutableName()
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Join(filepath.Dir(exe), ExecutableName())
}

// ByteOffsets converts sorted or unsorted UTF-16 offsets into byte offsets in s. Offsets past
// the end map to len(s). It walks s once.
func ByteOffsets(s string, utf16Offsets ...int) []int {
	out := make([]int, len(utf16Offsets))
	if len(utf16Offsets) == 0 {
		return out
	}
	// Map every requested offset; a small map keeps this simple and fast for few offsets.
	want := make(map[int][]int, len(utf16Offsets))
	maxWanted := 0
	for i, o := range utf16Offsets {
		want[o] = append(want[o], i)
		maxWanted = max(maxWanted, o)
		out[i] = len(s)
	}
	u16, b := 0, 0
	for b < len(s) && u16 <= maxWanted {
		if idx, ok := want[u16]; ok {
			for _, i := range idx {
				out[i] = b
			}
		}
		r, size := utf8.DecodeRuneInString(s[b:])
		if r >= 0x10000 {
			u16 += 2
		} else {
			u16++
		}
		b += size
	}
	if idx, ok := want[u16]; ok && b == len(s) {
		for _, i := range idx {
			out[i] = b
		}
	}
	return out
}
