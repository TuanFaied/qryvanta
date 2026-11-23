package plan

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// DefaultMaxBytes bounds the XML Parse reads, as executor.DefaultMaxPlanBytes bounds capture.
const DefaultMaxBytes = 32 << 20

// maxDepth bounds element nesting; real plans stay far below it.
const maxDepth = 2000

var (
	// ErrTooLarge is returned for a document over the size limit.
	ErrTooLarge = errors.New("the plan is larger than the plan size limit")
	// ErrUnsupportedVersion is returned for a showplan schema this parser does not know.
	ErrUnsupportedVersion = errors.New("unsupported showplan version")
	// ErrNotShowplan is returned for XML that is not a showplan document.
	ErrNotShowplan = errors.New("not a showplan XML document")
)

// Parse reads one showplan XML document, streaming, and returns its operator graph. maxBytes
// bounds the input (0 uses DefaultMaxBytes). Unknown elements and attributes are ignored. Schema
// versions 1.5 and later of major version 1 (SQL Server 2016 to 2022, Azure SQL) are supported;
// any other version is reported as ErrUnsupportedVersion, not guessed at.
func Parse(r io.Reader, maxBytes int64) (*Plan, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	lr := &io.LimitedReader{R: r, N: maxBytes + 1}
	dec := xml.NewDecoder(lr)
	dec.Strict = true
	p := &docParser{plan: &Plan{Statements: []Statement{}}}
	for {
		tok, err := dec.Token()
		if lr.N <= 0 {
			return nil, ErrTooLarge
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid plan XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if len(p.stack) >= maxDepth {
				return nil, fmt.Errorf("invalid plan XML: nested deeper than %d elements", maxDepth)
			}
			if err := p.start(t); err != nil {
				return nil, err
			}
		case xml.EndElement:
			p.end()
		}
	}
	if !p.sawRoot {
		return nil, ErrNotShowplan
	}
	return p.plan, nil
}

// frame is an open element.
type frame struct {
	name string
	// op is set on a RelOp frame; stmt on a statement frame (index into plan.Statements).
	op   *Operator
	stmt int
	// warnings is set on a Warnings frame: where its warnings go.
	warnings *[]Warning
	// pred is set on the frame of a predicate element.
	pred *predicateBuilder
	// missing is set on a MissingIndex frame.
	missing *missingIndex
}

type docParser struct {
	plan    *Plan
	stack   []frame
	sawRoot bool
	// impact is the Impact of the MissingIndexGroup being read.
	impact string
}

func (p *docParser) parent() *frame {
	if len(p.stack) == 0 {
		return nil
	}
	return &p.stack[len(p.stack)-1]
}

// currentOp is the innermost open RelOp.
func (p *docParser) currentOp() *Operator {
	for i := len(p.stack) - 1; i >= 0; i-- {
		if p.stack[i].op != nil {
			return p.stack[i].op
		}
	}
	return nil
}

// currentStmt is the innermost open statement, or nil.
func (p *docParser) currentStmt() *Statement {
	for i := len(p.stack) - 1; i >= 0; i-- {
		if p.stack[i].name != "" && p.stack[i].stmt >= 0 && strings.HasPrefix(p.stack[i].name, "Stmt") {
			return &p.plan.Statements[p.stack[i].stmt]
		}
	}
	return nil
}

// currentPred is the innermost open predicate, or nil.
func (p *docParser) currentPred() *predicateBuilder {
	for i := len(p.stack) - 1; i >= 0; i-- {
		if p.stack[i].op != nil {
			return nil // predicates never contain operators
		}
		if p.stack[i].pred != nil {
			return p.stack[i].pred
		}
	}
	return nil
}

func (p *docParser) currentMissing() *missingIndex {
	for i := len(p.stack) - 1; i >= 0; i-- {
		if p.stack[i].missing != nil {
			return p.stack[i].missing
		}
	}
	return nil
}

func attrs(se xml.StartElement) map[string]string {
	m := make(map[string]string, len(se.Attr))
	for _, a := range se.Attr {
		m[a.Name.Local] = a.Value
	}
	return m
}

func number(a map[string]string, name string) *float64 {
	v, ok := a[name]
	if !ok {
		return nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil
	}
	return &f
}

func boolAttr(a map[string]string, name string) bool {
	v := a[name]
	return v == "1" || strings.EqualFold(v, "true")
}

// unquote turns [name] into name ([a]]b] into a]b).
func unquote(s string) string {
	if len(s) >= 2 && s[0] == '[' && s[len(s)-1] == ']' {
		return strings.ReplaceAll(s[1:len(s)-1], "]]", "]")
	}
	return s
}

func columnOf(a map[string]string) Column {
	return Column{Database: unquote(a["Database"]), Schema: unquote(a["Schema"]), Table: unquote(a["Table"]),
		Alias: unquote(a["Alias"]), Name: unquote(a["Column"])}
}

// supportedVersion accepts showplan schema 1.5 and later of major version 1. The minor version
// is not a decimal fraction (2019 is 1.539, 2017 is 1.6), so it is compared as an integer.
func supportedVersion(v string) bool {
	major, minor, ok := strings.Cut(v, ".")
	if !ok || major != "1" || minor == "" {
		return false
	}
	n, err := strconv.Atoi(minor)
	if err != nil {
		return false
	}
	// 1.5, 1.6 (2016, 2017), 1.518, 1.539 (2019), 1.564 (2022) and later.
	return n == 5 || n == 6 || n >= 500
}

func (p *docParser) start(se xml.StartElement) error {
	name := se.Name.Local
	a := attrs(se)
	f := frame{name: name, stmt: -1}
	parent := p.parent()

	switch {
	case name == "ShowPlanXML":
		p.sawRoot = true
		p.plan.Version, p.plan.Build = a["Version"], a["Build"]
		if !supportedVersion(p.plan.Version) {
			return fmt.Errorf("%w %q (supported: 1.5 and later, SQL Server 2016 to 2022 and Azure SQL)", ErrUnsupportedVersion, p.plan.Version)
		}
	case !p.sawRoot:
		return ErrNotShowplan

	case strings.HasPrefix(name, "Stmt") && parent != nil && parent.name == "Statements":
		st := Statement{Text: a["StatementText"], Type: a["StatementType"], EstimatedRows: number(a, "StatementEstRows"),
			SubtreeCost: number(a, "StatementSubTreeCost"), Warnings: []Warning{}}
		if id, err := strconv.Atoi(a["StatementId"]); err == nil {
			st.ID = id
		}
		p.plan.Statements = append(p.plan.Statements, st)
		f.stmt = len(p.plan.Statements) - 1

	case name == "QueryPlan":
		if st := p.currentStmt(); st != nil {
			if dop, err := strconv.Atoi(a["DegreeOfParallelism"]); err == nil {
				st.DegreeOfParallelism = &dop
			} else {
				one := 1
				st.DegreeOfParallelism = &one
			}
		}
	case name == "QueryTimeStats":
		if st := p.currentStmt(); st != nil {
			st.ElapsedMs, st.CPUMs = number(a, "ElapsedTime"), number(a, "CpuTime")
		}

	case name == "RelOp":
		op := &Operator{PhysicalOp: a["PhysicalOp"], LogicalOp: a["LogicalOp"], Parallel: boolAttr(a, "Parallel"),
			ExecutionMode: a["EstimatedExecutionMode"], Objects: []Object{}, Predicates: []Predicate{}, Output: []Column{},
			Warnings: []Warning{}, Children: []*Operator{}}
		op.NodeID, _ = strconv.Atoi(a["NodeId"])
		op.Estimated = Estimates{Rows: number(a, "EstimateRows"), RowsRead: number(a, "EstimatedRowsRead"),
			RowSize: number(a, "AvgRowSize"), CPU: number(a, "EstimateCPU"), IO: number(a, "EstimateIO"),
			SubtreeCost: number(a, "EstimatedTotalSubtreeCost")}
		if rb, rw := number(a, "EstimateRebinds"), number(a, "EstimateRewinds"); rb != nil || rw != nil {
			execs := 1.0
			if rb != nil {
				execs += *rb
			}
			if rw != nil {
				execs += *rw
			}
			op.Estimated.Executions = &execs
		}
		if outer := p.currentOp(); outer != nil {
			outer.Children = append(outer.Children, op)
		} else if st := p.currentStmt(); st != nil && st.Root == nil {
			st.Root = op
		}
		f.op = op

	case name == "RunTimeCountersPerThread":
		op := p.currentOp()
		if op == nil {
			break
		}
		p.plan.Actual = true
		if op.Actual == nil {
			op.Actual = &Actuals{}
		}
		act := op.Actual
		act.Threads++
		add := func(dst **float64, v *float64) {
			if v == nil {
				return
			}
			if *dst == nil {
				z := 0.0
				*dst = &z
			}
			**dst += *v
		}
		add(&act.Rows, number(a, "ActualRows"))
		add(&act.RowsRead, number(a, "ActualRowsRead"))
		add(&act.Executions, number(a, "ActualExecutions"))
		add(&act.ElapsedMs, number(a, "ActualElapsedms"))
		add(&act.CPUMs, number(a, "ActualCPUms"))
		add(&act.LogicalReads, number(a, "ActualLogicalReads"))

	case name == "Object":
		if op := p.currentOp(); op != nil && p.currentMissing() == nil {
			obj := Object{Database: unquote(a["Database"]), Schema: unquote(a["Schema"]), Table: unquote(a["Table"]),
				Index: unquote(a["Index"]), Alias: unquote(a["Alias"]), IndexKind: a["IndexKind"]}
			if !containsObject(op.Objects, obj) {
				op.Objects = append(op.Objects, obj)
			}
		}

	case name == "ColumnReference":
		col := columnOf(a)
		switch {
		case p.currentMissing() != nil:
		case p.currentPred() != nil:
			p.currentPred().column(col)
		case parent != nil && parent.name == "OutputList" && len(p.stack) >= 2 && p.stack[len(p.stack)-2].op != nil:
			p.stack[len(p.stack)-2].op.Output = append(p.stack[len(p.stack)-2].op.Output, col)
		}

	case name == "ScalarOperator":
		if pb := p.currentPred(); pb != nil {
			pb.scalar(a["ScalarString"])
		}

	case name == "Predicate", name == "ProbeResidual", name == "Residual", name == "SeekPredicates",
		name == "HashKeysBuild", name == "HashKeysProbe":
		op := p.currentOp()
		if op == nil || p.currentPred() != nil || parent == nil {
			break
		}
		f.pred = &predicateBuilder{kind: predicateKind(name, parent.name), op: op}

	case name == "Prefix", name == "StartRange", name == "EndRange":
		if pb := p.currentPred(); pb != nil && pb.kind == PredicateSeek {
			pb.beginRange(a["ScanType"])
		}
	case name == "RangeColumns", name == "RangeExpressions":
		if pb := p.currentPred(); pb != nil && pb.kind == PredicateSeek {
			pb.rangePart = name
		}

	case name == "Warnings":
		switch {
		case parent != nil && parent.name == "QueryPlan":
			if st := p.currentStmt(); st != nil {
				f.warnings = &st.Warnings
			}
		case parent != nil && parent.op != nil:
			f.warnings = &parent.op.Warnings
		}
		if f.warnings != nil && boolAttr(a, "NoJoinPredicate") {
			*f.warnings = append(*f.warnings, Warning{Kind: WarningNoJoinPredicate,
				Text: "No join predicate: every row of one input is joined with every row of the other."})
		}
	case parent != nil && parent.warnings != nil:
		if w, ok := warningOf(name, a); ok {
			*parent.warnings = append(*parent.warnings, w)
		}

	case name == "MissingIndexGroup":
		p.impact = a["Impact"]
	case name == "MissingIndex":
		f.missing = &missingIndex{table: strings.Trim(joinNames(unquote(a["Schema"]), unquote(a["Table"])), "."), impact: p.impact}
	case name == "ColumnGroup":
		if m := p.currentMissing(); m != nil {
			m.usage = a["Usage"]
		}
	case name == "Column":
		if m := p.currentMissing(); m != nil {
			m.add(unquote(a["Name"]))
		}
	}

	p.stack = append(p.stack, f)
	return nil
}

func (p *docParser) end() {
	if len(p.stack) == 0 {
		return
	}
	f := p.stack[len(p.stack)-1]
	p.stack = p.stack[:len(p.stack)-1]
	switch {
	case f.pred != nil:
		f.pred.finish()
	case f.missing != nil:
		if st := p.currentStmt(); st != nil {
			st.Warnings = append(st.Warnings, Warning{Kind: WarningMissingIndex, Text: f.missing.text()})
		}
	case f.name == "Prefix" || f.name == "StartRange" || f.name == "EndRange":
		if pb := p.currentPred(); pb != nil && pb.kind == PredicateSeek {
			pb.endRange()
		}
	}
}

func joinNames(parts ...string) string {
	var out []string
	for _, s := range parts {
		if s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, ".")
}

func containsObject(list []Object, o Object) bool {
	for _, x := range list {
		if x == o {
			return true
		}
	}
	return false
}

// predicateKind names a predicate by its element and the operator element it belongs to.
func predicateKind(name, opElement string) string {
	switch name {
	case "SeekPredicates":
		return PredicateSeek
	case "HashKeysBuild":
		return PredicateHashBuild
	case "HashKeysProbe":
		return PredicateHashProbe
	case "ProbeResidual", "Residual":
		return PredicateJoin
	}
	switch opElement {
	case "NestedLoops", "Merge", "Hash":
		return PredicateJoin
	}
	return PredicateResidual
}

// warningOf reads one child of a Warnings element.
func warningOf(name string, a map[string]string) (Warning, bool) {
	switch name {
	case "PlanAffectingConvert":
		return Warning{Kind: WarningImplicitConversion,
			Text: fmt.Sprintf("Type conversion in expression (%s) may affect %q in query plan choice.", a["Expression"], a["ConvertIssue"])}, true
	case "SpillToTempDb":
		text := "Operator used tempdb to spill data during execution"
		if lvl := a["SpillLevel"]; lvl != "" {
			text += " with spill level " + lvl
		}
		if n := a["SpilledThreadCount"]; n != "" {
			text += " and " + n + " spilled thread(s)"
		}
		return Warning{Kind: WarningSpill, Text: text + "."}, true
	case "SortSpillDetails", "HashSpillDetails", "ExchangeSpillDetails":
		text := name[:len(name)-len("SpillDetails")] + " spilled to tempdb"
		if w := a["WritesToTempDb"]; w != "" {
			text += ": " + w + " page(s) written, " + a["ReadsFromTempDb"] + " read"
		}
		return Warning{Kind: WarningSpill, Text: text + "."}, true
	case "MemoryGrantWarning":
		return Warning{Kind: WarningMemoryGrant, Text: fmt.Sprintf("%s: requested %s KB, granted %s KB, used at most %s KB.",
			a["GrantWarningKind"], a["RequestedMemory"], a["GrantedMemory"], a["MaxUsedMemory"])}, true
	case "ColumnsWithNoStatistics":
		return Warning{Kind: WarningNoStatistics, Text: "Columns with no statistics."}, true
	case "UnmatchedIndexes":
		return Warning{Kind: WarningUnmatchedIndexes, Text: "A filtered index could not be used because of parameterization."}, true
	case "Wait":
		return Warning{}, false // waits are run-time details, not warnings about the plan
	}
	return Warning{Kind: WarningOther, Text: name}, true
}

// missingIndex collects one MissingIndex element.
type missingIndex struct {
	table, impact, usage string
	groups               []string
	current              []string
}

func (m *missingIndex) add(col string) {
	if len(m.groups) == 0 || !strings.HasPrefix(m.groups[len(m.groups)-1], m.usage+":") {
		m.flush()
		m.groups = append(m.groups, m.usage+":")
	}
	m.current = append(m.current, col)
}

func (m *missingIndex) flush() {
	if len(m.groups) > 0 && len(m.current) > 0 {
		m.groups[len(m.groups)-1] += " " + strings.Join(m.current, ", ")
	}
	m.current = nil
}

func (m *missingIndex) text() string {
	m.flush()
	s := "Missing index on " + m.table + " (" + strings.Join(m.groups, "; ") + ")"
	if m.impact != "" {
		s += ", estimated impact " + m.impact + "%"
	}
	return s + "."
}

// predicateBuilder collects one predicate.
type predicateBuilder struct {
	kind    string
	op      *Operator
	text    string
	columns []Column
	// seek ranges: the scan type and the columns and expressions of the open range.
	scanType  string
	rangePart string
	rangeCols []string
	rangeExpr []string
	ranges    []string
}

func (b *predicateBuilder) column(c Column) {
	for _, x := range b.columns {
		if x == c {
			goto rangeCol
		}
	}
	b.columns = append(b.columns, c)
rangeCol:
	if b.kind == PredicateSeek && b.rangePart == "RangeColumns" {
		b.rangeCols = append(b.rangeCols, qualified(c))
	}
}

func (b *predicateBuilder) scalar(s string) {
	if s == "" {
		return
	}
	if b.kind == PredicateSeek {
		if b.rangePart == "RangeExpressions" {
			b.rangeExpr = append(b.rangeExpr, s)
		}
		return
	}
	if b.text == "" {
		b.text = s
	}
}

func (b *predicateBuilder) beginRange(scanType string) {
	b.scanType, b.rangeCols, b.rangeExpr, b.rangePart = scanType, nil, nil, ""
}

var scanOps = map[string]string{"EQ": "=", "GT": ">", "GE": ">=", "LT": "<", "LE": "<=", "IS": "IS", "IS NOT": "IS NOT"}

func (b *predicateBuilder) endRange() {
	op := scanOps[b.scanType]
	if op == "" {
		op = b.scanType
	}
	for i, c := range b.rangeCols {
		if i < len(b.rangeExpr) {
			b.ranges = append(b.ranges, c+" "+op+" "+b.rangeExpr[i])
		}
	}
	b.rangePart = ""
}

func (b *predicateBuilder) finish() {
	text := b.text
	switch b.kind {
	case PredicateSeek:
		text = strings.Join(b.ranges, " AND ")
	case PredicateHashBuild, PredicateHashProbe:
		var names []string
		for _, c := range b.columns {
			names = append(names, qualified(c))
		}
		text = strings.Join(names, ", ")
	}
	cols := b.columns
	if cols == nil {
		cols = []Column{}
	}
	b.op.Predicates = append(b.op.Predicates, Predicate{Kind: b.kind, Text: text, Columns: cols})
}

// qualified writes a column as alias.column (or table.column), as SQL Server shows it.
func qualified(c Column) string {
	switch {
	case c.Alias != "":
		return c.Alias + "." + c.Name
	case c.Table != "":
		return c.Table + "." + c.Name
	}
	return c.Name
}
