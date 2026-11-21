package parser

import "strings"

// Scope is what a query can refer to at a position in the script: the sources of the query
// the position is in, then those of the queries around it, and the CTEs of its statement.
// Autocomplete uses it to know what "x." means and which columns are available.
type Scope struct {
	// Found is false when the position is not inside a statement the model describes.
	Found bool `json:"found"`
	// Sources are visible by name, innermost query first; an inner name shadows an outer one.
	Sources []ScopeSource `json:"sources"`
	// Ctes are the statement's common table expressions, whether or not FROM uses them yet
	// (WITH t AS (...) SELECT t. is written before its FROM).
	Ctes []ScopeSource `json:"ctes"`
}

// Scope source kinds.
const (
	ScopeTable    = "table"
	ScopeCte      = "cte"
	ScopeDerived  = "derived"
	ScopeFunction = "function"
	ScopeValues   = "values"
	ScopeVariable = "variable"
)

// ScopeSource is one name a query can qualify columns with.
type ScopeSource struct {
	// Name qualifies its columns: the alias, else the table, function or CTE name.
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Object is the table or function, for kinds table and function (its columns come from the
	// database's metadata).
	Object *ObjectName `json:"object,omitempty"`
	// Columns are the output columns the query defines, for cte, derived and values.
	Columns []string `json:"columns,omitempty"`
	// Tables are tables whose columns are also output columns, through SELECT * (or t.*).
	Tables []ObjectName `json:"tables,omitempty"`
	// Depth is 0 for the query at the position and grows for each enclosing query.
	Depth int `json:"depth"`
}

// ScopeAt returns the scope at a UTF-16 offset of the parsed script. res must come from a
// parse with Options{Model: true}. Names equal to ignore (a placeholder the caller inserted to
// make half-typed SQL parse) are left out of column lists.
func ScopeAt(res *Result, offset int, ignore string) Scope {
	if res == nil {
		return Scope{}
	}
	for _, b := range res.Batches {
		if st := statementAt(b.Statements, offset); st != nil {
			return scopeOf(st, offset, ignore)
		}
	}
	return Scope{}
}

func contains(s Span, offset int) bool { return offset >= s.Offset && offset <= s.Offset+s.Length }

// statementAt returns the innermost statement containing offset (inside IF, WHILE, blocks).
func statementAt(sts []Statement, offset int) *Statement {
	for i := range sts {
		st := &sts[i]
		if !contains(st.Span, offset) {
			continue
		}
		if inner := statementAt(st.Body, offset); inner != nil {
			return inner
		}
		if inner := statementAt(st.Else, offset); inner != nil {
			return inner
		}
		return st
	}
	return nil
}

type scoper struct {
	ctes   map[string]*Cte
	ignore string
	// offset is the position; a derived table containing it is not visible from inside itself.
	offset int
	// expanding guards against a CTE that selects from itself.
	expanding map[string]bool
}

func scopeOf(st *Statement, offset int, ignore string) Scope {
	sc := &scoper{ctes: map[string]*Cte{}, ignore: ignore, offset: offset, expanding: map[string]bool{}}
	for i := range st.Ctes {
		sc.ctes[strings.ToLower(st.Ctes[i].Name)] = &st.Ctes[i]
	}

	// The chain of queries around the offset, outermost first.
	var chain []*Query
	roots := []*Query{st.Query}
	for i := range st.Ctes {
		roots = append(roots, st.Ctes[i].Query)
	}
	for _, q := range roots {
		if c := queryChain(q, offset); c != nil {
			chain = c
			break
		}
	}
	if chain == nil {
		// UPDATE/DELETE: the WHERE, SET and FROM refer to the statement's own sources.
		chain = searchExpr(st.Where, offset)
		if chain == nil && st.From != nil {
			chain = searchSource(st.From, offset)
		}
	}

	out := Scope{Found: true, Sources: []ScopeSource{}, Ctes: []ScopeSource{}}
	seen := map[string]bool{}
	add := func(src ScopeSource) {
		key := strings.ToLower(src.Name)
		if src.Name == "" || seen[key] {
			return
		}
		seen[key] = true
		out.Sources = append(out.Sources, src)
	}
	depth := 0
	for i := len(chain) - 1; i >= 0; i-- {
		if chain[i].From == nil {
			continue // a set operation, or a SELECT without FROM: nothing to add at this level
		}
		for _, src := range sc.sources(chain[i].From) {
			src.Depth = depth
			add(src)
		}
		depth++
	}
	// UPDATE/DELETE target and FROM, outermost.
	if st.From != nil {
		for _, src := range sc.sources(st.From) {
			src.Depth = depth
			add(src)
		}
	}
	if st.Target != nil {
		for _, src := range sc.sources(st.Target) {
			src.Depth = depth
			add(src)
		}
	}

	for i := range st.Ctes {
		c := &st.Ctes[i]
		// A CTE cannot see itself unless it is recursive, nor the CTEs defined after it, but
		// listing them does no harm to completion; only the CTE being written is left out.
		if c.Query != nil && contains(c.Query.Span, offset) && !c.Recursive {
			continue
		}
		cols, tables := sc.cteColumns(c)
		out.Ctes = append(out.Ctes, ScopeSource{Name: c.Name, Kind: ScopeCte, Columns: cols, Tables: tables})
	}
	return out
}

// queryChain returns the queries from q down to the innermost one containing offset, or nil
// when q does not contain it.
func queryChain(q *Query, offset int) []*Query {
	if q == nil || !contains(q.Span, offset) {
		return nil
	}
	chain := []*Query{q}
	var inner []*Query
	switch {
	case q.Left != nil && contains(q.Left.Span, offset):
		inner = queryChain(q.Left, offset)
	case q.Right != nil && contains(q.Right.Span, offset):
		inner = queryChain(q.Right, offset)
	}
	if inner == nil && q.From != nil {
		inner = searchSource(q.From, offset)
	}
	if inner == nil {
		inner = searchExprs(offset, q.Where, q.Having, q.Offset, q.Fetch)
	}
	if inner == nil {
		for _, it := range q.Select {
			if inner = searchExpr(it.Expr, offset); inner != nil {
				break
			}
		}
	}
	if inner == nil {
		inner = searchExprs(offset, q.GroupBy...)
	}
	if inner == nil {
		for _, o := range q.OrderBy {
			if inner = searchExpr(o.Expr, offset); inner != nil {
				break
			}
		}
	}
	if inner == nil && q.Top != nil {
		inner = searchExpr(q.Top.Expr, offset)
	}
	return append(chain, inner...)
}

func searchSource(s *Source, offset int) []*Query {
	if s == nil || !contains(s.Span, offset) {
		return nil
	}
	switch s.Kind {
	case SourceDerived:
		return queryChain(s.Query, offset)
	case SourceJoin:
		if c := searchSource(s.Left, offset); c != nil {
			return c
		}
		if c := searchSource(s.Right, offset); c != nil {
			return c
		}
		return searchExpr(s.On, offset)
	case SourceFunction:
		return searchExprs(offset, s.Args...)
	}
	return nil
}

func searchExprs(offset int, es ...*Expr) []*Query {
	for _, e := range es {
		if c := searchExpr(e, offset); c != nil {
			return c
		}
	}
	return nil
}

// searchExpr finds a subquery containing offset inside an expression.
func searchExpr(e *Expr, offset int) []*Query {
	if e == nil || !contains(e.Span, offset) {
		return nil
	}
	if c := queryChain(e.Query, offset); c != nil {
		return c
	}
	kids := []*Expr{e.Left, e.Right, e.Operand, e.Low, e.High, e.Pattern, e.Escape, e.Else}
	kids = append(kids, e.Values...)
	kids = append(kids, e.Args...)
	for _, w := range e.Whens {
		kids = append(kids, w.When, w.Then)
	}
	if e.Over != nil {
		kids = append(kids, e.Over.PartitionBy...)
		for _, o := range e.Over.OrderBy {
			kids = append(kids, o.Expr)
		}
	}
	return searchExprs(offset, kids...)
}

// sources flattens a FROM tree into its named sources.
func (sc *scoper) sources(s *Source) []ScopeSource {
	if s == nil {
		return nil
	}
	switch s.Kind {
	case SourceJoin:
		return append(sc.sources(s.Left), sc.sources(s.Right)...)
	case SourceTable:
		if s.Name == nil {
			return nil
		}
		name := s.Name.Name
		if s.Alias != "" {
			name = s.Alias
		}
		if c, ok := sc.ctes[strings.ToLower(s.Name.Name)]; ok && s.Name.Schema == "" && s.Name.Database == "" {
			cols, tables := sc.cteColumns(c)
			return []ScopeSource{{Name: name, Kind: ScopeCte, Columns: cols, Tables: tables}}
		}
		obj := *s.Name
		return []ScopeSource{{Name: name, Kind: ScopeTable, Object: &obj}}
	case SourceDerived:
		if s.Query != nil && contains(s.Query.Span, sc.offset) {
			return nil
		}
		cols, tables := sc.outputColumns(s.Query)
		if len(s.Columns) > 0 {
			cols, tables = s.Columns, nil
		}
		return []ScopeSource{{Name: s.Alias, Kind: ScopeDerived, Columns: sc.clean(cols), Tables: tables}}
	case SourceFunction:
		if s.Name == nil {
			return nil
		}
		name := s.Name.Name
		if s.Alias != "" {
			name = s.Alias
		}
		obj := *s.Name
		src := ScopeSource{Name: name, Kind: ScopeFunction, Object: &obj}
		if len(s.Columns) > 0 {
			src.Columns = s.Columns
		}
		return []ScopeSource{src}
	case SourceValues:
		return []ScopeSource{{Name: s.Alias, Kind: ScopeValues, Columns: s.Columns}}
	case SourceVariable:
		name := s.Variable
		if s.Alias != "" {
			name = s.Alias
		}
		return []ScopeSource{{Name: name, Kind: ScopeVariable}}
	}
	return nil
}

func (sc *scoper) cteColumns(c *Cte) ([]string, []ObjectName) {
	if len(c.Columns) > 0 {
		return c.Columns, nil
	}
	key := strings.ToLower(c.Name)
	if sc.expanding[key] {
		return nil, nil
	}
	sc.expanding[key] = true
	defer delete(sc.expanding, key)
	cols, tables := sc.outputColumns(c.Query)
	return sc.clean(cols), tables
}

// outputColumns names a query's result columns: aliases and plain column references. Stars
// expand to the columns of CTEs and derived tables, and to the tables they cover (whose
// columns come from metadata). Unnamed expressions have no name and are skipped.
func (sc *scoper) outputColumns(q *Query) ([]string, []ObjectName) {
	if q == nil {
		return nil, nil
	}
	if q.Kind == QuerySetOp {
		return sc.outputColumns(q.Left)
	}
	var cols []string
	var tables []ObjectName
	for _, it := range q.Select {
		switch it.Kind {
		case "expr":
			if it.Alias != "" {
				cols = append(cols, it.Alias)
			} else if it.Expr != nil && it.Expr.Kind == ExprColumn && len(it.Expr.Parts) > 0 {
				if last := it.Expr.Parts[len(it.Expr.Parts)-1]; last != "*" {
					cols = append(cols, last)
				}
			}
		case "star":
			for _, src := range sc.sources(q.From) {
				if len(it.Qualifier) > 0 && !strings.EqualFold(it.Qualifier[len(it.Qualifier)-1], src.Name) {
					continue
				}
				cols = append(cols, src.Columns...)
				tables = append(tables, src.Tables...)
				if src.Kind == ScopeTable && src.Object != nil {
					tables = append(tables, *src.Object)
				}
			}
		}
	}
	return cols, tables
}

func (sc *scoper) clean(cols []string) []string {
	if sc.ignore == "" {
		return cols
	}
	out := cols[:0:0]
	for _, c := range cols {
		if !strings.EqualFold(c, sc.ignore) {
			out = append(out, c)
		}
	}
	return out
}
