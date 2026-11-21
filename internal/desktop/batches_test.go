package desktop

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/connection"
	"github.com/TuanFaied/qryvanta/internal/executor"
	"github.com/TuanFaied/qryvanta/internal/parser"
)

// twoBatchRunner reports two batches, each with one result set.
type twoBatchRunner struct{}

func (twoBatchRunner) Execute(_ context.Context, _ *sql.DB, _ executor.Request, sink executor.Sink) executor.Summary {
	bs := sink.(executor.BatchSink)
	for b := range 2 {
		_ = bs.BatchStart(executor.BatchRun{Batch: b, Line: 1 + 2*b})
		_ = sink.ResultSet(b, []executor.Column{{Name: "n", Type: "INT"}})
		_ = sink.Rows(b, [][]executor.Value{{int64(b)}})
	}
	return executor.Summary{Outcome: executor.OutcomeSucceeded, TotalRows: 2}
}

func TestQueryServiceReportsBatches(t *testing.T) {
	s, log, id := newQueryTestService(t, twoBatchRunner{})
	if _, err := s.Execute(context.Background(), "run-b", id, "", "SELECT 0; SELECT 1"); err != nil {
		t.Fatal(err)
	}
	var batches, sets []ExecutionEvent
	for _, e := range log.events {
		switch e.Kind {
		case KindBatch:
			batches = append(batches, e)
		case KindResultSet:
			sets = append(sets, e)
		}
	}
	if len(batches) != 2 || batches[1].Run == nil || batches[1].Run.Line != 3 {
		t.Fatalf("batch events = %+v", batches)
	}
	if len(sets) != 2 || sets[0].Batch != 0 || sets[1].Batch != 1 {
		t.Errorf("result sets should carry their batch: %+v", sets)
	}
}

type fixedParser struct {
	res *parser.Result
	err error
}

func (p fixedParser) Parse(context.Context, string, parser.Options) (*parser.Result, error) {
	return p.res, p.err
}
func (fixedParser) Check(context.Context) parser.Status { return parser.Status{} }
func (fixedParser) Close() error                        { return nil }

func syntaxService(t *testing.T, p parser.Parser) *QueryService {
	t.Helper()
	core, err := app.New(app.Options{
		ConfigPath:     filepath.Join(t.TempDir(), "config.json"),
		DisableFileLog: true,
		Credentials:    connection.NewMemoryStore(),
		Driver:         &fakeDriver{},
		Parser:         p,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = core.Close() })
	return newQueryService(core, func(ExecutionEvent) {})
}

func TestCheckSyntax(t *testing.T) {
	diag := parser.Diagnostic{Number: 46010, Message: "Incorrect syntax near FROM.", Line: 1, Column: 8}
	s := syntaxService(t, fixedParser{res: &parser.Result{Batches: []parser.Batch{{}, {}}, Diagnostics: []parser.Diagnostic{diag}}})
	got := s.CheckSyntax(context.Background(), "SELECT FROM\nGO\nSELECT 1")
	if !got.Available || got.Batches != 2 || len(got.Diagnostics) != 1 || got.Diagnostics[0] != diag {
		t.Errorf("CheckSyntax = %+v", got)
	}

	// A clean script has an empty list, not null, for the frontend.
	s = syntaxService(t, fixedParser{res: &parser.Result{Batches: []parser.Batch{{}}}})
	if got := s.CheckSyntax(context.Background(), "SELECT 1"); got.Diagnostics == nil || len(got.Diagnostics) != 0 {
		t.Errorf("clean CheckSyntax = %+v", got)
	}

	s = syntaxService(t, fixedParser{err: errors.Join(parser.ErrUnavailable, errors.New("not found"))})
	if got := s.CheckSyntax(context.Background(), "SELECT 1"); got.Available || got.Reason == "" || got.Diagnostics == nil {
		t.Errorf("unavailable CheckSyntax = %+v", got)
	}
}

func TestCompletionScopeNeverReturnsNullLists(t *testing.T) {
	// The parser is unavailable: the editor gets found = false and empty lists, and falls back.
	s := syntaxService(t, fixedParser{err: errors.Join(parser.ErrUnavailable, errors.New("not found"))})
	got := s.CompletionScope(context.Background(), "SELECT o. FROM dbo.Orders o", 9)
	if got.Found || got.Sources == nil || got.Ctes == nil {
		t.Errorf("CompletionScope = %+v", got)
	}
}
