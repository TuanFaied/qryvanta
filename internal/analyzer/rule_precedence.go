package analyzer

import (
	"fmt"
	"strings"

	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/parser"
)

// andOrPrecedence is QRY-LOGIC-001: AND and OR mixed without parentheses. AND binds tighter, so
// `a OR b AND c` means `a OR (b AND c)`, which is often not what was meant.
type andOrPrecedence struct{}

func (andOrPrecedence) Info() Info {
	return Info{
		ID:       "QRY-LOGIC-001",
		Title:    "AND and OR mixed without parentheses",
		Severity: diagnostics.SeverityMedium,
		Evidence: []diagnostics.EvidenceKind{diagnostics.EvidenceStatic},
	}
}

func (andOrPrecedence) Check(in *Input) ([]diagnostics.Finding, error) {
	var out []diagnostics.Finding
	inChain := map[*parser.Expr]bool{} // ORs already reported as part of an enclosing chain
	in.Walk(Visitor{Expr: func(e *parser.Expr) {
		if e.Kind != parser.ExprOr || inChain[e] {
			return
		}
		// The operands of a chain of unparenthesized ORs: a OR b AND c OR d.
		var operands []*parser.Expr
		var flatten func(*parser.Expr)
		flatten = func(x *parser.Expr) {
			if x.Kind == parser.ExprOr && (x == e || !x.Parenthesized) && x.Left != nil && x.Right != nil {
				inChain[x] = true
				flatten(x.Left)
				flatten(x.Right)
				return
			}
			operands = append(operands, x)
		}
		flatten(e)
		mixed := false
		parts := make([]string, len(operands))
		for i, op := range operands {
			parts[i] = in.Text(op.Span)
			if op.Kind == parser.ExprAnd && !op.Parenthesized {
				mixed = true
				parts[i] = "(" + parts[i] + ")"
			}
		}
		if !mixed {
			return
		}
		grouping := strings.Join(parts, " OR ")
		span := SpanOf(e.Span)
		out = append(out, diagnostics.Finding{
			Title: "AND and OR are mixed without parentheses",
			Explanation: fmt.Sprintf("AND is evaluated before OR, so SQL Server reads this condition as\n%s\n"+
				"If the OR was meant to apply first, add parentheses around it. Parentheses (or IN) also "+
				"make the intended grouping clear to the next reader.", grouping),
			Classification: diagnostics.Potential,
			Span:           span,
			Evidence: []diagnostics.Evidence{{
				Kind:    diagnostics.EvidenceStatic,
				Summary: fmt.Sprintf("The condition has %d OR operands and at least one unparenthesized AND.", len(operands)),
				Static:  &diagnostics.StaticEvidence{Span: span, Text: in.Text(e.Span)},
			}},
		})
	}})
	return out, nil
}
