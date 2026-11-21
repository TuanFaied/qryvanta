package analyzer

import (
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/parser"
)

// dateFunctions take a date or time and return a part of it or a value computed from it.
var dateFunctions = map[string]bool{
	"YEAR": true, "MONTH": true, "DAY": true, "DATEPART": true, "DATENAME": true, "DATEADD": true,
	"DATEDIFF": true, "DATEDIFF_BIG": true, "EOMONTH": true, "DATETRUNC": true, "FORMAT": true,
}

// wrappedColumn returns the column a date function or a CAST/CONVERT is applied to, directly.
func wrappedColumn(e *parser.Expr) *parser.Expr {
	switch {
	case e == nil:
	case e.Kind == parser.ExprCast:
		if columnKey(e.Operand) != "" {
			return e.Operand
		}
	case e.Kind == parser.ExprFunction && dateFunctions[strings.ToUpper(e.Name)] && e.Schema == "":
		for _, a := range e.Args {
			if columnKey(a) != "" {
				return a
			}
		}
	}
	return nil
}

// columnFree reports whether e refers to no column (a literal, variable or function of them),
// so it can move to the other side of a comparison.
func columnFree(e *parser.Expr) bool {
	r := referencesOf(e)
	return len(r.qualifiers) == 0 && !r.unqualified && !r.subquery
}

// nonSargableDate is QRY-DATE-001: a filter that applies a date function or a conversion to a
// column (YEAR(d) = 2024, CONVERT(date, d) = …, DATEADD(day, 30, d) > GETDATE()). SQL Server
// cannot use an index on the column to find the rows, and must compute the function for every
// row. A range on the column itself can.
type nonSargableDate struct{}

func (nonSargableDate) Info() Info {
	return Info{
		ID:       "QRY-DATE-001",
		Title:    "Function on a date column in a filter",
		Severity: diagnostics.SeverityLow,
		Evidence: []diagnostics.EvidenceKind{diagnostics.EvidenceStatic},
	}
}

func (nonSargableDate) Check(in *Input) ([]diagnostics.Finding, error) {
	var out []diagnostics.Finding
	for _, root := range in.predicateRoots() {
		walkCondition(root.expr, false, func(e *parser.Expr, _ bool) {
			if e.Kind != parser.ExprCompare || e.Quantifier != "" {
				return
			}
			fn, other, op := e.Left, e.Right, e.Op
			col := wrappedColumn(fn)
			if col == nil {
				fn, other, op = e.Right, e.Left, flip(e.Op)
				col = wrappedColumn(fn)
			}
			if col == nil || !columnFree(other) {
				return
			}
			fnText, colText := in.Text(fn.Span), in.Text(col.Span)
			span := SpanOf(e.Span)
			f := diagnostics.Finding{
				Title: fmt.Sprintf("%s in a filter cannot use an index on %s", fnText, colText),
				Explanation: fmt.Sprintf("The condition applies a function to %s, so SQL Server computes it for every row "+
					"and cannot seek an index on %s. Compare the column itself with a range instead.", colText, colText),
				Classification: diagnostics.Optimization,
				Span:           span,
				Evidence: []diagnostics.Evidence{staticEvidence(in, fn,
					fmt.Sprintf("The column %s is the argument of %s.", colText, functionLabel(fn)))},
			}
			if fn.Kind == parser.ExprCast && strings.EqualFold(fn.DataType, "date") {
				f.Explanation = fmt.Sprintf("Converting %s to date is the one conversion SQL Server can still seek with, but "+
					"its row estimates are often poor. A half-open range on %s is clearer and estimates better.", colText, colText)
			}
			if rewrite, ok := dateRange(in, fn, col, op, other); ok {
				f.Correction = &diagnostics.Correction{
					Description: "Compare the column with a range. The result is the same.",
					Span:        span,
					Replacement: rewrite,
				}
			}
			out = append(out, f)
		})
	}
	return out, nil
}

func functionLabel(fn *parser.Expr) string { return strings.ToUpper(fn.Name) }

// dateRange rewrites three common forms as a range on the column:
//
//	YEAR(c) = 2024                    → c >= '20240101' AND c < '20250101'
//	CAST(c AS date) = X, CONVERT(date, c) = X → c >= X AND c < DATEADD(day, 1, X)
//	DATEADD(unit, n, c) op X          → c op DATEADD(unit, -n, X), for units of fixed length
func dateRange(in *Input, fn, col *parser.Expr, op string, other *parser.Expr) (string, bool) {
	c, x := in.Text(col.Span), in.Text(other.Span)
	switch {
	case fn.Kind == parser.ExprFunction && strings.EqualFold(fn.Name, "YEAR") && op == "=":
		y, ok := numberValue(other)
		if !ok || !y.IsInt() {
			return "", false
		}
		n, err := strconv.Atoi(y.RatString())
		if err != nil || n < 1753 || n > 9998 {
			return "", false
		}
		return fmt.Sprintf("%s >= '%04d0101' AND %s < '%04d0101'", c, n, c, n+1), true
	case fn.Kind == parser.ExprCast && strings.EqualFold(fn.DataType, "date") && op == "=" && len(fn.Args) == 0:
		return fmt.Sprintf("%s >= %s AND %s < DATEADD(day, 1, %s)", c, x, c, x), true
	case fn.Kind == parser.ExprFunction && strings.EqualFold(fn.Name, "DATEADD") && len(fn.Args) == 3 && fn.Args[2] == col:
		unit := strings.ToLower(fn.Args[0].Value)
		switch unit {
		case "day", "dd", "d", "week", "wk", "ww", "hour", "hh", "minute", "mi", "n", "second", "ss", "s":
		default:
			return "", false // months and years vary in length: c + 1 month > x is not c > x - 1 month
		}
		n, ok := numberValue(fn.Args[1])
		if !ok || !n.IsInt() {
			return "", false
		}
		if _, ok := comparison(op, n); !ok {
			return "", false
		}
		return fmt.Sprintf("%s %s DATEADD(%s, %s, %s)", c, op, fn.Args[0].Value, new(big.Rat).Neg(n).RatString(), x), true
	}
	return "", false
}

// datePattern matches a date literal without a time: 2026-01-31, 20260131, 2026/01/31.
var datePattern = regexp.MustCompile(`^(\d{4})([-/]?)(\d{2})([-/]?)(\d{2})$`)

// endOfDayPattern matches a date with the last second of the day: 2026-01-31 23:59:59[.997].
var endOfDayPattern = regexp.MustCompile(`^(\d{4}([-/]?)\d{2}[-/]?\d{2})[ T]23:59:59(\.\d+)?$`)

// betweenDates is QRY-DATE-002: BETWEEN with a date (no time) as its upper bound,
// d BETWEEN '2026-01-01' AND '2026-01-31'. When d has a time of day, the range ends at midnight
// at the start of the last day, so that day's rows are missing. An upper bound of 23:59:59 has
// the same problem with fractions of a second.
type betweenDates struct{}

func (betweenDates) Info() Info {
	return Info{
		ID:       "QRY-DATE-002",
		Title:    "BETWEEN with a date as the end of a datetime range",
		Severity: diagnostics.SeverityMedium,
		Evidence: []diagnostics.EvidenceKind{diagnostics.EvidenceStatic, diagnostics.EvidenceMetadata},
	}
}

func (betweenDates) Check(in *Input) ([]diagnostics.Finding, error) {
	var out []diagnostics.Finding
	for _, root := range in.predicateRoots() {
		walkCondition(root.expr, false, func(e *parser.Expr, _ bool) {
			if e.Kind != parser.ExprBetween || e.Not || columnKey(e.Operand) == "" || !isLiteral(e.High, "string") {
				return
			}
			high := e.High.Value
			next, ok := nextDay(high)
			if !ok {
				return
			}
			c := in.Text(e.Operand.Span)
			span := SpanOf(e.Span)
			// Metadata: a date column has no time, so BETWEEN is right; a datetime column confirms it.
			class := diagnostics.Potential
			var typed []diagnostics.Evidence
			if r, ok := in.resolveColumn(root.from, e.Operand); ok {
				switch {
				case r.column.Type == "date":
					return
				case dateTimeTypes[r.column.Type]:
					class = diagnostics.Confirmed
					typed = append(typed, columnEvidence(r, "type", r.column.DataType,
						fmt.Sprintf("%s.%s is %s, which has a time of day.", r.table.FullName(), r.column.Name, r.column.DataType)))
				}
			}
			out = append(out, diagnostics.Finding{
				Title: fmt.Sprintf("BETWEEN … AND '%s' misses most of the last day if %s has a time", high, c),
				Explanation: fmt.Sprintf("BETWEEN includes its end, but '%s' means the very start of that day (or its last "+
					"whole second). If %s is a datetime with a time of day, rows later on the last day are not "+
					"included. If it is a date column, the range is correct. A half-open range (>= the start, < the "+
					"next day) works for both.", high, c),
				Classification: class,
				Span:           span,
				Evidence: append([]diagnostics.Evidence{staticEvidence(in, e.High,
					"The upper bound is a date without a time of day, or the last whole second of a day.")}, typed...),
				Correction: &diagnostics.Correction{
					Description: "Use a half-open range that ends before the next day.",
					Span:        span,
					Replacement: fmt.Sprintf("%s >= %s AND %s < %s", c, in.Text(e.Low.Span), c, quoteLike(e.High, next)),
				},
			})
		})
	}
	return out, nil
}

// nextDay returns the day after a date-only or end-of-day literal, in the literal's own format.
func nextDay(v string) (string, bool) {
	if m := endOfDayPattern.FindStringSubmatch(v); m != nil {
		v = m[1]
	}
	m := datePattern.FindStringSubmatch(v)
	if m == nil || m[2] != m[4] {
		return "", false
	}
	sep := m[2]
	t, err := time.Parse("2006"+sep+"01"+sep+"02", v)
	if err != nil {
		return "", false
	}
	return t.AddDate(0, 0, 1).Format("2006" + sep + "01" + sep + "02"), true
}

// quoteLike writes value as a string literal of the same kind as lit (N” or ”).
func quoteLike(lit *parser.Expr, value string) string {
	if lit.National {
		return "N'" + value + "'"
	}
	return "'" + value + "'"
}
