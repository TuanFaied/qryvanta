//go:build parser

package app

// The static rules against the real parser: positive, negative and nested cases per rule, and
// the sample queries. Run (after building the sidecar):
//
//	QRYVANTA_PARSER_PATH=bin/qryvanta-tsqlparser.exe go test -tags parser ./internal/app/

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/parser"
)

type ruleCase struct {
	name string
	sql  string
	// want is the SQL each finding of the rule covers, in order; empty for none.
	want []string
	// fix is each finding's correction ("" for none); nil skips the check.
	fix []string
	// class is the classification of every finding; "" skips the check.
	class diagnostics.Classification
}

func runRuleCases(t *testing.T, rule string, cases []ruleCase) {
	t.Helper()
	c := parserCore(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := c.Analyze(context.Background(), tc.sql, AnalysisTarget{})
			if err != nil {
				t.Fatal(err)
			}
			if a.SyntaxErrors > 0 {
				t.Fatalf("the case does not parse: %s", tc.sql)
			}
			if len(a.Failures) > 0 {
				t.Fatalf("rule failures: %+v", a.Failures)
			}
			var got, fixes []string
			for _, f := range a.Findings {
				if f.RuleID != rule {
					continue
				}
				got = append(got, parser.SpanText(tc.sql, parser.Span{Offset: f.Span.Offset, Length: f.Span.Length}))
				fix := ""
				if f.Correction != nil {
					fix = f.Correction.Replacement
				}
				fixes = append(fixes, fix)
				if tc.class != "" && f.Classification != tc.class {
					t.Errorf("classification = %s, want %s", f.Classification, tc.class)
				}
			}
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("findings cover %q, want %q", got, tc.want)
			}
			if tc.fix != nil && strings.Join(fixes, "|") != strings.Join(tc.fix, "|") {
				t.Fatalf("corrections = %q, want %q", fixes, tc.fix)
			}
		})
	}
}

func TestRuleAndOrPrecedence(t *testing.T) {
	runRuleCases(t, "QRY-LOGIC-001", []ruleCase{
		{name: "or then and", sql: "SELECT * FROM t WHERE a = 1 OR b = 2 AND c = 3", want: []string{"a = 1 OR b = 2 AND c = 3"}, class: diagnostics.Potential},
		{name: "and then or", sql: "SELECT * FROM t WHERE a = 1 AND b = 2 OR c = 3", want: []string{"a = 1 AND b = 2 OR c = 3"}},
		{name: "one finding per chain", sql: "SELECT * FROM t WHERE a = 1 OR b = 2 OR c = 3 AND d = 4", want: []string{"a = 1 OR b = 2 OR c = 3 AND d = 4"}},
		{name: "two chains", sql: "SELECT * FROM t WHERE (a = 1 OR b = 2 AND c = 3) AND (d = 1 OR e = 2 AND f = 3)",
			want: []string{"a = 1 OR b = 2 AND c = 3", "d = 1 OR e = 2 AND f = 3"}},
		{name: "or grouped", sql: "SELECT * FROM t WHERE (a = 1 OR b = 2) AND c = 3"},
		{name: "and grouped", sql: "SELECT * FROM t WHERE a = 1 OR (b = 2 AND c = 3)"},
		{name: "only or", sql: "SELECT * FROM t WHERE a = 1 OR b = 2 OR c = 3"},
		{name: "only and", sql: "SELECT * FROM t WHERE a = 1 AND b = 2 AND c = 3"},
		{name: "in a cte", sql: "WITH c AS (SELECT a FROM t WHERE a = 1 OR b = 2 AND c = 3) SELECT * FROM c", want: []string{"a = 1 OR b = 2 AND c = 3"}},
		{name: "in exists", sql: "SELECT * FROM t WHERE EXISTS (SELECT 1 FROM u WHERE u.a = 1 OR u.b = 2 AND u.c = 3)", want: []string{"u.a = 1 OR u.b = 2 AND u.c = 3"}},
		{name: "second union branch", sql: "SELECT a FROM t UNION ALL SELECT a FROM u WHERE x = 1 OR y = 2 AND z = 3", want: []string{"x = 1 OR y = 2 AND z = 3"}},
		{name: "join on", sql: "SELECT * FROM a JOIN b ON b.id = a.id OR b.x = 1 AND a.y = 2", want: []string{"b.id = a.id OR b.x = 1 AND a.y = 2"}},
		{name: "case when", sql: "SELECT CASE WHEN a = 1 OR b = 2 AND c = 3 THEN 1 END FROM t", want: []string{"a = 1 OR b = 2 AND c = 3"}},
		{name: "having", sql: "SELECT a FROM t GROUP BY a HAVING COUNT(*) > 1 OR MAX(b) = 2 AND MIN(b) = 1", want: []string{"COUNT(*) > 1 OR MAX(b) = 2 AND MIN(b) = 1"}},
		{name: "string literal", sql: "SELECT 'a = 1 OR b = 2 AND c = 3' FROM t"},
		{name: "comment", sql: "-- WHERE a = 1 OR b = 2 AND c = 3\nSELECT * FROM t /* OR x AND y */"},
	})
}

func TestRuleNotInNull(t *testing.T) {
	runRuleCases(t, "QRY-NULL-002", []ruleCase{
		{name: "subquery", sql: "SELECT * FROM dbo.Customers AS c WHERE c.CustomerID NOT IN (SELECT o.CustomerID FROM dbo.Orders AS o)",
			want:  []string{"c.CustomerID NOT IN (SELECT o.CustomerID FROM dbo.Orders AS o)"},
			fix:   []string{"NOT EXISTS (SELECT 1 FROM dbo.Orders AS o WHERE o.CustomerID = c.CustomerID)"},
			class: diagnostics.Potential},
		{name: "subquery with where", sql: "SELECT * FROM c WHERE c.id NOT IN (SELECT o.cid FROM o WHERE o.x = 1 OR o.y = 2)",
			want: []string{"c.id NOT IN (SELECT o.cid FROM o WHERE o.x = 1 OR o.y = 2)"},
			fix:  []string{"NOT EXISTS (SELECT 1 FROM o WHERE (o.x = 1 OR o.y = 2) AND o.cid = c.id)"}},
		{name: "not of in", sql: "SELECT * FROM c WHERE NOT (c.id IN (SELECT o.cid FROM o))",
			want: []string{"NOT (c.id IN (SELECT o.cid FROM o))"}, fix: []string{"NOT EXISTS (SELECT 1 FROM o WHERE o.cid = c.id)"}},
		{name: "variable operand", sql: "SELECT 1 WHERE @id NOT IN (SELECT o.cid FROM o)",
			want: []string{"@id NOT IN (SELECT o.cid FROM o)"}, fix: []string{"NOT EXISTS (SELECT 1 FROM o WHERE o.cid = @id)"}},
		{name: "unqualified operand has no rewrite", sql: "SELECT * FROM c WHERE CustomerID NOT IN (SELECT CustomerID FROM o)",
			want: []string{"CustomerID NOT IN (SELECT CustomerID FROM o)"}, fix: []string{""}},
		{name: "same alias inside has no rewrite", sql: "SELECT * FROM t AS a WHERE a.id NOT IN (SELECT a.id FROM u AS a)",
			want: []string{"a.id NOT IN (SELECT a.id FROM u AS a)"}, fix: []string{""}},
		{name: "grouped subquery has no rewrite", sql: "SELECT * FROM c WHERE c.id NOT IN (SELECT o.cid FROM o GROUP BY o.cid)",
			want: []string{"c.id NOT IN (SELECT o.cid FROM o GROUP BY o.cid)"}, fix: []string{""}},
		{name: "null in the list", sql: "SELECT * FROM t WHERE a NOT IN (1, NULL)", want: []string{"a NOT IN (1, NULL)"}, class: diagnostics.Confirmed},
		{name: "silenced by is not null", sql: "SELECT * FROM c WHERE c.id NOT IN (SELECT o.cid FROM o WHERE o.cid IS NOT NULL)"},
		{name: "silenced, unqualified", sql: "SELECT * FROM c WHERE c.id NOT IN (SELECT cid FROM o WHERE x = 1 AND cid IS NOT NULL)"},
		{name: "is not null on another column", sql: "SELECT * FROM c WHERE c.id NOT IN (SELECT o.cid FROM o WHERE o.x IS NOT NULL)",
			want: []string{"c.id NOT IN (SELECT o.cid FROM o WHERE o.x IS NOT NULL)"}},
		{name: "literal select", sql: "SELECT * FROM c WHERE c.id NOT IN (SELECT 1 FROM o)"},
		{name: "in", sql: "SELECT * FROM c WHERE c.id IN (SELECT o.cid FROM o)"},
		{name: "not in a list", sql: "SELECT * FROM t WHERE a NOT IN (1, 2)"},
		{name: "not exists", sql: "SELECT * FROM c WHERE NOT EXISTS (SELECT 1 FROM o WHERE o.cid = c.id)"},
		{name: "in a cte", sql: "WITH x AS (SELECT * FROM c WHERE c.id NOT IN (SELECT o.cid FROM o)) SELECT * FROM x",
			want: []string{"c.id NOT IN (SELECT o.cid FROM o)"}},
		{name: "in a union branch", sql: "SELECT 1 UNION SELECT c.id FROM c WHERE c.id NOT IN (SELECT o.cid FROM o)",
			want: []string{"c.id NOT IN (SELECT o.cid FROM o)"}},
		{name: "string literal", sql: "SELECT 'x NOT IN (SELECT y FROM z)'"},
	})
}

func TestRuleMissingJoinPredicate(t *testing.T) {
	runRuleCases(t, "QRY-JOIN-001", []ruleCase{
		{name: "comma join", sql: "SELECT * FROM dbo.Orders AS o, dbo.Customers AS c WHERE o.OrderDate >= '2026-01-01'",
			want: []string{"dbo.Orders AS o, dbo.Customers AS c"}, class: diagnostics.Potential},
		{name: "comma join without where", sql: "SELECT * FROM a, b", want: []string{"a, b"}},
		{name: "comma join linked", sql: "SELECT * FROM o, c WHERE c.id = o.cid AND o.x = 1"},
		{name: "three tables, one link missing", sql: "SELECT * FROM a, b, c WHERE a.id = b.id", want: []string{"a, b, c"}},
		{name: "three tables linked", sql: "SELECT * FROM a, b, c WHERE a.id = b.id AND b.y = c.y"},
		{name: "unqualified columns cannot be checked", sql: "SELECT * FROM a, b WHERE x = y"},
		{name: "single-row aggregate", sql: "SELECT * FROM t, (SELECT COUNT(*) AS n FROM u) AS x"},
		{name: "single-row top 1", sql: "SELECT * FROM t, (SELECT TOP (1) u.a FROM u ORDER BY u.a) AS x"},
		{name: "cross join is explicit", sql: "SELECT * FROM a CROSS JOIN b"},
		{name: "apply is explicit", sql: "SELECT * FROM a CROSS APPLY (SELECT TOP (1) * FROM b WHERE b.id = a.id) AS x"},
		{name: "on one side", sql: "SELECT * FROM a JOIN b ON a.x = a.y", want: []string{"a.x = a.y"}},
		{name: "on a constant", sql: "SELECT * FROM a INNER JOIN b ON 1 = 1", want: []string{"1 = 1"}},
		{name: "left join on the right side only", sql: "SELECT * FROM a LEFT JOIN b ON b.active = 1", want: []string{"b.active = 1"}},
		{name: "on linked", sql: "SELECT * FROM a JOIN b ON b.id = a.id"},
		{name: "on with a variable and a link", sql: "SELECT * FROM a JOIN b ON b.id = a.id AND b.x = @x"},
		{name: "on with unqualified columns", sql: "SELECT * FROM a JOIN b ON id = aid"},
		{name: "nested join chain", sql: "SELECT * FROM a JOIN b ON b.id = a.id JOIN c ON c.k = c.j", want: []string{"c.k = c.j"}},
		{name: "in a subquery", sql: "SELECT * FROM t WHERE EXISTS (SELECT 1 FROM a, b WHERE a.x = t.x)", want: []string{"a, b"}},
		{name: "in a cte", sql: "WITH x AS (SELECT * FROM a JOIN b ON 1 = 1) SELECT * FROM x", want: []string{"1 = 1"}},
		{name: "update from", sql: "UPDATE a SET x = 1 FROM a, b WHERE a.y = 1", want: []string{"a, b"}},
		{name: "string literal", sql: "SELECT 'FROM a, b' FROM t"},
	})
}

func TestRuleLeftJoinCancelled(t *testing.T) {
	runRuleCases(t, "QRY-JOIN-002", []ruleCase{
		{name: "equality", sql: "SELECT * FROM a LEFT JOIN b ON b.id = a.id WHERE b.x = 1",
			want: []string{"b.x = 1"}, fix: []string{"b.id = a.id AND b.x = 1"}, class: diagnostics.Confirmed},
		{name: "or of rejecting conditions", sql: "SELECT * FROM a LEFT JOIN b ON b.id = a.id WHERE b.x = 1 OR b.y = 2",
			want: []string{"b.x = 1 OR b.y = 2"}, fix: []string{"b.id = a.id AND (b.x = 1 OR b.y = 2)"}},
		{name: "in, like, between", sql: "SELECT * FROM a LEFT JOIN b ON b.id = a.id WHERE b.x IN (1, 2) AND b.n LIKE 'a%' AND b.d BETWEEN 1 AND 2 AND a.z = 1",
			want: []string{"b.x IN (1, 2)", "b.n LIKE 'a%'", "b.d BETWEEN 1 AND 2"}},
		{name: "right join", sql: "SELECT * FROM a RIGHT JOIN b ON b.id = a.id WHERE a.x > 0", want: []string{"a.x > 0"}},
		{name: "second left join", sql: "SELECT * FROM a LEFT JOIN b ON b.id = a.id LEFT JOIN c ON c.id = b.id WHERE c.x = 1", want: []string{"c.x = 1"}},
		{name: "update from", sql: "UPDATE a SET x = 1 FROM a LEFT JOIN b ON b.id = a.id WHERE b.x = 1", want: []string{"b.x = 1"}},
		{name: "in a derived table", sql: "SELECT * FROM (SELECT a.id FROM a LEFT JOIN b ON b.id = a.id WHERE b.x <> 0) AS d", want: []string{"b.x <> 0"}},
		{name: "or is null keeps unmatched rows", sql: "SELECT * FROM a LEFT JOIN b ON b.id = a.id WHERE b.x = 1 OR b.x IS NULL"},
		{name: "anti join", sql: "SELECT * FROM a LEFT JOIN b ON b.id = a.id WHERE b.id IS NULL"},
		{name: "left side", sql: "SELECT * FROM a LEFT JOIN b ON b.id = a.id WHERE a.x = 1"},
		{name: "isnull", sql: "SELECT * FROM a LEFT JOIN b ON b.id = a.id WHERE ISNULL(b.x, 0) = 0"},
		{name: "not in a subquery", sql: "SELECT * FROM a LEFT JOIN b ON b.id = a.id WHERE b.x NOT IN (SELECT c.x FROM c)"},
		{name: "condition in on", sql: "SELECT * FROM a LEFT JOIN b ON b.id = a.id AND b.x = 1"},
		{name: "inner join", sql: "SELECT * FROM a JOIN b ON b.id = a.id WHERE b.x = 1"},
		{name: "full join is not reported", sql: "SELECT * FROM a FULL JOIN b ON b.id = a.id WHERE b.x = 1"},
		{name: "string literal", sql: "SELECT * FROM a LEFT JOIN b ON b.id = a.id WHERE a.s = 'b.x = 1'"},
	})
}

// TestSampleQueries: each buggy sample query that a static rule covers produces that rule's
// finding, and no sample (corrected or not) produces any other.
func TestSampleQueries(t *testing.T) {
	c := parserCore(t)
	want := map[string][]string{
		"04_not_in_with_null.buggy.sql":        {"QRY-NULL-002"},
		"06_and_or_precedence.buggy.sql":       {"QRY-LOGIC-001"},
		"10_missing_join_predicate.buggy.sql":  {"QRY-JOIN-001"},
		"05_date_boundary.buggy.sql":           {"QRY-DATE-002"},
		"07_count_vs_count_distinct.buggy.sql": {"QRY-COUNT-002"},
	}
	files, err := filepath.Glob(filepath.Join("..", "..", "tests", "sample_queries", "*.sql"))
	if err != nil || len(files) < 20 {
		t.Fatalf("sample queries: %v (%d files)", err, len(files))
	}
	for _, path := range files {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			a, err := c.Analyze(context.Background(), string(data), AnalysisTarget{})
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, f := range a.Findings {
				got = append(got, f.RuleID)
			}
			if len(a.Failures) > 0 || !slices.Equal(got, want[name]) {
				t.Fatalf("rules = %v (failures %+v), want %v", got, a.Failures, want[name])
			}
		})
	}
}
