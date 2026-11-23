package plan

import (
	"strings"

	"github.com/TuanFaied/qryvanta/internal/parser"
)

// MapPredicates connects the predicates of a linked plan statement to the clause of the query
// they come from, only where that is unambiguous (D6): the predicate's table columns and exactly
// one condition of the statement (an AND operand of a WHERE, HAVING or ON, in any query of the
// statement) refer to the same columns. A column matches when the names are equal and the
// query's qualifier, if any, is the plan column's alias or table. Predicates without table
// columns, or that match no condition or several, get no clause.
func MapPredicates(st *Statement, stmt parser.Statement) {
	conds := conditionsOf(stmt)
	for _, op := range allOperators(st) {
		for i := range op.Predicates {
			pr := &op.Predicates[i]
			var cols []Column
			for _, c := range pr.Columns {
				if c.Table != "" && c.Name != "" {
					cols = append(cols, c)
				}
			}
			if len(cols) == 0 {
				continue
			}
			var match *parser.Expr
			n := 0
			for _, cond := range conds {
				if sameColumns(cols, cond.columns) {
					match = cond.expr
					n++
				}
			}
			if n == 1 {
				s := match.Span
				pr.Clause = &Clause{Offset: s.Offset, Length: s.Length, Line: s.Line, Column: s.Column}
			}
		}
	}
}

func allOperators(st *Statement) []*Operator {
	var out []*Operator
	st.Walk(func(o *Operator) { out = append(out, o) })
	return out
}

// queryColumn is a column reference of the query: its qualifier (may be empty) and name.
type queryColumn struct{ qualifier, name string }

type condition struct {
	expr    *parser.Expr
	columns []queryColumn
}

// conditionsOf lists the AND operands of every WHERE, HAVING and ON of the statement, with the
// columns each refers to.
func conditionsOf(stmt parser.Statement) []condition {
	var out []condition
	add := func(e *parser.Expr) {
		for _, c := range splitAnd(e) {
			out = append(out, condition{expr: c, columns: columnsOf(c)})
		}
	}
	var query func(q *parser.Query)
	var source func(s *parser.Source)
	var statement func(s *parser.Statement)
	query = func(q *parser.Query) {
		if q == nil {
			return
		}
		add(q.Where)
		add(q.Having)
		source(q.From)
		query(q.Left)
		query(q.Right)
		for _, e := range subqueries(q.Where, q.Having) {
			query(e)
		}
	}
	source = func(s *parser.Source) {
		if s == nil {
			return
		}
		add(s.On)
		query(s.Query)
		source(s.Left)
		source(s.Right)
	}
	statement = func(s *parser.Statement) {
		for i := range s.Ctes {
			query(s.Ctes[i].Query)
		}
		query(s.Query)
		source(s.From)
		add(s.Where)
		for i := range s.Body {
			statement(&s.Body[i])
		}
		for i := range s.Else {
			statement(&s.Else[i])
		}
	}
	statement(&stmt)
	return out
}

func splitAnd(e *parser.Expr) []*parser.Expr {
	if e == nil {
		return nil
	}
	if e.Kind == parser.ExprAnd {
		return append(splitAnd(e.Left), splitAnd(e.Right)...)
	}
	return []*parser.Expr{e}
}

// subqueries returns the queries nested in expressions (EXISTS, IN, scalar subqueries).
func subqueries(es ...*parser.Expr) []*parser.Query {
	var out []*parser.Query
	var walk func(*parser.Expr)
	walk = func(e *parser.Expr) {
		if e == nil {
			return
		}
		if e.Query != nil {
			out = append(out, e.Query)
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
	}
	for _, e := range es {
		walk(e)
	}
	return out
}

// columnsOf lists the distinct column references of a condition, without entering subqueries.
func columnsOf(e *parser.Expr) []queryColumn {
	var out []queryColumn
	seen := map[queryColumn]bool{}
	var walk func(*parser.Expr)
	walk = func(e *parser.Expr) {
		if e == nil {
			return
		}
		if e.Kind == parser.ExprColumn && len(e.Parts) > 0 {
			c := queryColumn{name: strings.ToLower(e.Parts[len(e.Parts)-1])}
			if len(e.Parts) > 1 {
				c.qualifier = strings.ToLower(e.Parts[len(e.Parts)-2])
			}
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
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
	}
	walk(e)
	return out
}

func columnMatches(pc Column, qc queryColumn) bool {
	if !strings.EqualFold(pc.Name, qc.name) {
		return false
	}
	return qc.qualifier == "" || strings.EqualFold(qc.qualifier, pc.Alias) || strings.EqualFold(qc.qualifier, pc.Table)
}

// sameColumns reports whether every plan column matches a query column and the other way round.
func sameColumns(plan []Column, query []queryColumn) bool {
	if len(query) == 0 {
		return false
	}
	for _, pc := range plan {
		found := false
		for _, qc := range query {
			if columnMatches(pc, qc) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, qc := range query {
		found := false
		for _, pc := range plan {
			if columnMatches(pc, qc) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
