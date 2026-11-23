package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/instrumentation"
	"github.com/TuanFaied/qryvanta/internal/parser"
	"github.com/TuanFaied/qryvanta/internal/parser/parsertest"
)

// recorder keeps the step events of a diagnosis.
type recorder struct{ events []StepEvent }

func (r *recorder) emit(e StepEvent) { r.events = append(r.events, e) }

func (r *recorder) statuses() string {
	var parts []string
	for _, e := range r.events {
		parts = append(parts, string(e.Step)+":"+string(e.Status))
	}
	return strings.Join(parts, " ")
}

func finalSteps(d Diagnosis) string {
	var parts []string
	for _, s := range d.Steps {
		parts = append(parts, string(s.Step)+":"+string(s.Status))
	}
	return strings.Join(parts, " ")
}

func TestDiagnoseWithoutAConnection(t *testing.T) {
	c := analyzeCore(t, parsertest.Fake{})
	var r recorder
	d, err := c.Diagnose(context.Background(), "SELECT 1", DiagnoseOptions{Emit: r.emit})
	if err != nil {
		t.Fatal(err)
	}
	want := "parse:running parse:done static:running static:done metadata:running metadata:skipped probes:skipped runProbes:skipped"
	if got := r.statuses(); got != want {
		t.Fatalf("events = %s\nwant     %s", got, want)
	}
	if got := finalSteps(d); got != "parse:done static:done metadata:skipped probes:skipped runProbes:skipped" {
		t.Fatalf("steps = %s", got)
	}
	if !strings.Contains(d.Steps[2].Reason, "No connection") || d.Steps[3].Reason != "Probes need a connection." {
		t.Fatalf("reasons = %+v", d.Steps)
	}
	if d.ID == "" || c.analyses.get(d.ID) == nil {
		t.Fatal("the diagnosis is not kept for RunProbes")
	}
}

func TestDiagnoseReportsRefusalsAndSyntaxErrors(t *testing.T) {
	writes := parsertest.Fake{Statements: func(string) []parser.Statement {
		return []parser.Statement{{Kind: parser.StmtDelete, Type: "DeleteStatement", Effects: []parser.Effect{{Kind: parser.EffectWrite,
			Object: &parser.ObjectName{Schema: "dbo", Name: "Orders"}}}}}
	}}
	c := analyzeCore(t, writes)
	d, err := c.Diagnose(context.Background(), "DELETE FROM dbo.Orders", DiagnoseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Refusals) != 1 || !strings.Contains(d.Steps[0].Reason, "read-only mode would refuse 1 statement(s)") {
		t.Fatalf("refusals = %+v, parse step = %+v", d.Refusals, d.Steps[0])
	}
}

func TestDiagnoseParserFailure(t *testing.T) {
	c := analyzeCore(t, parsertest.Fake{Err: fmt.Errorf("%w: missing", parser.ErrUnavailable)})
	var r recorder
	_, err := c.Diagnose(context.Background(), "SELECT 1", DiagnoseOptions{Emit: r.emit})
	if !errors.Is(err, parser.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if got := r.statuses(); got != "parse:running parse:failed static:skipped metadata:skipped probes:skipped runProbes:skipped" {
		t.Fatalf("events = %s", got)
	}
}

func TestDiagnoseCancelledPartWay(t *testing.T) {
	c := analyzeCore(t, parsertest.Fake{})
	ctx, cancel := context.WithCancel(context.Background())
	var r recorder
	d, err := c.Diagnose(ctx, "SELECT 1", DiagnoseOptions{Emit: func(e StepEvent) {
		r.emit(e)
		if e.Step == StepStatic && e.Status == StepDone {
			cancel() // the user presses Stop after the static rules
		}
	}})
	if !errors.Is(err, context.Canceled) || !IsStopped(err) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got := finalSteps(d); got != "parse:done static:done metadata:skipped probes:skipped runProbes:skipped" {
		t.Fatalf("steps = %s", got)
	}
	for _, s := range d.Steps[2:] {
		if s.Reason != "Cancelled." {
			t.Fatalf("step %s reason = %q", s.Step, s.Reason)
		}
	}
}

// TestDiagnoseAsksOnlyForProposedProbes: a connected diagnosis with nothing to measure never
// calls Approve and runs no probe. (The approval path with real findings runs in the CLI and
// integration tests, which need the real parser.)
func TestDiagnoseAsksOnlyForProposedProbes(t *testing.T) {
	runner := &probeRunner{}
	c, id, _ := probeCore(t, runner, 5, parsertest.Fake{}, "x")
	var asked []instrumentation.Probe
	approve := func(_ context.Context, probes []instrumentation.Probe) ([]string, error) {
		asked = probes
		return []string{"p1"}, nil
	}
	d, err := c.Diagnose(context.Background(), "SELECT 1", DiagnoseOptions{Target: AnalysisTarget{ConnectionID: id, Database: "Sales"}, Approve: approve})
	if err != nil {
		t.Fatal(err)
	}
	if asked != nil || finalSteps(d) != "parse:done static:done metadata:done probes:skipped runProbes:skipped" {
		t.Fatalf("asked = %v, steps = %s", asked, finalSteps(d))
	}
	if runner.count() != 0 {
		t.Fatal("a probe ran without being proposed")
	}
}
