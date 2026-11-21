package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/connection"
	"github.com/TuanFaied/qryvanta/internal/executor"
	"github.com/TuanFaied/qryvanta/internal/parser"
	"github.com/TuanFaied/qryvanta/internal/parser/parsertest"
)

// readOnlyCore is a connected core in read-only mode (the default settings).
func readOnlyCore(t *testing.T, p parser.Parser, r executor.Runner) (*Core, string) {
	t.Helper()
	c, err := New(Options{
		ConfigPath:     filepath.Join(t.TempDir(), "config.json"),
		DisableFileLog: true,
		Credentials:    connection.NewMemoryStore(),
		Driver:         nopDriver{},
		Access:         noAccess{},
		Runner:         r,
		Parser:         p,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	prof, _ := c.Connections().Create(connection.Profile{Name: "x", Server: "db", Auth: connection.AuthWindows}, "")
	if _, err := c.Pool().Connect(context.Background(), prof.ID); err != nil {
		t.Fatal(err)
	}
	return c, prof.ID
}

// updateOrders describes every script as one UPDATE of dbo.Orders.
var updateOrders = parsertest.Fake{Statements: func(string) []parser.Statement {
	return []parser.Statement{{Kind: parser.StmtUpdate, Type: "UpdateStatement", Span: parser.Span{Length: 26, Line: 1, Column: 1},
		Effects: []parser.Effect{{Kind: parser.EffectWrite, Object: &parser.ObjectName{Schema: "dbo", Name: "Orders"},
			Span: parser.Span{Offset: 7, Length: 10, Line: 1, Column: 8}}}}}
}}

func TestReadOnlyRefusalNeverReachesTheRunner(t *testing.T) {
	r := &batchRunner{}
	c, id := readOnlyCore(t, updateOrders, r)
	sum := c.Execute(context.Background(), id, executor.Request{SQL: "UPDATE dbo.Orders SET a = 1"}, &executor.Collector{})
	if sum.Outcome != executor.OutcomeRefused || len(sum.Refusals) != 1 {
		t.Fatalf("summary = %+v", sum)
	}
	if f := sum.Refusals[0]; f.Statement != "UPDATE" || f.Line != 1 || f.Offset != 7 || !strings.Contains(f.Reason, "dbo.Orders") {
		t.Errorf("refusal = %+v", f)
	}
	if !strings.Contains(sum.Error, "Line 1, UPDATE") {
		t.Errorf("error = %q", sum.Error)
	}
	if r.got.SQL != "" || r.got.Batches != nil {
		t.Fatal("a refused script must not reach the runner")
	}
}

func TestReadOnlyRunsAllowedScripts(t *testing.T) {
	r := &batchRunner{}
	c, id := readOnlyCore(t, parsertest.Fake{}, r)
	sum := c.Execute(context.Background(), id, executor.Request{SQL: "SELECT 1"}, &executor.Collector{})
	if sum.Outcome != executor.OutcomeSucceeded || len(r.got.Batches) != 1 || !r.got.Policy.ReadOnly {
		t.Fatalf("summary = %+v, request = %+v", sum, r.got)
	}
}

// Without the parser, read-only mode cannot check the script, so it runs nothing.
func TestReadOnlyRefusesWithoutTheParser(t *testing.T) {
	r := &batchRunner{}
	c, id := readOnlyCore(t, parsertest.Fake{Err: fmt.Errorf("%w: not found", parser.ErrUnavailable)}, r)
	sum := c.Execute(context.Background(), id, executor.Request{SQL: "SELECT 1"}, &executor.Collector{})
	if sum.Outcome != executor.OutcomeRefused || !strings.Contains(sum.Error, "parser is not available") ||
		!strings.Contains(sum.Error, "Settings") {
		t.Fatalf("summary = %+v", sum)
	}
	if r.got.SQL != "" {
		t.Fatal("the script must not run")
	}

	// A cancelled check is reported as cancelled, not refused.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c2, id2 := readOnlyCore(t, parsertest.Fake{Err: context.Canceled}, r)
	if sum := c2.Execute(ctx, id2, executor.Request{SQL: "SELECT 1"}, &executor.Collector{}); sum.Outcome != executor.OutcomeCancelled {
		t.Errorf("cancelled check: %+v", sum)
	}
}

// Batches the caller already split are checked one at a time; lines refer to the script.
func TestReadOnlyChecksCallerBatches(t *testing.T) {
	r := &batchRunner{}
	c, id := readOnlyCore(t, updateOrders, r)
	sum := c.Execute(context.Background(), id, executor.Request{Batches: []executor.Batch{
		{SQL: "UPDATE dbo.Orders SET a = 1", Line: 4, Repeat: 1},
	}}, &executor.Collector{})
	if sum.Outcome != executor.OutcomeRefused || sum.Refusals[0].Line != 4 || sum.Refusals[0].Batch != 0 {
		t.Fatalf("summary = %+v", sum)
	}
}

// With read-only mode off, nothing is checked: writes run (under the user's confirmation).
func TestWritesAllowedSkipsTheCheck(t *testing.T) {
	r := &batchRunner{}
	c, id := coreWith(t, updateOrders, r)
	if sum := c.Execute(context.Background(), id, executor.Request{SQL: "UPDATE dbo.Orders SET a = 1"}, &executor.Collector{}); sum.Outcome != executor.OutcomeSucceeded {
		t.Fatalf("summary = %+v", sum)
	}
}
