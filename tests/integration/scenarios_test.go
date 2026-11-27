//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/executor"
	"github.com/TuanFaied/qryvanta/internal/instrumentation"
)

// TestScenarioSuite is the acceptance suite of the diagnostic engine (architecture section 9):
// for each scenario in tests/sample_queries/manifest.json, the whole debugging flow (with every
// probe within budget approved, and the actual plan) must produce the expected finding on the buggy
// query, by rule ID and classification, and must not produce it on the corrected query. Findings
// the manifest lists under planEvidence must carry evidence from both the actual and the estimated
// plan; corrected queries never carry plan evidence.
func TestScenarioSuite(t *testing.T) {
	core, target, ctx := analysisCore(t)
	m := loadManifest(t)
	if len(m.Scenarios) != 10 {
		t.Fatalf("scenarios = %d, want 10", len(m.Scenarios))
	}
	approveAll := func(_ context.Context, probes []instrumentation.Probe) ([]string, error) {
		var ids []string
		for _, p := range probes {
			if !p.OverBudget {
				ids = append(ids, p.ID)
			}
		}
		return ids, nil
	}
	diagnose := func(t *testing.T, file string, mode executor.PlanMode) app.Diagnosis {
		t.Helper()
		d, err := core.Diagnose(ctx, readSample(t, file), app.DiagnoseOptions{Target: target, Approve: approveAll, Plan: mode})
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range d.Steps {
			if s.Status == app.StepFailed {
				t.Fatalf("step %s failed: %s", s.Step, s.Reason)
			}
		}
		if d.ProbeRun != nil {
			for _, r := range d.ProbeRun.Results {
				if r.Error != "" {
					t.Fatalf("probe %s failed: %s", r.ProbeID, r.Error)
				}
			}
		}
		if d.PlanMode != mode {
			t.Fatalf("plan mode = %q, want %q", d.PlanMode, mode)
		}
		return d
	}
	planEvidence := func(f diagnostics.Finding) *diagnostics.PlanEvidence {
		for _, e := range f.Evidence {
			if e.Plan != nil {
				return e.Plan
			}
		}
		return nil
	}
	describe := func(d app.Diagnosis) string {
		var parts []string
		for _, f := range d.Findings {
			parts = append(parts, f.RuleID+"/"+string(f.Classification))
		}
		return strings.Join(parts, ", ")
	}

	for _, sc := range m.Scenarios {
		t.Run(sc.ID+" "+sc.Title, func(t *testing.T) {
			if len(sc.Findings) == 0 && sc.Undetected == "" {
				t.Fatal("the manifest gives neither expected findings nor why there are none")
			}
			buggy := diagnose(t, sc.Buggy.File, executor.PlanActual)
			for _, want := range sc.Findings {
				found := false
				for _, f := range buggy.Findings {
					found = found || (f.RuleID == want.Rule && string(f.Classification) == want.Classification)
				}
				if !found {
					t.Errorf("buggy query: no %s/%s finding (got %s)", want.Rule, want.Classification, describe(buggy))
				}
			}
			for _, mode := range []executor.PlanMode{executor.PlanActual, executor.PlanEstimated} {
				d := buggy
				if mode == executor.PlanEstimated && len(sc.PlanEvidence) > 0 {
					d = diagnose(t, sc.Buggy.File, mode)
				}
				for _, want := range sc.PlanEvidence {
					found := false
					for _, f := range d.Findings {
						if ev := planEvidence(f); f.RuleID == want.Rule && ev != nil {
							found = found || (ev.Operator == want.Operator && ev.Actual == (mode == executor.PlanActual))
						}
					}
					if !found {
						t.Errorf("buggy query, %s plan: no %s finding with plan evidence from %s (got %s)", mode, want.Rule, want.Operator, describe(d))
					}
				}
			}
			correct := diagnose(t, sc.Correct.File, executor.PlanActual)
			for _, f := range correct.Findings {
				if ev := planEvidence(f); ev != nil {
					t.Errorf("corrected query has plan evidence: %s %s (node %d)", f.RuleID, f.Title, ev.NodeID)
				}
			}
			for _, want := range sc.Findings {
				for _, f := range correct.Findings {
					if f.RuleID == want.Rule {
						t.Errorf("corrected query still has %s: %s", f.RuleID, f.Title)
					}
				}
			}
		})
	}
}
