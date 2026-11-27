//go:build parser

package golden

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TuanFaied/qryvanta/internal/analyzer"
	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/parser"
	"github.com/TuanFaied/qryvanta/internal/parser/sidecar"
	"github.com/TuanFaied/qryvanta/internal/plan"
)

// The plan rules and confirmations over every captured plan, with its sample query parsed by the
// real parser and no catalog: what Execute with debugging finds from the plan alone.
func TestPlanFindingsOnCapturedPlans(t *testing.T) {
	path := os.Getenv("QRYVANTA_PARSER_PATH")
	if path == "" {
		t.Skip("QRYVANTA_PARSER_PATH not set; build the sidecar with `wails3 task common:build:sidecar`")
	}
	c := sidecar.New(sidecar.Options{Path: path, Timeout: 20 * time.Second})
	t.Cleanup(func() { _ = c.Close() })
	files, _ := filepath.Glob(filepath.Join("fixtures", "*.sqlplan"))
	if len(files) < 40 {
		t.Fatalf("plan fixtures = %d", len(files))
	}
	for _, f := range files {
		base := strings.TrimSuffix(filepath.Base(f), ".sqlplan")
		t.Run(base, func(t *testing.T) {
			sample := base[:strings.LastIndex(base, ".")]
			sql, err := os.ReadFile(filepath.Join("..", "sample_queries", sample+".sql"))
			if err != nil {
				t.Fatal(err)
			}
			res, err := c.Parse(context.Background(), string(sql), parser.Options{Model: true})
			if err != nil {
				t.Fatal(err)
			}
			in, _ := os.Open(f)
			p, err := plan.Parse(in, 0)
			in.Close()
			if err != nil {
				t.Fatal(err)
			}
			input := &analyzer.Input{SQL: string(sql), Result: res, Plans: []analyzer.PlanDoc{{Batch: 0, Plan: p}}}
			input.LinkPlans()
			rep, err := analyzer.NewEngine(analyzer.Default()).Analyze(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			if len(rep.Failures) > 0 {
				t.Fatalf("rule failures: %+v", rep.Failures)
			}
			found := analyzer.ConfirmWithPlans(input, rep.Findings)
			byRule := map[string]diagnostics.Finding{}
			for _, x := range found {
				if err := diagnostics.Validate(x, string(sql)); err != nil {
					t.Fatalf("%s: %v", x.RuleID, err)
				}
				byRule[x.RuleID] = x
				for _, e := range x.Evidence {
					if e.Plan != nil && e.Plan.Actual && !p.Actual {
						t.Fatalf("%s: measured evidence from an estimated plan: %+v", x.RuleID, e)
					}
				}
			}

			switch {
			case strings.HasSuffix(sample, ".correct"):
				// Corrected queries: no findings, from the plan or otherwise.
				if len(found) > 0 {
					t.Fatalf("findings on a corrected query: %+v", found)
				}
			case sample == "10_missing_join_predicate.buggy":
				// The plan's "No Join Predicate" warning confirms the cartesian product.
				j := byRule["QRY-JOIN-001"]
				if j.Classification != diagnostics.Confirmed || j.Evidence[len(j.Evidence)-1].Plan == nil {
					t.Fatalf("QRY-JOIN-001 = %+v, want Confirmed with plan evidence", j)
				}
				if ev := j.Evidence[len(j.Evidence)-1].Plan; ev.Operator != "Nested Loops" || ev.Actual != p.Actual {
					t.Fatalf("plan evidence = %+v", ev)
				}
				if _, dup := byRule["QRY-PLAN-004"]; dup {
					t.Fatal("QRY-PLAN-004 reported next to the QRY-JOIN-001 it confirms")
				}
			}
			if !p.Actual {
				for _, id := range []string{"QRY-PLAN-001", "QRY-PLAN-003"} {
					if _, ok := byRule[id]; ok {
						t.Fatalf("%s from an estimated plan", id)
					}
				}
			}
		})
	}
}
