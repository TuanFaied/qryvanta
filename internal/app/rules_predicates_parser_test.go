//go:build parser

package app

// Predicate, date and COUNT rules against the real parser (#68). runRuleCases is in
// rules_parser_test.go.

import (
	"testing"

	"github.com/TuanFaied/qryvanta/internal/diagnostics"
)

func TestRuleContradiction(t *testing.T) {
	runRuleCases(t, "QRY-PRED-001", []ruleCase{
		{name: "two equalities", sql: "SELECT * FROM t WHERE x = 1 AND x = 2", want: []string{"x = 1 AND x = 2"}, class: diagnostics.Confirmed},
		{name: "empty range", sql: "SELECT * FROM t WHERE x > 5 AND x < 3", want: []string{"x > 5 AND x < 3"}},
		{name: "touching exclusive bounds", sql: "SELECT * FROM t WHERE x > 5 AND x <= 5", want: []string{"x > 5 AND x <= 5"}},
		{name: "is null and a comparison", sql: "SELECT * FROM t WHERE t.x IS NULL AND t.x = 1", want: []string{"t.x IS NULL AND t.x = 1"}},
		{name: "is null and is not null", sql: "SELECT * FROM t WHERE x IS NULL AND y = 2 AND x IS NOT NULL", want: []string{"x IS NULL AND y = 2 AND x IS NOT NULL"}},
		{name: "equal and not equal", sql: "SELECT * FROM t WHERE x = 3 AND x <> 3", want: []string{"x = 3 AND x <> 3"}},
		{name: "literal on the left", sql: "SELECT * FROM t WHERE 10 < x AND x < 5", want: []string{"10 < x AND x < 5"}},
		{name: "negative and decimal", sql: "SELECT * FROM t WHERE x = -1.5 AND x >= 0", want: []string{"x = -1.5 AND x >= 0"}},
		{name: "same value written differently", sql: "SELECT * FROM t WHERE x = 1 AND x = 1.0"},
		{name: "overlapping range", sql: "SELECT * FROM t WHERE x >= 5 AND x <= 5"},
		{name: "different columns", sql: "SELECT * FROM t WHERE x = 1 AND y = 2"},
		{name: "or", sql: "SELECT * FROM t WHERE x = 1 OR x = 2"},
		{name: "under not", sql: "SELECT * FROM t WHERE NOT (x = 1 AND x = 2)"},
		{name: "strings are not compared", sql: "SELECT * FROM t WHERE s = 'a' AND s = 'A'"},
		{name: "variables are not compared", sql: "SELECT * FROM t WHERE x = @a AND x = @b"},
		{name: "in an or branch", sql: "SELECT * FROM t WHERE (x = 1 AND x = 2) OR y = 3", want: []string{"x = 1 AND x = 2"}},
		{name: "join on", sql: "SELECT * FROM a JOIN b ON b.id = a.id AND b.k > 3 AND b.k < 1", want: []string{"b.k > 3 AND b.k < 1"}},
		{name: "having", sql: "SELECT a FROM t GROUP BY a HAVING a > 10 AND a < 0", want: []string{"a > 10 AND a < 0"}},
		{name: "in a cte", sql: "WITH c AS (SELECT * FROM t WHERE x = 1 AND x = 2) SELECT * FROM c", want: []string{"x = 1 AND x = 2"}},
		{name: "in a subquery", sql: "SELECT * FROM t WHERE EXISTS (SELECT 1 FROM u WHERE u.x = 1 AND u.x = 2)", want: []string{"u.x = 1 AND u.x = 2"}},
		{name: "union branch", sql: "SELECT 1 UNION SELECT x FROM t WHERE x < 0 AND x > 0", want: []string{"x < 0 AND x > 0"}},
		{name: "delete", sql: "DELETE FROM t WHERE x = 1 AND x = 2", want: []string{"x = 1 AND x = 2"}},
		{name: "string literal", sql: "SELECT 'x = 1 AND x = 2' FROM t"},
	})
}

func TestRuleRedundancy(t *testing.T) {
	runRuleCases(t, "QRY-PRED-002", []ruleCase{
		{name: "weaker bound", sql: "SELECT * FROM t WHERE x > 5 AND x > 3", want: []string{"x > 3"}, class: diagnostics.Optimization},
		{name: "weaker bound first", sql: "SELECT * FROM t WHERE x < 10 AND x < 3", want: []string{"x < 10"}},
		{name: "implied by equality", sql: "SELECT * FROM t WHERE x = 4 AND x > 3", want: []string{"x > 3"}},
		{name: "implied not equal", sql: "SELECT * FROM t WHERE x = 1 AND x <> 2", want: []string{"x <> 2"}},
		{name: "duplicate", sql: "SELECT * FROM t WHERE a.x = b.y AND c = 'z' AND a.x  =  b.y", want: []string{"a.x  =  b.y"}},
		{name: "same bound twice", sql: "SELECT * FROM t WHERE x >= 1 AND x >= 1.0", want: []string{"x >= 1.0"}},
		{name: "a real range", sql: "SELECT * FROM t WHERE x > 3 AND x < 10"},
		{name: "inclusive and exclusive", sql: "SELECT * FROM t WHERE x >= 3 AND x > 3", want: []string{"x >= 3"}},
		{name: "is not null with a comparison", sql: "SELECT * FROM t WHERE x IS NOT NULL AND x > 3"},
		{name: "contradiction is not redundancy", sql: "SELECT * FROM t WHERE x = 1 AND x = 2"},
		{name: "or", sql: "SELECT * FROM t WHERE x > 5 OR x > 3"},
		{name: "nested", sql: "SELECT * FROM (SELECT * FROM t WHERE x > 5 AND x > 3) AS d", want: []string{"x > 3"}},
	})
}

func TestRuleNonSargableDate(t *testing.T) {
	runRuleCases(t, "QRY-DATE-001", []ruleCase{
		{name: "year", sql: "SELECT * FROM o WHERE YEAR(o.OrderDate) = 2024",
			want: []string{"YEAR(o.OrderDate) = 2024"}, fix: []string{"o.OrderDate >= '20240101' AND o.OrderDate < '20250101'"},
			class: diagnostics.Optimization},
		{name: "year on the right", sql: "SELECT * FROM o WHERE 2024 = YEAR(OrderDate)",
			want: []string{"2024 = YEAR(OrderDate)"}, fix: []string{"OrderDate >= '20240101' AND OrderDate < '20250101'"}},
		{name: "year with a range has no rewrite", sql: "SELECT * FROM o WHERE YEAR(d) >= 2024", want: []string{"YEAR(d) >= 2024"}, fix: []string{""}},
		{name: "month", sql: "SELECT * FROM o WHERE MONTH(d) = 1", want: []string{"MONTH(d) = 1"}, fix: []string{""}},
		{name: "datepart", sql: "SELECT * FROM o WHERE DATEPART(year, d) = 2024", want: []string{"DATEPART(year, d) = 2024"}},
		{name: "convert to date", sql: "SELECT * FROM o WHERE CONVERT(date, d) = '2024-01-05'",
			want: []string{"CONVERT(date, d) = '2024-01-05'"}, fix: []string{"d >= '2024-01-05' AND d < DATEADD(day, 1, '2024-01-05')"}},
		{name: "cast to date", sql: "SELECT * FROM o WHERE CAST(d AS date) = @day",
			want: []string{"CAST(d AS date) = @day"}, fix: []string{"d >= @day AND d < DATEADD(day, 1, @day)"}},
		{name: "convert to varchar", sql: "SELECT * FROM o WHERE CONVERT(varchar(10), d, 120) = '2024-01-05'", want: []string{"CONVERT(varchar(10), d, 120) = '2024-01-05'"}, fix: []string{""}},
		{name: "dateadd days", sql: "SELECT * FROM o WHERE DATEADD(day, 30, d) > GETDATE()",
			want: []string{"DATEADD(day, 30, d) > GETDATE()"}, fix: []string{"d > DATEADD(day, -30, GETDATE())"}},
		{name: "dateadd months has no rewrite", sql: "SELECT * FROM o WHERE DATEADD(month, 1, d) > GETDATE()", want: []string{"DATEADD(month, 1, d) > GETDATE()"}, fix: []string{""}},
		{name: "range on the column", sql: "SELECT * FROM o WHERE d >= '20240101' AND d < '20250101'"},
		{name: "function on the value side", sql: "SELECT * FROM o WHERE d > DATEADD(day, -30, GETDATE())"},
		{name: "both sides columns", sql: "SELECT * FROM a JOIN b ON YEAR(a.d) = YEAR(b.d)"},
		{name: "in the select list", sql: "SELECT YEAR(d) FROM o"},
		{name: "join on", sql: "SELECT * FROM a JOIN b ON b.id = a.id AND YEAR(b.d) = 2024", want: []string{"YEAR(b.d) = 2024"}},
		{name: "in a cte", sql: "WITH c AS (SELECT * FROM o WHERE YEAR(d) = 2024) SELECT * FROM c", want: []string{"YEAR(d) = 2024"}},
		{name: "string literal", sql: "SELECT 'WHERE YEAR(d) = 2024'"},
	})
}

func TestRuleBetweenDates(t *testing.T) {
	runRuleCases(t, "QRY-DATE-002", []ruleCase{
		{name: "date end", sql: "SELECT * FROM o WHERE OrderDate BETWEEN '2026-01-01' AND '2026-01-31'",
			want:  []string{"OrderDate BETWEEN '2026-01-01' AND '2026-01-31'"},
			fix:   []string{"OrderDate >= '2026-01-01' AND OrderDate < '2026-02-01'"},
			class: diagnostics.Potential},
		{name: "compact format, year end", sql: "SELECT * FROM o WHERE o.d BETWEEN '20251201' AND '20251231'",
			fix: []string{"o.d >= '20251201' AND o.d < '20260101'"}, want: []string{"o.d BETWEEN '20251201' AND '20251231'"}},
		{name: "leap day", sql: "SELECT * FROM o WHERE d BETWEEN N'2028-02-01' AND N'2028-02-29'",
			want: []string{"d BETWEEN N'2028-02-01' AND N'2028-02-29'"}, fix: []string{"d >= N'2028-02-01' AND d < N'2028-03-01'"}},
		{name: "last second", sql: "SELECT * FROM o WHERE d BETWEEN '2026-01-01' AND '2026-01-31 23:59:59'",
			want: []string{"d BETWEEN '2026-01-01' AND '2026-01-31 23:59:59'"}, fix: []string{"d >= '2026-01-01' AND d < '2026-02-01'"}},
		{name: "explicit time", sql: "SELECT * FROM o WHERE d BETWEEN '2026-01-01' AND '2026-01-31 12:00'"},
		{name: "numbers", sql: "SELECT * FROM o WHERE x BETWEEN 1 AND 10"},
		{name: "not a date", sql: "SELECT * FROM o WHERE s BETWEEN 'a' AND 'm'"},
		{name: "not between", sql: "SELECT * FROM o WHERE d NOT BETWEEN '2026-01-01' AND '2026-01-31'"},
		{name: "half-open range", sql: "SELECT * FROM o WHERE d >= '2026-01-01' AND d < '2026-02-01'"},
		{name: "in a subquery", sql: "SELECT * FROM c WHERE EXISTS (SELECT 1 FROM o WHERE o.d BETWEEN '2026-01-01' AND '2026-01-31')",
			want: []string{"o.d BETWEEN '2026-01-01' AND '2026-01-31'"}},
		{name: "string literal", sql: "SELECT 'd BETWEEN ''2026-01-01'' AND ''2026-01-31'''"},
	})
}

func TestRuleCountSkipsNulls(t *testing.T) {
	runRuleCases(t, "QRY-COUNT-001", []ruleCase{
		{name: "count column", sql: "SELECT COUNT(Email) FROM Customers", want: []string{"COUNT(Email)"}, fix: []string{"COUNT(*)"}, class: diagnostics.Potential},
		{name: "in having", sql: "SELECT a FROM t GROUP BY a HAVING COUNT(t.b) > 1", want: []string{"COUNT(t.b)"}},
		{name: "count star", sql: "SELECT COUNT(*) FROM t"},
		{name: "count literal", sql: "SELECT COUNT(1) FROM t"},
		{name: "count distinct", sql: "SELECT COUNT(DISTINCT Email) FROM Customers"},
		{name: "is not null in where", sql: "SELECT COUNT(Email) FROM Customers WHERE Email IS NOT NULL"},
		{name: "comparison in where", sql: "SELECT COUNT(c.Email) FROM Customers AS c WHERE c.Email LIKE '%@x.com'"},
		{name: "inner join key", sql: "SELECT COUNT(o.OrderID) FROM o JOIN oi ON oi.OrderID = o.OrderID"},
		{name: "outer join brings NULLs back", sql: "SELECT COUNT(oi.OrderID) FROM o LEFT JOIN oi ON oi.OrderID = o.OrderID", want: []string{"COUNT(oi.OrderID)"}},
		{name: "in a derived table", sql: "SELECT * FROM (SELECT COUNT(x) AS n FROM t) AS d", want: []string{"COUNT(x)"}},
	})
}

func TestRuleCountAtJoinGrain(t *testing.T) {
	runRuleCases(t, "QRY-COUNT-002", []ruleCase{
		{name: "count with a sum of the other table", sql: "SELECT o.CustomerID, COUNT(o.OrderID) AS Orders, SUM(oi.Quantity) AS Units " +
			"FROM dbo.Orders AS o JOIN dbo.OrderItems AS oi ON oi.OrderID = o.OrderID GROUP BY o.CustomerID",
			want: []string{"COUNT(o.OrderID)"}, fix: []string{"COUNT(DISTINCT o.OrderID)"}, class: diagnostics.Potential},
		{name: "count distinct", sql: "SELECT COUNT(DISTINCT o.OrderID), SUM(oi.Quantity) FROM o JOIN oi ON oi.OrderID = o.OrderID"},
		{name: "count only", sql: "SELECT c.Name, COUNT(o.OrderID) FROM c JOIN o ON o.cid = c.id GROUP BY c.Name"},
		{name: "same table aggregates", sql: "SELECT COUNT(o.OrderID), SUM(o.Total) FROM o JOIN c ON c.id = o.cid"},
		{name: "no join", sql: "SELECT COUNT(o.OrderID), SUM(o.Total) FROM o"},
		{name: "count star is left to the probe", sql: "SELECT COUNT(*), SUM(oi.Quantity) FROM o JOIN oi ON oi.OrderID = o.OrderID"},
		{name: "unqualified", sql: "SELECT COUNT(OrderID), SUM(oi.Quantity) FROM o JOIN oi ON oi.OrderID = o.OrderID"},
		{name: "in a cte", sql: "WITH x AS (SELECT COUNT(o.id) AS n, SUM(i.q) AS q FROM o JOIN i ON i.oid = o.id) SELECT * FROM x",
			want: []string{"COUNT(o.id)"}},
	})
}
