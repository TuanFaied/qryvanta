package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/connection"
	"github.com/TuanFaied/qryvanta/internal/parser"
	"github.com/TuanFaied/qryvanta/internal/parser/parsertest"
)

func analyzeCore(t *testing.T, p parser.Parser) *Core {
	t.Helper()
	c, err := New(Options{
		ConfigPath:     filepath.Join(t.TempDir(), "config.json"),
		DisableFileLog: true,
		Credentials:    connection.NewMemoryStore(),
		Parser:         p,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestAnalyzeReportsAnUnavailableParser(t *testing.T) {
	c := analyzeCore(t, parsertest.Fake{Err: fmt.Errorf("%w: not found", parser.ErrUnavailable)})
	if _, err := c.Analyze(context.Background(), "SELECT 1", AnalysisTarget{}); !errors.Is(err, parser.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestAnalyzeCountsStatements(t *testing.T) {
	c := analyzeCore(t, parsertest.Fake{})
	a, err := c.Analyze(context.Background(), "SELECT 1", AnalysisTarget{})
	if err != nil {
		t.Fatal(err)
	}
	if a.Statements != 1 || a.Rules == 0 || len(a.Findings) != 0 || a.Findings == nil || a.Failures == nil {
		t.Fatalf("analysis = %+v", a)
	}
}
