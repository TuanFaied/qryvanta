package app

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/connection"
	"github.com/TuanFaied/qryvanta/internal/executor"
	"github.com/TuanFaied/qryvanta/internal/parser"
)

// splitParser splits on "\nGO\n" (and "\nGO 3\n"), as the real parser would, or fails.
type splitParser struct{ err error }

func (p splitParser) Parse(_ context.Context, sql string, _ parser.Options) (*parser.Result, error) {
	if p.err != nil {
		return nil, p.err
	}
	res := &parser.Result{}
	line := 1
	for i, part := range strings.Split(sql, "\nGO") {
		// "GO 3" repeats the batch before it.
		if strings.HasPrefix(part, " 3\n") {
			res.Batches[i-1].Repeat = 3
			part = strings.TrimPrefix(part, " 3\n")
		} else {
			part = strings.TrimPrefix(part, "\n")
		}
		res.Batches = append(res.Batches, parser.Batch{Index: i, Text: part, Line: line, Repeat: 1})
		line += strings.Count(part, "\n") + 2
	}
	return res, nil
}
func (splitParser) Check(context.Context) parser.Status { return parser.Status{} }
func (splitParser) Close() error                        { return nil }

// batchRunner records the batches it was given.
type batchRunner struct{ got executor.Request }

func (r *batchRunner) Execute(_ context.Context, _ *sql.DB, req executor.Request, _ executor.Sink) executor.Summary {
	r.got = req
	return executor.Summary{Outcome: executor.OutcomeSucceeded}
}

func coreWith(t *testing.T, p parser.Parser, r executor.Runner) (*Core, string) {
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

func TestExecuteSplitsScriptsIntoBatchesWithTheParser(t *testing.T) {
	r := &batchRunner{}
	c, id := coreWith(t, splitParser{}, r)
	sum := c.Execute(context.Background(), id, executor.Request{SQL: "CREATE TABLE #t (a int);\nGO 3\nSELECT * FROM #t;"}, &executor.Collector{})
	if sum.Outcome != executor.OutcomeSucceeded {
		t.Fatalf("summary = %+v", sum)
	}
	got := r.got.Batches
	if len(got) != 2 || got[0].Repeat != 3 || got[1].SQL != "SELECT * FROM #t;" || got[1].Line != 3 {
		t.Fatalf("batches = %+v", got)
	}
	if !r.got.Policy.ReadOnly {
		t.Error("the policy must still come from the settings")
	}
}

func TestExecuteKeepsBatchesTheCallerAlreadySplit(t *testing.T) {
	r := &batchRunner{}
	c, id := coreWith(t, splitParser{err: fmt.Errorf("%w: test", parser.ErrUnavailable)}, r)
	given := []executor.Batch{{SQL: "SELECT 1", Line: 1, Repeat: 1}, {SQL: "SELECT 2", Line: 3, Repeat: 1}}
	_ = c.Execute(context.Background(), id, executor.Request{SQL: "ignored", Batches: given}, &executor.Collector{})
	if len(r.got.Batches) != 2 || r.got.Batches[1].SQL != "SELECT 2" {
		t.Fatalf("batches = %+v", r.got.Batches)
	}
}

func TestWithoutTheParserScriptsWithGoAreRefused(t *testing.T) {
	r := &batchRunner{}
	c, id := coreWith(t, splitParser{err: fmt.Errorf("%w: the parser was not found at x", parser.ErrUnavailable)}, r)

	sum := c.Execute(context.Background(), id, executor.Request{SQL: "SELECT 1;\n  go  \nSELECT 2;"}, &executor.Collector{})
	if sum.Outcome != executor.OutcomeFailed || !strings.Contains(sum.Error, "GO separators") ||
		!strings.Contains(sum.Error, "was not found") {
		t.Fatalf("summary = %+v", sum)
	}
	if r.got.SQL != "" || r.got.Batches != nil {
		t.Fatal("a refused script must not reach the runner")
	}

	// GO n is a separator too.
	if sum := c.Execute(context.Background(), id, executor.Request{SQL: "SELECT 1\r\nGO 5\r\n"}, &executor.Collector{}); sum.Outcome != executor.OutcomeFailed {
		t.Fatalf("GO 5: %+v", sum)
	}

	// Without GO lines the script runs as one batch; GO inside a word or after code is not a separator.
	for _, sql := range []string{"SELECT 1 AS GOAL", "SELECT 'x' -- GO\n", "SELECT 1 GO"} {
		sum := c.Execute(context.Background(), id, executor.Request{SQL: sql}, &executor.Collector{})
		if sum.Outcome != executor.OutcomeSucceeded || len(r.got.Batches) != 1 || r.got.Batches[0].SQL != sql {
			t.Fatalf("%q: summary %+v, batches %+v", sql, sum, r.got.Batches)
		}
	}
}
