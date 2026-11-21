package analyzer

import (
	"fmt"
	"strings"

	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/parser"
)

// notInNull is QRY-NULL-002: NOT IN over a subquery (or a list) that can contain NULL. `x NOT IN
// (…, NULL)` is never true, so one NULL returned by the subquery makes the predicate reject every
// row.
type notInNull struct{}

func (notInNull) Info() Info {
	return Info{
		ID:       "QRY-NULL-002",
		Title:    "NOT IN with a subquery that can return NULL",
		Severity: diagnostics.SeverityHigh,
		Evidence: []diagnostics.EvidenceKind{diagnostics.EvidenceStatic},
	}
}

func (notInNull) Check(in *Input) ([]diagnostics.Finding, error) {
	var out []diagnostics.Finding
	in.Walk(Visitor{Expr: func(e *parser.Expr) {
		// NOT (x IN (…)) behaves like x NOT IN (…): NOT UNKNOWN is UNKNOWN. The finding covers the NOT.
		whole := e
		if e.Kind == parser.ExprNot && e.Operand != nil && e.Operand.Kind == parser.ExprIn && !e.Operand.Not {
			e = e.Operand
		} else if e.Kind != parser.ExprIn || !e.Not {
			return
		}
		if e.Operand == nil {
			return
		}
		span := SpanOf(whole.Span)
		text := in.Text(whole.Span)
		evidence := func(summary string) []diagnostics.Evidence {
			return []diagnostics.Evidence{{
				Kind:    diagnostics.EvidenceStatic,
				Summary: summary,
				Static:  &diagnostics.StaticEvidence{Span: span, Text: text},
			}}
		}

		// A NULL in the list: provably never true.
		for _, v := range e.Values {
			if isLiteral(v, "null") {
				out = append(out, diagnostics.Finding{
					Title: "NOT IN with NULL in the list is never true",
					Explanation: "x NOT IN (a, NULL) means x <> a AND x <> NULL. x <> NULL is UNKNOWN for every row, " +
						"so the whole predicate is never true and the condition rejects every row. Remove NULL " +
						"from the list.",
					Classification: diagnostics.Confirmed,
					Span:           span,
					Evidence:       evidence("The NOT IN list contains the NULL literal."),
				})
				return
			}
		}

		q := e.Query
		if q == nil || excludesNull(q) {
			return
		}
		f := diagnostics.Finding{
			Title: "NOT IN with a subquery returns no rows if the subquery returns a NULL",
			Explanation: "x NOT IN (SELECT y …) is never true when any y is NULL: x <> NULL is UNKNOWN, so the " +
				"predicate rejects every row, without an error. Unless the column cannot be NULL, use NOT EXISTS, " +
				"or add WHERE y IS NOT NULL to the subquery.",
			Classification: diagnostics.Potential,
			Span:           span,
			Evidence:       evidence("NOT IN compares with every value the subquery returns, NULLs included."),
		}
		if rewrite, ok := notExists(in, e); ok {
			f.Correction = &diagnostics.Correction{
				Description: "Use NOT EXISTS, which NULLs in the subquery do not affect. Unlike NOT IN, it also keeps " +
					"rows where " + in.Text(e.Operand.Span) + " is NULL.",
				Span:        span,
				Replacement: rewrite,
			}
		}
		out = append(out, f)
	}})
	return out, nil
}

// excludesNull reports whether a subquery provably returns no NULL: it selects a non-NULL
// literal, or its WHERE requires the selected column IS NOT NULL.
func excludesNull(q *parser.Query) bool {
	if q.Kind != parser.QuerySelect || len(q.Select) != 1 || q.Select[0].Expr == nil {
		return false
	}
	sel := q.Select[0].Expr
	if isLiteral(sel, "") {
		return !isLiteral(sel, "null")
	}
	if sel.Kind != parser.ExprColumn {
		return false
	}
	selQ, selQualified := qualifier(sel)
	for _, c := range conjuncts(q.Where) {
		if c.Kind != parser.ExprIsNull || !c.Not || columnName(c.Operand) != columnName(sel) {
			continue
		}
		if cq, ok := qualifier(c.Operand); !ok || !selQualified || cq == selQ {
			return true
		}
	}
	return false
}

// notExists rewrites `x NOT IN (SELECT y FROM f WHERE w)` as
// `NOT EXISTS (SELECT 1 FROM f WHERE w AND y = x)`, when that is safe: a single-column SELECT with
// a FROM and no grouping or row limits, and an x that cannot be captured by the subquery's own
// tables (a variable, a literal, or a column qualified with a name the subquery does not use).
func notExists(in *Input, e *parser.Expr) (string, bool) {
	q := e.Query
	if q.Kind != parser.QuerySelect || q.From == nil || len(q.Select) != 1 || q.Select[0].Expr == nil ||
		len(q.GroupBy) > 0 || q.Having != nil || q.Top != nil || q.Offset != nil {
		return "", false
	}
	x := e.Operand
	switch x.Kind {
	case parser.ExprVariable, parser.ExprLiteral:
	case parser.ExprColumn:
		qual, ok := qualifier(x)
		if !ok {
			return "", false
		}
		for _, n := range sourceNames(q.From) {
			if n == qual {
				return "", false
			}
		}
	default:
		return "", false
	}
	cond := in.Text(q.Select[0].Expr.Span) + " = " + in.Text(x.Span)
	if q.Where != nil {
		cond = wrapOr(q.Where, in.Text(q.Where.Span)) + " AND " + cond
	}
	return fmt.Sprintf("NOT EXISTS (SELECT 1 FROM %s WHERE %s)", strings.TrimSpace(in.Text(q.From.Span)), cond), true
}
