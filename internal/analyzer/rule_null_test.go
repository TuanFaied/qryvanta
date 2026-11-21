package analyzer

import (
	"context"
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/parser"
)

// span finds the first occurrence of part in sql (ASCII only).
func span(sql, part string) parser.Span {
	i := strings.Index(sql, part)
	if i < 0 {
		panic("no " + part)
	}
	return parser.Span{Offset: i, Length: len(part), Line: 1, Column: i + 1}
}

// whereInput is SELECT a FROM t WHERE <left> <op> <right>, modelled by hand.
func whereInput(left, op, right string) *Input {
	cond := left + " " + op + " " + right
	sql := "SELECT a FROM t WHERE " + cond
	lit := func(text string) *parser.Expr {
		if text == "NULL" {
			return &parser.Expr{Kind: parser.ExprLiteral, LiteralType: "null", Span: span(sql, text)}
		}
		return &parser.Expr{Kind: parser.ExprColumn, Parts: []string{text}, Span: span(sql, text)}
	}
	l, r := lit(left), lit(right)
	if left == right { // NULL = NULL: the right one is the second occurrence
		r.Span = parser.Span{Offset: len(sql) - len(right), Length: len(right), Line: 1, Column: len(sql) - len(right) + 1}
	}
	where := &parser.Expr{Kind: parser.ExprCompare, Op: op, Left: l, Right: r, Span: span(sql, cond)}
	stmt := parser.Statement{Kind: parser.StmtSelect, Span: parser.Span{Length: len(sql), Line: 1, Column: 1}, Query: &parser.Query{
		Kind:  parser.QuerySelect,
		From:  &parser.Source{Kind: parser.SourceTable, Name: &parser.ObjectName{Name: "t"}, Span: span(sql, "t ")},
		Where: where,
	}}
	return &Input{SQL: sql, Result: &parser.Result{Batches: []parser.Batch{{Text: sql, Length: len(sql), Statements: []parser.Statement{stmt}}}}}
}

func TestNullComparison(t *testing.T) {
	cases := []struct {
		left, op, right string
		want            string // the correction, "" for none, "-" for no finding
	}{
		{"b", "=", "NULL", "b IS NULL"},
		{"b", "<>", "NULL", "b IS NOT NULL"},
		{"b", "!=", "NULL", "b IS NOT NULL"},
		{"NULL", "=", "b", "b IS NULL"},
		{"NULL", "=", "NULL", ""},
		{"b", ">", "NULL", "-"}, // also UNKNOWN, but QRY-NULL-001 is about = and <>
		{"b", "=", "c", "-"},
	}
	for _, c := range cases {
		t.Run(c.left+c.op+c.right, func(t *testing.T) {
			in := whereInput(c.left, c.op, c.right)
			rep, err := NewEngine(Default()).Analyze(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			if len(rep.Failures) > 0 {
				t.Fatalf("failures: %+v", rep.Failures)
			}
			if c.want == "-" {
				if len(rep.Findings) != 0 {
					t.Fatalf("findings = %+v, want none", rep.Findings)
				}
				return
			}
			if len(rep.Findings) != 1 {
				t.Fatalf("findings = %+v, want one", rep.Findings)
			}
			f := rep.Findings[0]
			cond := c.left + " " + c.op + " " + c.right
			if f.RuleID != "QRY-NULL-001" || f.Classification != diagnostics.Confirmed || f.Severity != diagnostics.SeverityHigh {
				t.Errorf("finding = %+v", f)
			}
			if got := parser.SpanText(in.SQL, parser.Span{Offset: f.Span.Offset, Length: f.Span.Length}); got != cond {
				t.Errorf("span covers %q, want %q", got, cond)
			}
			if len(f.Evidence) != 1 || f.Evidence[0].Static == nil || f.Evidence[0].Static.Text != cond {
				t.Errorf("evidence = %+v", f.Evidence)
			}
			switch {
			case c.want == "" && f.Correction != nil:
				t.Errorf("correction = %+v, want none", f.Correction)
			case c.want != "" && (f.Correction == nil || f.Correction.Replacement != c.want):
				t.Errorf("correction = %+v, want %q", f.Correction, c.want)
			}
		})
	}
}

func TestWalkReachesNestedNodes(t *testing.T) {
	col := func(name string) *parser.Expr { return &parser.Expr{Kind: parser.ExprColumn, Parts: []string{name}} }
	sub := &parser.Query{Kind: parser.QuerySelect, Where: col("in_subquery")}
	in := &Input{Result: &parser.Result{Batches: []parser.Batch{{Statements: []parser.Statement{{
		Kind: parser.StmtIf,
		Body: []parser.Statement{{
			Kind: parser.StmtSelect,
			Ctes: []parser.Cte{{Name: "c", Query: &parser.Query{Kind: parser.QuerySelect, Where: col("in_cte")}}},
			Query: &parser.Query{
				Kind: parser.QuerySetOp,
				Left: &parser.Query{Kind: parser.QuerySelect, From: &parser.Source{
					Kind: parser.SourceJoin, On: col("in_on"),
					Left:  &parser.Source{Kind: parser.SourceDerived, Query: &parser.Query{Kind: parser.QuerySelect, Having: col("in_derived")}},
					Right: &parser.Source{Kind: parser.SourceTable},
				}},
				Right: &parser.Query{Kind: parser.QuerySelect, Select: []parser.SelectItem{{Kind: "expr", Expr: &parser.Expr{
					Kind:  parser.ExprCase,
					Whens: []parser.When{{When: &parser.Expr{Kind: parser.ExprExists, Query: sub}, Then: col("in_then")}},
				}}}},
				OrderBy: []parser.OrderItem{{Expr: &parser.Expr{Kind: parser.ExprFunction, Over: &parser.Over{PartitionBy: []*parser.Expr{col("in_over")}}}}},
			},
		}},
		Else: []parser.Statement{{Kind: parser.StmtDelete, Where: col("in_else")}},
	}}}}}}
	var seen []string
	in.Walk(Visitor{Expr: func(e *parser.Expr) {
		if e.Kind == parser.ExprColumn {
			seen = append(seen, e.Parts[0])
		}
	}})
	got := strings.Join(seen, ",")
	for _, want := range []string{"in_cte", "in_derived", "in_on", "in_subquery", "in_then", "in_over", "in_else"} {
		if !strings.Contains(got, want) {
			t.Errorf("Walk did not reach %s (saw %s)", want, got)
		}
	}
}
