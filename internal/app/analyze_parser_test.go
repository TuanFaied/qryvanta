//go:build parser

package app

// The analyzer against the real parser: QRY-NULL-001 wherever a comparison can appear, with
// spans that are script-absolute UTF-16. Run (after building the sidecar):
//
//	QRYVANTA_PARSER_PATH=bin/qryvanta-tsqlparser.exe go test -tags parser ./internal/app/

import (
	"context"
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/parser"
)

func TestAnalyzeFindsNullComparisons(t *testing.T) {
	c := parserCore(t)
	cases := []struct {
		name string
		sql  string
		want []string // the SQL each finding covers, in order
		fix  []string // each finding's correction ("" for none)
	}{
		{"where", "SELECT * FROM Sales.Customers WHERE Region = NULL", []string{"Region = NULL"}, []string{"Region IS NULL"}},
		{"not equal", "SELECT * FROM t WHERE t.a <> NULL OR t.b != NULL", []string{"t.a <> NULL", "t.b != NULL"}, []string{"t.a IS NOT NULL", "t.b IS NOT NULL"}},
		{"null first", "SELECT * FROM t WHERE NULL = a", []string{"NULL = a"}, []string{"a IS NULL"}},
		{"is null is fine", "SELECT * FROM t WHERE a IS NULL AND b IS NOT NULL", nil, nil},
		{"join on", "SELECT * FROM a JOIN b ON b.id = a.id AND b.x = NULL", []string{"b.x = NULL"}, []string{"b.x IS NULL"}},
		{"having", "SELECT a FROM t GROUP BY a HAVING MAX(b) = NULL", []string{"MAX(b) = NULL"}, []string{"MAX(b) IS NULL"}},
		{"case", "SELECT CASE WHEN a = NULL THEN 1 END FROM t", []string{"a = NULL"}, []string{"a IS NULL"}},
		{"exists subquery", "SELECT * FROM t WHERE EXISTS (SELECT 1 FROM u WHERE u.k = NULL)", []string{"u.k = NULL"}, []string{"u.k IS NULL"}},
		{"cte and derived table", "WITH c AS (SELECT a FROM t WHERE a = NULL) SELECT * FROM (SELECT a FROM c WHERE a <> NULL) d",
			[]string{"a = NULL", "a <> NULL"}, []string{"a IS NULL", "a IS NOT NULL"}},
		{"update and if", "IF @x = NULL UPDATE t SET a = 1 WHERE b = NULL", []string{"@x = NULL", "b = NULL"}, []string{"@x IS NULL", "b IS NULL"}},
		{"null = null", "SELECT 1 WHERE NULL = NULL", []string{"NULL = NULL"}, []string{""}},
		{"second batch", "SELECT 1\nGO\nSELECT * FROM t WHERE a = NULL", []string{"a = NULL"}, []string{"a IS NULL"}},
		{"after wide characters", "SELECT N'é𝄞' AS x FROM t WHERE a = NULL", []string{"a = NULL"}, []string{"a IS NULL"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := c.Analyze(context.Background(), tc.sql)
			if err != nil {
				t.Fatal(err)
			}
			if len(a.Failures) > 0 {
				t.Fatalf("rule failures: %+v", a.Failures)
			}
			var got, fixes []string
			for _, f := range a.Findings {
				got = append(got, parser.SpanText(tc.sql, parser.Span{Offset: f.Span.Offset, Length: f.Span.Length}))
				fix := ""
				if f.Correction != nil {
					fix = f.Correction.Replacement
				}
				fixes = append(fixes, fix)
			}
			if strings.Join(got, "|") != strings.Join(tc.want, "|") || strings.Join(fixes, "|") != strings.Join(tc.fix, "|") {
				t.Fatalf("findings cover %q with corrections %q; want %q and %q", got, fixes, tc.want, tc.fix)
			}
		})
	}
}

func TestAnalyzeSkipsBatchesWithSyntaxErrors(t *testing.T) {
	c := parserCore(t)
	sql := "SELECT * FROM t WHERE a = NULL\nGO\nSELECT FROM WHERE b = NULL"
	a, err := c.Analyze(context.Background(), sql)
	if err != nil {
		t.Fatal(err)
	}
	if a.SyntaxErrors == 0 || a.Statements != 1 || len(a.Findings) != 1 || a.Findings[0].Span.Line != 1 {
		t.Fatalf("analysis = %+v; want one finding from the first batch, and the second reported as a syntax error", a)
	}
}
