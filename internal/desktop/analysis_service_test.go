package desktop

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/connection"
	"github.com/TuanFaied/qryvanta/internal/parser"
	"github.com/TuanFaied/qryvanta/internal/parser/parsertest"
)

func newAnalysisService(t *testing.T, p parser.Parser) *AnalysisService {
	t.Helper()
	core, err := app.New(app.Options{
		ConfigPath:     filepath.Join(t.TempDir(), "config.json"),
		DisableFileLog: true,
		Credentials:    connection.NewMemoryStore(),
		Parser:         p,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = core.Close() })
	return NewAnalysisService(core)
}

func TestAnalysisServiceParserUnavailable(t *testing.T) {
	s := newAnalysisService(t, parsertest.Fake{Err: fmt.Errorf("%w: sidecar not found", parser.ErrUnavailable)})
	a := s.Analyze(context.Background(), "SELECT 1", "", "")
	if a.Available || !strings.Contains(a.Reason, "sidecar not found") {
		t.Fatalf("analysis = %+v, want unavailable with the reason", a)
	}
	if a.Findings == nil || a.Failures == nil {
		t.Fatal("lists must not be null")
	}
}

func TestAnalysisServiceNoFindings(t *testing.T) {
	a := newAnalysisService(t, parsertest.Fake{}).Analyze(context.Background(), "SELECT 1", "", "")
	if !a.Available || a.Statements != 1 || a.Rules == 0 || a.Findings == nil || len(a.Findings) != 0 || a.Failures == nil {
		t.Fatalf("analysis = %+v", a)
	}
}
