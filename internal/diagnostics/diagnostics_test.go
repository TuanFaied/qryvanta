package diagnostics

import (
	"strings"
	"testing"
)

const sql = "SELECT * FROM t WHERE a = NULL"

func valid() Finding {
	span := Span{Offset: 22, Length: 8, Line: 1, Column: 23}
	return Finding{
		RuleID:         "QRY-NULL-001",
		Title:          "Comparison with NULL",
		Explanation:    "Never true.",
		Severity:       SeverityHigh,
		Classification: Confirmed,
		Span:           span,
		Evidence: []Evidence{{
			Kind:    EvidenceStatic,
			Summary: "a = NULL is UNKNOWN for every row.",
			Static:  &StaticEvidence{Span: span, Text: "a = NULL"},
		}},
		Correction: &Correction{Description: "Use IS NULL.", Span: span, Replacement: "a IS NULL"},
	}
}

func TestValidFindingPasses(t *testing.T) {
	if err := Validate(valid(), sql); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Finding)
		want   string
	}{
		{"confirmed without evidence", func(f *Finding) { f.Evidence = nil }, "confirmed finding needs evidence"},
		{"span after the SQL", func(f *Finding) { f.Span.Length = 9 }, "ends after the SQL"},
		{"span far outside", func(f *Finding) { f.Span.Offset = 500 }, "ends after the SQL"},
		{"negative offset", func(f *Finding) { f.Span.Offset = -1 }, "empty or negative"},
		{"empty span", func(f *Finding) { f.Span.Length = 0 }, "empty or negative"},
		{"zero line", func(f *Finding) { f.Span.Line = 0 }, "1-based"},
		{"no rule ID", func(f *Finding) { f.RuleID = "" }, "rule ID is empty"},
		{"no title", func(f *Finding) { f.Title = "" }, "title is empty"},
		{"no explanation", func(f *Finding) { f.Explanation = "" }, "explanation is empty"},
		{"bad severity", func(f *Finding) { f.Severity = "severe" }, "unknown severity"},
		{"bad classification", func(f *Finding) { f.Classification = "maybe" }, "unknown classification"},
		{"evidence span outside", func(f *Finding) { f.Evidence[0].Static.Span.Offset = 40 }, "evidence 1: span"},
		{"evidence without payload", func(f *Finding) { f.Evidence[0].Static = nil }, "exactly its own payload"},
		{"evidence with the wrong payload", func(f *Finding) {
			f.Evidence[0].Static = nil
			f.Evidence[0].Probe = &ProbeEvidence{SQL: "SELECT 1", Result: "1"}
		}, "without a static payload"},
		{"evidence with two payloads", func(f *Finding) {
			f.Evidence[0].Probe = &ProbeEvidence{SQL: "SELECT 1", Result: "1"}
		}, "exactly its own payload"},
		{"evidence without summary", func(f *Finding) { f.Evidence[0].Summary = "" }, "summary is empty"},
		{"unknown evidence kind", func(f *Finding) { f.Evidence[0].Kind = "hunch" }, "unknown kind"},
		{"metadata without property", func(f *Finding) {
			f.Evidence[0] = Evidence{Kind: EvidenceMetadata, Summary: "s", Metadata: &MetadataEvidence{Object: "dbo.t"}}
		}, "object and a property"},
		{"plan without operator", func(f *Finding) {
			f.Evidence[0] = Evidence{Kind: EvidencePlan, Summary: "s", Plan: &PlanEvidence{Property: "rows"}}
		}, "operator and a property"},
		{"probe without result", func(f *Finding) {
			f.Evidence[0] = Evidence{Kind: EvidenceProbe, Summary: "s", Probe: &ProbeEvidence{SQL: "SELECT 1"}}
		}, "SQL and its result"},
		{"correction span outside", func(f *Finding) { f.Correction.Span.Offset = 30 }, "correction span"},
		{"correction without description", func(f *Finding) { f.Correction.Description = "" }, "correction: description"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := valid()
			c.change(&f)
			err := Validate(f, sql)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Validate() = %v, want an error containing %q", err, c.want)
			}
		})
	}
}

func TestPotentialFindingNeedsNoEvidence(t *testing.T) {
	f := valid()
	f.Classification = Potential
	f.Evidence = nil
	if err := Validate(f, sql); err != nil {
		t.Fatal(err)
	}
}

func TestSpanLengthCountsUTF16(t *testing.T) {
	// "é" is one UTF-16 unit (two bytes); "𝄞" is two units (four bytes).
	text := "SELECT 'é𝄞'"
	f := valid()
	f.Evidence = nil
	f.Classification = Potential
	f.Correction = nil
	f.Span = Span{Offset: 0, Length: 12, Line: 1, Column: 1}
	if err := Validate(f, text); err != nil {
		t.Fatalf("whole text: %v", err)
	}
	f.Span.Length = 13
	if err := Validate(f, text); err == nil {
		t.Fatal("a span one unit past the end passed")
	}
}

func TestSeverityRank(t *testing.T) {
	order := []Severity{SeverityCritical, SeverityHigh, SeverityMedium, SeverityLow, SeverityInformational, "bogus"}
	for i := 1; i < len(order); i++ {
		if order[i-1].Rank() >= order[i].Rank() {
			t.Errorf("%s should rank before %s", order[i-1], order[i])
		}
	}
}
