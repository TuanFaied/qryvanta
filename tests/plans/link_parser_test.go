//go:build parser

package golden

// Links every captured plan to its sample query, parsed by the real parser. Run:
//
//	QRYVANTA_PARSER_PATH=bin/qryvanta-tsqlparser.exe go test -tags parser ./tests/plans/

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TuanFaied/qryvanta/internal/parser"
	"github.com/TuanFaied/qryvanta/internal/parser/sidecar"
	"github.com/TuanFaied/qryvanta/internal/plan"
)

func TestPlansLinkToTheirScript(t *testing.T) {
	path := os.Getenv("QRYVANTA_PARSER_PATH")
	if path == "" {
		t.Skip("QRYVANTA_PARSER_PATH not set; build the sidecar with `wails3 task common:build:sidecar`")
	}
	c := sidecar.New(sidecar.Options{Path: path, Timeout: 20 * time.Second})
	t.Cleanup(func() { _ = c.Close() })

	files, _ := filepath.Glob(filepath.Join("fixtures", "*.sqlplan"))
	byText := 0
	for _, f := range files {
		base := filepath.Base(f)
		sample := base[:strings.Index(base, ".buggy.")+len(".buggy")]
		if !strings.Contains(base, ".buggy.") {
			sample = base[:strings.Index(base, ".correct.")+len(".correct")]
		}
		sql, err := os.ReadFile(filepath.Join("..", "sample_queries", sample+".sql"))
		if err != nil {
			t.Fatal(err)
		}
		res, err := c.Parse(context.Background(), string(sql), parser.Options{Model: true})
		if err != nil || len(res.Batches) != 1 {
			t.Fatalf("%s: %v", sample, err)
		}
		in, _ := os.Open(f)
		p, err := plan.Parse(in, 0)
		in.Close()
		if err != nil {
			t.Fatal(err)
		}
		plan.Link(p, 0, string(sql), res.Batches[0])
		for _, st := range p.Statements {
			if st.Script == nil || st.Script.Batch != 0 || st.Script.Statement != 0 {
				t.Fatalf("%s: statement %d is not linked to the query: %+v", base, st.ID, st.Script)
			}
			if st.Script.By == "text" {
				byText++
			}
		}
	}
	// Plans match by text, apart from auto-parameterized ones (sample 05's literals become @1),
	// which match by position.
	if byText < len(files)-2 {
		t.Fatalf("only %d of %d plans matched by text", byText, len(files))
	}
}

// TestPredicatesMapToClauses maps plan predicates to the query's conditions where that is
// unambiguous: every mapped clause refers to the predicate's columns, and the sample with a wrong
// join key maps its seek to the ON condition and its residual to the WHERE condition.
func TestPredicatesMapToClauses(t *testing.T) {
	path := os.Getenv("QRYVANTA_PARSER_PATH")
	if path == "" {
		t.Skip("QRYVANTA_PARSER_PATH not set")
	}
	c := sidecar.New(sidecar.Options{Path: path, Timeout: 20 * time.Second})
	t.Cleanup(func() { _ = c.Close() })

	files, _ := filepath.Glob(filepath.Join("fixtures", "*.sqlplan"))
	mapped := 0
	for _, f := range files {
		base := filepath.Base(f)
		cut := strings.Index(base, ".buggy.")
		if cut < 0 {
			cut = strings.Index(base, ".correct.")
		}
		sample := base[:cut] + "." + strings.Split(base[cut+1:], ".")[0]
		sqlBytes, err := os.ReadFile(filepath.Join("..", "sample_queries", sample+".sql"))
		if err != nil {
			t.Fatal(err)
		}
		sql := string(sqlBytes)
		res, err := c.Parse(context.Background(), sql, parser.Options{Model: true})
		if err != nil {
			t.Fatal(err)
		}
		in, _ := os.Open(f)
		p, err := plan.Parse(in, 0)
		in.Close()
		if err != nil {
			t.Fatal(err)
		}
		plan.Link(p, 0, sql, res.Batches[0])
		clauses := map[string]string{}
		for i := range p.Statements {
			st := &p.Statements[i]
			plan.MapPredicates(st, res.Batches[0].Statements[st.Script.Statement])
			st.Walk(func(o *plan.Operator) {
				for _, pr := range o.Predicates {
					if pr.Clause == nil {
						continue
					}
					mapped++
					text := parser.SpanText(sql, parser.Span{Offset: pr.Clause.Offset, Length: pr.Clause.Length})
					for _, col := range pr.Columns {
						if col.Table != "" && !strings.Contains(strings.ToLower(text), strings.ToLower(col.Name)) {
							t.Errorf("%s: %s predicate %q mapped to %q, which does not use %s", base, pr.Kind, pr.Text, text, col.Name)
						}
					}
					clauses[pr.Kind] = text
				}
			})
		}
		if strings.HasPrefix(base, "08_wrong_join_key.buggy.") {
			if clauses["seek"] != "p.ProductID = oi.OrderItemID" || clauses["residual"] != "oi.OrderID = 103" {
				t.Errorf("%s: clauses = %v", base, clauses)
			}
		}
	}
	if mapped == 0 {
		t.Fatal("no predicate was mapped to a clause")
	}
}
