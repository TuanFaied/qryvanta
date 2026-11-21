package analyzer

import (
	"fmt"
	"strings"

	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/parser"
)

// walkShallow visits e and every expression below it, without entering subqueries (they are
// queries of their own).
func walkShallow(e *parser.Expr, fn func(*parser.Expr)) {
	if e == nil {
		return
	}
	fn(e)
	for _, c := range []*parser.Expr{e.Left, e.Right, e.Operand, e.Low, e.High, e.Pattern, e.Escape, e.Else} {
		walkShallow(c, fn)
	}
	for _, list := range [][]*parser.Expr{e.Values, e.Args} {
		for _, c := range list {
			walkShallow(c, fn)
		}
	}
	for _, w := range e.Whens {
		walkShallow(w.When, fn)
		walkShallow(w.Then, fn)
	}
	if e.Over != nil {
		for _, c := range e.Over.PartitionBy {
			walkShallow(c, fn)
		}
		for _, o := range e.Over.OrderBy {
			walkShallow(o.Expr, fn)
		}
	}
}

// aggregatesOf returns the aggregate calls of a SELECT: in its select list, HAVING and ORDER BY.
func aggregatesOf(q *parser.Query) []*parser.Expr {
	var out []*parser.Expr
	visit := func(e *parser.Expr) {
		walkShallow(e, func(x *parser.Expr) {
			if x.Kind == parser.ExprFunction && x.Aggregate {
				out = append(out, x)
			}
		})
	}
	for _, item := range q.Select {
		visit(item.Expr)
	}
	visit(q.Having)
	for _, o := range q.OrderBy {
		visit(o.Expr)
	}
	return out
}

// countOfColumn returns the column of COUNT(column), without DISTINCT.
func countOfColumn(e *parser.Expr) *parser.Expr {
	if e.Kind != parser.ExprFunction || !strings.EqualFold(e.Name, "COUNT") || e.Schema != "" || e.Distinct || len(e.Args) != 1 {
		return nil
	}
	if columnKey(e.Args[0]) == "" {
		return nil // COUNT(*), COUNT(1), COUNT(expression)
	}
	return e.Args[0]
}

// countSkipsNulls is QRY-COUNT-001: COUNT(column) counts the rows where the column is not NULL,
// which differs from COUNT(*) when the column is nullable.
type countSkipsNulls struct{}

func (countSkipsNulls) Info() Info {
	return Info{
		ID:       "QRY-COUNT-001",
		Title:    "COUNT(column) skips NULLs",
		Severity: diagnostics.SeverityInformational,
		Evidence: []diagnostics.EvidenceKind{diagnostics.EvidenceStatic},
	}
}

func (countSkipsNulls) Check(in *Input) ([]diagnostics.Finding, error) {
	var out []diagnostics.Finding
	in.Walk(Visitor{Query: func(q *parser.Query) {
		if q.Kind != parser.QuerySelect {
			return
		}
		for _, agg := range aggregatesOf(q) {
			col := countOfColumn(agg)
			if col == nil || notNullIn(q, columnKey(col)) {
				continue
			}
			c := in.Text(col.Span)
			span := SpanOf(agg.Span)
			out = append(out, diagnostics.Finding{
				Title: fmt.Sprintf("COUNT(%s) does not count rows where %s is NULL", c, c),
				Explanation: fmt.Sprintf("COUNT(%s) counts the rows where %s has a value. If the column can be NULL, "+
					"that is fewer than the number of rows. Use COUNT(*) to count rows, or keep COUNT(%s) if counting "+
					"values is the intent.", c, c, c),
				Classification: diagnostics.Potential,
				Span:           span,
				Evidence: []diagnostics.Evidence{staticEvidence(in, agg,
					"The query does not require the column to be non-NULL (no IS NOT NULL, comparison or inner join on it).")},
				Correction: &diagnostics.Correction{
					Description: "If the intent is to count rows, use COUNT(*).",
					Span:        span,
					Replacement: "COUNT(*)",
				},
			})
		}
	}})
	return out, nil
}

// notNullIn reports whether the query only produces rows where the column is not NULL: a WHERE
// condition rejects NULL in it, or an inner join compares it with = and the FROM has no outer
// join that could add NULLs back.
func notNullIn(q *parser.Query, key string) bool {
	rejects := func(c *parser.Expr) bool {
		switch c.Kind {
		case parser.ExprIsNull:
			return c.Not && columnKey(c.Operand) == key
		case parser.ExprCompare:
			return columnKey(c.Left) == key || columnKey(c.Right) == key
		case parser.ExprIn:
			return columnKey(c.Operand) == key && !(c.Not && c.Query != nil)
		case parser.ExprBetween, parser.ExprLike:
			return columnKey(c.Operand) == key
		}
		return false
	}
	for _, c := range conjuncts(q.Where) {
		if rejects(c) {
			return true
		}
	}
	js := joins(q.From)
	for _, j := range js {
		switch j.JoinType {
		case parser.JoinLeft, parser.JoinRight, parser.JoinFull, parser.JoinOuterApply:
			return false
		}
	}
	for _, j := range js {
		for _, c := range conjuncts(j.On) {
			if c.Kind == parser.ExprCompare && c.Op == "=" && (columnKey(c.Left) == key || columnKey(c.Right) == key) {
				return true
			}
		}
	}
	return false
}

// countAtJoinGrain is QRY-COUNT-002: COUNT(t.column) without DISTINCT in a query that joins t to
// another table whose columns other aggregates use. The aggregates then run at the grain of the
// join: if a t row matches several rows of the other table, it is counted once per match.
// COUNT(*) over a join needs the fan-out probe (#70) to say anything, so it is not reported
// statically.
type countAtJoinGrain struct{}

func (countAtJoinGrain) Info() Info {
	return Info{
		ID:       "QRY-COUNT-002",
		Title:    "COUNT over a join may count rows more than once",
		Severity: diagnostics.SeverityMedium,
		Evidence: []diagnostics.EvidenceKind{diagnostics.EvidenceStatic},
	}
}

func (countAtJoinGrain) Check(in *Input) ([]diagnostics.Finding, error) {
	var out []diagnostics.Finding
	in.Walk(Visitor{Query: func(q *parser.Query) {
		if q.Kind != parser.QuerySelect || len(joins(q.From)) == 0 {
			return
		}
		names := sourceNames(q.From)
		aggs := aggregatesOf(q)
		for _, agg := range aggs {
			col := countOfColumn(agg)
			if col == nil {
				continue
			}
			t, ok := qualifier(col)
			if !ok || !contains(names, t) {
				continue
			}
			// Another aggregate over a different table of the join.
			var other *parser.Expr
			for _, a := range aggs {
				if a == agg {
					continue
				}
				r := referencesOf(a)
				for qn := range r.qualifiers {
					if qn != t && contains(names, qn) {
						other = a
					}
				}
			}
			if other == nil {
				continue
			}
			c := in.Text(col.Span)
			span := SpanOf(agg.Span)
			out = append(out, diagnostics.Finding{
				Title: fmt.Sprintf("COUNT(%s) counts joined rows, not distinct %s rows", c, t),
				Explanation: fmt.Sprintf("The query joins %s to other tables, and %s aggregates their columns, so the "+
					"aggregates run once per joined row. If a %s row matches several rows of another table, COUNT(%s) "+
					"counts it once per match. To count %s rows, use COUNT(DISTINCT %s) with a column that identifies a "+
					"row of %s.", t, in.Text(other.Span), t, c, t, c, t),
				Classification: diagnostics.Potential,
				Span:           span,
				Evidence: []diagnostics.Evidence{
					staticEvidence(in, agg, fmt.Sprintf("COUNT without DISTINCT on a column of %s.", t)),
					staticEvidence(in, other, "Another aggregate uses a different table of the join, so the rows are at the join's grain."),
				},
				Correction: &diagnostics.Correction{
					Description: fmt.Sprintf("Count each %s row once (if %s identifies a row of %s).", t, c, t),
					Span:        span,
					Replacement: "COUNT(DISTINCT " + c + ")",
				},
			})
		}
	}})
	return out, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
