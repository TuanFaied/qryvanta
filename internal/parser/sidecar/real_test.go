//go:build parser

package sidecar

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/TuanFaied/qryvanta/internal/parser"
)

// These tests run against the real sidecar. Build it (wails3 task common:build:sidecar) and set
// QRYVANTA_PARSER_PATH, then: go test -tags parser ./internal/parser/...
func realClient(t *testing.T) *Client {
	t.Helper()
	path := os.Getenv("QRYVANTA_PARSER_PATH")
	if path == "" {
		t.Skip("QRYVANTA_PARSER_PATH not set; build the sidecar with `wails3 task common:build:sidecar`")
	}
	c := New(Options{Path: path, Timeout: 10 * time.Second})
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestRealSidecarParsesScripts(t *testing.T) {
	c := realClient(t)
	st := c.Check(context.Background())
	if !st.Available || st.Protocol != ProtocolVersion || st.Version == "" || st.ScriptDom == "" {
		t.Fatalf("status = %+v", st)
	}

	sql := "SELECT N'🐘 Müller' AS name;\nGO 2\n-- comment only\nGO\nSELECT x FROM WHERE;\n"
	res, err := c.Parse(context.Background(), sql, parser.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Batches) != 2 || res.Batches[0].Repeat != 2 || res.Batches[1].Line != 5 {
		t.Fatalf("batches = %+v", res.Batches)
	}
	for _, b := range res.Batches {
		if sql[b.ByteOffset:b.ByteOffset+b.ByteLength] != b.Text {
			t.Errorf("byte range of batch %d does not match its text", b.Index)
		}
	}
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v", res.Diagnostics)
	}
	d := res.Diagnostics[0]
	// ScriptDom reports "Incorrect syntax near 'FROM'"; the byte offset must land on it, after
	// the four-byte emoji and two-byte ü earlier in the script.
	if d.Line != 5 || d.Column != 10 || !strings.HasPrefix(sql[d.ByteOffset:], "FROM WHERE") {
		t.Errorf("diagnostic = %+v, points at %q", d, sql[d.ByteOffset:])
	}
}

func TestRealSidecarReportsProtocolErrors(t *testing.T) {
	c := realClient(t)
	_, err := c.Parse(context.Background(), "SELECT 1", parser.Options{CompatLevel: 75})
	var pe *ProtocolError
	if !errors.As(err, &pe) || pe.Code != "unsupported_compat_level" {
		t.Fatalf("err = %v", err)
	}
	// The process is still usable.
	if _, err := c.Parse(context.Background(), "SELECT 1", parser.Options{CompatLevel: 160}); err != nil {
		t.Fatal(err)
	}
}

func TestRealSidecarHandlesLargeScripts(t *testing.T) {
	c := realClient(t)
	var b strings.Builder
	for range 5000 {
		b.WriteString("SELECT a, b, N'<&>' FROM dbo.t WHERE a = 1;\nGO\n")
	}
	res, err := c.Parse(context.Background(), b.String(), parser.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Batches) != 5000 || len(res.Diagnostics) != 0 {
		t.Fatalf("batches %d, diagnostics %d", len(res.Batches), len(res.Diagnostics))
	}
}
