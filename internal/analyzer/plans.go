package analyzer

import (
	"fmt"
	"strings"

	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/parser"
	"github.com/TuanFaied/qryvanta/internal/plan"
)

// Plan evidence (architecture D3, D6): rules that read execution plans, and the upgrades plans
// give other findings. Plans are linked to the script before the rules run (LinkPlans), so every
// plan finding points at the statement, or the clause, it is about.

// PlanDoc is one plan document with the batch (0-based) that produced it.
type PlanDoc struct {
	Batch int
	Plan  *plan.Plan
}

// LinkPlans links each plan's statements to the parsed script and maps their predicates to the
// query's conditions (plan.Link, plan.MapPredicates).
func (in *Input) LinkPlans() {
	if in.Result == nil {
		return
	}
	for _, d := range in.Plans {
		if d.Plan == nil || d.Batch < 0 || d.Batch >= len(in.Result.Batches) {
			continue
		}
		b := in.Result.Batches[d.Batch]
		plan.Link(d.Plan, d.Batch, in.SQL, b)
		for i := range d.Plan.Statements {
			st := &d.Plan.Statements[i]
			if st.Script != nil && st.Script.Statement < len(b.Statements) {
				plan.MapPredicates(st, b.Statements[st.Script.Statement])
			}
		}
	}
}

// planStatement is a linked plan statement with the span of its script statement.
type planStatement struct {
	st   *plan.Statement
	span parser.Span
}

// planStatements lists the plan statements linked to the script.
func (in *Input) planStatements() []planStatement {
	var out []planStatement
	for _, d := range in.Plans {
		if d.Plan == nil {
			continue
		}
		for i := range d.Plan.Statements {
			st := &d.Plan.Statements[i]
			l := st.Script
			if l == nil || l.Batch >= len(in.Result.Batches) || l.Statement >= len(in.Result.Batches[l.Batch].Statements) {
				continue
			}
			out = append(out, planStatement{st: st, span: in.Result.Batches[l.Batch].Statements[l.Statement].Span})
		}
	}
	return out
}

func isActual(in *Input) bool {
	for _, d := range in.Plans {
		if d.Plan != nil && d.Plan.Actual {
			return true
		}
	}
	return false
}

// operatorSpan is the clause an operator's predicates map to, or the statement.
func operatorSpan(o *plan.Operator, stmt parser.Span) parser.Span {
	var found *plan.Clause
	for _, p := range o.Predicates {
		if p.Clause != nil {
			if found != nil && (found.Offset != p.Clause.Offset || found.Length != p.Clause.Length) {
				return stmt // several clauses: the statement
			}
			found = p.Clause
		}
	}
	if found == nil {
		return stmt
	}
	return parser.Span{Offset: found.Offset, Length: found.Length, Line: found.Line, Column: found.Column}
}

func planEvidence(o *plan.Operator, actual bool, property, value, summary string) diagnostics.Evidence {
	return diagnostics.Evidence{
		Kind:    diagnostics.EvidencePlan,
		Summary: summary,
		Plan:    &diagnostics.PlanEvidence{NodeID: o.NodeID, Operator: o.PhysicalOp, Property: property, Value: value, Actual: actual},
	}
}

func fmtRows(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%.1f", v)
}

var planOnly = []diagnostics.EvidenceKind{diagnostics.EvidencePlan}

// estimateGap is QRY-PLAN-001: an operator whose actual rows differ from the estimate by at least
// plan.GapFactor (and plan.GapMinRows). Only the operator where the gap starts is reported: its
// parents inherit the misestimate.
type estimateGap struct{}

func (estimateGap) Info() Info {
	return Info{ID: "QRY-PLAN-001", Title: "Estimated and actual rows far apart", Severity: diagnostics.SeverityLow, Evidence: planOnly}
}

func (estimateGap) Check(in *Input) ([]diagnostics.Finding, error) {
	var out []diagnostics.Finding
	for _, ps := range in.planStatements() {
		ps.st.Walk(func(o *plan.Operator) {
			g, ok := plan.EstimateGap(o)
			if !ok || !g.Big {
				return
			}
			for _, c := range o.Children {
				if cg, ok := plan.EstimateGap(c); ok && cg.Big {
					return // the gap starts below
				}
			}
			dir := "over"
			if g.Under {
				dir = "under"
			}
			span := SpanOf(operatorSpan(o, ps.span))
			out = append(out, diagnostics.Finding{
				Title: fmt.Sprintf("%s: %s rows estimated, %s actual (%s-estimated ×%.0f)", o.PhysicalOp, fmtRows(g.EstimatedRows), fmtRows(g.ActualRows), dir, g.Factor),
				Explanation: fmt.Sprintf("The optimizer expected %s rows from %s (node %d) and %s came. Plans chosen for the wrong row count "+
					"pick the wrong join types, memory grants and access paths. Common causes: stale statistics (UPDATE STATISTICS), "+
					"a predicate the optimizer cannot estimate (functions on columns, see QRY-DATE-001), or correlated columns.",
					fmtRows(g.EstimatedRows), o.PhysicalOp, o.NodeID, fmtRows(g.ActualRows)),
				Classification: diagnostics.Optimization,
				Span:           span,
				Evidence: []diagnostics.Evidence{planEvidence(o, true, "rows (all executions)",
					fmt.Sprintf("estimated %s, actual %s", fmtRows(g.EstimatedRows), fmtRows(g.ActualRows)),
					fmt.Sprintf("Node %d returned %s rows against %s estimated (measured).", o.NodeID, fmtRows(g.ActualRows), fmtRows(g.EstimatedRows)))},
			})
		})
	}
	return out, nil
}

// conversion is one implicit conversion a plan reports.
type conversion struct {
	op     *plan.Operator // the operator whose predicate converts, or the root for a statement warning
	text   string
	clause *plan.Clause
}

// conversions lists a statement's implicit conversions: PlanAffectingConvert warnings about a
// CONVERT_IMPLICIT, and CONVERT_IMPLICIT in seek and join predicates.
func conversions(st *plan.Statement) []conversion {
	var out []conversion
	for _, w := range st.Warnings {
		// Only implicit conversions: an explicit CAST or CONVERT in the query is what was meant.
		if w.Kind == plan.WarningImplicitConversion && st.Root != nil && strings.Contains(strings.ToUpper(w.Text), "CONVERT_IMPLICIT") {
			out = append(out, conversion{op: st.Root, text: w.Text})
		}
	}
	st.Walk(func(o *plan.Operator) {
		for i := range o.Predicates {
			p := &o.Predicates[i]
			if (p.Kind == plan.PredicateSeek || p.Kind == plan.PredicateJoin) && strings.Contains(strings.ToUpper(p.Text), "CONVERT_IMPLICIT") {
				out = append(out, conversion{op: o, text: p.Text, clause: p.Clause})
			}
		}
	})
	return out
}

// implicitConversion is QRY-PLAN-002: the plan converts a column implicitly where it affects the
// estimate or a seek. A join type mismatch (QRY-JOIN-004) at the same place is confirmed by it
// instead (ConfirmWithPlans).
type implicitConversion struct{}

func (implicitConversion) Info() Info {
	return Info{ID: "QRY-PLAN-002", Title: "Implicit conversion in the plan", Severity: diagnostics.SeverityMedium, Evidence: planOnly}
}

func (implicitConversion) Check(in *Input) ([]diagnostics.Finding, error) {
	var out []diagnostics.Finding
	actual := isActual(in)
	for _, ps := range in.planStatements() {
		for _, c := range conversions(ps.st) {
			span := ps.span
			if c.clause != nil {
				span = parser.Span{Offset: c.clause.Offset, Length: c.clause.Length, Line: c.clause.Line, Column: c.clause.Column}
			}
			out = append(out, diagnostics.Finding{
				Title: "SQL Server converts a column implicitly",
				Explanation: "The plan converts a column's values to another type (CONVERT_IMPLICIT) to compare them. The conversion runs " +
					"for every row, can prevent an index seek, and makes the row estimate a guess. Compare values of the column's own type, " +
					"or convert the other side explicitly.",
				Classification: diagnostics.Potential,
				Span:           SpanOf(span),
				Evidence: []diagnostics.Evidence{planEvidence(c.op, actual, "implicit conversion", c.text,
					fmt.Sprintf("The plan reports an implicit conversion at %s (node %d).", c.op.PhysicalOp, c.op.NodeID))},
			})
		}
	}
	return out, nil
}

// spill is QRY-PLAN-003: a sort or hash that spilled to tempdb (actual plans).
type spill struct{}

func (spill) Info() Info {
	return Info{ID: "QRY-PLAN-003", Title: "Spill to tempdb", Severity: diagnostics.SeverityLow, Evidence: planOnly}
}

func (spill) Check(in *Input) ([]diagnostics.Finding, error) {
	if !isActual(in) {
		return nil, nil // spills are measured: only actual plans report them
	}
	var out []diagnostics.Finding
	for _, ps := range in.planStatements() {
		ps.st.Walk(func(o *plan.Operator) {
			for _, w := range o.Warnings {
				if w.Kind != plan.WarningSpill {
					continue
				}
				out = append(out, diagnostics.Finding{
					Title: fmt.Sprintf("%s spilled to tempdb", o.PhysicalOp),
					Explanation: "The operator ran out of its memory grant and wrote rows to tempdb, which is much slower. The grant " +
						"was sized from the row estimate: an under-estimate (see QRY-PLAN-001) or missing statistics are the usual cause.",
					Classification: diagnostics.Optimization,
					Span:           SpanOf(ps.span),
					Evidence:       []diagnostics.Evidence{planEvidence(o, true, "warning", w.Text, w.Text)},
				})
				return
			}
		})
	}
	return out, nil
}

// noJoinPredicate is QRY-PLAN-004: the plan reports a join without a join predicate. It confirms
// QRY-JOIN-001 when that finding exists (ConfirmWithPlans removes this one then).
type noJoinPredicate struct{}

func (noJoinPredicate) Info() Info {
	return Info{ID: "QRY-PLAN-004", Title: "Join without a predicate in the plan", Severity: diagnostics.SeverityHigh, Evidence: planOnly}
}

func (noJoinPredicate) Check(in *Input) ([]diagnostics.Finding, error) {
	var out []diagnostics.Finding
	actual := isActual(in)
	for _, ps := range in.planStatements() {
		ps.st.Walk(func(o *plan.Operator) {
			for _, w := range o.Warnings {
				if w.Kind == plan.WarningNoJoinPredicate {
					out = append(out, diagnostics.Finding{
						Title: fmt.Sprintf("%s joins without a join predicate", o.PhysicalOp),
						Explanation: "SQL Server's plan joins two inputs without any condition: every row of one is paired with every " +
							"row of the other (a cartesian product). Add the join condition.",
						Classification: diagnostics.Confirmed,
						Span:           SpanOf(ps.span),
						Evidence:       []diagnostics.Evidence{planEvidence(o, actual, "warning", "No Join Predicate", w.Text)},
					})
					return
				}
			}
		})
	}
	return out, nil
}

func within(inner, outer diagnostics.Span) bool {
	return inner.Offset >= outer.Offset && inner.Offset+inner.Length <= outer.Offset+outer.Length
}

func overlaps(a, b diagnostics.Span) bool {
	return a.Offset < b.Offset+b.Length && b.Offset < a.Offset+a.Length
}

// ConfirmWithPlans upgrades findings with the plans' evidence and drops the plan findings they
// make redundant:
//
//   - QRY-JOIN-001 (no join predicate) in a statement whose plan warns "No Join Predicate";
//   - QRY-JOIN-004 (join type mismatch) where the plan converts implicitly at the same condition;
//   - QRY-JOIN-003 (fan-out) where an actual join operator mapped to the join's condition returned
//     more rows than its outer input: measured row multiplication.
//
// Upgraded findings become Confirmed with plan evidence. A finding that would become invalid keeps
// its old form.
func ConfirmWithPlans(in *Input, findings []diagnostics.Finding) []diagnostics.Finding {
	stmts := in.planStatements()
	if len(stmts) == 0 {
		return findings
	}
	actual := isActual(in)
	drop := map[int]bool{}
	confirmed := map[int]bool{}
	confirm := func(i int, ev diagnostics.Evidence) {
		if confirmed[i] {
			return // one piece of plan evidence is enough
		}
		f := findings[i]
		f.Evidence = append(append([]diagnostics.Evidence{}, f.Evidence...), ev)
		f.Classification = diagnostics.Confirmed
		if diagnostics.Validate(f, in.SQL) == nil {
			findings[i] = f
			confirmed[i] = true
		}
	}
	for i, f := range findings {
		switch f.RuleID {
		case "QRY-JOIN-001":
			for _, ps := range stmts {
				if !within(f.Span, SpanOf(ps.span)) {
					continue
				}
				ps.st.Walk(func(o *plan.Operator) {
					for _, w := range o.Warnings {
						if w.Kind == plan.WarningNoJoinPredicate {
							confirm(i, planEvidence(o, actual, "warning", "No Join Predicate",
								fmt.Sprintf("The plan's %s (node %d) has no join predicate.", o.PhysicalOp, o.NodeID)))
							for j, g := range findings {
								if !confirmed[i] {
									break // the finding kept its old form: keep the plan finding too
								}
								if g.RuleID == "QRY-PLAN-004" && within(g.Span, SpanOf(ps.span)) {
									drop[j] = true
								}
							}
						}
					}
				})
			}
		case "QRY-JOIN-004":
			for _, ps := range stmts {
				for _, c := range conversions(ps.st) {
					if c.clause == nil {
						continue
					}
					cs := diagnostics.Span{Offset: c.clause.Offset, Length: c.clause.Length}
					if !overlaps(cs, f.Span) {
						continue
					}
					confirm(i, planEvidence(c.op, actual, "implicit conversion", c.text,
						fmt.Sprintf("The plan converts the join column implicitly at %s (node %d).", c.op.PhysicalOp, c.op.NodeID)))
					for j, g := range findings {
						if !confirmed[i] {
							break // the finding kept its old form: keep the plan finding too
						}
						if g.RuleID == "QRY-PLAN-002" && overlaps(g.Span, f.Span) {
							drop[j] = true
						}
					}
				}
			}
		case "QRY-JOIN-003":
			if !actual {
				continue
			}
			for _, ps := range stmts {
				ps.st.Walk(func(o *plan.Operator) {
					if o.Actual == nil || o.Actual.Rows == nil || len(o.Children) == 0 || !strings.Contains(o.LogicalOp, "Join") {
						return
					}
					cs := SpanOf(operatorSpan(o, ps.span))
					if cs == SpanOf(ps.span) || !within(cs, f.Span) {
						return // not mapped to this join's condition
					}
					outer := o.Children[0]
					if outer.Actual == nil || outer.Actual.Rows == nil || *o.Actual.Rows <= *outer.Actual.Rows {
						return
					}
					confirm(i, planEvidence(o, true, "actual rows",
						fmt.Sprintf("%s out of %s outer rows", fmtRows(*o.Actual.Rows), fmtRows(*outer.Actual.Rows)),
						fmt.Sprintf("The %s (node %d) returned %s rows from %s outer rows: rows multiplied (measured).",
							o.PhysicalOp, o.NodeID, fmtRows(*o.Actual.Rows), fmtRows(*outer.Actual.Rows))))
				})
			}
		}
	}
	if len(drop) == 0 {
		return findings
	}
	out := make([]diagnostics.Finding, 0, len(findings))
	for i, f := range findings {
		if !drop[i] {
			out = append(out, f)
		}
	}
	return out
}
