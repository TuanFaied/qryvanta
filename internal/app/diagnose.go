package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/TuanFaied/qryvanta/internal/analyzer"
	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/executor"
	"github.com/TuanFaied/qryvanta/internal/instrumentation"
	"github.com/TuanFaied/qryvanta/internal/parser"
	"github.com/TuanFaied/qryvanta/internal/plan"
	"github.com/TuanFaied/qryvanta/internal/security"
)

// The debugging flow (architecture section 6): the desktop app's "Execute with debugging" and
// "qryvanta-cli diagnose" run the same steps here.

// Step is one step of a diagnosis, in order.
type Step string

const (
	StepParse     Step = "parse"     // parse with the QueryModel, and check the read-only policy
	StepStatic    Step = "static"    // rules that need only the query
	StepMetadata  Step = "metadata"  // rules that read the catalog, and upgrades of static findings
	StepPlan      Step = "plan"      // capture or take the execution plan; plan rules and confirmations
	StepProbes    Step = "probes"    // propose probes, and ask for approval
	StepRunProbes Step = "runProbes" // run the approved probes and update the findings
)

// Steps lists the steps in the order they run.
var Steps = []Step{StepParse, StepStatic, StepMetadata, StepPlan, StepProbes, StepRunProbes}

// StepStatus is where a step is.
type StepStatus string

const (
	StepRunning StepStatus = "running"
	StepDone    StepStatus = "done"
	// StepSkipped: the step did not apply (no connection, nothing to measure, not approved,
	// cancelled before it started); Reason says why.
	StepSkipped StepStatus = "skipped"
	StepFailed  StepStatus = "failed"
	// StepWaiting: probes were proposed and wait for approval through RunProbes.
	StepWaiting StepStatus = "waiting"
)

// StepEvent reports a step's status. Findings is the list after the step, when it changed them.
type StepEvent struct {
	Step     Step                  `json:"step"`
	Status   StepStatus            `json:"status"`
	Reason   string                `json:"reason"`
	Findings []diagnostics.Finding `json:"findings,omitempty"`
}

// DiagnoseOptions control a diagnosis.
type DiagnoseOptions struct {
	// Target is the connection and database for metadata and probes; the zero value runs the
	// static rules only.
	Target AnalysisTarget
	// Approve chooses which proposed probes run (by ID). Nil stops after proposing them: the
	// caller approves later with RunProbes (the desktop app does, from the analysis panel).
	Approve func(ctx context.Context, probes []instrumentation.Probe) ([]string, error)
	// Emit receives every step event as it happens. It may be nil.
	Emit func(StepEvent)
	// Plan captures an execution plan of sql through Execute (under the execution policy and
	// limits): PlanEstimated compiles it without running it, PlanActual runs it. PlanNone, or no
	// connection in Target, skips plans. Ignored when Plans is set.
	Plan executor.PlanMode
	// Plans, when set, supplies plans already captured for exactly sql (the desktop app's run
	// with "Include actual plan"), and may wait for them. When it returns none, Plan applies.
	Plans func(ctx context.Context) []executor.Plan
}

// Diagnosis is the result of the debugging flow.
type Diagnosis struct {
	Analysis
	// Refusals are the statements read-only mode would refuse to run.
	Refusals []executor.Refusal `json:"refusals"`
	// Steps is the final status of every step, in order.
	Steps []StepEvent `json:"steps"`
	// ProbeRun is set when approved probes ran.
	ProbeRun *ProbeRun `json:"probeRun,omitempty"`
	// PlanMode is the kind of plan the findings used, empty without one.
	PlanMode executor.PlanMode `json:"planMode,omitempty"`
}

// Diagnose runs the debugging flow over sql: parse and check the policy, the static rules, the
// metadata rules, the plan rules, propose probes, and run the approved ones. It executes nothing
// but approved probes and, with opts.Plan set to PlanActual, the query itself for its plan. Cancelling ctx stops it between steps (and running probes); the remaining steps are
// reported as skipped and ctx's error is returned with what was found so far. A parser that cannot
// run is an error (wrapping parser.ErrUnavailable).
func (c *Core) Diagnose(ctx context.Context, sql string, opts DiagnoseOptions) (Diagnosis, error) {
	d := Diagnosis{
		Analysis: Analysis{ID: newID(), Probes: []instrumentation.Probe{},
			Report:   analyzer.Report{Findings: []diagnostics.Finding{}, Failures: []analyzer.RuleFailure{}},
			Metadata: MetadataStatus{Unresolved: []UnresolvedTable{}}},
		Refusals: []executor.Refusal{},
		Steps:    []StepEvent{},
	}
	emit := func(e StepEvent) {
		if e.Status != StepRunning {
			d.Steps = append(d.Steps, StepEvent{Step: e.Step, Status: e.Status, Reason: e.Reason})
		}
		if opts.Emit != nil {
			opts.Emit(e)
		}
	}
	// stop reports the steps from `from` on as skipped (cancelled) and returns ctx's error.
	stop := func(from Step) (Diagnosis, error) {
		started := false
		for _, s := range Steps {
			if s == from {
				started = true
			}
			if started {
				emit(StepEvent{Step: s, Status: StepSkipped, Reason: "Cancelled."})
			}
		}
		return d, ctx.Err()
	}

	// 1. Parse, and check the read-only policy.
	emit(StepEvent{Step: StepParse, Status: StepRunning})
	res, err := c.parser.Parse(ctx, sql, parser.Options{Model: true})
	if err != nil {
		if ctx.Err() != nil {
			return stop(StepParse)
		}
		emit(StepEvent{Step: StepParse, Status: StepFailed, Reason: err.Error()})
		for _, s := range Steps[1:] {
			emit(StepEvent{Step: s, Status: StepSkipped, Reason: "The query could not be parsed."})
		}
		return d, err
	}
	d.SyntaxErrors = len(res.Diagnostics)
	for _, b := range res.Batches {
		d.Statements += len(b.Statements)
	}
	if c.ExecutionPolicy().ReadOnly {
		d.Refusals = security.ReadOnlyRefusals(res)
	}
	emit(StepEvent{Step: StepParse, Status: StepDone, Reason: parseSummary(len(res.Batches), d.Statements, d.SyntaxErrors, len(d.Refusals))})

	// 2. Static rules.
	if ctx.Err() != nil {
		return stop(StepStatic)
	}
	emit(StepEvent{Step: StepStatic, Status: StepRunning})
	engine := analyzer.NewEngine(analyzer.Default())
	rep, err := engine.Analyze(ctx, &analyzer.Input{SQL: sql, Result: res})
	if err != nil {
		return stop(StepStatic)
	}
	d.Report = rep
	emit(StepEvent{Step: StepStatic, Status: StepDone, Reason: countFindings(len(rep.Findings)), Findings: rep.Findings})

	// 3. Metadata rules.
	if ctx.Err() != nil {
		return stop(StepMetadata)
	}
	emit(StepEvent{Step: StepMetadata, Status: StepRunning})
	var cat *analyzer.Catalog
	cat, d.Metadata = c.catalog(ctx, res, opts.Target)
	if !d.Metadata.Checked {
		emit(StepEvent{Step: StepMetadata, Status: StepSkipped, Reason: d.Metadata.Reason})
	} else {
		rep, err = engine.Analyze(ctx, &analyzer.Input{SQL: sql, Result: res, Catalog: cat})
		if err != nil {
			return stop(StepMetadata)
		}
		d.Report = rep
		reason := countFindings(len(rep.Findings))
		if n := len(d.Metadata.Unresolved); n > 0 {
			reason += fmt.Sprintf("; %d table(s) could not be checked", n)
		}
		emit(StepEvent{Step: StepMetadata, Status: StepDone, Reason: reason, Findings: rep.Findings})
	}
	// 4. Plans.
	if ctx.Err() != nil {
		return stop(StepPlan)
	}
	ev, ok := c.diagnosePlan(ctx, sql, res, cat, engine, opts, &d)
	if !ok {
		return stop(StepPlan)
	}
	emit(ev)
	for _, f := range d.Failures {
		c.log.Warn("a diagnostic rule failed", "rule", f.RuleID, "error", f.Message)
	}

	// 5. Propose probes.
	if ctx.Err() != nil {
		return stop(StepProbes)
	}
	rec := &analysisRecord{id: d.ID, target: opts.Target, sql: sql, findings: d.Findings}
	defer func() {
		c.log.Debug("query diagnosed", "statements", d.Statements, "findings", len(d.Findings), "probes", len(d.Probes))
	}()
	if !d.Metadata.Checked {
		c.analyses.add(rec)
		emit(StepEvent{Step: StepProbes, Status: StepSkipped, Reason: "Probes need a connection."})
		emit(StepEvent{Step: StepRunProbes, Status: StepSkipped, Reason: "No probes were proposed."})
		return d, nil
	}
	emit(StepEvent{Step: StepProbes, Status: StepRunning})
	d.Probes = c.planProbes(ctx, d.Findings)
	rec.probes = d.Probes
	c.analyses.add(rec)
	runnable := 0
	for _, p := range d.Probes {
		if !p.OverBudget {
			runnable++
		}
	}
	switch {
	case len(d.Probes) == 0:
		emit(StepEvent{Step: StepProbes, Status: StepSkipped, Reason: "No finding can be measured with a probe."})
		emit(StepEvent{Step: StepRunProbes, Status: StepSkipped, Reason: "No probes were proposed."})
		return d, nil
	case runnable == 0:
		emit(StepEvent{Step: StepProbes, Status: StepDone, Reason: fmt.Sprintf("%d probe(s) proposed, all over the probe budget.", len(d.Probes))})
		emit(StepEvent{Step: StepRunProbes, Status: StepSkipped, Reason: "Every probe is over the probe budget (Settings)."})
		return d, nil
	case opts.Approve == nil:
		emit(StepEvent{Step: StepProbes, Status: StepWaiting, Reason: fmt.Sprintf("%d probe(s) proposed; approve them to run.", runnable)})
		return d, nil
	}
	emit(StepEvent{Step: StepProbes, Status: StepDone, Reason: fmt.Sprintf("%d probe(s) proposed.", runnable)})

	// 6. Run the approved probes.
	ids, err := opts.Approve(ctx, d.Probes)
	if err != nil || ctx.Err() != nil {
		if ctx.Err() != nil {
			return stop(StepRunProbes)
		}
		emit(StepEvent{Step: StepRunProbes, Status: StepFailed, Reason: err.Error()})
		return d, nil
	}
	if len(ids) == 0 {
		emit(StepEvent{Step: StepRunProbes, Status: StepSkipped, Reason: "No probes were approved."})
		return d, nil
	}
	emit(StepEvent{Step: StepRunProbes, Status: StepRunning})
	run, err := c.RunProbes(ctx, d.ID, ids)
	if err != nil {
		emit(StepEvent{Step: StepRunProbes, Status: StepFailed, Reason: err.Error()})
		return d, nil
	}
	d.ProbeRun = &run
	d.Findings = run.Findings
	if ctx.Err() != nil {
		emit(StepEvent{Step: StepRunProbes, Status: StepSkipped, Reason: "Cancelled; findings were not changed by unfinished probes.", Findings: run.Findings})
		return d, ctx.Err()
	}
	failed := 0
	for _, r := range run.Results {
		if r.Error != "" {
			failed++
		}
	}
	reason := fmt.Sprintf("%d probe(s) ran; %d finding(s) dismissed", len(run.Results), len(run.Dismissed))
	if failed > 0 {
		reason += fmt.Sprintf("; %d failed", failed)
	}
	emit(StepEvent{Step: StepRunProbes, Status: StepDone, Reason: reason + ".", Findings: run.Findings})
	return d, nil
}

// diagnosePlan is the plan step: it takes or captures the plans, re-runs the rules with them and
// applies the plans' confirmations to d. It returns the step's final event, or false when ctx was
// cancelled during it.
func (c *Core) diagnosePlan(ctx context.Context, sql string, res *parser.Result, cat *analyzer.Catalog,
	engine *analyzer.Engine, opts DiagnoseOptions, d *Diagnosis) (StepEvent, bool) {
	skip := func(reason string) (StepEvent, bool) {
		return StepEvent{Step: StepPlan, Status: StepSkipped, Reason: reason}, true
	}
	fail := func(reason string) (StepEvent, bool) {
		return StepEvent{Step: StepPlan, Status: StepFailed, Reason: reason}, true
	}
	running := func(reason string) {
		if opts.Emit != nil {
			opts.Emit(StepEvent{Step: StepPlan, Status: StepRunning, Reason: reason})
		}
	}
	var docs []executor.Plan
	mode := opts.Plan
	note := ""
	if opts.Plans != nil {
		running("Waiting for the run's plan.")
		docs = opts.Plans(ctx)
		if ctx.Err() != nil {
			return StepEvent{}, false
		}
		if len(docs) > 0 {
			mode = docs[0].Mode
		} else {
			note = "The run gave no plan for this SQL. "
		}
	}
	if len(docs) == 0 {
		switch {
		case mode == executor.PlanNone:
			return skip(note + "No plan was requested.")
		case opts.Target.ConnectionID == "":
			return skip(note + "Plans need a connection.")
		case d.SyntaxErrors > 0:
			return skip(note + "The script has syntax errors.")
		}
		running("Capturing the " + string(mode) + " plan.")
		sink := &planCollector{}
		sum := c.Execute(ctx, opts.Target.ConnectionID, executor.Request{SQL: sql, Database: opts.Target.Database,
			Limits: c.ExecutionLimits(), Plan: mode}, sink)
		if ctx.Err() != nil {
			return StepEvent{}, false
		}
		docs = sink.plans
		if len(docs) == 0 {
			reason := "The server returned no plan."
			if sum.Error != "" {
				reason = "The plan could not be captured: " + sum.Error
			}
			return fail(note + reason)
		}
	}
	in := analyzer.Input{SQL: sql, Result: res, Catalog: cat}
	broken := 0
	for _, doc := range docs {
		if doc.Execution > 1 {
			continue // GO n: the first execution's plan stands for the batch
		}
		p, err := plan.Parse(strings.NewReader(doc.XML), 0)
		if err != nil {
			broken++
			continue
		}
		in.Plans = append(in.Plans, analyzer.PlanDoc{Batch: doc.Batch, Plan: p})
	}
	if len(in.Plans) == 0 {
		return fail(fmt.Sprintf("None of the %d plan(s) could be read.", len(docs)))
	}
	in.LinkPlans()
	rep, err := engine.Analyze(ctx, &in)
	if err != nil {
		return StepEvent{}, false
	}
	rep.Findings = analyzer.ConfirmWithPlans(&in, rep.Findings)
	d.Report = rep
	d.PlanMode = mode
	label := "Estimated"
	if mode == executor.PlanActual {
		label = "Actual"
	}
	reason := fmt.Sprintf("%s%s plan, %d document(s): %s", note, label, len(in.Plans), countFindings(len(rep.Findings)))
	if broken > 0 {
		reason = strings.TrimSuffix(reason, ".") + fmt.Sprintf("; %d plan(s) could not be read.", broken)
	}
	return StepEvent{Step: StepPlan, Status: StepDone, Reason: reason, Findings: rep.Findings}, true
}

// planCollector keeps the plans of a run and drops its rows.
type planCollector struct{ plans []executor.Plan }

func (*planCollector) ResultSet(int, []executor.Column) error { return nil }
func (*planCollector) Rows(int, [][]executor.Value) error     { return nil }
func (*planCollector) Message(executor.Message) error         { return nil }
func (s *planCollector) Plan(p executor.Plan) error {
	s.plans = append(s.plans, p)
	return nil
}

func countFindings(n int) string {
	if n == 1 {
		return "1 finding."
	}
	return fmt.Sprintf("%d findings.", n)
}

func parseSummary(batches, statements, syntaxErrors, refusals int) string {
	s := fmt.Sprintf("%d batch(es), %d statement(s)", batches, statements)
	if syntaxErrors > 0 {
		s += fmt.Sprintf("; %d syntax error(s): those batches are not analyzed", syntaxErrors)
	}
	if refusals > 0 {
		s += fmt.Sprintf("; read-only mode would refuse %d statement(s)", refusals)
	}
	return s + "."
}

// IsStopped reports whether err ended a diagnosis early (cancelled or timed out).
func IsStopped(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
