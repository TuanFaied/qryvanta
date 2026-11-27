package desktop

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/connection"
	"github.com/TuanFaied/qryvanta/internal/parser"
	"github.com/TuanFaied/qryvanta/internal/parser/parsertest"
)

func testAnalysisService(t *testing.T, p parser.Parser) *AnalysisService {
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
	return NewAnalysisService(core, nil)
}

func TestAnalysisServiceParserUnavailable(t *testing.T) {
	s := testAnalysisService(t, parsertest.Fake{Err: fmt.Errorf("%w: sidecar not found", parser.ErrUnavailable)})
	a := s.Analyze(context.Background(), "SELECT 1", "", "")
	if a.Available || !strings.Contains(a.Reason, "sidecar not found") {
		t.Fatalf("analysis = %+v, want unavailable with the reason", a)
	}
	if a.Findings == nil || a.Failures == nil {
		t.Fatal("lists must not be null")
	}
}

func TestAnalysisServiceNoFindings(t *testing.T) {
	a := testAnalysisService(t, parsertest.Fake{}).Analyze(context.Background(), "SELECT 1", "", "")
	if !a.Available || a.Statements != 1 || a.Rules == 0 || a.Findings == nil || len(a.Findings) != 0 || a.Failures == nil {
		t.Fatalf("analysis = %+v", a)
	}
}

// diagnosisLog keeps diagnosis events.
type diagnosisLog struct {
	mu     sync.Mutex
	events []DiagnosisEvent
}

func (l *diagnosisLog) add(e DiagnosisEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}

func (l *diagnosisLog) statuses() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var parts []string
	for _, e := range l.events {
		parts = append(parts, string(e.Step)+":"+string(e.Status))
	}
	return strings.Join(parts, " ")
}

func TestDiagnoseStreamsStepStatuses(t *testing.T) {
	log := &diagnosisLog{}
	s := testAnalysisService(t, parsertest.Fake{})
	s.emit = log.add
	a := s.Diagnose(context.Background(), "run-1", "SELECT 1", "", "", "", "")
	want := "parse:running parse:done static:running static:done metadata:running metadata:skipped plan:skipped probes:skipped runProbes:skipped"
	if got := log.statuses(); got != want {
		t.Fatalf("events = %s\nwant     %s", got, want)
	}
	for _, e := range log.events {
		if e.RunID != "run-1" {
			t.Fatalf("event without the run ID: %+v", e)
		}
	}
	if !a.Available || a.Stopped || len(a.Steps) != 6 || a.ID == "" || a.Findings == nil || a.Probes == nil {
		t.Fatalf("analysis = %+v", a)
	}
}

func TestDiagnoseCancelledPartWay(t *testing.T) {
	log := &diagnosisLog{}
	s := testAnalysisService(t, parsertest.Fake{})
	ctx, cancel := context.WithCancel(context.Background())
	s.emit = func(e DiagnosisEvent) {
		log.add(e)
		if e.Step == "static" && e.Status == "done" {
			cancel() // Stop while the metadata step would start
		}
	}
	a := s.Diagnose(ctx, "run-2", "SELECT 1", "", "", "", "")
	if !a.Available || !a.Stopped {
		t.Fatalf("analysis = %+v, want stopped with the findings so far", a)
	}
	if got := log.statuses(); !strings.HasSuffix(got, "static:done metadata:skipped plan:skipped probes:skipped runProbes:skipped") {
		t.Fatalf("events = %s", got)
	}
	if a.Steps[2].Reason != "Cancelled." {
		t.Fatalf("steps = %+v", a.Steps)
	}
}

func TestDiagnoseParserUnavailable(t *testing.T) {
	s := testAnalysisService(t, parsertest.Fake{Err: fmt.Errorf("%w: sidecar not found", parser.ErrUnavailable)})
	s.emit = func(DiagnosisEvent) {}
	a := s.Diagnose(context.Background(), "run-3", "SELECT 1", "", "", "", "")
	if a.Available || a.Stopped || !strings.Contains(a.Reason, "sidecar not found") || a.Steps[0].Status != "failed" {
		t.Fatalf("analysis = %+v", a)
	}
}
