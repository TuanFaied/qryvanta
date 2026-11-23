package plan

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/parser"
)

const ns = `xmlns="http://schemas.microsoft.com/sqlserver/2004/07/showplan"`

// doc wraps statements in a showplan document of the given version.
func doc(version, statements string) string {
	return `<ShowPlanXML ` + ns + ` Version="` + version + `" Build="16.0.1000.6"><BatchSequence><Batch><Statements>` +
		statements + `</Statements></Batch></BatchSequence></ShowPlanXML>`
}

func mustParse(t *testing.T, xml string) *Plan {
	t.Helper()
	p, err := Parse(strings.NewReader(xml), 0)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func f(v float64) *float64 { return &v }

func eq(a, b *float64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

// A parallel actual plan: a Gather Streams over a parallel hash join, with counters on 2 threads.
const parallelPlan = `<StmtSimple StatementText="SELECT o.OrderID FROM dbo.Orders o JOIN dbo.Payments p ON p.OrderID = o.OrderID" StatementId="1" StatementType="SELECT" StatementEstRows="100" StatementSubTreeCost="1.5">
<QueryPlan DegreeOfParallelism="2"><QueryTimeStats ElapsedTime="12" CpuTime="20"/>
<RelOp NodeId="0" PhysicalOp="Parallelism" LogicalOp="Gather Streams" EstimateRows="100" EstimateIO="0" EstimateCPU="0.03" AvgRowSize="11" EstimatedTotalSubtreeCost="1.5" Parallel="1" EstimateRebinds="0" EstimateRewinds="0">
 <OutputList><ColumnReference Database="[S]" Schema="[dbo]" Table="[Orders]" Alias="[o]" Column="OrderID"/></OutputList>
 <RunTimeInformation><RunTimeCountersPerThread Thread="0" ActualRows="90" ActualExecutions="1" ActualElapsedms="12" ActualCPUms="1"/></RunTimeInformation>
 <Parallelism>
 <RelOp NodeId="1" PhysicalOp="Hash Match" LogicalOp="Inner Join" EstimateRows="100" EstimateIO="0" EstimateCPU="0.2" AvgRowSize="11" EstimatedTotalSubtreeCost="1.4" Parallel="1" EstimateRebinds="0" EstimateRewinds="0">
  <Warnings><HashSpillDetails GrantedMemoryKb="1024" UsedMemoryKb="1024" WritesToTempDb="16" ReadsFromTempDb="16"/></Warnings>
  <RunTimeInformation>
   <RunTimeCountersPerThread Thread="1" ActualRows="40" ActualExecutions="1" ActualElapsedms="10" ActualCPUms="9"/>
   <RunTimeCountersPerThread Thread="2" ActualRows="50" ActualExecutions="1" ActualElapsedms="11" ActualCPUms="8"/>
  </RunTimeInformation>
  <Hash>
   <HashKeysBuild><ColumnReference Database="[S]" Schema="[dbo]" Table="[Orders]" Alias="[o]" Column="OrderID"/></HashKeysBuild>
   <HashKeysProbe><ColumnReference Database="[S]" Schema="[dbo]" Table="[Payments]" Alias="[p]" Column="OrderID"/></HashKeysProbe>
   <ProbeResidual><ScalarOperator ScalarString="[S].[dbo].[Payments].[OrderID] as [p].[OrderID]=[S].[dbo].[Orders].[OrderID] as [o].[OrderID]"><Compare CompareOp="EQ"/></ScalarOperator></ProbeResidual>
  </Hash>
 </RelOp>
 </Parallelism>
</RelOp></QueryPlan></StmtSimple>`

func TestParallelPlanSumsThreads(t *testing.T) {
	p := mustParse(t, doc("1.564", parallelPlan))
	if !p.Actual || p.Version != "1.564" || len(p.Statements) != 1 {
		t.Fatalf("plan = %+v", p)
	}
	st := p.Statements[0]
	if *st.DegreeOfParallelism != 2 || !eq(st.ElapsedMs, f(12)) || !eq(st.CPUMs, f(20)) || !eq(st.EstimatedRows, f(100)) {
		t.Fatalf("statement = %+v", st)
	}
	gather := st.Root
	if gather.PhysicalOp != "Parallelism" || !gather.Parallel || len(gather.Children) != 1 || gather.Output[0].Alias != "o" {
		t.Fatalf("root = %+v", gather)
	}
	hash := gather.Children[0]
	a := hash.Actual
	if a.Threads != 2 || !eq(a.Rows, f(90)) || !eq(a.Executions, f(2)) || !eq(a.ElapsedMs, f(21)) || !eq(a.CPUMs, f(17)) {
		t.Fatalf("hash actuals = %+v", a)
	}
	kinds := map[string]string{}
	for _, pr := range hash.Predicates {
		kinds[pr.Kind] = pr.Text
	}
	if kinds[PredicateHashBuild] != "o.OrderID" || kinds[PredicateHashProbe] != "p.OrderID" ||
		!strings.Contains(kinds[PredicateJoin], "[p].[OrderID]=") {
		t.Fatalf("predicates = %+v", hash.Predicates)
	}
	if len(hash.Warnings) != 1 || hash.Warnings[0].Kind != WarningSpill || !strings.Contains(hash.Warnings[0].Text, "16 page(s) written") {
		t.Fatalf("warnings = %+v", hash.Warnings)
	}
}

// A nested loops join whose inner seek runs once per outer row.
const nestedLoops = `<StmtSimple StatementText="SELECT 1" StatementId="1" StatementType="SELECT"><QueryPlan>
<RelOp NodeId="0" PhysicalOp="Nested Loops" LogicalOp="Inner Join" EstimateRows="9" AvgRowSize="10" EstimatedTotalSubtreeCost="0.1" EstimateRebinds="0" EstimateRewinds="0">
 <Warnings NoJoinPredicate="1"/>
 <RunTimeInformation><RunTimeCountersPerThread Thread="0" ActualRows="1500" ActualExecutions="1"/></RunTimeInformation>
 <NestedLoops Optimized="0">
  <Predicate><ScalarOperator ScalarString="[a].[x]=[b].[y]"><ColumnReference Table="[A]" Alias="[a]" Column="x"/></ScalarOperator></Predicate>
  <RelOp NodeId="1" PhysicalOp="Clustered Index Scan" LogicalOp="Clustered Index Scan" EstimateRows="500" AvgRowSize="10" EstimatedTotalSubtreeCost="0.01" EstimateRebinds="0" EstimateRewinds="0">
   <RunTimeInformation><RunTimeCountersPerThread Thread="0" ActualRows="500" ActualExecutions="1" ActualLogicalReads="4"/></RunTimeInformation>
   <IndexScan><Object Database="[S]" Schema="[dbo]" Table="[A]" Index="[PK_A]" Alias="[a]" IndexKind="Clustered"/></IndexScan>
  </RelOp>
  <RelOp NodeId="2" PhysicalOp="Clustered Index Seek" LogicalOp="Clustered Index Seek" EstimateRows="3" AvgRowSize="10" EstimatedTotalSubtreeCost="0.05" EstimateRebinds="480" EstimateRewinds="19">
   <RunTimeInformation><RunTimeCountersPerThread Thread="0" ActualRows="1500" ActualExecutions="500" ActualLogicalReads="1000"/></RunTimeInformation>
   <IndexScan>
    <Object Database="[S]" Schema="[dbo]" Table="[B]" Index="[PK_B]" Alias="[b]" IndexKind="Clustered"/>
    <SeekPredicates><SeekPredicateNew><SeekKeys>
     <StartRange ScanType="GE"><RangeColumns><ColumnReference Table="[B]" Alias="[b]" Column="k"/></RangeColumns><RangeExpressions><ScalarOperator ScalarString="(10)"/></RangeExpressions></StartRange>
     <EndRange ScanType="LT"><RangeColumns><ColumnReference Table="[B]" Alias="[b]" Column="k"/></RangeColumns><RangeExpressions><ScalarOperator ScalarString="(20)"/></RangeExpressions></EndRange>
    </SeekKeys></SeekPredicateNew></SeekPredicates>
   </IndexScan>
  </RelOp>
 </NestedLoops>
</RelOp></QueryPlan></StmtSimple>`

func TestNestedLoopsWithManyExecutions(t *testing.T) {
	p := mustParse(t, doc("1.539", nestedLoops))
	root := p.Statements[0].Root
	if len(root.Children) != 2 || root.Warnings[0].Kind != WarningNoJoinPredicate {
		t.Fatalf("root = %+v", root)
	}
	if root.Predicates[0].Kind != PredicateJoin || root.Predicates[0].Text != "[a].[x]=[b].[y]" || root.Predicates[0].Columns[0].Name != "x" {
		t.Fatalf("join predicate = %+v", root.Predicates)
	}
	seek := root.Children[1]
	if !eq(seek.Estimated.Executions, f(500)) || !eq(seek.Actual.Executions, f(500)) || !eq(seek.Actual.Rows, f(1500)) ||
		!eq(seek.Actual.LogicalReads, f(1000)) || !eq(seek.Estimated.Rows, f(3)) {
		t.Fatalf("seek = %+v %+v", seek.Estimated, seek.Actual)
	}
	if seek.Objects[0] != (Object{Database: "S", Schema: "dbo", Table: "B", Index: "PK_B", Alias: "b", IndexKind: "Clustered"}) {
		t.Fatalf("objects = %+v", seek.Objects)
	}
	if len(seek.Predicates) != 1 || seek.Predicates[0].Kind != PredicateSeek || seek.Predicates[0].Text != "b.k >= (10) AND b.k < (20)" {
		t.Fatalf("seek predicate = %+v", seek.Predicates)
	}
	// Elapsed and CPU times were not reported: absent, not zero.
	if seek.Actual.ElapsedMs != nil || seek.Actual.CPUMs != nil {
		t.Fatalf("unreported times = %v %v", seek.Actual.ElapsedMs, seek.Actual.CPUMs)
	}
}

func TestEstimatedPlanHasNoActualValues(t *testing.T) {
	est := strings.NewReplacer(
		`<RunTimeInformation><RunTimeCountersPerThread Thread="0" ActualRows="1500" ActualExecutions="1"/></RunTimeInformation>`, "",
		`<RunTimeInformation><RunTimeCountersPerThread Thread="0" ActualRows="500" ActualExecutions="1" ActualLogicalReads="4"/></RunTimeInformation>`, "",
		`<RunTimeInformation><RunTimeCountersPerThread Thread="0" ActualRows="1500" ActualExecutions="500" ActualLogicalReads="1000"/></RunTimeInformation>`, "",
	).Replace(nestedLoops)
	p := mustParse(t, doc("1.564", est))
	if p.Actual || p.Statements[0].ElapsedMs != nil {
		t.Fatal("an estimated plan reports measured values")
	}
	p.Statements[0].Walk(func(o *Operator) {
		if o.Actual != nil {
			t.Fatalf("node %d has actual values", o.NodeID)
		}
	})
	data, _ := json.Marshal(p.Statements[0].Root)
	if !strings.Contains(string(data), `"actual":null`) || strings.Contains(string(data), `"rows":0`) {
		t.Fatalf("absent values must be null in JSON, never 0: %s", data)
	}
}

func TestStatementWarnings(t *testing.T) {
	st := `<StmtSimple StatementText="SELECT * FROM dbo.T WHERE c = 1" StatementId="1" StatementType="SELECT"><QueryPlan DegreeOfParallelism="1">
<MissingIndexes><MissingIndexGroup Impact="87.5"><MissingIndex Database="[S]" Schema="[dbo]" Table="[T]">
 <ColumnGroup Usage="EQUALITY"><Column Name="[c]" ColumnId="2"/></ColumnGroup>
 <ColumnGroup Usage="INCLUDE"><Column Name="[d]" ColumnId="3"/><Column Name="[e]" ColumnId="4"/></ColumnGroup>
</MissingIndex></MissingIndexGroup></MissingIndexes>
<Warnings>
 <PlanAffectingConvert ConvertIssue="Seek Plan" Expression="CONVERT_IMPLICIT(nvarchar(10),[S].[dbo].[T].[c],0)=[@1]"/>
 <MemoryGrantWarning GrantWarningKind="Excessive Grant" RequestedMemory="10240" GrantedMemory="10240" MaxUsedMemory="512"/>
 <ColumnsWithNoStatistics><ColumnReference Column="z"/></ColumnsWithNoStatistics>
 <Wait WaitType="PAGEIOLATCH_SH" WaitTimeMs="3"/>
 <SomethingNewer/>
</Warnings>
<RelOp NodeId="0" PhysicalOp="Table Scan" LogicalOp="Table Scan" EstimateRows="1" EstimatedTotalSubtreeCost="0.1">
 <Warnings><SpillToTempDb SpillLevel="2" SpilledThreadCount="1"/></Warnings>
 <TableScan><Predicate><ScalarOperator ScalarString="[S].[dbo].[T].[c]=(1)"/></Predicate><Object Database="[S]" Schema="[dbo]" Table="[T]"/></TableScan>
</RelOp></QueryPlan></StmtSimple>`
	p := mustParse(t, doc("1.564", st))
	s := p.Statements[0]
	want := []string{WarningMissingIndex, WarningImplicitConversion, WarningMemoryGrant, WarningNoStatistics, WarningOther}
	if len(s.Warnings) != len(want) {
		t.Fatalf("warnings = %+v", s.Warnings)
	}
	for i, k := range want {
		if s.Warnings[i].Kind != k {
			t.Errorf("warning %d = %+v, want %s", i, s.Warnings[i], k)
		}
	}
	if s.Warnings[0].Text != "Missing index on dbo.T (EQUALITY: c; INCLUDE: d, e), estimated impact 87.5%." {
		t.Errorf("missing index = %q", s.Warnings[0].Text)
	}
	if !strings.Contains(s.Warnings[1].Text, `"Seek Plan"`) || !strings.Contains(s.Warnings[2].Text, "Excessive Grant: requested 10240 KB") {
		t.Errorf("warnings = %+v", s.Warnings)
	}
	op := s.Root
	if op.Warnings[0].Kind != WarningSpill || op.Warnings[0].Text != "Operator used tempdb to spill data during execution with spill level 2 and 1 spilled thread(s)." {
		t.Errorf("operator warning = %+v", op.Warnings)
	}
	if op.Predicates[0].Kind != PredicateResidual || op.Objects[0].Table != "T" {
		t.Errorf("operator = %+v", op)
	}
}

func TestStatementsWithoutPlansAndNestedStatements(t *testing.T) {
	st := `<StmtSimple StatementText="SET NOCOUNT ON" StatementId="1" StatementType="SET ON/OFF"/>
<StmtCond StatementText="IF @x = 1" StatementId="2" StatementType="COND"><Condition/><Then><Statements>
 <StmtSimple StatementText="SELECT 1" StatementId="3" StatementType="SELECT"><QueryPlan><RelOp NodeId="0" PhysicalOp="Constant Scan" LogicalOp="Constant Scan" EstimateRows="1" EstimatedTotalSubtreeCost="0"/></QueryPlan></StmtSimple>
</Statements></Then></StmtCond>`
	p := mustParse(t, doc("1.5", st))
	if len(p.Statements) != 3 || p.Statements[0].Root != nil || p.Statements[0].DegreeOfParallelism != nil ||
		p.Statements[1].Type != "COND" || p.Statements[2].Root == nil || p.Statements[2].ID != 3 {
		t.Fatalf("statements = %+v", p.Statements)
	}
}

func TestParseRejects(t *testing.T) {
	cases := map[string]struct {
		xml  string
		max  int64
		want error
	}{
		"old version":     {doc("1.2", ""), 0, ErrUnsupportedVersion},
		"unknown major":   {doc("2.0", ""), 0, ErrUnsupportedVersion},
		"no version":      {`<ShowPlanXML ` + ns + `/>`, 0, ErrUnsupportedVersion},
		"not a showplan":  {`<html><body/></html>`, 0, ErrNotShowplan},
		"empty":           {``, 0, ErrNotShowplan},
		"too large":       {doc("1.564", nestedLoops), 200, ErrTooLarge},
		"broken XML":      {`<ShowPlanXML ` + ns + ` Version="1.564"><BatchSequence>`, 0, nil},
		"mismatched tags": {`<ShowPlanXML ` + ns + ` Version="1.564"><a></b></ShowPlanXML>`, 0, nil},
	}
	for name, c := range cases {
		_, err := Parse(strings.NewReader(c.xml), c.max)
		if err == nil || (c.want != nil && !errors.Is(err, c.want)) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
	deep := doc("1.564", strings.Repeat("<a>", maxDepth+5)+strings.Repeat("</a>", maxDepth+5))
	if _, err := Parse(strings.NewReader(deep), 0); err == nil || !strings.Contains(err.Error(), "nested deeper") {
		t.Errorf("deep nesting: %v", err)
	}
}

func TestSupportedVersions(t *testing.T) {
	for _, v := range []string{"1.5", "1.6", "1.518", "1.539", "1.564", "1.600"} {
		if !supportedVersion(v) {
			t.Errorf("%s should be supported", v)
		}
	}
	for _, v := range []string{"1.4", "1.1", "1.10", "2.0", "1", "x.5", ""} {
		if supportedVersion(v) {
			t.Errorf("%s should not be supported", v)
		}
	}
}

func TestUnquote(t *testing.T) {
	for in, want := range map[string]string{"[dbo]": "dbo", "[a]]b]": "a]b", "plain": "plain", "[": "[", "": ""} {
		if got := unquote(in); got != want {
			t.Errorf("unquote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLink(t *testing.T) {
	sql := "SELECT a FROM t;\nSELECT b\n  FROM u WHERE x = 5;"
	b := parser.Batch{Statements: []parser.Statement{
		{Span: parser.Span{Offset: 0, Length: 16}},
		{Span: parser.Span{Offset: 17, Length: len(sql) - 17}},
	}}
	// The second statement was auto-parameterized: its text does not match, its position does.
	p := &Plan{Statements: []Statement{{Text: "SELECT a FROM t"}, {Text: "SELECT b FROM u WHERE x = @1"}}}
	Link(p, 3, sql, b)
	if l := p.Statements[0].Script; l == nil || *l != (ScriptLink{Batch: 3, Statement: 0, By: "text"}) {
		t.Fatalf("first = %+v", l)
	}
	if l := p.Statements[1].Script; l == nil || *l != (ScriptLink{Batch: 3, Statement: 1, By: "position"}) {
		t.Fatalf("second = %+v", l)
	}
	// Whitespace differences still match by text; extra plan statements stay unlinked.
	p = &Plan{Statements: []Statement{{Text: "SELECT b FROM u WHERE x = 5"}, {Text: "SELECT a   FROM t"}, {Text: "SELECT 9"}}}
	Link(p, 0, sql, b)
	if p.Statements[0].Script.Statement != 1 || p.Statements[1].Script.Statement != 0 || p.Statements[2].Script != nil {
		t.Fatalf("links = %+v %+v %+v", p.Statements[0].Script, p.Statements[1].Script, p.Statements[2].Script)
	}
	// Two identical statements are ambiguous by text.
	dup := "SELECT 1; SELECT 1;"
	bd := parser.Batch{Statements: []parser.Statement{{Span: parser.Span{Offset: 0, Length: 9}}, {Span: parser.Span{Offset: 10, Length: 9}}}}
	p = &Plan{Statements: []Statement{{Text: "SELECT 1"}}}
	Link(p, 0, dup, bd)
	if p.Statements[0].Script != nil {
		t.Fatalf("an ambiguous statement was linked: %+v", p.Statements[0].Script)
	}
}

func TestLinkIgnoresLeadingComments(t *testing.T) {
	sql := "-- why\n/* more */ SELECT a FROM t;"
	b := parser.Batch{Statements: []parser.Statement{{Span: parser.Span{Offset: 18, Length: 16}}}}
	p := &Plan{Statements: []Statement{{Text: "-- why\n/* more */ SELECT a FROM t"}, {Text: "SELECT 2"}}}
	Link(p, 0, sql, b)
	if l := p.Statements[0].Script; l == nil || l.By != "text" {
		t.Fatalf("link = %+v", l)
	}
}
