package analyzer

import (
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/parser"
	"github.com/TuanFaied/qryvanta/internal/plan"
)

// Plan rules over hand-built plans already linked to a one-statement script. The captured plans
// of the sample queries are covered in tests/plans (real parser).

const planSQL = "SELECT * FROM dbo.A AS a JOIN dbo.B AS b ON b.X = a.Y WHERE a.Z = 1"

func num(v float64) *float64 { return &v }

// clauseOf locates text in planSQL as a plan clause.
func clauseOf(t *testing.T, text string) *plan.Clause {
	t.Helper()
	i := strings.Index(planSQL, text)
	if i < 0 {
		t.Fatalf("%q is not in the SQL", text)
	}
	return &plan.Clause{Offset: i, Length: len(text), Line: 1, Column: i + 1}
}

func planInput(actual bool, root *plan.Operator, warnings ...plan.Warning) *Input {
	res := &parser.Result{Batches: []parser.Batch{{Statements: []parser.Statement{{
		Span: parser.Span{Offset: 0, Length: len(planSQL), Line: 1, Column: 1},
	}}}}}
	p := &plan.Plan{Actual: actual, Statements: []plan.Statement{{
		ID: 1, Text: planSQL, Root: root, Warnings: warnings,
		Script: &plan.ScriptLink{Batch: 0, Statement: 0, By: "text"},
	}}}
	return &Input{SQL: planSQL, Result: res, Plans: []PlanDoc{{Batch: 0, Plan: p}}}
}

func op(id int, physical string, est, act float64, children ...*plan.Operator) *plan.Operator {
	o := &plan.Operator{NodeID: id, PhysicalOp: physical, LogicalOp: physical, Estimated: plan.Estimates{Rows: num(est), Executions: num(1)}, Children: children}
	if act >= 0 {
		o.Actual = &plan.Actuals{Rows: num(act), Executions: num(1)}
	}
	return o
}

// check runs a rule as the engine would (RuleID and Severity from Info) and validates each finding.
func check(t *testing.T, r Rule, in *Input) []diagnostics.Finding {
	t.Helper()
	fs, err := r.Check(in)
	if err != nil {
		t.Fatal(err)
	}
	for i := range fs {
		fs[i].RuleID, fs[i].Severity = r.Info().ID, r.Info().Severity
		if err := diagnostics.Validate(fs[i], in.SQL); err != nil {
			t.Fatalf("%s: invalid finding: %v", fs[i].RuleID, err)
		}
		for _, e := range fs[i].Evidence {
			if e.Kind != diagnostics.EvidencePlan || e.Plan == nil {
				t.Fatalf("%s: evidence %+v is not plan evidence", fs[i].RuleID, e)
			}
		}
	}
	return fs
}

func TestEstimateGapReportsWhereTheGapStarts(t *testing.T) {
	// The scan under-estimates; the join above inherits it.
	scan := op(2, "Clustered Index Scan", 10, 5000)
	root := op(0, "Hash Match", 10, 5000, op(1, "Index Seek", 100, 100), scan)
	fs := check(t, estimateGap{}, planInput(true, root))
	if len(fs) != 1 || fs[0].Evidence[0].Plan.NodeID != 2 || fs[0].Classification != diagnostics.Optimization {
		t.Fatalf("findings = %+v, want one for node 2", fs)
	}
	if !strings.Contains(fs[0].Title, "10 rows estimated, 5000 actual") || !fs[0].Evidence[0].Plan.Actual {
		t.Fatalf("finding = %+v", fs[0])
	}

	// The join's own estimate is wrong: its inputs were right.
	root = op(0, "Nested Loops", 50, 9000, op(1, "Index Seek", 100, 100), op(2, "Index Seek", 90, 90))
	if fs := check(t, estimateGap{}, planInput(true, root)); len(fs) != 1 || fs[0].Evidence[0].Plan.NodeID != 0 {
		t.Fatalf("findings = %+v, want one for node 0", fs)
	}

	// Small differences are not reported: factor 9, or factor 10 but fewer than 100 rows apart.
	for _, r := range [][2]float64{{1, 9}, {10, 100}, {100, 950}} {
		if fs := check(t, estimateGap{}, planInput(true, op(0, "Table Scan", r[0], r[1]))); len(fs) != 0 {
			t.Fatalf("%v: findings = %+v", r, fs)
		}
	}
	if fs := check(t, estimateGap{}, planInput(true, op(0, "Table Scan", 1000, 10))); len(fs) != 1 || !strings.Contains(fs[0].Title, "over-estimated") {
		t.Fatalf("over-estimate: %+v", fs)
	}
}

func TestEstimatedPlansGiveNoMeasuredFindings(t *testing.T) {
	sort := op(1, "Sort", 10, -1, op(2, "Table Scan", 10, -1))
	sort.Warnings = []plan.Warning{{Kind: plan.WarningSpill, Text: "Operator used tempdb to spill data"}}
	in := planInput(false, op(0, "Hash Match", 10, -1, sort))
	for _, r := range []Rule{estimateGap{}, spill{}} {
		if fs := check(t, r, in); len(fs) != 0 {
			t.Fatalf("%s: findings on an estimated plan: %+v", r.Info().ID, fs)
		}
	}
	join003 := diagnostics.Finding{RuleID: "QRY-JOIN-003", Title: "t", Explanation: "e", Severity: diagnostics.SeverityMedium,
		Classification: diagnostics.Potential, Span: diagnostics.Span{Offset: 0, Length: len(planSQL)}}
	if out := ConfirmWithPlans(in, []diagnostics.Finding{join003}); out[0].Classification != diagnostics.Potential {
		t.Fatalf("an estimated plan confirmed fan-out: %+v", out[0])
	}
}

func TestSpill(t *testing.T) {
	sort := op(1, "Sort", 10, 5000)
	sort.Warnings = []plan.Warning{{Kind: plan.WarningSpill, Text: "Operator used tempdb to spill data during execution with spill level 1"}}
	fs := check(t, spill{}, planInput(true, op(0, "Select", 10, 5000, sort)))
	if len(fs) != 1 || fs[0].Evidence[0].Plan.NodeID != 1 || fs[0].Title != "Sort spilled to tempdb" {
		t.Fatalf("findings = %+v", fs)
	}
}

func TestImplicitConversion(t *testing.T) {
	on := clauseOf(t, "b.X = a.Y")
	seek := op(1, "Index Seek", 1, -1)
	seek.Predicates = []plan.Predicate{{Kind: plan.PredicateSeek, Text: "[b].[X]=CONVERT_IMPLICIT(nvarchar(10),[a].[Y],0)", Clause: on}}
	fs := check(t, implicitConversion{}, planInput(false, op(0, "Nested Loops", 1, -1, op(2, "Scan", 1, -1), seek)))
	if len(fs) != 1 || fs[0].Span.Offset != on.Offset || fs[0].Classification != diagnostics.Potential || fs[0].Evidence[0].Plan.Actual {
		t.Fatalf("findings = %+v, want one at the ON, estimated", fs)
	}

	// A residual predicate's conversion, and an explicit CONVERT the query wrote, are not reported.
	seek.Predicates[0].Kind = plan.PredicateResidual
	explicit := plan.Warning{Kind: plan.WarningImplicitConversion,
		Text: `Type conversion in expression (CONVERT(nvarchar(10),[a].[Y],0)) may affect "Cardinality Estimate" in query plan choice`}
	if fs := check(t, implicitConversion{}, planInput(false, op(0, "Nested Loops", 1, -1, seek), explicit)); len(fs) != 0 {
		t.Fatalf("findings = %+v", fs)
	}
	implicit := plan.Warning{Kind: plan.WarningImplicitConversion,
		Text: `Type conversion in expression (CONVERT_IMPLICIT(nvarchar(10),[a].[Y],0)) may affect "SeekPlan" in query plan choice`}
	if fs := check(t, implicitConversion{}, planInput(false, op(0, "Nested Loops", 1, -1, seek), implicit)); len(fs) != 1 || fs[0].Span.Length != len(planSQL) {
		t.Fatalf("statement warning: findings = %+v, want one on the statement", fs)
	}
}

func finding(t *testing.T, rule, text string) diagnostics.Finding {
	c := clauseOf(t, text)
	return diagnostics.Finding{RuleID: rule, Title: "t", Explanation: "e", Severity: diagnostics.SeverityHigh,
		Classification: diagnostics.Potential, Span: diagnostics.Span{Offset: c.Offset, Length: c.Length, Line: 1, Column: c.Column},
		Evidence: []diagnostics.Evidence{{Kind: diagnostics.EvidenceStatic, Summary: "s",
			Static: &diagnostics.StaticEvidence{Span: diagnostics.Span{Offset: c.Offset, Length: c.Length, Line: 1, Column: c.Column}, Text: text}}}}
}

func TestNoJoinPredicateConfirmsJoin001(t *testing.T) {
	loops := op(0, "Nested Loops", 1000, -1, op(1, "Scan", 10, -1), op(2, "Scan", 100, -1))
	loops.Warnings = []plan.Warning{{Kind: plan.WarningNoJoinPredicate, Text: "No Join Predicate"}}
	in := planInput(false, loops)
	alone := check(t, noJoinPredicate{}, in)
	if len(alone) != 1 || alone[0].Classification != diagnostics.Confirmed || alone[0].Evidence[0].Plan.NodeID != 0 {
		t.Fatalf("findings = %+v", alone)
	}
	join001 := finding(t, "QRY-JOIN-001", "dbo.A AS a JOIN dbo.B AS b")
	out := ConfirmWithPlans(in, []diagnostics.Finding{join001, alone[0]})
	if len(out) != 1 || out[0].RuleID != "QRY-JOIN-001" || out[0].Classification != diagnostics.Confirmed {
		t.Fatalf("out = %+v, want QRY-JOIN-001 confirmed and QRY-PLAN-004 dropped", out)
	}
	if ev := out[0].Evidence; len(ev) != 2 || ev[1].Plan == nil || ev[1].Plan.NodeID != 0 || ev[1].Plan.Actual {
		t.Fatalf("evidence = %+v, want the static one and the plan's (estimated, node 0)", ev)
	}
	if join001.Classification != diagnostics.Potential || len(join001.Evidence) != 1 {
		t.Fatal("ConfirmWithPlans changed its input finding's evidence")
	}
}

func TestImplicitConversionConfirmsJoin004(t *testing.T) {
	on := clauseOf(t, "b.X = a.Y")
	hash := op(0, "Hash Match", 10, -1, op(1, "Scan", 10, -1), op(2, "Scan", 10, -1))
	hash.Predicates = []plan.Predicate{{Kind: plan.PredicateJoin, Text: "[b].[X]=CONVERT_IMPLICIT(varchar(10),[a].[Y],0)", Clause: on}}
	in := planInput(false, hash)
	conv := check(t, implicitConversion{}, in)
	out := ConfirmWithPlans(in, append([]diagnostics.Finding{finding(t, "QRY-JOIN-004", "b.X = a.Y")}, conv...))
	if len(out) != 1 || out[0].RuleID != "QRY-JOIN-004" || out[0].Classification != diagnostics.Confirmed {
		t.Fatalf("out = %+v", out)
	}

	// A conversion at another condition confirms nothing.
	hash.Predicates[0].Clause = clauseOf(t, "a.Z = 1")
	out = ConfirmWithPlans(in, []diagnostics.Finding{finding(t, "QRY-JOIN-004", "b.X = a.Y")})
	if out[0].Classification != diagnostics.Potential {
		t.Fatalf("confirmed by a conversion elsewhere: %+v", out[0])
	}
}

func TestActualFanOutConfirmsJoin003(t *testing.T) {
	on := clauseOf(t, "b.X = a.Y")
	join := op(0, "Nested Loops", 100, 340, op(1, "Scan", 100, 100), op(2, "Index Seek", 1, 340))
	join.LogicalOp = "Inner Join"
	join.Predicates = []plan.Predicate{{Kind: plan.PredicateJoin, Text: "[b].[X]=[a].[Y]", Clause: on}}
	f := finding(t, "QRY-JOIN-003", "b.X = a.Y")
	out := ConfirmWithPlans(planInput(true, join), []diagnostics.Finding{f})
	if out[0].Classification != diagnostics.Confirmed || !strings.Contains(out[0].Evidence[1].Summary, "340 rows from 100 outer rows") {
		t.Fatalf("out = %+v", out[0])
	}

	// No multiplication, or a join not mapped to the condition: unchanged.
	join.Actual.Rows = num(100)
	if out := ConfirmWithPlans(planInput(true, join), []diagnostics.Finding{f}); out[0].Classification != diagnostics.Potential {
		t.Fatalf("confirmed without fan-out: %+v", out[0])
	}
	join.Actual.Rows, join.Predicates[0].Clause = num(340), nil
	if out := ConfirmWithPlans(planInput(true, join), []diagnostics.Finding{f}); out[0].Classification != diagnostics.Potential {
		t.Fatalf("confirmed by an unmapped join: %+v", out[0])
	}
}

func TestNoPlansChangeNothing(t *testing.T) {
	in := &Input{SQL: planSQL, Result: &parser.Result{}}
	f := finding(t, "QRY-JOIN-001", "dbo.A AS a JOIN dbo.B AS b")
	if out := ConfirmWithPlans(in, []diagnostics.Finding{f}); len(out) != 1 || out[0].Classification != diagnostics.Potential {
		t.Fatalf("out = %+v", out)
	}
	for _, r := range []Rule{estimateGap{}, implicitConversion{}, spill{}, noJoinPredicate{}} {
		if fs := check(t, r, in); len(fs) != 0 {
			t.Fatalf("%s without plans: %+v", r.Info().ID, fs)
		}
	}
}
