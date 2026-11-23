// Package plan turns SQL Server showplan XML into a product-neutral operator graph (architecture
// D6): statements, each with a tree of operators and their estimated and, for actual plans,
// measured values. The plan view, the plan rules and the row-count overlay use this model and
// never the XML.
//
// Values that are not in the plan are absent (nil, null in JSON), never zero: an estimated plan
// has no actual values at all, and an operator that never ran has measured zeros.
package plan

// Plan is one showplan document.
type Plan struct {
	// Version is the showplan schema version (1.5 and later are supported); Build is the server's.
	Version string `json:"version"`
	Build   string `json:"build,omitempty"`
	// Actual is true when the plan carries run-time counters (STATISTICS XML).
	Actual     bool        `json:"actual"`
	Statements []Statement `json:"statements"`
}

// Statement is one statement of the plan, in document order (statements inside IF and WHILE
// included). Statements without a query plan (SET, DECLARE, control of flow) have no Root.
type Statement struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
	Type string `json:"type"`
	// EstimatedRows and SubtreeCost are the optimizer's estimates for the statement.
	EstimatedRows *float64 `json:"estimatedRows"`
	SubtreeCost   *float64 `json:"subtreeCost"`
	// DegreeOfParallelism is the plan's DOP (1 for a serial plan); absent without a query plan.
	DegreeOfParallelism *int `json:"degreeOfParallelism"`
	// ElapsedMs and CPUMs are the measured times of the statement (actual plans only).
	ElapsedMs *float64  `json:"elapsedMs"`
	CPUMs     *float64  `json:"cpuMs"`
	Warnings  []Warning `json:"warnings"`
	Root      *Operator `json:"root"`
	// Script links the statement to its statement in the parsed script, when unambiguous (Link).
	Script *ScriptLink `json:"script,omitempty"`
}

// Operator is one physical operator of a plan.
type Operator struct {
	NodeID     int    `json:"nodeId"`
	PhysicalOp string `json:"physicalOp"`
	LogicalOp  string `json:"logicalOp"`
	Parallel   bool   `json:"parallel"`
	// ExecutionMode is Row or Batch, as estimated.
	ExecutionMode string    `json:"executionMode,omitempty"`
	Estimated     Estimates `json:"estimated"`
	// Actual holds the measured values, summed over threads; nil for an estimated plan.
	Actual     *Actuals    `json:"actual"`
	Objects    []Object    `json:"objects"`
	Predicates []Predicate `json:"predicates"`
	Output     []Column    `json:"output"`
	Warnings   []Warning   `json:"warnings"`
	Children   []*Operator `json:"children"`
}

// Estimates are the optimizer's values for one execution of an operator, except Executions.
type Estimates struct {
	Rows *float64 `json:"rows"`
	// RowsRead is the rows an access method reads before its residual predicate.
	RowsRead *float64 `json:"rowsRead"`
	// Executions is 1 + rebinds + rewinds.
	Executions  *float64 `json:"executions"`
	RowSize     *float64 `json:"rowSize"`
	CPU         *float64 `json:"cpu"`
	IO          *float64 `json:"io"`
	SubtreeCost *float64 `json:"subtreeCost"`
}

// Actuals are measured over all executions and threads of an operator.
type Actuals struct {
	Rows         *float64 `json:"rows"`
	RowsRead     *float64 `json:"rowsRead"`
	Executions   *float64 `json:"executions"`
	ElapsedMs    *float64 `json:"elapsedMs"`
	CPUMs        *float64 `json:"cpuMs"`
	LogicalReads *float64 `json:"logicalReads"`
	// Threads is the number of threads that reported counters (several for parallel operators).
	Threads int `json:"threads"`
}

// Object is a table, view or index an operator reads or writes. Names are unquoted.
type Object struct {
	Database  string `json:"database,omitempty"`
	Schema    string `json:"schema,omitempty"`
	Table     string `json:"table,omitempty"`
	Index     string `json:"index,omitempty"`
	Alias     string `json:"alias,omitempty"`
	IndexKind string `json:"indexKind,omitempty"`
}

// Predicate kinds.
const (
	PredicateSeek      = "seek"      // the seek of an index seek
	PredicateResidual  = "residual"  // a predicate applied to each row an operator reads
	PredicateJoin      = "join"      // the join condition of a nested loops, hash or merge join
	PredicateHashBuild = "hashBuild" // the build keys of a hash join
	PredicateHashProbe = "hashProbe" // the probe keys of a hash join
)

// Predicate is a condition an operator applies, as SQL Server writes it, with the columns it uses.
type Predicate struct {
	Kind    string   `json:"kind"`
	Text    string   `json:"text"`
	Columns []Column `json:"columns"`
}

// Column is a column reference. Computed values have only a Name (Expr1002).
type Column struct {
	Database string `json:"database,omitempty"`
	Schema   string `json:"schema,omitempty"`
	Table    string `json:"table,omitempty"`
	Alias    string `json:"alias,omitempty"`
	Name     string `json:"name"`
}

// Warning kinds.
const (
	WarningImplicitConversion = "implicitConversion" // a conversion that affects cardinality or seeks
	WarningNoJoinPredicate    = "noJoinPredicate"
	WarningSpill              = "spillToTempDb"
	WarningMemoryGrant        = "memoryGrant"
	WarningMissingIndex       = "missingIndex"
	WarningNoStatistics       = "columnsWithNoStatistics"
	WarningUnmatchedIndexes   = "unmatchedIndexes"
	WarningOther              = "other"
)

// Warning is a warning SQL Server attached to a statement or an operator.
type Warning struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// ScriptLink locates a plan statement in the parsed script.
type ScriptLink struct {
	// Batch and Statement are 0-based indexes into the script's batches and their top-level
	// statements.
	Batch     int `json:"batch"`
	Statement int `json:"statement"`
	// By is "text" (the normalised text matched one statement) or "position" (the plan and the
	// batch have the same number of statements).
	By string `json:"by"`
}

// Walk calls fn for every operator of the statement, parents first.
func (s *Statement) Walk(fn func(*Operator)) {
	var walk func(*Operator)
	walk = func(o *Operator) {
		if o == nil {
			return
		}
		fn(o)
		for _, c := range o.Children {
			walk(c)
		}
	}
	walk(s.Root)
}
