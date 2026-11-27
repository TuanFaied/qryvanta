package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/executor"
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
	want := "parse:running parse:done static:running static:done metadata:running metadata:skipped plan:skipped probes:skipped runProbes:skipped"
	if got := r.statuses(); got != want {
		t.Fatalf("events = %s\nwant     %s", got, want)
	}
	if got := finalSteps(d); got != "parse:done static:done metadata:skipped plan:skipped probes:skipped runProbes:skipped" {
		t.Fatalf("steps = %s", got)
	}
	if !strings.Contains(d.Steps[2].Reason, "No connection") || d.Steps[3].Reason != "No plan was requested." || d.Steps[4].Reason != "Probes need a connection." {
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
	if got := r.statuses(); got != "parse:running parse:failed static:skipped metadata:skipped plan:skipped probes:skipped runProbes:skipped" {
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
	if got := finalSteps(d); got != "parse:done static:done metadata:skipped plan:skipped probes:skipped runProbes:skipped" {
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
	if asked != nil || finalSteps(d) != "parse:done static:done metadata:done plan:skipped probes:skipped runProbes:skipped" {
		t.Fatalf("asked = %v, steps = %s", asked, finalSteps(d))
	}
	if runner.count() != 0 {
		t.Fatal("a probe ran without being proposed")
	}
}

// planFake parses any script as one statement, so a plan links to it by position.
var planFake = parsertest.Fake{Statements: func(sql string) []parser.Statement {
	return []parser.Statement{{Kind: "Select", Span: parser.Span{Offset: 0, Length: len(sql), Line: 1, Column: 1}}}
}}

func TestDiagnoseUsesGivenPlans(t *testing.T) {
	sql, err := os.ReadFile(filepath.Join("..", "..", "tests", "sample_queries", "10_missing_join_predicate.buggy.sql"))
	if err != nil {
		t.Fatal(err)
	}
	xml, err := os.ReadFile(filepath.Join("..", "..", "tests", "plans", "fixtures", "10_missing_join_predicate.buggy.actual.sqlplan"))
	if err != nil {
		t.Fatal(err)
	}
	c := analyzeCore(t, planFake)
	var r recorder
	d, err := c.Diagnose(context.Background(), string(sql), DiagnoseOptions{Emit: r.emit, Plan: executor.PlanEstimated,
		Plans: func(context.Context) []executor.Plan {
			return []executor.Plan{{Mode: executor.PlanActual, Batch: 0, Execution: 1, Statement: 1, XML: string(xml)}}
		}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.statuses(), "plan:running plan:done") || d.PlanMode != executor.PlanActual {
		t.Fatalf("events = %s, plan mode = %q", r.statuses(), d.PlanMode)
	}
	if got := d.Steps[3].Reason; !strings.HasPrefix(got, "Actual plan, 1 document(s)") {
		t.Fatalf("plan step reason = %q", got)
	}
	// Without a model, QRY-JOIN-001 cannot run: the plan's warning is reported on its own.
	found := false
	for _, f := range d.Findings {
		found = found || (f.RuleID == "QRY-PLAN-004" && f.Classification == diagnostics.Confirmed && f.Evidence[0].Plan.Actual)
	}
	if !found {
		t.Fatalf("findings = %+v, want QRY-PLAN-004 from the actual plan", d.Findings)
	}
}

func TestDiagnosePlanFallbacks(t *testing.T) {
	c := analyzeCore(t, planFake)
	none := func(context.Context) []executor.Plan { return nil }
	d, _ := c.Diagnose(context.Background(), "SELECT 1", DiagnoseOptions{Plan: executor.PlanEstimated, Plans: none})
	if s := d.Steps[3]; s.Status != StepSkipped || s.Reason != "The run gave no plan for this SQL. Plans need a connection." {
		t.Fatalf("plan step = %+v", s)
	}
	broken := func(context.Context) []executor.Plan {
		return []executor.Plan{{Mode: executor.PlanEstimated, Execution: 1, XML: "<not a plan"}}
	}
	d, _ = c.Diagnose(context.Background(), "SELECT 1", DiagnoseOptions{Plans: broken})
	if s := d.Steps[3]; s.Status != StepFailed || s.Reason != "None of the 1 plan(s) could be read." || d.PlanMode != "" {
		t.Fatalf("plan step = %+v", s)
	}
}
