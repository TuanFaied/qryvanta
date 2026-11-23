package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/executor"
	"github.com/TuanFaied/qryvanta/internal/instrumentation"
	"github.com/TuanFaied/qryvanta/internal/parser"
	"github.com/TuanFaied/qryvanta/internal/security"
)

// Probes (architecture decision D4): generated read-only queries that measure what a finding
// claims. They are planned with each analysis, kept here (the UI only sees them and names the
// ones the user approved), checked against the read-only allowlist, and run under the session
// guard with the probe timeout.

const (
	// probeParallelism is how many probes run at once.
	probeParallelism = 2
	// keptAnalyses is how many analyses keep their probe plans for approval.
	keptAnalyses = 16
	// probeMaxRows bounds a probe's result: each template returns one row.
	probeMaxRows = 10
)

// ErrAnalysisNotFound is returned for an analysis that is unknown or too old; analyze again.
var ErrAnalysisNotFound = errors.New("the analysis is no longer available; analyze again")

// analysisRecord is what an analysis keeps for its probes, for this session.
type analysisRecord struct {
	id       string
	target   AnalysisTarget
	sql      string
	findings []diagnostics.Finding
	probes   []instrumentation.Probe
	// runs are the probe runs of the analysis, kept for the session (Phase 8 stores them).
	runs []ProbeRun
}

// analysisStore keeps the most recent analyses.
type analysisStore struct {
	mu      sync.Mutex
	records []*analysisRecord
}

func (s *analysisStore) add(r *analysisRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, r)
	if len(s.records) > keptAnalyses {
		s.records = s.records[len(s.records)-keptAnalyses:]
	}
}

func (s *analysisStore) get(id string) *analysisRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.records {
		if r.id == id {
			return r
		}
	}
	return nil
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// planProbes generates the probes for an analysis's findings, keeps only those the read-only
// allowlist accepts (a template that fails it is a bug, logged and dropped), and marks the ones
// beyond the per-run budget.
func (c *Core) planProbes(ctx context.Context, findings []diagnostics.Finding) []instrumentation.Probe {
	planned := instrumentation.Plan(findings, math.MaxInt)
	out := make([]instrumentation.Probe, 0, len(planned))
	for _, p := range planned {
		if err := c.checkProbe(ctx, p); err != nil {
			c.log.Error("a generated probe failed the read-only check and was dropped", "probe_kind", p.Kind, "error", err)
			continue
		}
		out = append(out, p)
	}
	budget := c.Config().Execution.MaxProbesPerRun
	for i := range out {
		out[i].OverBudget = i >= budget
	}
	return out
}

// checkProbe parses a probe and applies the read-only allowlist (execution policy layer 1).
func (c *Core) checkProbe(ctx context.Context, p instrumentation.Probe) error {
	res, err := c.parser.Parse(ctx, p.SQL, parser.Options{Model: true})
	if err != nil {
		return err
	}
	if len(res.Diagnostics) > 0 {
		return fmt.Errorf("syntax error: %s", res.Diagnostics[0].Message)
	}
	if refusals := security.ReadOnlyRefusals(res); len(refusals) > 0 {
		return fmt.Errorf("refused: %s", refusals[0].Reason)
	}
	return nil
}

// ProbeRun is the result of running the approved probes of an analysis.
type ProbeRun struct {
	Results []instrumentation.Result `json:"results"`
	// Findings are the analysis's findings after the probes: confirmed ones carry probe evidence.
	Findings []diagnostics.Finding `json:"findings"`
	// Dismissed are findings the probes showed to be no problem, with the reason.
	Dismissed []instrumentation.Dismissal `json:"dismissed"`
}

// RunProbes runs the probes the user approved for an analysis: only probes that analysis
// planned, never over budget, at most probeParallelism at a time, each with the probe timeout
// under the read-only session guard. Cancelling ctx cancels them all. Probe failures are part of
// the results; the error is for a request that cannot run at all.
func (c *Core) RunProbes(ctx context.Context, analysisID string, probeIDs []string) (ProbeRun, error) {
	rec := c.analyses.get(analysisID)
	if rec == nil {
		return ProbeRun{}, ErrAnalysisNotFound
	}
	if len(probeIDs) == 0 {
		return ProbeRun{}, errors.New("no probes were approved")
	}
	byID := map[string]instrumentation.Probe{}
	for _, p := range rec.probes {
		byID[p.ID] = p
	}
	var approved []instrumentation.Probe
	seen := map[string]bool{}
	for _, id := range probeIDs {
		p, ok := byID[id]
		switch {
		case !ok:
			return ProbeRun{}, fmt.Errorf("probe %q is not part of this analysis", id)
		case p.OverBudget:
			return ProbeRun{}, fmt.Errorf("probe %s is over the probe budget (Settings: maximum probe queries per run)", id)
		case seen[id]:
			continue
		}
		seen[id] = true
		approved = append(approved, p)
	}
	db, err := c.pool.DB(rec.target.ConnectionID)
	if err != nil {
		return ProbeRun{}, err
	}

	cfg := c.Config().Execution
	limits := executor.Limits{Timeout: time.Duration(cfg.ProbeTimeoutSecs) * time.Second, MaxRows: probeMaxRows, MaxBytes: 1 << 20}
	policy := executor.Policy{ReadOnly: true, LockTimeout: time.Duration(cfg.LockTimeoutSeconds) * time.Second}
	results := make([]instrumentation.Result, len(approved))
	sem := make(chan struct{}, probeParallelism)
	var wg sync.WaitGroup
	for i, p := range approved {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[i] = instrumentation.Result{ProbeID: p.ID, Error: "cancelled"}
				return
			}
			results[i] = c.runProbe(ctx, db, p, limits, policy)
		}()
	}
	wg.Wait()

	findings, dismissed := instrumentation.Apply(rec.findings, rec.probes, results, rec.sql)
	run := ProbeRun{Results: results, Findings: findings, Dismissed: dismissed}
	c.analyses.mu.Lock()
	rec.runs = append(rec.runs, run)
	c.analyses.mu.Unlock()
	failed := 0
	for _, r := range results {
		if r.Error != "" {
			failed++
		}
	}
	c.log.Info("probes ran", "profile_id", rec.target.ConnectionID, "probes", len(results), "failed", failed,
		"dismissed_findings", len(dismissed))
	return run, nil
}

// runProbe runs one probe through the executor (the same runner, guard and limits as user
// queries, with the probe's parameters) and reads its row.
func (c *Core) runProbe(ctx context.Context, db *sql.DB, p instrumentation.Probe, limits executor.Limits, policy executor.Policy) instrumentation.Result {
	params := make([]executor.Param, len(p.Params))
	for i, prm := range p.Params {
		params[i] = executor.Param{Name: prm.Name, Value: prm.Value}
	}
	var col executor.Collector
	began := time.Now()
	sum := c.runner.Execute(ctx, db, executor.Request{SQL: p.SQL, Params: params, Limits: limits, Policy: policy}, &col)
	elapsed := time.Since(began).Milliseconds()
	fail := func(msg string) instrumentation.Result {
		return instrumentation.Result{ProbeID: p.ID, ElapsedMs: elapsed, Error: msg}
	}
	switch {
	case sum.Outcome != executor.OutcomeSucceeded:
		if sum.Error != "" {
			return fail(sum.Error)
		}
		return fail(string(sum.Outcome))
	case sum.ErrorCount > 0:
		for _, m := range sum.Messages {
			if m.Kind == "error" {
				return fail(m.Text)
			}
		}
		return fail("the probe reported an error")
	case len(col.Columns) == 0 || len(col.Data) == 0 || len(col.Data[0]) == 0:
		return fail("the probe returned no row")
	}
	names := make([]string, len(col.Columns[0]))
	for i, c := range col.Columns[0] {
		names[i] = c.Name
	}
	r, err := instrumentation.Interpret(p, names, col.Data[0][0])
	if err != nil {
		return fail(err.Error())
	}
	r.ElapsedMs = elapsed
	return r
}
