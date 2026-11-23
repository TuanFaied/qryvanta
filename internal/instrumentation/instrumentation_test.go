package instrumentation

import (
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/diagnostics"
)

var (
	orders = diagnostics.TableRef{Database: "Sales", Schema: "dbo", Name: "Orders"}
	// A name that needs quoting: a space, a closing bracket and a quote.
	tricky = diagnostics.TableRef{Database: "Sales DB", Schema: "my schema", Name: "Order]Items'"}
)

func TestTemplates(t *testing.T) {
	cases := []struct {
		name   string
		m      diagnostics.Measurement
		sql    []string // fragments the SQL must contain
		params []Param
	}{
		{"fan-out", diagnostics.Measurement{Kind: diagnostics.MeasureFanOut, Table: tricky, Columns: []string{"Order]ID", "Line No"}},
			[]string{"FROM [Sales DB].[my schema].[Order]]Items']", "WHERE [Order]]ID] IS NOT NULL AND [Line No] IS NOT NULL",
				"GROUP BY [Order]]ID], [Line No]", "MAX(g.matches) AS max_rows_per_value"}, nil},
		{"unmatched", diagnostics.Measurement{Kind: diagnostics.MeasureUnmatched, Table: orders, Columns: []string{"CustomerID"},
			Other: &tricky, OtherColumns: []string{"Cust ID"}},
			[]string{"FROM [Sales].[dbo].[Orders] AS t", "NOT EXISTS (SELECT 1 FROM [Sales DB].[my schema].[Order]]Items'] AS o WHERE o.[Cust ID] = t.[CustomerID])",
				"t.[CustomerID] IS NULL"}, nil},
		{"nulls", diagnostics.Measurement{Kind: diagnostics.MeasureNulls, Table: tricky, Columns: []string{"x]y"}},
			[]string{"FROM [Sales DB].[my schema].[Order]]Items']", "WHERE [x]]y] IS NULL"}, nil},
		{"range end", diagnostics.Measurement{Kind: diagnostics.MeasureRangeEnd, Table: orders, Columns: []string{"OrderDate"},
			Values: []string{"2026-01-31'; DROP TABLE x; --", "2026-02-01"}},
			[]string{"WHERE [OrderDate] > @p1 AND [OrderDate] < @p2"},
			[]Param{{"p1", "2026-01-31'; DROP TABLE x; --"}, {"p2", "2026-02-01"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := Build(c.m)
			if err != nil {
				t.Fatal(err)
			}
			for _, frag := range c.sql {
				if !strings.Contains(p.SQL, frag) {
					t.Errorf("SQL does not contain %q:\n%s", frag, p.SQL)
				}
			}
			if strings.Contains(p.SQL, "DROP") {
				t.Errorf("a value was copied into the SQL:\n%s", p.SQL)
			}
			if len(p.Params) != len(c.params) {
				t.Fatalf("params = %v, want %v", p.Params, c.params)
			}
			for i := range c.params {
				if p.Params[i] != c.params[i] {
					t.Errorf("param %d = %v, want %v", i, p.Params[i], c.params[i])
				}
			}
			if p.Title == "" || p.Proves == "" {
				t.Errorf("probe has no title or explanation: %+v", p)
			}
		})
	}
}

func TestBuildRejectsBadMeasurements(t *testing.T) {
	long := strings.Repeat("x", 129)
	for name, m := range map[string]diagnostics.Measurement{
		"no columns":          {Kind: diagnostics.MeasureFanOut, Table: orders},
		"unquotable table":    {Kind: diagnostics.MeasureFanOut, Table: diagnostics.TableRef{Database: "S", Schema: "dbo", Name: long}, Columns: []string{"a"}},
		"unquotable column":   {Kind: diagnostics.MeasureNulls, Table: orders, Columns: []string{"a\x00b"}},
		"empty database":      {Kind: diagnostics.MeasureNulls, Table: diagnostics.TableRef{Schema: "dbo", Name: "Orders"}, Columns: []string{"a"}},
		"unmatched one-sided": {Kind: diagnostics.MeasureUnmatched, Table: orders, Columns: []string{"a"}},
		"range without end":   {Kind: diagnostics.MeasureRangeEnd, Table: orders, Columns: []string{"a"}},
		"unknown kind":        {Kind: "guess", Table: orders, Columns: []string{"a"}},
	} {
		if _, err := Build(m); err == nil {
			t.Errorf("%s: Build succeeded", name)
		}
	}
}

func fanOutOn(cols ...string) diagnostics.Measurement {
	return diagnostics.Measurement{Kind: diagnostics.MeasureFanOut, Table: orders, Columns: cols}
}

func TestPlanDedupesAndAppliesTheBudget(t *testing.T) {
	findings := []diagnostics.Finding{
		{Measurements: []diagnostics.Measurement{fanOutOn("A"), fanOutOn("B")}},
		{Measurements: []diagnostics.Measurement{fanOutOn("a")}}, // the same measurement, another case
		{},
		{Measurements: []diagnostics.Measurement{fanOutOn("C"), {Kind: diagnostics.MeasureFanOut}}}, // the second is invalid
	}
	probes := Plan(findings, 2)
	if len(probes) != 3 {
		t.Fatalf("probes = %+v, want 3", probes)
	}
	if probes[0].ID != "p1" || probes[1].ID != "p2" || probes[2].ID != "p3" {
		t.Errorf("ids = %s, %s, %s", probes[0].ID, probes[1].ID, probes[2].ID)
	}
	if len(probes[0].Findings) != 2 || probes[0].Findings[0] != 0 || probes[0].Findings[1] != 1 {
		t.Errorf("p1 serves findings %v, want [0 1]", probes[0].Findings)
	}
	if probes[0].OverBudget || probes[1].OverBudget || !probes[2].OverBudget {
		t.Errorf("over budget = %v %v %v, want false false true", probes[0].OverBudget, probes[1].OverBudget, probes[2].OverBudget)
	}
	if none := Plan(findings, 0); !none[0].OverBudget {
		t.Error("a budget of 0 must put every probe over budget")
	}
}

func TestInterpret(t *testing.T) {
	fan, _ := Build(fanOutOn("OrderID"))
	fan.ID = "p1"
	r, err := Interpret(fan, []string{"key_values", "repeated_values", "max_rows_per_value", "avg_rows_per_value"},
		[]any{int64(12), int64(3), int64(4), 1.75})
	if err != nil || !r.ShowsProblem || !strings.Contains(r.Summary, "up to 4 rows per value, 1.75 on average") {
		t.Fatalf("fan-out = %+v, %v", r, err)
	}
	r, _ = Interpret(fan, []string{"key_values", "repeated_values", "max_rows_per_value", "avg_rows_per_value"},
		[]any{int64(0), nil, nil, nil}) // an empty table: aggregates are NULL
	if r.ShowsProblem || r.Values["max_rows_per_value"] != "NULL" {
		t.Fatalf("empty fan-out = %+v", r)
	}

	un, _ := Build(diagnostics.Measurement{Kind: diagnostics.MeasureUnmatched, Table: orders, Columns: []string{"CustomerID"},
		Other: &diagnostics.TableRef{Database: "Sales", Schema: "dbo", Name: "Customers"}, OtherColumns: []string{"CustomerID"}})
	r, _ = Interpret(un, []string{"total_rows", "unmatched_rows", "null_key_rows"}, []any{int64(12), int64(3), "2"})
	if !r.ShowsProblem || !strings.Contains(r.Summary, "3 of 12 dbo.Orders rows have no match in dbo.Customers (2 of them with NULL in CustomerID)") {
		t.Fatalf("unmatched = %+v", r)
	}

	nulls, _ := Build(diagnostics.Measurement{Kind: diagnostics.MeasureNulls, Table: orders, Columns: []string{"CustomerID"}})
	if r, _ = Interpret(nulls, []string{"null_rows"}, []any{int64(0)}); r.ShowsProblem {
		t.Fatalf("no NULLs = %+v", r)
	}
	if _, err := Interpret(nulls, []string{"other"}, []any{int64(0)}); err == nil {
		t.Fatal("a missing column must be an error")
	}
	if _, err := Interpret(nulls, []string{"null_rows"}, []any{}); err == nil {
		t.Fatal("a short row must be an error")
	}
}

func TestApplyConfirmsDismissesAndKeeps(t *testing.T) {
	sql := "SELECT * FROM a JOIN b ON b.id = a.id"
	span := diagnostics.Span{Offset: 14, Length: 23, Line: 1, Column: 15}
	finding := func(title string, ms ...diagnostics.Measurement) diagnostics.Finding {
		return diagnostics.Finding{RuleID: "QRY-JOIN-003", Title: title, Explanation: "e", Severity: diagnostics.SeverityMedium,
			Classification: diagnostics.Potential, Span: span, Measurements: ms}
	}
	findings := []diagnostics.Finding{
		finding("confirmed", fanOutOn("A")),
		finding("dismissed", fanOutOn("B")),
		finding("failed probe", fanOutOn("C")),
		finding("not approved", fanOutOn("D")),
		finding("one of two confirms", fanOutOn("B"), fanOutOn("A")),
		finding("no probes"),
	}
	probes := Plan(findings, 10)
	results := []Result{
		{ProbeID: "p1", ShowsProblem: true, Summary: "A repeats.", Values: map[string]string{"max_rows_per_value": "4"}},
		{ProbeID: "p2", Summary: "B is unique."},
		{ProbeID: "p3", Error: "timeout"},
	}
	kept, dismissed := Apply(findings, probes, results, sql)
	var titles []string
	for _, f := range kept {
		titles = append(titles, f.Title)
	}
	if strings.Join(titles, ",") != "confirmed,failed probe,not approved,one of two confirms,no probes" {
		t.Fatalf("kept = %v", titles)
	}
	if len(dismissed) != 1 || dismissed[0].Finding.Title != "dismissed" || dismissed[0].Reason != "B is unique." {
		t.Fatalf("dismissed = %+v", dismissed)
	}
	c := kept[0]
	if c.Classification != diagnostics.Confirmed || c.Evidence[len(c.Evidence)-1].Kind != diagnostics.EvidenceProbe ||
		!strings.Contains(c.Evidence[len(c.Evidence)-1].Probe.SQL, "GROUP BY [A]") ||
		c.Evidence[len(c.Evidence)-1].Probe.Result != "max_rows_per_value = 4" {
		t.Fatalf("confirmed = %+v", c)
	}
	if kept[1].Classification != diagnostics.Potential || kept[2].Classification != diagnostics.Potential {
		t.Error("findings whose probes failed or did not run must stay as they were")
	}
	if findings[0].Classification != diagnostics.Potential {
		t.Error("Apply must not change the input")
	}
}
