//go:build parser

package app

// Autocomplete scopes against the real parser. Run (after building the sidecar):
//
//	QRYVANTA_PARSER_PATH=bin/qryvanta-tsqlparser.exe go test -tags parser ./internal/app/

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/TuanFaied/qryvanta/internal/connection"
	"github.com/TuanFaied/qryvanta/internal/parser"
	"github.com/TuanFaied/qryvanta/internal/parser/sidecar"
)

func parserCore(t *testing.T) *Core {
	t.Helper()
	path := os.Getenv("QRYVANTA_PARSER_PATH")
	if path == "" {
		t.Skip("QRYVANTA_PARSER_PATH not set; build the sidecar with `wails3 task common:build:sidecar`")
	}
	c, err := New(Options{
		ConfigPath:     filepath.Join(t.TempDir(), "config.json"),
		DisableFileLog: true,
		Credentials:    connection.NewMemoryStore(),
		Parser:         sidecar.New(sidecar.Options{Path: path, Timeout: 20 * time.Second}),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// scopeAt finds the scope at the "|" in sql (the bar is removed first).
func scopeAt(t *testing.T, c *Core, sql string) parser.Scope {
	t.Helper()
	i := strings.Index(sql, "|")
	if i < 0 {
		t.Fatalf("no cursor in %q", sql)
	}
	text := sql[:i] + sql[i+1:]
	offset := len(utf16.Encode([]rune(sql[:i])))
	scope, err := c.CompletionScope(context.Background(), text, offset)
	if err != nil {
		t.Fatalf("%q: %v", sql, err)
	}
	return scope
}

// describe renders sources as "name:kind[cols](tables)@depth" for compact comparisons.
func describe(srcs []parser.ScopeSource) string {
	var parts []string
	for _, s := range srcs {
		d := s.Name + ":" + s.Kind
		if s.Object != nil {
			d += "=" + strings.Trim(s.Object.Schema+"."+s.Object.Name, ".")
		}
		if len(s.Columns) > 0 {
			d += "[" + strings.Join(s.Columns, ",") + "]"
		}
		if len(s.Tables) > 0 {
			var ts []string
			for _, tb := range s.Tables {
				ts = append(ts, strings.Trim(tb.Schema+"."+tb.Name, "."))
			}
			d += "(" + strings.Join(ts, ",") + ")"
		}
		if s.Depth > 0 {
			d += "@" + string(rune('0'+s.Depth))
		}
		parts = append(parts, d)
	}
	return strings.Join(parts, " ")
}

func TestCompletionScopeFromTheModel(t *testing.T) {
	c := parserCore(t)
	for _, tc := range []struct {
		name, sql, sources, ctes string
	}{
		{"CTE before its FROM", "WITH t AS (SELECT a, b AS bee, a + 1 FROM dbo.X) SELECT t.|",
			"", "t:cte[a,bee]"},
		{"CTE used with an alias", "WITH t AS (SELECT a FROM dbo.X) SELECT q.| FROM t AS q",
			"q:cte[a]", "t:cte[a]"},
		{"CTE with a column list", "WITH t (x, y) AS (SELECT 1, 2) SELECT * FROM t WHERE t.|",
			"t:cte[x,y]", "t:cte[x,y]"},
		{"CTE over SELECT *", "WITH t AS (SELECT * FROM dbo.Orders) SELECT t.|",
			"", "t:cte(dbo.Orders)"},
		{"CTE over another CTE", "WITH a AS (SELECT id FROM dbo.T), b AS (SELECT a.*, 1 AS one FROM a) SELECT b.|",
			"", "a:cte[id] b:cte[id,one]"},
		{"derived table", "SELECT d.| FROM (SELECT o.OrderID, c.Name AS Customer FROM dbo.Orders o JOIN dbo.Customers c ON c.CustomerID = o.CustomerID) AS d",
			"d:derived[OrderID,Customer]", ""},
		{"inside the derived table", "SELECT * FROM (SELECT o.| FROM dbo.Orders o) AS d",
			"o:table=dbo.Orders", ""},
		{"VALUES with column names", "SELECT v.| FROM (VALUES (1, 2)) AS v (x, y)",
			"v:values[x,y]", ""},
		{"table-valued function", "SELECT f.| FROM dbo.fnOrders(1) AS f",
			"f:function=dbo.fnOrders", ""},
		{"correlated subquery sees the outer alias", "SELECT * FROM dbo.Orders o WHERE EXISTS (SELECT 1 FROM dbo.Items i WHERE i.OrderID = o.|)",
			"i:table=dbo.Items o:table=dbo.Orders@1", ""},
		{"inner alias shadows the outer one", "SELECT * FROM dbo.Orders x WHERE x.Id IN (SELECT x.| FROM dbo.Items x)",
			"x:table=dbo.Items", ""},
		{"set operation branch", "SELECT a FROM dbo.X UNION ALL SELECT y.| FROM dbo.Y AS y",
			"y:table=dbo.Y", ""},
		{"empty select list", "SELECT | FROM dbo.Orders o JOIN dbo.Customers c ON c.CustomerID = o.CustomerID",
			"o:table=dbo.Orders c:table=dbo.Customers", ""},
		{"half-typed WHERE", "SELECT * FROM dbo.Orders o WHERE o.|",
			"o:table=dbo.Orders", ""},
		// Only the text up to the cursor parses here, so the scope is empty and the editor's
		// heuristic finds "o" in the text instead.
		{"text after the cursor does not parse", "SELECT o.| FROM dbo.Orders o WHERE",
			"", ""},
		{"UPDATE ... FROM", "UPDATE o SET o.| = 1 FROM dbo.Orders o",
			"o:table=dbo.Orders", ""},
		{"second GO batch", "SELECT 1\nGO\nSELECT c.| FROM dbo.Customers c",
			"c:table=dbo.Customers", ""},
		// The heuristic's table-reference cases (context.test.ts), as regression cases.
		{"schema, database and alias", "SELECT o.| FROM dbo.Orders AS o JOIN Sales.dbo.Customers c ON c.id = o.cid LEFT JOIN Products ON 1 = 1 WHERE 1 = 1",
			"o:table=dbo.Orders c:table=dbo.Customers Products:table=Products", ""},
		{"comma joins, brackets and hints", "SELECT | FROM [Order Details] od WITH (NOLOCK), dbo.Products AS p",
			"od:table=Order Details p:table=dbo.Products", ""},
		{"functions and table variables", "SELECT | FROM dbo.fn_rows(1) AS f JOIN @t AS t ON 1 = 1",
			"f:function=dbo.fn_rows t:variable", ""},
		{"keywords are not aliases", "SELECT | FROM Orders JOIN Customers ON 1 = 1 WHERE x = 1 ORDER BY x",
			"Orders:table=Orders Customers:table=Customers", ""},
	} {
		s := scopeAt(t, c, tc.sql)
		if !s.Found {
			t.Errorf("%s: not found", tc.name)
			continue
		}
		if got := describe(s.Sources); got != tc.sources {
			t.Errorf("%s: sources\n  got:  %s\n  want: %s", tc.name, got, tc.sources)
		}
		if got := describe(s.Ctes); got != tc.ctes {
			t.Errorf("%s: ctes\n  got:  %s\n  want: %s", tc.name, got, tc.ctes)
		}
	}
}

// When the text around the cursor does not parse even with a placeholder, the editor falls
// back to its heuristic.
func TestCompletionScopeFallsBackForUnparsableText(t *testing.T) {
	c := parserCore(t)
	for _, sql := range []string{
		"SELEC o.| FROM ((( dbo.Orders o",
		"SELECT * FROM dbo.Orders o WHERE (o.|",
	} {
		if s := scopeAt(t, c, sql); s.Found {
			t.Errorf("%q: found %+v, want a fallback", sql, s)
		}
	}
}
