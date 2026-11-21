package analyzer

import (
	"math/big"
	"strings"

	"github.com/TuanFaied/qryvanta/internal/parser"
)

// predicateRoot is a filter condition with the FROM clause its columns refer to.
type predicateRoot struct {
	expr *parser.Expr
	from *parser.Source
}

// predicateRoots lists every filter condition of the input: WHERE and HAVING of each query,
// the ON of each join, and the WHERE of UPDATE and DELETE.
func (in *Input) predicateRoots() []predicateRoot {
	var out []predicateRoot
	add := func(e *parser.Expr, from *parser.Source) {
		if e != nil {
			out = append(out, predicateRoot{e, from})
		}
	}
	addFrom := func(from *parser.Source) {
		for _, j := range joins(from) {
			add(j.On, from)
		}
	}
	in.Walk(Visitor{
		Statement: func(s *parser.Statement) {
			if s.Kind == parser.StmtUpdate || s.Kind == parser.StmtDelete {
				from := s.From
				if from == nil {
					from = s.Target
				}
				add(s.Where, from)
				addFrom(s.From)
			}
		},
		Query: func(q *parser.Query) {
			add(q.Where, q.From)
			add(q.Having, q.From)
			addFrom(q.From)
		},
	})
	return out
}

// walkCondition visits e and the AND, OR and NOT operands below it, without entering
// subqueries or other expressions. negated says whether a NOT is above the node.
func walkCondition(e *parser.Expr, negated bool, fn func(e *parser.Expr, negated bool)) {
	if e == nil {
		return
	}
	fn(e, negated)
	switch e.Kind {
	case parser.ExprAnd, parser.ExprOr:
		walkCondition(e.Left, negated, fn)
		walkCondition(e.Right, negated, fn)
	case parser.ExprNot:
		walkCondition(e.Operand, !negated, fn)
	}
}

// numberValue returns the value of a numeric literal (integer, numeric or real), with an
// optional unary minus or plus.
func numberValue(e *parser.Expr) (*big.Rat, bool) {
	if e == nil {
		return nil, false
	}
	if e.Kind == parser.ExprUnary && (e.Op == "-" || e.Op == "+") {
		v, ok := numberValue(e.Operand)
		if ok && e.Op == "-" {
			v.Neg(v)
		}
		return v, ok
	}
	if e.Kind != parser.ExprLiteral {
		return nil, false
	}
	switch e.LiteralType {
	case "integer", "numeric", "real":
		v, ok := new(big.Rat).SetString(e.Value)
		return v, ok
	}
	return nil, false
}

// columnKey identifies a column reference as written (lower-case, parts joined with dots).
func columnKey(e *parser.Expr) string {
	if e == nil || e.Kind != parser.ExprColumn || len(e.Parts) == 0 || columnName(e) == "*" {
		return ""
	}
	return strings.ToLower(strings.Join(e.Parts, "."))
}

// rangeSet is the set of values a simple condition on one column allows: an interval (lo, hi;
// nil means unbounded), or every value but ne. Every comparison with NULL is UNKNOWN, so a
// rangeSet never contains NULL.
type rangeSet struct {
	lo, hi       *big.Rat
	loInc, hiInc bool
	ne           *big.Rat
}

// comparison converts `column op number` into a rangeSet.
func comparison(op string, v *big.Rat) (rangeSet, bool) {
	switch op {
	case "=":
		return rangeSet{lo: v, hi: v, loInc: true, hiInc: true}, true
	case "<>", "!=":
		return rangeSet{ne: v}, true
	case ">":
		return rangeSet{lo: v}, true
	case ">=", "!<":
		return rangeSet{lo: v, loInc: true}, true
	case "<":
		return rangeSet{hi: v}, true
	case "<=", "!>":
		return rangeSet{hi: v, hiInc: true}, true
	}
	return rangeSet{}, false
}

// flip turns `number op column` into `column op' number`.
func flip(op string) string {
	switch op {
	case ">":
		return "<"
	case "<":
		return ">"
	case ">=":
		return "<="
	case "<=":
		return ">="
	case "!<":
		return "!>"
	case "!>":
		return "!<"
	}
	return op
}

func (r rangeSet) contains(v *big.Rat) bool {
	if r.ne != nil {
		return r.ne.Cmp(v) != 0
	}
	if r.lo != nil {
		if c := v.Cmp(r.lo); c < 0 || (c == 0 && !r.loInc) {
			return false
		}
	}
	if r.hi != nil {
		if c := v.Cmp(r.hi); c > 0 || (c == 0 && !r.hiInc) {
			return false
		}
	}
	return true
}

// point returns the single value of an equality.
func (r rangeSet) point() (*big.Rat, bool) {
	if r.ne == nil && r.lo != nil && r.hi != nil && r.lo.Cmp(r.hi) == 0 {
		return r.lo, true
	}
	return nil, false
}

// disjoint reports whether no value satisfies both.
func (r rangeSet) disjoint(o rangeSet) bool {
	if r.ne != nil || o.ne != nil {
		a, b := r, o
		if a.ne == nil {
			a, b = b, a
		}
		p, ok := b.point()
		return ok && p.Cmp(a.ne) == 0
	}
	// The larger lower bound against the smaller upper bound.
	lo, loInc := r.lo, r.loInc
	if o.lo != nil && (lo == nil || o.lo.Cmp(lo) > 0 || (o.lo.Cmp(lo) == 0 && !o.loInc)) {
		lo, loInc = o.lo, o.loInc
	}
	hi, hiInc := r.hi, r.hiInc
	if o.hi != nil && (hi == nil || o.hi.Cmp(hi) < 0 || (o.hi.Cmp(hi) == 0 && !o.hiInc)) {
		hi, hiInc = o.hi, o.hiInc
	}
	if lo == nil || hi == nil {
		return false
	}
	c := lo.Cmp(hi)
	return c > 0 || (c == 0 && !(loInc && hiInc))
}

// within reports whether every value of r is in o (r implies o).
func (r rangeSet) within(o rangeSet) bool {
	if o.ne != nil {
		if r.ne != nil {
			return r.ne.Cmp(o.ne) == 0
		}
		return !r.contains(o.ne)
	}
	if r.ne != nil {
		return o.lo == nil && o.hi == nil
	}
	if o.lo != nil {
		if r.lo == nil {
			return false
		}
		if c := r.lo.Cmp(o.lo); c < 0 || (c == 0 && r.loInc && !o.loInc) {
			return false
		}
	}
	if o.hi != nil {
		if r.hi == nil {
			return false
		}
		if c := r.hi.Cmp(o.hi); c > 0 || (c == 0 && r.hiInc && !o.hiInc) {
			return false
		}
	}
	return true
}
