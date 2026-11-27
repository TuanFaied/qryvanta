package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/executor"
	"github.com/TuanFaied/qryvanta/internal/instrumentation"
)

// Exit codes of "diagnose", documented in the usage text.
const (
	exitFindings      = 1 // findings at or above --fail-on
	exitNotDiagnosed  = 4 // the diagnosis could not run (parser unavailable, connection failed)
	diagnoseJSONModel = 1 // version of the JSON document
)

// stdinIsTerminal reports whether stdin is an interactive terminal. Tests replace it.
var stdinIsTerminal = func() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

type diagnoseOptions struct {
	conn, database, file, query, format, probes, failOn, plan string
	timeout                                                   int
}

func parseDiagnoseFlags(args []string) (diagnoseOptions, error) {
	var o diagnoseOptions
	fs := flag.NewFlagSet("diagnose", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.conn, "conn", "", "connection name or ID (without it, only the static rules run)")
	fs.StringVar(&o.database, "database", "", "database (default: the connection's)")
	fs.StringVar(&o.file, "file", "", "SQL file")
	fs.StringVar(&o.query, "query", "", "SQL text")
	fs.StringVar(&o.format, "format", "text", "text or json")
	fs.StringVar(&o.probes, "probes", "", "none, ask or all (default: ask on a terminal, none otherwise)")
	fs.StringVar(&o.plan, "plan", "", "estimated, actual or none (default: estimated with --conn, none otherwise)")
	fs.StringVar(&o.failOn, "fail-on", "high", "lowest severity that makes the exit code 1")
	fs.IntVar(&o.timeout, "timeout", 0, "stop the diagnosis after this many seconds (0: no limit)")
	if err := fs.Parse(args); err != nil {
		return o, fmt.Errorf("%w: %v", errUsage, err)
	}
	if fs.NArg() > 0 {
		return o, fmt.Errorf("%w: unexpected argument %q", errUsage, fs.Arg(0))
	}
	if o.file != "" && o.query != "" {
		return o, fmt.Errorf("%w: use either --file or --query", errUsage)
	}
	if o.format != "text" && o.format != "json" {
		return o, fmt.Errorf("%w: --format must be text or json", errUsage)
	}
	if !diagnostics.Severity(o.failOn).Valid() {
		return o, fmt.Errorf("%w: --fail-on must be critical, high, medium, low or informational", errUsage)
	}
	if o.timeout < 0 {
		return o, fmt.Errorf("%w: --timeout must not be negative", errUsage)
	}
	switch o.plan {
	case "":
		o.plan = "none"
		if o.conn != "" {
			o.plan = "estimated"
		}
	case "none":
	case "estimated", "actual":
		if o.conn == "" {
			return o, fmt.Errorf("%w: --plan %s needs --conn", errUsage, o.plan)
		}
	default:
		return o, fmt.Errorf("%w: --plan must be estimated, actual or none", errUsage)
	}
	sqlFromStdin := o.file == "" && o.query == ""
	switch o.probes {
	case "":
		o.probes = "none"
		if !sqlFromStdin && o.format == "text" && stdinIsTerminal() {
			o.probes = "ask"
		}
	case "none", "all":
	case "ask":
		if sqlFromStdin {
			return o, fmt.Errorf("%w: --probes ask reads the answer from stdin, so the SQL must come from --file or --query", errUsage)
		}
	default:
		return o, fmt.Errorf("%w: --probes must be none, ask or all", errUsage)
	}
	return o, nil
}

// diagnoseResult is the JSON output of "diagnose". Findings use the finding schema of the
// diagnostic engine (diagnostics.Finding), which session reports use as well (Phase 8).
type diagnoseResult struct {
	Version      int                         `json:"version"`
	Connection   string                      `json:"connection,omitempty"`
	Database     string                      `json:"database,omitempty"`
	Outcome      string                      `json:"outcome"`
	Plan         executor.PlanMode           `json:"plan,omitempty"`
	Steps        []app.StepEvent             `json:"steps"`
	Statements   int                         `json:"statements"`
	SyntaxErrors int                         `json:"syntaxErrors"`
	Refusals     any                         `json:"refusals"`
	Metadata     app.MetadataStatus          `json:"metadata"`
	Findings     []diagnostics.Finding       `json:"findings"`
	Dismissed    []instrumentation.Dismissal `json:"dismissed"`
	Probes       []probeOutput               `json:"probes"`
	RuleFailures any                         `json:"ruleFailures"`
}

type probeOutput struct {
	instrumentation.Probe
	Result *instrumentation.Result `json:"result,omitempty"`
}

func cmdDiagnose(ctx context.Context, args []string, opts app.Options, stdout, stderr io.Writer) error {
	o, err := parseDiagnoseFlags(args)
	if err != nil {
		return err
	}
	sql, err := readBatch(runOptions{file: o.file, query: o.query})
	if err != nil {
		return err
	}
	if o.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(o.timeout)*time.Second)
		defer cancel()
	}

	core, err := app.New(opts)
	if err != nil {
		return err
	}
	defer core.Close()

	res := diagnoseResult{Version: diagnoseJSONModel}
	var target app.AnalysisTarget
	if o.conn != "" {
		prof, err := resolveProfile(core.Connections(), o.conn)
		if err != nil {
			return err
		}
		server, err := core.Pool().Connect(ctx, prof.ID)
		if err != nil {
			return &exitError{code: exitNotDiagnosed, err: fmt.Errorf("connection %q failed: %w", prof.Name, err)}
		}
		target = app.AnalysisTarget{ConnectionID: prof.ID, Database: o.database}
		if target.Database == "" {
			target.Database = server.Database
		}
		res.Connection, res.Database = prof.Name, target.Database
	} else if o.format == "text" {
		fmt.Fprintln(stderr, "qryvanta-cli: no --conn: only the static rules run (no metadata, plans or probes).")
	}

	text := o.format == "text"
	dopts := app.DiagnoseOptions{Target: target, Emit: func(e app.StepEvent) {
		if text && e.Status != app.StepRunning {
			fmt.Fprintf(stderr, "  %-10s %-8s %s\n", e.Step, e.Status, e.Reason)
		}
	}}
	if o.plan != "none" {
		dopts.Plan = executor.PlanMode(o.plan)
	}
	switch o.probes {
	case "all":
		dopts.Approve = func(_ context.Context, probes []instrumentation.Probe) ([]string, error) {
			return runnableIDs(probes), nil
		}
	case "ask":
		dopts.Approve = func(_ context.Context, probes []instrumentation.Probe) ([]string, error) {
			return askForProbes(probes, stdin, stderr)
		}
	}

	d, err := core.Diagnose(ctx, sql, dopts)
	switch {
	case err == nil:
		res.Outcome = "completed"
	case app.IsStopped(err):
		res.Outcome = "cancelled"
		if errors.Is(err, context.DeadlineExceeded) {
			res.Outcome = "timedOut"
		}
	default:
		return &exitError{code: exitNotDiagnosed, err: err}
	}
	res.Plan = d.PlanMode
	res.Steps, res.Statements, res.SyntaxErrors = d.Steps, d.Statements, d.SyntaxErrors
	res.Refusals, res.Metadata, res.Findings, res.RuleFailures = d.Refusals, d.Metadata, d.Findings, d.Failures
	res.Dismissed = []instrumentation.Dismissal{}
	results := map[string]instrumentation.Result{}
	if d.ProbeRun != nil {
		res.Dismissed = d.ProbeRun.Dismissed
		for _, r := range d.ProbeRun.Results {
			results[r.ProbeID] = r
		}
	}
	res.Probes = []probeOutput{}
	for _, p := range d.Probes {
		po := probeOutput{Probe: p}
		if r, ok := results[p.ID]; ok {
			po.Result = &r
		}
		res.Probes = append(res.Probes, po)
	}

	if o.format == "json" {
		if err := writeJSON(stdout, res); err != nil {
			return err
		}
	} else {
		writeDiagnosis(stdout, res)
	}

	switch {
	case res.Outcome != "completed":
		return &exitError{code: exitStopped, err: fmt.Errorf("diagnosis %s", res.Outcome), silent: o.format == "json"}
	case countAtOrAbove(res.Findings, diagnostics.Severity(o.failOn)) > 0:
		return &exitError{code: exitFindings, err: fmt.Errorf("findings at or above %s", o.failOn), silent: true}
	}
	return nil
}

func runnableIDs(probes []instrumentation.Probe) []string {
	ids := []string{}
	for _, p := range probes {
		if !p.OverBudget {
			ids = append(ids, p.ID)
		}
	}
	return ids
}

// askForProbes lists the probes in full on stderr and reads y/N from in.
func askForProbes(probes []instrumentation.Probe, in io.Reader, out io.Writer) ([]string, error) {
	ids := runnableIDs(probes)
	fmt.Fprintln(out, "\nProbes (read-only queries that measure the findings):")
	for _, p := range probes {
		state := ""
		if p.OverBudget {
			state = " [over budget: will not run]"
		}
		fmt.Fprintf(out, "\n%s. %s%s\n   %s\n", p.ID, p.Title, state, p.Proves)
		for _, line := range strings.Split(p.SQL, "\n") {
			fmt.Fprintf(out, "     %s\n", line)
		}
		for _, prm := range p.Params {
			fmt.Fprintf(out, "     @%s = '%s'\n", prm.Name, prm.Value)
		}
	}
	if len(ids) == 0 {
		return ids, nil
	}
	fmt.Fprintf(out, "\nRun %d probe(s)? [y/N] ", len(ids))
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return ids, nil
	}
	return []string{}, nil
}

func countAtOrAbove(findings []diagnostics.Finding, s diagnostics.Severity) int {
	n := 0
	for _, f := range findings {
		if f.Severity.Rank() <= s.Rank() {
			n++
		}
	}
	return n
}

var classificationLabel = map[diagnostics.Classification]string{
	diagnostics.Confirmed:    "Confirmed issue",
	diagnostics.Potential:    "Potential issue",
	diagnostics.Optimization: "Optimization suggestion",
}

// writeDiagnosis prints the findings for people.
func writeDiagnosis(w io.Writer, r diagnoseResult) {
	where := ""
	if r.Connection != "" {
		where = fmt.Sprintf(" on %s (%s)", r.Connection, r.Database)
	}
	if len(r.Findings) == 0 {
		fmt.Fprintf(w, "No findings%s.\n", where)
	} else {
		fmt.Fprintf(w, "%d finding(s)%s:\n", len(r.Findings), where)
	}
	for _, f := range r.Findings {
		fmt.Fprintf(w, "\n%s · %s · %s · line %d, column %d\n  %s\n", strings.ToUpper(string(f.Severity)),
			classificationLabel[f.Classification], f.RuleID, f.Span.Line, f.Span.Column, f.Title)
		for _, line := range strings.Split(f.Explanation, "\n") {
			fmt.Fprintf(w, "  %s\n", line)
		}
		for _, e := range f.Evidence {
			if e.Plan != nil {
				kind := "estimated"
				if e.Plan.Actual {
					kind = "actual"
				}
				fmt.Fprintf(w, "  - plan evidence (%s, node %d %s): %s\n", kind, e.Plan.NodeID, e.Plan.Operator, e.Summary)
				continue
			}
			fmt.Fprintf(w, "  - %s evidence: %s\n", e.Kind, e.Summary)
		}
		if f.Correction != nil {
			fmt.Fprintf(w, "  Suggested correction (not applied): %s\n    %s\n", f.Correction.Description, f.Correction.Replacement)
		}
	}
	if len(r.Dismissed) > 0 {
		fmt.Fprintln(w, "\nDismissed by probes:")
		for _, d := range r.Dismissed {
			fmt.Fprintf(w, "  - %s: %s\n", d.Finding.Title, d.Reason)
		}
	}
	if len(r.Metadata.Unresolved) > 0 {
		fmt.Fprintln(w, "\nNot checked against metadata:")
		for _, u := range r.Metadata.Unresolved {
			fmt.Fprintf(w, "  - %s: %s\n", u.Name, u.Reason)
		}
	}
}
