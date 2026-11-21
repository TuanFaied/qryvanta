package analyzer

import (
	"fmt"
	"strings"

	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/parser"
)

// A condition is one operand of an AND chain that the predicate rules understand: a comparison
// of a column with a number (in either order), or IS [NOT] NULL on a column.
type condition struct {
	expr   *parser.Expr
	column string
	isNull bool // column IS NULL
	notNul bool // column IS NOT NULL
	values rangeSet
}

func conditionOf(e *parser.Expr) (condition, bool) {
	switch e.Kind {
	case parser.ExprIsNull:
		if k := columnKey(e.Operand); k != "" {
			return condition{expr: e, column: k, isNull: !e.Not, notNul: e.Not}, true
		}
	case parser.ExprCompare:
		if e.Quantifier != "" {
			return condition{}, false
		}
		col, num, op := e.Left, e.Right, e.Op
		if columnKey(col) == "" {
			col, num, op = e.Right, e.Left, flip(e.Op)
		}
		k := columnKey(col)
		v, ok := numberValue(num)
		if k == "" || !ok {
			return condition{}, false
		}
		if r, ok := comparison(op, v); ok {
			return condition{expr: e, column: k, values: r}, true
		}
	}
	return condition{}, false
}

// andChains calls fn once for each chain of ANDs in a filter condition (not under a NOT), with
// the chain's root and its operands.
func (in *Input) andChains(fn func(root *parser.Expr, operands []*parser.Expr)) {
	seen := map[*parser.Expr]bool{}
	for _, root := range in.predicateRoots() {
		walkCondition(root.expr, false, func(e *parser.Expr, negated bool) {
			if e.Kind != parser.ExprAnd || seen[e] || negated {
				return
			}
			var mark func(*parser.Expr)
			mark = func(x *parser.Expr) {
				if x.Kind == parser.ExprAnd {
					seen[x] = true
					mark(x.Left)
					mark(x.Right)
				}
			}
			mark(e)
			fn(e, conjuncts(e))
		})
	}
}

// pairSpan covers two operands of a chain, from the first one's start to the second one's end.
func pairSpan(a, b *parser.Expr) diagnostics.Span {
	if b.Span.Offset < a.Span.Offset {
		a, b = b, a
	}
	s := SpanOf(a.Span)
	s.Length = b.Span.Offset + b.Span.Length - a.Span.Offset
	return s
}

func staticEvidence(in *Input, e *parser.Expr, summary string) diagnostics.Evidence {
	return diagnostics.Evidence{
		Kind:    diagnostics.EvidenceStatic,
		Summary: summary,
		Static:  &diagnostics.StaticEvidence{Span: SpanOf(e.Span), Text: in.Text(e.Span)},
	}
}

// contradiction is QRY-PRED-001: two conditions of one AND chain on the same column that no
// value satisfies (x = 1 AND x = 2, x > 5 AND x < 3, x IS NULL AND x = 1). Only comparisons of a
// column with numbers, and IS [NOT] NULL, are analyzed; anything else is not provable and skipped.
type contradiction struct{}

func (contradiction) Info() Info {
	return Info{
		ID:       "QRY-PRED-001",
		Title:    "Contradictory conditions",
		Severity: diagnostics.SeverityHigh,
		Evidence: []diagnostics.EvidenceKind{diagnostics.EvidenceStatic},
	}
}

func (contradiction) Check(in *Input) ([]diagnostics.Finding, error) {
	var out []diagnostics.Finding
	in.andChains(func(_ *parser.Expr, operands []*parser.Expr) {
		conds := conditions(operands)
		for i := range conds {
			for j := i + 1; j < len(conds); j++ {
				a, b := conds[i], conds[j]
				if a.column != b.column || !contradicts(a, b) {
					continue
				}
				at, bt := in.Text(a.expr.Span), in.Text(b.expr.Span)
				out = append(out, diagnostics.Finding{
					Title: fmt.Sprintf("\"%s\" and \"%s\" can never both be true", at, bt),
					Explanation: fmt.Sprintf("No value of %s satisfies both conditions (a comparison is never true for NULL), "+
						"so the AND that combines them is never true. In a WHERE without OR, the query returns no rows. "+
						"One of the values is probably wrong, or OR was meant.", a.column),
					Classification: diagnostics.Confirmed,
					Span:           pairSpan(a.expr, b.expr),
					Evidence: []diagnostics.Evidence{
						staticEvidence(in, a.expr, "The first condition."),
						staticEvidence(in, b.expr, "A condition on the same column that excludes every value the first allows."),
					},
				})
			}
		}
	})
	return out, nil
}

func contradicts(a, b condition) bool {
	switch {
	case a.isNull && b.isNull, a.notNul && b.notNul:
		return false
	case a.isNull || b.isNull:
		return true // IS NULL with IS NOT NULL or with any comparison
	case a.notNul || b.notNul:
		return false
	}
	return a.values.disjoint(b.values)
}

func conditions(operands []*parser.Expr) []condition {
	var out []condition
	for _, op := range operands {
		if c, ok := conditionOf(op); ok {
			out = append(out, c)
		}
	}
	return out
}

// redundancy is QRY-PRED-002: a condition of an AND chain that another one already implies
// (x > 5 AND x > 3, x = 4 AND x > 3), or the same condition written twice.
type redundancy struct{}

func (redundancy) Info() Info {
	return Info{
		ID:       "QRY-PRED-002",
		Title:    "Redundant condition",
		Severity: diagnostics.SeverityLow,
		Evidence: []diagnostics.EvidenceKind{diagnostics.EvidenceStatic},
	}
}

func (redundancy) Check(in *Input) ([]diagnostics.Finding, error) {
	var out []diagnostics.Finding
	in.andChains(func(_ *parser.Expr, operands []*parser.Expr) {
		reported := map[*parser.Expr]bool{}
		report := func(keep, drop *parser.Expr, explanation string) {
			reported[drop] = true
			kt, dt := in.Text(keep.Span), in.Text(drop.Span)
			out = append(out, diagnostics.Finding{
				Title:          fmt.Sprintf("\"%s\" adds nothing to \"%s\"", dt, kt),
				Explanation:    explanation + " Remove it, or check whether another value was meant.",
				Classification: diagnostics.Optimization,
				Span:           SpanOf(drop.Span),
				Evidence: []diagnostics.Evidence{
					staticEvidence(in, keep, "This condition is already required."),
					staticEvidence(in, drop, "This condition holds whenever the other one does."),
				},
			})
		}
		// The same condition twice, as written (whitespace aside).
		first := map[string]*parser.Expr{}
		for _, op := range operands {
			key := strings.Join(strings.Fields(in.Text(op.Span)), " ")
			if prev, ok := first[key]; ok {
				report(prev, op, "The same condition appears twice in this AND.")
				continue
			}
			first[key] = op
		}
		// A comparison implied by another on the same column.
		conds := conditions(operands)
		for i := range conds {
			for j := range conds {
				a, b := conds[i], conds[j]
				if i == j || a.column != b.column || reported[a.expr] || reported[b.expr] ||
					a.isNull || a.notNul || b.isNull || b.notNul {
					continue
				}
				// b is redundant when a implies it; of two equivalent conditions, the later one is.
				if a.values.within(b.values) && (!b.values.within(a.values) || i < j) {
					report(a.expr, b.expr, fmt.Sprintf("Every row that satisfies %s also satisfies %s.",
						in.Text(a.expr.Span), in.Text(b.expr.Span)))
				}
			}
		}
	})
	return out, nil
}
