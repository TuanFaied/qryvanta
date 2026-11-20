package parser

// QueryModel: Qryvanta's own description of parsed T-SQL, produced by the parser and consumed by
// diagnostics (Phase 5), plans (Phase 6) and visualization (Phase 7). It mirrors the sidecar's
// Model.cs; see that file and docs/architecture D2 for the rules:
//
//   - every node has a Span (script-absolute, UTF-16 offsets; use SpanText to slice the script);
//   - parentheses are not nodes: Parenthesized is set on the wrapped node;
//   - anything not modelled has Kind "unsupported" and Type = the ScriptDom class;
//   - identifiers are unquoted; absent values are zero; unknown fields and kinds must be ignored.

// ModelVersion is the QueryModel version this package understands. It changes only when a
// field's meaning changes or a field is removed.
const ModelVersion = 1

// Span locates a node in the script. Offset and Length are UTF-16 code units; Line and Column
// are 1-based.
type Span struct {
	Offset int `json:"offset"`
	Length int `json:"length"`
	Line   int `json:"line"`
	Column int `json:"column"`
}

// ObjectName is up to server.database.schema.name.
type ObjectName struct {
	Server   string `json:"server,omitempty"`
	Database string `json:"database,omitempty"`
	Schema   string `json:"schema,omitempty"`
	Name     string `json:"name"`
}

// Statement kinds.
const (
	StmtSelect      = "select"
	StmtInsert      = "insert"
	StmtUpdate      = "update"
	StmtDelete      = "delete"
	StmtMerge       = "merge"
	StmtTruncate    = "truncate"
	StmtDDL         = "ddl"
	StmtExec        = "exec"
	StmtDeclare     = "declare"
	StmtSet         = "set"
	StmtSetOption   = "setOption"
	StmtIf          = "if"
	StmtWhile       = "while"
	StmtBlock       = "block"
	StmtTry         = "try"
	StmtTransaction = "transaction"
	StmtUse         = "use"
	StmtPrint       = "print"
	StmtWaitFor     = "waitfor"
	StmtReturn      = "return"
	StmtOther       = "other"
)

// Statement is one statement of a batch.
type Statement struct {
	Kind string `json:"kind"`
	// Type is the ScriptDom statement class (for example CreateTableStatement).
	Type string `json:"type"`
	Span Span   `json:"span"`
	Ctes []Cte  `json:"ctes,omitempty"`
	// Query is the query of a SELECT, or the source query of INSERT ... SELECT.
	Query *Query `json:"query,omitempty"`
	// Into is the SELECT ... INTO target.
	Into *ObjectName `json:"into,omitempty"`
	// Target of INSERT, UPDATE, DELETE, MERGE and TRUNCATE.
	Target *Source `json:"target,omitempty"`
	// From and Where of UPDATE and DELETE.
	From  *Source `json:"from,omitempty"`
	Where *Expr   `json:"where,omitempty"`
	// Condition of IF and WHILE.
	Condition *Expr `json:"condition,omitempty"`
	// Body: statements inside IF (then), WHILE, BEGIN...END and TRY.
	Body []Statement `json:"body,omitempty"`
	// Else: the ELSE of IF, and the CATCH block of TRY.
	Else []Statement `json:"else,omitempty"`
	// Procedure called by EXEC, unless it runs dynamic SQL.
	Procedure *ObjectName `json:"procedure,omitempty"`
	// Action of a transaction statement: begin, commit, rollback, save.
	Action string `json:"action,omitempty"`
}

// Cte is one common table expression of a WITH clause.
type Cte struct {
	Name      string   `json:"name"`
	Columns   []string `json:"columns,omitempty"`
	Query     *Query   `json:"query"`
	Recursive bool     `json:"recursive,omitempty"`
	Span      Span     `json:"span"`
}

// Query kinds.
const (
	QuerySelect = "select"
	QuerySetOp  = "setOp"
)

// Query is a query expression: one SELECT (Kind "select") or a set operation (Kind "setOp":
// Op union, unionAll, except or intersect, with Left and Right). OrderBy, Offset and Fetch belong
// to the whole query.
type Query struct {
	Kind          string `json:"kind"`
	Span          Span   `json:"span"`
	Parenthesized bool   `json:"parenthesized,omitempty"`

	Distinct bool         `json:"distinct,omitempty"`
	Top      *Top         `json:"top,omitempty"`
	Select   []SelectItem `json:"select,omitempty"`
	// From is the FROM clause as one tree; a comma list is joins with JoinType "comma".
	From    *Source `json:"from,omitempty"`
	Where   *Expr   `json:"where,omitempty"`
	GroupBy []*Expr `json:"groupBy,omitempty"`
	Having  *Expr   `json:"having,omitempty"`

	Op    string `json:"op,omitempty"`
	Left  *Query `json:"left,omitempty"`
	Right *Query `json:"right,omitempty"`

	OrderBy []OrderItem `json:"orderBy,omitempty"`
	Offset  *Expr       `json:"offset,omitempty"`
	Fetch   *Expr       `json:"fetch,omitempty"`
}

// Top is TOP (n) [PERCENT] [WITH TIES].
type Top struct {
	Expr     *Expr `json:"expr"`
	Percent  bool  `json:"percent,omitempty"`
	WithTies bool  `json:"withTies,omitempty"`
}

// SelectItem is a select-list item: Kind "expr" (Expr, Alias) or "star" (* or Qualifier.*).
type SelectItem struct {
	Kind      string   `json:"kind"`
	Expr      *Expr    `json:"expr,omitempty"`
	Alias     string   `json:"alias,omitempty"`
	Qualifier []string `json:"qualifier,omitempty"`
	Span      Span     `json:"span"`
}

// OrderItem is one ORDER BY element.
type OrderItem struct {
	Expr       *Expr `json:"expr"`
	Descending bool  `json:"descending,omitempty"`
}

// Source kinds and join types.
const (
	SourceTable    = "table"
	SourceDerived  = "derived"
	SourceJoin     = "join"
	SourceFunction = "function"
	SourceVariable = "variable"
	SourceValues   = "values"

	JoinInner      = "inner"
	JoinLeft       = "left"
	JoinRight      = "right"
	JoinFull       = "full"
	JoinCross      = "cross"
	JoinCrossApply = "crossApply"
	JoinOuterApply = "outerApply"
	// JoinComma is an old-style comma join: it never has a predicate.
	JoinComma = "comma"
)

// Source is a FROM-clause source: a table (Cte when it names a CTE in scope), a derived table
// (Query), a join (JoinType, Left, Right, On), a table-valued function (Name, Args), a table
// variable (Variable) or a VALUES list.
type Source struct {
	Kind          string `json:"kind"`
	Span          Span   `json:"span"`
	Parenthesized bool   `json:"parenthesized,omitempty"`
	Alias         string `json:"alias,omitempty"`

	Name    *ObjectName `json:"name,omitempty"`
	Cte     bool        `json:"cte,omitempty"`
	Query   *Query      `json:"query,omitempty"`
	Columns []string    `json:"columns,omitempty"`

	JoinType string  `json:"joinType,omitempty"`
	Left     *Source `json:"left,omitempty"`
	Right    *Source `json:"right,omitempty"`
	On       *Expr   `json:"on,omitempty"`

	Args     []*Expr `json:"args,omitempty"`
	Variable string  `json:"variable,omitempty"`
	Type     string  `json:"type,omitempty"`
}

// Expression kinds.
const (
	ExprAnd        = "and"
	ExprOr         = "or"
	ExprNot        = "not"
	ExprCompare    = "compare"
	ExprIn         = "in"
	ExprBetween    = "between"
	ExprLike       = "like"
	ExprIsNull     = "isNull"
	ExprExists     = "exists"
	ExprColumn     = "column"
	ExprLiteral    = "literal"
	ExprVariable   = "variable"
	ExprFunction   = "function"
	ExprSubquery   = "subquery"
	ExprArithmetic = "arithmetic"
	ExprUnary      = "unary"
	ExprCast       = "cast"
	ExprCase       = "case"

	// KindUnsupported marks a statement, query, source or expression the model does not
	// describe; Type names the ScriptDom class ("TooDeep" for expressions nested too deeply).
	KindUnsupported = "unsupported"
)

// Expr is a scalar or boolean expression. Which fields are set depends on Kind; see Model.cs
// for the full list. In short: and/or use Left and Right; not, isNull and unary use Operand;
// compare uses Op, Left and Right (or Quantifier and Query for = ANY/ALL (subquery)); in uses
// Operand with Values or Query; between uses Operand, Low and High; like uses Operand, Pattern
// and Escape; Not negates in, between, like and isNull; column has Parts (the last part is the
// column, "*" for a wildcard); literal has LiteralType and Value; function has Name, Schema,
// Args, Distinct, Aggregate and Over; cast has Name, DataType, Operand and Args (style); case
// has Operand (simple CASE), Whens and Else.
type Expr struct {
	Kind          string `json:"kind"`
	Span          Span   `json:"span"`
	Parenthesized bool   `json:"parenthesized,omitempty"`

	Op         string  `json:"op,omitempty"`
	Not        bool    `json:"not,omitempty"`
	Left       *Expr   `json:"left,omitempty"`
	Right      *Expr   `json:"right,omitempty"`
	Operand    *Expr   `json:"operand,omitempty"`
	Values     []*Expr `json:"values,omitempty"`
	Query      *Query  `json:"query,omitempty"`
	Quantifier string  `json:"quantifier,omitempty"`
	Low        *Expr   `json:"low,omitempty"`
	High       *Expr   `json:"high,omitempty"`
	Pattern    *Expr   `json:"pattern,omitempty"`
	Escape     *Expr   `json:"escape,omitempty"`

	Parts       []string `json:"parts,omitempty"`
	LiteralType string   `json:"literalType,omitempty"`
	Value       string   `json:"value,omitempty"`
	National    bool     `json:"national,omitempty"`

	Name      string  `json:"name,omitempty"`
	Schema    string  `json:"schema,omitempty"`
	Args      []*Expr `json:"args,omitempty"`
	Distinct  bool    `json:"distinct,omitempty"`
	Aggregate bool    `json:"aggregate,omitempty"`
	Over      *Over   `json:"over,omitempty"`
	DataType  string  `json:"dataType,omitempty"`

	Whens []When `json:"whens,omitempty"`
	Else  *Expr  `json:"else,omitempty"`

	Type string `json:"type,omitempty"`
}

// Over is a window specification.
type Over struct {
	PartitionBy []*Expr     `json:"partitionBy,omitempty"`
	OrderBy     []OrderItem `json:"orderBy,omitempty"`
	// Frame is the ROWS/RANGE clause as written.
	Frame string `json:"frame,omitempty"`
}

// When is one WHEN ... THEN ... of a CASE.
type When struct {
	When *Expr `json:"when"`
	Then *Expr `json:"then"`
}

// SpanText returns the script text a span covers. sql must be the script that was parsed.
func SpanText(sql string, s Span) string {
	b := ByteOffsets(sql, s.Offset, s.Offset+s.Length)
	return sql[b[0]:b[1]]
}
