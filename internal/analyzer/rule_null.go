package analyzer

import (
	"fmt"

	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/parser"
)

// nullComparison is QRY-NULL-001: `x = NULL`, `x <> NULL` and `x != NULL`. With ANSI_NULLS ON
// (the default, and the only setting future SQL Server versions support) the comparison is
// UNKNOWN for every row, so it is never true and a filter on it removes every row.
type nullComparison struct{}

func (nullComparison) Info() Info {
	return Info{
		ID:       "QRY-NULL-001",
		Title:    "Comparison with NULL using = or <>",
		Severity: diagnostics.SeverityHigh,
		Evidence: []diagnostics.EvidenceKind{diagnostics.EvidenceStatic},
	}
}

func (nullComparison) Check(in *Input) ([]diagnostics.Finding, error) {
	var out []diagnostics.Finding
	in.Walk(Visitor{Expr: func(e *parser.Expr) {
		if e.Kind != parser.ExprCompare || e.Quantifier != "" || e.Left == nil || e.Right == nil {
			return
		}
		equal := e.Op == "="
		if !equal && e.Op != "<>" && e.Op != "!=" {
			return
		}
		other := e.Left
		switch {
		case isNullLiteral(e.Right):
		case isNullLiteral(e.Left):
			other = e.Right
		default:
			return
		}
		text := in.Text(e.Span)
		test := "IS NOT NULL"
		if equal {
			test = "IS NULL"
		}
		span := SpanOf(e.Span)
		var correction *diagnostics.Correction
		if !isNullLiteral(other) { // NULL = NULL has nothing to test
			correction = &diagnostics.Correction{
				Description: "Use " + test + ".",
				Span:        span,
				Replacement: in.Text(other.Span) + " " + test,
			}
		}
		out = append(out, diagnostics.Finding{
			// Titles and summaries are plain text: the panel does not render Markdown.
			Title: fmt.Sprintf("\"%s\" is never true", text),
			Explanation: fmt.Sprintf("Comparing with NULL using %s gives UNKNOWN, not TRUE or FALSE, for every row, "+
				"so a WHERE, ON or HAVING condition on it never matches. To test for missing values, use %s.", e.Op, test),
			Classification: diagnostics.Confirmed,
			Span:           span,
			Evidence: []diagnostics.Evidence{{
				Kind:    diagnostics.EvidenceStatic,
				Summary: fmt.Sprintf("It compares with the NULL literal using %s.", e.Op),
				Static:  &diagnostics.StaticEvidence{Span: span, Text: text},
			}},
			Correction: correction,
		})
	}})
	return out, nil
}

func isNullLiteral(e *parser.Expr) bool {
	return e.Kind == parser.ExprLiteral && e.LiteralType == "null"
}
