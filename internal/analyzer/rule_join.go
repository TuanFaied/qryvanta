package analyzer

import (
	"fmt"
	"strings"

	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/parser"
)

// missingJoinPredicate is QRY-JOIN-001: a join that pairs every row of one side with every row
// of the other without saying so. Two shapes:
//
//   - a comma join (FROM a, b) with no WHERE condition that refers to both sides;
//   - an INNER, LEFT, RIGHT or FULL JOIN whose ON does not refer to both sides (ON a.x = a.y,
//     ON 1 = 1).
//
// CROSS JOIN and APPLY are explicit and never reported. A condition with unqualified columns or a
// subquery cannot be checked without metadata, so it silences the rule. Names outside the join
// (an outer query's, in a correlated subquery) do not link its sides.
type missingJoinPredicate struct{}

func (missingJoinPredicate) Info() Info {
	return Info{
		ID:       "QRY-JOIN-001",
		Title:    "JOIN without a predicate (cartesian product)",
		Severity: diagnostics.SeverityHigh,
		Evidence: []diagnostics.EvidenceKind{diagnostics.EvidenceStatic},
	}
}

func (missingJoinPredicate) Check(in *Input) ([]diagnostics.Finding, error) {
	var out []diagnostics.Finding
	for _, fc := range in.fromClauses() {
		for _, j := range joins(fc.from) {
			left, right := sourceNames(j.Left), sourceNames(j.Right)
			if len(left) == 0 || len(right) == 0 {
				continue
			}
			switch j.JoinType {
			case parser.JoinComma:
				if singleRow(j.Left) || singleRow(j.Right) || linked(fc.where, left, right) {
					continue
				}
				span := SpanOf(j.Span)
				out = append(out, diagnostics.Finding{
					Title: fmt.Sprintf("Comma join without a join condition between %s and %s", list(left), list(right)),
					Explanation: "In FROM a, b every row of a is paired with every row of b (a cartesian product), " +
						"unless the WHERE clause links them. No WHERE condition refers to both sides, so the result " +
						"has rows(a) × rows(b) rows. Add the join condition, preferably as JOIN … ON, or write " +
						"CROSS JOIN if the product is intended.",
					Classification: diagnostics.Potential,
					Span:           span,
					Evidence: []diagnostics.Evidence{{
						Kind: diagnostics.EvidenceStatic,
						Summary: fmt.Sprintf("No WHERE condition refers to both %s and %s.",
							list(left), list(right)),
						Static: &diagnostics.StaticEvidence{Span: span, Text: in.Text(j.Span)},
					}},
				})
			case parser.JoinInner, parser.JoinLeft, parser.JoinRight, parser.JoinFull:
				if j.On == nil {
					continue
				}
				refs := referencesOf(j.On)
				if refs.unqualified || refs.subquery || (refs.touches(left) && refs.touches(right)) {
					continue
				}
				span := SpanOf(j.On.Span)
				missing := right
				if refs.touches(right) {
					missing = left
				}
				out = append(out, diagnostics.Finding{
					Title: fmt.Sprintf("The ON condition does not refer to %s", list(missing)),
					Explanation: fmt.Sprintf("ON %s does not link the two sides of the join, so every row on one "+
						"side is paired with every matching row on the other: a cartesian product, filtered. "+
						"Add the condition that links %s to %s.", in.Text(j.On.Span), list(left), list(right)),
					Classification: diagnostics.Potential,
					Span:           span,
					Evidence: []diagnostics.Evidence{{
						Kind:    diagnostics.EvidenceStatic,
						Summary: fmt.Sprintf("The ON condition refers to no column of %s.", list(missing)),
						Static:  &diagnostics.StaticEvidence{Span: span, Text: in.Text(j.On.Span)},
					}},
				})
			}
		}
	}
	return out, nil
}

// linked reports whether some WHERE condition (an AND operand) refers to both sides, or could:
// conditions the rule cannot check (unqualified columns, subqueries) count as linking.
func linked(where *parser.Expr, left, right []string) bool {
	for _, c := range conjuncts(where) {
		r := referencesOf(c)
		if r.unqualified || r.subquery || (r.touches(left) && r.touches(right)) {
			return true
		}
	}
	return false
}

// singleRow reports whether a source provably has at most one row: a derived table that selects
// without FROM, aggregates without GROUP BY, or is limited to TOP (1).
func singleRow(s *parser.Source) bool {
	if s.Kind != parser.SourceDerived || s.Query == nil || s.Query.Kind != parser.QuerySelect {
		return false
	}
	q := s.Query
	if q.Top != nil && !q.Top.Percent && isLiteral(q.Top.Expr, "integer") && q.Top.Expr.Value == "1" {
		return true
	}
	if q.From == nil {
		return true
	}
	if len(q.GroupBy) > 0 || len(q.Select) == 0 {
		return false
	}
	for _, item := range q.Select {
		if item.Expr == nil || item.Expr.Kind != parser.ExprFunction || !item.Expr.Aggregate || item.Expr.Over != nil {
			return false
		}
	}
	return true
}

// list formats names for a message.
func list(names []string) string { return strings.Join(names, ", ") }

// leftJoinCancelled is QRY-JOIN-002: a WHERE condition on the optional side of a LEFT (or RIGHT)
// JOIN that rejects NULLs. Rows without a match have NULL in every column of that side, so the
// condition removes them and the outer join behaves as an inner join.
type leftJoinCancelled struct{}

func (leftJoinCancelled) Info() Info {
	return Info{
		ID:       "QRY-JOIN-002",
		Title:    "Outer join cancelled by a WHERE condition",
		Severity: diagnostics.SeverityHigh,
		Evidence: []diagnostics.EvidenceKind{diagnostics.EvidenceStatic},
	}
}

func (leftJoinCancelled) Check(in *Input) ([]diagnostics.Finding, error) {
	var out []diagnostics.Finding
	for _, fc := range in.fromClauses() {
		reported := map[*parser.Expr]bool{}
		for _, j := range joins(fc.from) {
			var optional *parser.Source
			kind := ""
			switch j.JoinType {
			case parser.JoinLeft:
				optional, kind = j.Right, "LEFT"
			case parser.JoinRight:
				optional, kind = j.Left, "RIGHT"
			default:
				continue
			}
			names := sourceNames(optional)
			if len(names) == 0 {
				continue
			}
			for _, c := range conjuncts(fc.where) {
				if reported[c] || !rejectsNull(c, names) {
					continue
				}
				reported[c] = true
				span := SpanOf(c.Span)
				pred := in.Text(c.Span)
				f := diagnostics.Finding{
					Title: fmt.Sprintf("WHERE %s turns the %s JOIN into an inner join", pred, kind),
					Explanation: fmt.Sprintf("Rows that the %s JOIN adds without a match have NULL in every column of %s. "+
						"%s is not true for NULL, so WHERE removes exactly those rows and the result is the same as an "+
						"INNER JOIN. To filter %s but keep the unmatched rows, put the condition in ON. If an inner "+
						"join is what you want, write INNER JOIN.", kind, list(names), pred, list(names)),
					Classification: diagnostics.Confirmed,
					Span:           span,
					Evidence: []diagnostics.Evidence{
						{
							Kind:    diagnostics.EvidenceStatic,
							Summary: fmt.Sprintf("%s is on the optional side of a %s JOIN.", list(names), kind),
							Static:  &diagnostics.StaticEvidence{Span: SpanOf(optional.Span), Text: in.Text(optional.Span)},
						},
						{
							Kind:    diagnostics.EvidenceStatic,
							Summary: fmt.Sprintf("This WHERE condition is UNKNOWN when the columns of %s are NULL.", list(names)),
							Static:  &diagnostics.StaticEvidence{Span: span, Text: pred},
						},
					},
				}
				if j.On != nil {
					f.Correction = &diagnostics.Correction{
						Description: "Move the condition into ON (and remove it from WHERE) to keep the unmatched rows.",
						Span:        SpanOf(j.On.Span),
						Replacement: wrapOr(j.On, in.Text(j.On.Span)) + " AND " + wrapOr(c, pred),
					}
				}
				out = append(out, f)
			}
		}
	}
	return out, nil
}

// rejectsNull reports whether e is provably not true when every column of the named sources is
// NULL. Only direct column references count: ISNULL(r.x, 0) = 0 or CASE expressions may be true.
func rejectsNull(e *parser.Expr, names []string) bool {
	on := func(x *parser.Expr) bool {
		q, ok := qualifier(x)
		if !ok {
			return false
		}
		for _, n := range names {
			if n == q {
				return true
			}
		}
		return false
	}
	switch e.Kind {
	case parser.ExprAnd:
		return rejectsNull(e.Left, names) || rejectsNull(e.Right, names)
	case parser.ExprOr:
		return rejectsNull(e.Left, names) && rejectsNull(e.Right, names)
	case parser.ExprCompare:
		return on(e.Left) || on(e.Right)
	case parser.ExprIn:
		// NULL NOT IN (an empty subquery) is true.
		return on(e.Operand) && !(e.Not && e.Query != nil)
	case parser.ExprBetween, parser.ExprLike:
		return on(e.Operand)
	}
	return false
}
