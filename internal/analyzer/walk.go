package analyzer

import "github.com/TuanFaied/qryvanta/internal/parser"

// Visitor has one callback per node type; nil callbacks are skipped. Callbacks receive
// pointers into the parse result and must not modify it.
type Visitor struct {
	Statement func(*parser.Statement)
	Query     func(*parser.Query)
	Source    func(*parser.Source)
	Expr      func(*parser.Expr)
}

// Walk visits every node of every analyzed batch, parents before children: statements (nested
// ones too), CTEs, queries (set operation branches, derived tables and subqueries too), sources
// and expressions.
func (in *Input) Walk(v Visitor) {
	if in.Result == nil {
		return
	}
	for bi := range in.Result.Batches {
		stmts := in.Result.Batches[bi].Statements
		for i := range stmts {
			v.statement(&stmts[i])
		}
	}
}

func (v *Visitor) statement(s *parser.Statement) {
	if v.Statement != nil {
		v.Statement(s)
	}
	for i := range s.Ctes {
		v.query(s.Ctes[i].Query)
	}
	v.query(s.Query)
	v.source(s.Target)
	v.source(s.From)
	v.expr(s.Where)
	v.expr(s.Condition)
	for i := range s.Body {
		v.statement(&s.Body[i])
	}
	for i := range s.Else {
		v.statement(&s.Else[i])
	}
}

func (v *Visitor) query(q *parser.Query) {
	if q == nil {
		return
	}
	if v.Query != nil {
		v.Query(q)
	}
	if q.Top != nil {
		v.expr(q.Top.Expr)
	}
	for i := range q.Select {
		v.expr(q.Select[i].Expr)
	}
	v.source(q.From)
	v.expr(q.Where)
	v.exprs(q.GroupBy)
	v.expr(q.Having)
	v.query(q.Left)
	v.query(q.Right)
	v.orderBy(q.OrderBy)
	v.expr(q.Offset)
	v.expr(q.Fetch)
}

func (v *Visitor) source(s *parser.Source) {
	if s == nil {
		return
	}
	if v.Source != nil {
		v.Source(s)
	}
	v.query(s.Query)
	v.source(s.Left)
	v.source(s.Right)
	v.expr(s.On)
	v.exprs(s.Args)
}

func (v *Visitor) expr(e *parser.Expr) {
	if e == nil {
		return
	}
	if v.Expr != nil {
		v.Expr(e)
	}
	v.expr(e.Left)
	v.expr(e.Right)
	v.expr(e.Operand)
	v.exprs(e.Values)
	v.query(e.Query)
	v.expr(e.Low)
	v.expr(e.High)
	v.expr(e.Pattern)
	v.expr(e.Escape)
	v.exprs(e.Args)
	if e.Over != nil {
		v.exprs(e.Over.PartitionBy)
		v.orderBy(e.Over.OrderBy)
	}
	for i := range e.Whens {
		v.expr(e.Whens[i].When)
		v.expr(e.Whens[i].Then)
	}
	v.expr(e.Else)
}

func (v *Visitor) exprs(es []*parser.Expr) {
	for _, e := range es {
		v.expr(e)
	}
}

func (v *Visitor) orderBy(items []parser.OrderItem) {
	for i := range items {
		v.expr(items[i].Expr)
	}
}
