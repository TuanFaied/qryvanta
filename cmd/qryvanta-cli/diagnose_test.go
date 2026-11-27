package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/instrumentation"
)

func diagnoseCmd(t *testing.T, ctx context.Context, opts app.Options, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := cmdDiagnose(ctx, args, opts, &out, &errOut)
	code = report(err, &bytes.Buffer{}, &errOut)
	return out.String(), errOut.String(), code
}

func terminal(t *testing.T, is bool) {
	t.Helper()
	old := stdinIsTerminal
	stdinIsTerminal = func() bool { return is }
	t.Cleanup(func() { stdinIsTerminal = old })
}

func TestDiagnoseFlags(t *testing.T) {
	terminal(t, false)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--format", "csv", "--query", "SELECT 1"}, "--format must be text or json"},
		{[]string{"--probes", "some", "--query", "SELECT 1"}, "--probes must be none, ask or all"},
		{[]string{"--fail-on", "severe", "--query", "SELECT 1"}, "--fail-on must be"},
		{[]string{"--probes", "ask"}, "the SQL must come from --file or --query"},
		{[]string{"--file", "a.sql", "--query", "SELECT 1"}, "either --file or --query"},
		{[]string{"--timeout", "-1", "--query", "SELECT 1"}, "--timeout must not be negative"},
		{[]string{"--query", "SELECT 1", "extra"}, "unexpected argument"},
		{[]string{"--plan", "both", "--query", "SELECT 1"}, "--plan must be estimated, actual or none"},
		{[]string{"--plan", "actual", "--query", "SELECT 1"}, "--plan actual needs --conn"},
	} {
		_, stderr, code := diagnoseCmd(t, context.Background(), app.Options{}, c.args...)
		if code != 2 || !strings.Contains(stderr, c.want) {
			t.Errorf("%v: exit %d, stderr %q; want 2 and %q", c.args, code, stderr, c.want)
		}
	}
}

func TestDiagnosePlanModeDefaults(t *testing.T) {
	terminal(t, false)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--query", "SELECT 1"}, "none"},
		{[]string{"--query", "SELECT 1", "--conn", "x"}, "estimated"},
		{[]string{"--query", "SELECT 1", "--conn", "x", "--plan", "actual"}, "actual"},
		{[]string{"--query", "SELECT 1", "--conn", "x", "--plan", "none"}, "none"},
		{[]string{"--query", "SELECT 1", "--plan", "none"}, "none"},
	} {
		if o, err := parseDiagnoseFlags(c.args); err != nil || o.plan != c.want {
			t.Errorf("%v: plan = %q (%v), want %q", c.args, o.plan, err, c.want)
		}
	}
}

func TestDiagnoseProbeModeDefaults(t *testing.T) {
	terminal(t, true)
	if o, _ := parseDiagnoseFlags([]string{"--query", "SELECT 1"}); o.probes != "ask" {
		t.Errorf("terminal: probes = %s, want ask", o.probes)
	}
	if o, _ := parseDiagnoseFlags([]string{"--query", "SELECT 1", "--format", "json"}); o.probes != "none" {
		t.Errorf("JSON output: probes = %s, want none", o.probes)
	}
	if o, _ := parseDiagnoseFlags(nil); o.probes != "none" {
		t.Errorf("SQL from stdin: probes = %s, want none", o.probes)
	}
	terminal(t, false)
	if o, _ := parseDiagnoseFlags([]string{"--query", "SELECT 1"}); o.probes != "none" {
		t.Errorf("no terminal: probes = %s, want none", o.probes)
	}
}

func TestDiagnoseStaticOnlyJSON(t *testing.T) {
	terminal(t, false)
	opts, _, _ := testOpts(t)
	stdout, _, code := diagnoseCmd(t, context.Background(), opts, "--query", "SELECT 1", "--format", "json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var res diagnoseResult
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}
	if res.Version != 1 || res.Outcome != "completed" || len(res.Steps) != 6 || res.Findings == nil || res.Probes == nil ||
		res.Steps[2].Status != app.StepSkipped || res.Connection != "" {
		t.Fatalf("result = %+v", res)
	}
}

func TestDiagnoseCancelledExitsWith3(t *testing.T) {
	terminal(t, false)
	opts, _, _ := testOpts(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stdout, _, code := diagnoseCmd(t, ctx, opts, "--query", "SELECT 1", "--format", "json")
	if code != exitStopped {
		t.Fatalf("exit %d, want %d", code, exitStopped)
	}
	if !strings.Contains(stdout, `"outcome": "cancelled"`) {
		t.Fatalf("stdout = %s", stdout)
	}
}

func TestDiagnoseUnknownConnection(t *testing.T) {
	terminal(t, false)
	opts, _, _ := testOpts(t)
	_, stderr, code := diagnoseCmd(t, context.Background(), opts, "--query", "SELECT 1", "--conn", "nope")
	if code == 0 || !strings.Contains(stderr, "nope") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
}

func TestAskForProbes(t *testing.T) {
	probes := []instrumentation.Probe{
		{ID: "p1", Title: "Rows per OrderID", Proves: "fan-out", SQL: "SELECT 1\nFROM [S].[dbo].[T];",
			Params: []instrumentation.Param{{Name: "p1", Value: "2026-01-31"}}},
		{ID: "p2", Title: "Over", Proves: "x", SQL: "SELECT 2;", OverBudget: true},
	}
	var out bytes.Buffer
	ids, err := askForProbes(probes, strings.NewReader("y\n"), &out)
	if err != nil || len(ids) != 1 || ids[0] != "p1" {
		t.Fatalf("ids = %v, %v", ids, err)
	}
	for _, want := range []string{"p1. Rows per OrderID", "FROM [S].[dbo].[T];", "@p1 = '2026-01-31'", "[over budget: will not run]", "Run 1 probe(s)? [y/N]"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("prompt does not show %q:\n%s", want, out.String())
		}
	}
	for _, answer := range []string{"n\n", "\n", ""} {
		if ids, _ := askForProbes(probes, strings.NewReader(answer), &bytes.Buffer{}); len(ids) != 0 {
			t.Errorf("answer %q approved %v", answer, ids)
		}
	}
}

func TestFailOnThreshold(t *testing.T) {
	fs := []diagnostics.Finding{{Severity: diagnostics.SeverityMedium}, {Severity: diagnostics.SeverityLow}}
	for sev, want := range map[diagnostics.Severity]int{
		diagnostics.SeverityHigh: 0, diagnostics.SeverityMedium: 1, diagnostics.SeverityInformational: 2,
	} {
		if got := countAtOrAbove(fs, sev); got != want {
			t.Errorf("at or above %s = %d, want %d", sev, got, want)
		}
	}
}

func TestWriteDiagnosisText(t *testing.T) {
	var out bytes.Buffer
	writeDiagnosis(&out, diagnoseResult{
		Connection: "Fixture", Database: "Sales",
		Findings: []diagnostics.Finding{{RuleID: "QRY-NULL-001", Title: `"a = NULL" is never true`, Explanation: "Line one.\nLine two.",
			Severity: diagnostics.SeverityHigh, Classification: diagnostics.Confirmed, Span: diagnostics.Span{Line: 3, Column: 7},
			Evidence: []diagnostics.Evidence{{Kind: diagnostics.EvidenceStatic, Summary: "Compares with NULL."},
				{Kind: diagnostics.EvidencePlan, Summary: "No join predicate.", Plan: &diagnostics.PlanEvidence{NodeID: 3, Operator: "Nested Loops", Property: "warning", Value: "No Join Predicate", Actual: true}}},
			Correction: &diagnostics.Correction{Description: "Use IS NULL.", Replacement: "a IS NULL"}}},
		Dismissed: []instrumentation.Dismissal{{Finding: diagnostics.Finding{Title: "fan-out"}, Reason: "unique today"}},
		Metadata:  app.MetadataStatus{Unresolved: []app.UnresolvedTable{{Name: "dbo.Missing", Reason: "not found"}}},
	})
	for _, want := range []string{
		"1 finding(s) on Fixture (Sales):",
		"HIGH · Confirmed issue · QRY-NULL-001 · line 3, column 7",
		`"a = NULL" is never true`, "  Line two.", "- static evidence: Compares with NULL.",
		"- plan evidence (actual, node 3 Nested Loops): No join predicate.",
		"Suggested correction (not applied): Use IS NULL.", "a IS NULL",
		"Dismissed by probes:", "fan-out: unique today", "dbo.Missing: not found",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output does not contain %q:\n%s", want, out.String())
		}
	}
}
