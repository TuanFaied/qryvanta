package app

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TuanFaied/qryvanta/internal/connection"
	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/executor"
	"github.com/TuanFaied/qryvanta/internal/instrumentation"
	"github.com/TuanFaied/qryvanta/internal/parser"
	"github.com/TuanFaied/qryvanta/internal/parser/parsertest"
)

// probeRunner answers fan-out probes with a repeated key, and records what it was asked.
type probeRunner struct {
	mu       sync.Mutex
	requests []executor.Request
	running  atomic.Int32
	peak     atomic.Int32
	delay    time.Duration
}

func (r *probeRunner) Execute(ctx context.Context, _ *sql.DB, req executor.Request, sink executor.Sink) executor.Summary {
	r.mu.Lock()
	r.requests = append(r.requests, req)
	r.mu.Unlock()
	n := r.running.Add(1)
	defer r.running.Add(-1)
	for {
		p := r.peak.Load()
		if n <= p || r.peak.CompareAndSwap(p, n) {
			break
		}
	}
	select {
	case <-time.After(r.delay):
	case <-ctx.Done():
		return executor.Summary{Outcome: executor.OutcomeCancelled}
	}
	cols := []executor.Column{{Name: "key_values"}, {Name: "repeated_values"}, {Name: "max_rows_per_value"}, {Name: "avg_rows_per_value"}}
	_ = sink.ResultSet(0, cols)
	_ = sink.Rows(0, [][]executor.Value{{int64(10), int64(2), int64(3), 1.4}})
	return executor.Summary{Outcome: executor.OutcomeSucceeded}
}

func (r *probeRunner) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

const probeSQL = "SELECT * FROM a JOIN b ON b.id = a.id"

func probeFinding(columns ...string) diagnostics.Finding {
	var ms []diagnostics.Measurement
	for _, c := range columns {
		ms = append(ms, diagnostics.Measurement{Kind: diagnostics.MeasureFanOut,
			Table: diagnostics.TableRef{Database: "Sales", Schema: "dbo", Name: "b"}, Columns: []string{c}})
	}
	return diagnostics.Finding{RuleID: "QRY-JOIN-003", Title: "fan-out", Explanation: "e", Severity: diagnostics.SeverityMedium,
		Classification: diagnostics.Potential, Span: diagnostics.Span{Offset: 14, Length: 23, Line: 1, Column: 15}, Measurements: ms}
}

// probeCore is a connected core with an analysis whose findings need the given probe columns.
func probeCore(t *testing.T, runner *probeRunner, budget int, p parser.Parser, columns ...string) (*Core, string, []instrumentation.Probe) {
	t.Helper()
	c, err := New(Options{
		ConfigPath:     t.TempDir() + "/config.json",
		DisableFileLog: true,
		Credentials:    connection.NewMemoryStore(),
		Driver:         nopDriver{},
		Access:         noAccess{},
		Runner:         runner,
		Parser:         p,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	cfg := c.Config()
	cfg.Execution.MaxProbesPerRun = budget
	cfg.Execution.ProbeTimeoutSecs = 7
	if err := c.UpdateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	prof, err := c.Connections().Create(connectionProfile(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Pool().Connect(context.Background(), prof.ID); err != nil {
		t.Fatal(err)
	}
	findings := []diagnostics.Finding{probeFinding(columns...)}
	probes := c.planProbes(context.Background(), findings)
	c.analyses.add(&analysisRecord{id: "a1", target: AnalysisTarget{ConnectionID: prof.ID, Database: "Sales"}, sql: probeSQL,
		findings: findings, probes: probes})
	return c, prof.ID, probes
}

func TestPlanProbesAppliesTheBudgetAndTheAllowlist(t *testing.T) {
	_, _, probes := probeCore(t, &probeRunner{}, 1, parsertest.Fake{}, "x", "y")
	if len(probes) != 2 || probes[0].OverBudget || !probes[1].OverBudget {
		t.Fatalf("probes = %+v, want the second over a budget of 1", probes)
	}
	// A probe the allowlist refuses (here, a parser that sees a write) is dropped.
	writes := parsertest.Fake{Statements: func(string) []parser.Statement {
		return []parser.Statement{{Kind: parser.StmtInsert, Effects: []parser.Effect{{Kind: parser.EffectWrite,
			Object: &parser.ObjectName{Schema: "dbo", Name: "T"}}}}}
	}}
	_, _, probes = probeCore(t, &probeRunner{}, 5, writes, "x")
	if len(probes) != 0 {
		t.Fatalf("probes = %+v, want none", probes)
	}
}

func TestRunProbesRefusesWhatWasNotApproved(t *testing.T) {
	runner := &probeRunner{}
	c, _, _ := probeCore(t, runner, 1, parsertest.Fake{}, "x", "y")
	ctx := context.Background()
	if _, err := c.RunProbes(ctx, "nope", []string{"p1"}); !errors.Is(err, ErrAnalysisNotFound) {
		t.Errorf("unknown analysis: %v", err)
	}
	if _, err := c.RunProbes(ctx, "a1", nil); err == nil {
		t.Error("an empty approval must be refused")
	}
	if _, err := c.RunProbes(ctx, "a1", []string{"p9"}); err == nil || !strings.Contains(err.Error(), "not part of this analysis") {
		t.Errorf("unknown probe: %v", err)
	}
	if _, err := c.RunProbes(ctx, "a1", []string{"p1", "p2"}); err == nil || !strings.Contains(err.Error(), "over the probe budget") {
		t.Errorf("over budget: %v", err)
	}
	if runner.count() != 0 {
		t.Fatalf("probes ran without a valid approval: %d", runner.count())
	}
}

func TestRunProbesRunsUnderTheGuardAndConfirms(t *testing.T) {
	runner := &probeRunner{}
	c, _, probes := probeCore(t, runner, 5, parsertest.Fake{}, "x")
	run, err := c.RunProbes(context.Background(), "a1", []string{"p1", "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if runner.count() != 1 {
		t.Fatalf("runs = %d, want 1 (approving twice runs once)", runner.count())
	}
	req := runner.requests[0]
	if req.SQL != probes[0].SQL || !req.Policy.ReadOnly || req.Limits.Timeout != 7*time.Second || req.Limits.MaxRows != probeMaxRows {
		t.Fatalf("request = %+v", req)
	}
	if len(run.Results) != 1 || !run.Results[0].ShowsProblem || run.Results[0].Error != "" {
		t.Fatalf("results = %+v", run.Results)
	}
	if len(run.Findings) != 1 || run.Findings[0].Classification != diagnostics.Confirmed || len(run.Dismissed) != 0 {
		t.Fatalf("findings = %+v, dismissed = %+v", run.Findings, run.Dismissed)
	}
	if rec := c.analyses.get("a1"); len(rec.runs) != 1 {
		t.Fatalf("the run is not kept for the session: %d", len(rec.runs))
	}
}

func TestRunProbesBoundsParallelismAndCancels(t *testing.T) {
	runner := &probeRunner{delay: 50 * time.Millisecond}
	c, _, _ := probeCore(t, runner, 5, parsertest.Fake{}, "a", "b", "c", "d", "e")
	if _, err := c.RunProbes(context.Background(), "a1", []string{"p1", "p2", "p3", "p4", "p5"}); err != nil {
		t.Fatal(err)
	}
	if peak := runner.peak.Load(); peak != probeParallelism {
		t.Fatalf("peak parallelism = %d, want %d", peak, probeParallelism)
	}

	slow := &probeRunner{delay: time.Minute}
	c2, _, _ := probeCore(t, slow, 5, parsertest.Fake{}, "a", "b", "c")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	began := time.Now()
	run, err := c2.RunProbes(ctx, "a1", []string{"p1", "p2", "p3"})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(began) > 5*time.Second {
		t.Fatal("cancelling did not stop the probes")
	}
	for _, r := range run.Results {
		if r.Error == "" {
			t.Fatalf("a cancelled probe has a result: %+v", r)
		}
	}
	if run.Findings[0].Classification != diagnostics.Potential {
		t.Fatal("cancelled probes must not change findings")
	}
}

func TestRunProbesNeedsTheConnection(t *testing.T) {
	c, id, _ := probeCore(t, &probeRunner{}, 5, parsertest.Fake{}, "x")
	if err := c.Pool().Disconnect(id); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RunProbes(context.Background(), "a1", []string{"p1"}); err == nil {
		t.Fatal("probes ran without a connection")
	}
}

func connectionProfile() connection.Profile {
	return connection.Profile{Name: "x", Server: "db", Auth: connection.AuthWindows}
}
