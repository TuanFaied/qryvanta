package analyzer

import (
	"strings"

	"github.com/TuanFaied/qryvanta/internal/parser"
)

// Helpers over the QueryModel shared by the rules. Names are compared case-insensitively, as
// SQL Server does with the default collations.

// sourceNames returns the names a FROM source (or join tree) makes visible: each leaf's alias,
// or the table, CTE, function or variable name when it has none. Lower-case.
func sourceNames(s *parser.Source) []string {
	if s == nil {
		return nil
	}
	if s.Kind == parser.SourceJoin {
		return append(sourceNames(s.Left), sourceNames(s.Right)...)
	}
	switch {
	case s.Alias != "":
		return []string{strings.ToLower(s.Alias)}
	case s.Name != nil:
		return []string{strings.ToLower(s.Name.Name)}
	case s.Variable != "":
		return []string{strings.ToLower(s.Variable)}
	}
	return nil
}

// joins returns the join nodes of a FROM tree, outermost first. It does not enter derived
// tables: their queries are analyzed on their own.
func joins(s *parser.Source) []*parser.Source {
	if s == nil || s.Kind != parser.SourceJoin {
		return nil
	}
	out := []*parser.Source{s}
	out = append(out, joins(s.Left)...)
	return append(out, joins(s.Right)...)
}

// conjuncts splits e at its ANDs (parenthesized or not): the conditions that must all hold.
func conjuncts(e *parser.Expr) []*parser.Expr {
	if e == nil {
		return nil
	}
	if e.Kind == parser.ExprAnd {
		return append(conjuncts(e.Left), conjuncts(e.Right)...)
	}
	return []*parser.Expr{e}
}

// qualifier returns the lower-case table or alias a column reference names ("o" in o.ID,
// "orders" in dbo.Orders.ID); ok is false for an unqualified column or another expression.
func qualifier(e *parser.Expr) (q string, ok bool) {
	if e == nil || e.Kind != parser.ExprColumn || len(e.Parts) < 2 {
		return "", false
	}
	return strings.ToLower(e.Parts[len(e.Parts)-2]), true
}

// columnName returns the lower-case column a column reference names.
func columnName(e *parser.Expr) string {
	if e == nil || e.Kind != parser.ExprColumn || len(e.Parts) == 0 {
		return ""
	}
	return strings.ToLower(e.Parts[len(e.Parts)-1])
}

// references lists what an expression refers to, without entering subqueries.
type references struct {
	// qualifiers of qualified column references, lower-case.
	qualifiers map[string]bool
	// unqualified is true when a column has no qualifier (its table is unknown without metadata).
	unqualified bool
	// subquery is true when the expression contains a subquery.
	subquery bool
}

func referencesOf(e *parser.Expr) references {
	r := references{qualifiers: map[string]bool{}}
	var walk func(*parser.Expr)
	walk = func(e *parser.Expr) {
		if e == nil {
			return
		}
		if e.Kind == parser.ExprColumn {
			if q, ok := qualifier(e); ok {
				r.qualifiers[q] = true
			} else if columnName(e) != "*" {
				r.unqualified = true
			}
		}
		if e.Query != nil {
			r.subquery = true
		}
		for _, c := range []*parser.Expr{e.Left, e.Right, e.Operand, e.Low, e.High, e.Pattern, e.Escape, e.Else} {
			walk(c)
		}
		for _, list := range [][]*parser.Expr{e.Values, e.Args} {
			for _, c := range list {
				walk(c)
			}
		}
		for _, w := range e.Whens {
			walk(w.When)
			walk(w.Then)
		}
		if e.Over != nil {
			for _, c := range e.Over.PartitionBy {
				walk(c)
			}
			for _, o := range e.Over.OrderBy {
				walk(o.Expr)
			}
		}
	}
	walk(e)
	return r
}

// touches reports whether r refers to any of names.
func (r references) touches(names []string) bool {
	for _, n := range names {
		if r.qualifiers[n] {
			return true
		}
	}
	return false
}

// isLiteral reports whether e is a literal of the given type ("" for any type).
func isLiteral(e *parser.Expr, literalType string) bool {
	return e != nil && e.Kind == parser.ExprLiteral && (literalType == "" || e.LiteralType == literalType)
}

// queryFrom is a FROM clause with its WHERE: a SELECT's, or an UPDATE's or DELETE's.
type queryFrom struct {
	from  *parser.Source
	where *parser.Expr
}

// fromClauses lists every FROM clause of the input with its WHERE.
func (in *Input) fromClauses() []queryFrom {
	var out []queryFrom
	in.Walk(Visitor{
		Statement: func(s *parser.Statement) {
			if s.From != nil {
				out = append(out, queryFrom{s.From, s.Where})
			}
		},
		Query: func(q *parser.Query) {
			if q.From != nil {
				out = append(out, queryFrom{q.From, q.Where})
			}
		},
	})
	return out
}

// wrapOr puts parentheses around an OR condition so it can be joined with AND.
func wrapOr(e *parser.Expr, text string) string {
	if e.Kind == parser.ExprOr && !e.Parenthesized {
		return "(" + text + ")"
	}
	return text
}

// outerJoined reports whether s is on the side of an outer join that gets NULLs for rows
// without a match (the right of a LEFT JOIN, the left of a RIGHT JOIN, either side of FULL).
func outerJoined(from, s *parser.Source) bool {
	for _, j := range joins(from) {
		switch j.JoinType {
		case parser.JoinLeft, parser.JoinOuterApply:
			if containsSource(leaves(j.Right), s) {
				return true
			}
		case parser.JoinRight:
			if containsSource(leaves(j.Left), s) {
				return true
			}
		case parser.JoinFull:
			return true
		}
	}
	return false
}
