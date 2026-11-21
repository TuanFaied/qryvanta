package analyzer

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/parser"
)

// fakeRule returns fixed findings, an error, or panics.
type fakeRule struct {
	info     Info
	findings []diagnostics.Finding
	err      error
	panic    bool
}

func (r fakeRule) Info() Info { return r.info }

func (r fakeRule) Check(*Input) ([]diagnostics.Finding, error) {
	if r.panic {
		panic("boom")
	}
	return r.findings, r.err
}

func info(id string) Info {
	return Info{ID: id, Title: "Fake " + id, Severity: diagnostics.SeverityMedium, Evidence: []diagnostics.EvidenceKind{diagnostics.EvidenceStatic}}
}

const testSQL = "SELECT a FROM t WHERE b = 1 AND c = 2"

// at is a potential finding (no evidence needed) at offset with length.
func at(offset, length int, title string) diagnostics.Finding {
	return diagnostics.Finding{
		Title:          title,
		Explanation:    "Explained.",
		Classification: diagnostics.Potential,
		Span:           diagnostics.Span{Offset: offset, Length: length, Line: 1, Column: offset + 1},
	}
}

func mustRegistry(t *testing.T, rules ...Rule) *Registry {
	t.Helper()
	r, err := NewRegistry(rules...)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func analyze(t *testing.T, rules ...Rule) Report {
	t.Helper()
	rep, err := NewEngine(mustRegistry(t, rules...)).Analyze(context.Background(), &Input{SQL: testSQL, Result: &parser.Result{}})
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestEngineIsolatesFailingRules(t *testing.T) {
	rep := analyze(t,
		fakeRule{info: info("QRY-TEST-001"), panic: true},
		fakeRule{info: info("QRY-TEST-002"), err: errors.New("no luck"), findings: []diagnostics.Finding{at(22, 5, "kept despite the error")}},
		fakeRule{info: info("QRY-TEST-003"), findings: []diagnostics.Finding{at(32, 5, "after the panic")}},
	)
	if rep.Rules != 3 {
		t.Errorf("Rules = %d, want 3", rep.Rules)
	}
	if len(rep.Findings) != 2 {
		t.Fatalf("findings = %+v, want the two from the rules that did not panic", rep.Findings)
	}
	want := []RuleFailure{{RuleID: "QRY-TEST-001", Message: "panicked: boom"}, {RuleID: "QRY-TEST-002", Message: "no luck"}}
	if len(rep.Failures) != 2 || rep.Failures[0] != want[0] || rep.Failures[1] != want[1] {
		t.Errorf("failures = %+v, want %+v", rep.Failures, want)
	}
}

func TestEngineDropsInvalidFindings(t *testing.T) {
	confirmedWithoutEvidence := at(22, 5, "confirmed, no evidence")
	confirmedWithoutEvidence.Classification = diagnostics.Confirmed
	outside := at(30, 50, "outside the SQL")
	wrongID := at(22, 5, "wrong rule ID")
	wrongID.RuleID = "QRY-OTHER-001"
	planEvidence := at(22, 5, "undeclared evidence")
	planEvidence.Evidence = []diagnostics.Evidence{{Kind: diagnostics.EvidencePlan, Summary: "s", Plan: &diagnostics.PlanEvidence{Operator: "Hash Match", Property: "rows"}}}

	rep := analyze(t, fakeRule{info: info("QRY-TEST-001"), findings: []diagnostics.Finding{
		confirmedWithoutEvidence, outside, wrongID, planEvidence, at(22, 5, "valid"),
	}})
	if len(rep.Findings) != 1 || rep.Findings[0].Title != "valid" {
		t.Fatalf("findings = %+v, want only the valid one", rep.Findings)
	}
	wants := []string{"confirmed finding needs evidence", "ends after the SQL", "does not match the rule", "plan evidence is not declared"}
	if len(rep.Failures) != len(wants) {
		t.Fatalf("failures = %+v", rep.Failures)
	}
	for i, w := range wants {
		if !strings.Contains(rep.Failures[i].Message, w) || rep.Failures[i].RuleID != "QRY-TEST-001" {
			t.Errorf("failure %d = %+v, want %q", i, rep.Failures[i], w)
		}
	}
}

func TestEngineFillsRuleIDAndSeverity(t *testing.T) {
	own := at(22, 5, "own severity")
	own.Severity = diagnostics.SeverityLow
	rep := analyze(t, fakeRule{info: info("QRY-TEST-001"), findings: []diagnostics.Finding{at(22, 5, "default"), own}})
	if len(rep.Findings) != 2 {
		t.Fatalf("findings = %+v", rep.Findings)
	}
	for _, f := range rep.Findings {
		if f.RuleID != "QRY-TEST-001" {
			t.Errorf("RuleID = %q", f.RuleID)
		}
		if f.Evidence == nil {
			t.Error("Evidence is nil; the frontend expects a list")
		}
	}
	if rep.Findings[0].Severity != diagnostics.SeverityMedium || rep.Findings[1].Severity != diagnostics.SeverityLow {
		t.Errorf("severities = %s, %s; want medium (the rule's) then low (the finding's)", rep.Findings[0].Severity, rep.Findings[1].Severity)
	}
}

func TestEngineOrdersFindingsStably(t *testing.T) {
	high := at(22, 5, "same place, higher severity")
	high.Severity = diagnostics.SeverityHigh
	rules := []Rule{
		fakeRule{info: info("QRY-TEST-002"), findings: []diagnostics.Finding{at(32, 5, "last"), at(22, 15, "enclosing"), at(22, 5, "b, rule 2")}},
		fakeRule{info: info("QRY-TEST-001"), findings: []diagnostics.Finding{at(22, 5, "b, rule 1 z"), at(22, 5, "b, rule 1 a"), high, at(7, 1, "first")}},
	}
	want := []string{"first", "enclosing", "same place, higher severity", "b, rule 1 a", "b, rule 1 z", "b, rule 2", "last"}
	for run := 0; run < 5; run++ {
		rep := analyze(t, rules...)
		var got []string
		for _, f := range rep.Findings {
			got = append(got, f.Title)
		}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("order = %q, want %q", got, want)
		}
	}
}

func TestEngineStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewEngine(mustRegistry(t, fakeRule{info: info("QRY-TEST-001")})).Analyze(ctx, &Input{SQL: testSQL, Result: &parser.Result{}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestRegistryRejectsBadRules(t *testing.T) {
	bad := info("QRY-TEST-001")
	bad.Severity = "awful"
	noEvidence := info("QRY-TEST-002")
	noEvidence.Evidence = nil
	cases := []struct {
		name  string
		rules []Rule
		want  string
	}{
		{"duplicate ID", []Rule{fakeRule{info: info("QRY-TEST-001")}, fakeRule{info: info("QRY-TEST-001")}}, "registered twice"},
		{"malformed ID", []Rule{fakeRule{info: info("NULL-1")}}, "does not look like"},
		{"lower-case ID", []Rule{fakeRule{info: info("qry-null-001")}}, "does not look like"},
		{"bad severity", []Rule{fakeRule{info: bad}}, "unknown severity"},
		{"no evidence kinds", []Rule{fakeRule{info: noEvidence}}, "declares no evidence"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := NewRegistry(c.rules...); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

func TestRegistryKeepsIDOrder(t *testing.T) {
	r := mustRegistry(t, fakeRule{info: info("QRY-TEST-002")}, fakeRule{info: info("QRY-NULL-001")}, fakeRule{info: info("QRY-TEST-001")})
	var ids []string
	for _, rule := range r.Rules() {
		ids = append(ids, rule.Info().ID)
	}
	if strings.Join(ids, ",") != "QRY-NULL-001,QRY-TEST-001,QRY-TEST-002" {
		t.Fatalf("ids = %v", ids)
	}
}

// TestBuiltinRules checks that the built-in rules register and that docs/features/rules.md
// documents every one of them.
func TestBuiltinRules(t *testing.T) {
	r := Default()
	doc, err := os.ReadFile("../../docs/features/rules.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range r.Rules() {
		id := rule.Info().ID
		if !strings.Contains(string(doc), "### "+id) {
			t.Errorf("docs/features/rules.md has no \"### %s\" section", id)
		}
	}
}
