package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/parser"
)

// fakeParser splits on "GO" lines and reports "ERR" as a syntax error.
type fakeParser struct {
	err    error
	gotOpt parser.Options
}

func (f *fakeParser) Parse(_ context.Context, sql string, opts parser.Options) (*parser.Result, error) {
	f.gotOpt = opts
	if f.err != nil {
		return nil, f.err
	}
	res := &parser.Result{CompatLevel: 170, Batches: []parser.Batch{}, Diagnostics: []parser.Diagnostic{}}
	line := 1
	for i, part := range strings.Split(sql, "\nGO\n") {
		res.Batches = append(res.Batches, parser.Batch{Index: i, Text: part, Length: len(part), Line: line, Column: 1, Repeat: 1})
		line += strings.Count(part, "\n") + 2
	}
	if i := strings.Index(sql, "ERR"); i >= 0 {
		res.Diagnostics = append(res.Diagnostics, parser.Diagnostic{Number: 46010, Message: "Incorrect syntax near 'ERR'.", Line: 3, Column: 8})
	}
	return res, nil
}

func (f *fakeParser) Check(context.Context) parser.Status {
	return parser.Status{Available: f.err == nil}
}
func (f *fakeParser) Close() error { return nil }

func parseCmd(t *testing.T, p parser.Parser, args ...string) (string, string, int) {
	t.Helper()
	opts, _, _ := testOpts(t)
	opts.Parser = p
	var out, errOut bytes.Buffer
	err := cmdParse(context.Background(), args, opts, &out, &errOut)
	code := report(err, &bytes.Buffer{}, &errOut) // writes to errOut, so before reading it
	return out.String(), errOut.String(), code
}

func TestParseCommandPrintsBatchesAndErrors(t *testing.T) {
	p := &fakeParser{}
	out, errOut, code := parseCmd(t, p, "--query", "SELECT 1\nGO\nSELECT ERR", "--compat", "150")
	if code != exitQueryError {
		t.Errorf("exit = %d, want %d for syntax errors", code, exitQueryError)
	}
	if !strings.Contains(out, "Batch 1: line 1") || !strings.Contains(out, "Batch 2: line 3") {
		t.Errorf("stdout = %q", out)
	}
	if !strings.Contains(errOut, "Line 3, column 8: Incorrect syntax near 'ERR'. (parser error 46010)") {
		t.Errorf("stderr = %q", errOut)
	}
	if p.gotOpt.CompatLevel != 150 {
		t.Errorf("compat level = %d", p.gotOpt.CompatLevel)
	}

	if _, _, code := parseCmd(t, &fakeParser{}, "--query", "SELECT 1"); code != 0 {
		t.Errorf("clean script: exit %d", code)
	}
}

func TestParseCommandJSON(t *testing.T) {
	out, _, code := parseCmd(t, &fakeParser{}, "--query", "SELECT 1\nGO\nSELECT 2", "--json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var res parser.Result
	if err := json.Unmarshal([]byte(out), &res); err != nil || len(res.Batches) != 2 {
		t.Fatalf("json = %s (%v)", out, err)
	}
}

func TestParseCommandParserUnavailableAndUsage(t *testing.T) {
	unavailable := &fakeParser{err: fmt.Errorf("%w: the parser was not found at x", parser.ErrUnavailable)}
	_, errOut, code := parseCmd(t, unavailable, "--query", "SELECT 1")
	if code != 1 || !strings.Contains(errOut, "parser unavailable: the parser was not found") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
	for _, args := range [][]string{{"--query", "a", "--file", "b"}, {"SELECT 1"}, {"--query", " "}} {
		if _, _, code := parseCmd(t, &fakeParser{}, args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
	if !errors.Is(unavailable.err, parser.ErrUnavailable) {
		t.Fatal("fake should wrap ErrUnavailable")
	}
}

func TestCoreClosesTheParser(t *testing.T) {
	opts, _, _ := testOpts(t)
	p := &closeRecorder{}
	opts.Parser = p
	core, err := app.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if core.Parser() != p {
		t.Fatal("Core.Parser should return the injected parser")
	}
	_ = core.Close()
	if !p.closed {
		t.Error("Core.Close did not close the parser")
	}
}

type closeRecorder struct {
	fakeParser
	closed bool
}

func (c *closeRecorder) Close() error {
	c.closed = true
	return nil
}
