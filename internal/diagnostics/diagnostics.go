// Package diagnostics defines what Qryvanta reports about a query (architecture decision D3): a
// Finding, the Evidence behind it, its Severity and its Classification.
//
// The types carry no behavior beyond Validate, which enforces the rules every finding must
// satisfy before anyone sees it: a Confirmed finding needs evidence, and every position must lie
// inside the analyzed SQL. internal/analyzer runs Validate on every finding a rule returns.
package diagnostics

import (
	"errors"
	"fmt"
)

// Severity says how much a finding matters.
type Severity string

const (
	SeverityCritical      Severity = "critical"
	SeverityHigh          Severity = "high"
	SeverityMedium        Severity = "medium"
	SeverityLow           Severity = "low"
	SeverityInformational Severity = "informational"
)

// Rank orders severities from Critical (0) to Informational (4); unknown values sort last.
func (s Severity) Rank() int {
	switch s {
	case SeverityCritical:
		return 0
	case SeverityHigh:
		return 1
	case SeverityMedium:
		return 2
	case SeverityLow:
		return 3
	case SeverityInformational:
		return 4
	}
	return 5
}

// Valid reports whether s is one of the defined severities.
func (s Severity) Valid() bool { return s.Rank() < 5 }

// Classification says how sure a finding is.
type Classification string

const (
	// Confirmed: the evidence proves the issue (for example `col = NULL`, or a measured fan-out).
	Confirmed Classification = "confirmed"
	// Potential: the query may be wrong, depending on data or intent the evidence does not settle.
	Potential Classification = "potential"
	// Optimization: the query is correct, but could be faster or clearer.
	Optimization Classification = "optimization"
)

// Valid reports whether c is one of the defined classifications.
func (c Classification) Valid() bool {
	return c == Confirmed || c == Potential || c == Optimization
}

// Span locates text in the analyzed SQL. Offset and Length are UTF-16 code units, like the
// parser's positions and the editor's; Line and Column are 1-based.
type Span struct {
	Offset int `json:"offset"`
	Length int `json:"length"`
	Line   int `json:"line"`
	Column int `json:"column"`
}

// EvidenceKind is where a piece of evidence comes from, from cheapest to most expensive.
type EvidenceKind string

const (
	// EvidenceStatic comes from the parsed query alone.
	EvidenceStatic EvidenceKind = "static"
	// EvidenceMetadata comes from catalog views (keys, nullability, data types).
	EvidenceMetadata EvidenceKind = "metadata"
	// EvidencePlan comes from an estimated or actual execution plan.
	EvidencePlan EvidenceKind = "plan"
	// EvidenceProbe comes from a generated, approved, read-only probe query (D4).
	EvidenceProbe EvidenceKind = "probe"
)

// Valid reports whether k is one of the defined evidence kinds.
func (k EvidenceKind) Valid() bool {
	return k == EvidenceStatic || k == EvidenceMetadata || k == EvidencePlan || k == EvidenceProbe
}

// Evidence is one fact behind a finding. Exactly the payload that matches Kind is set.
type Evidence struct {
	Kind EvidenceKind `json:"kind"`
	// Summary states the fact in one sentence, for people.
	Summary  string            `json:"summary"`
	Static   *StaticEvidence   `json:"static,omitempty"`
	Metadata *MetadataEvidence `json:"metadata,omitempty"`
	Plan     *PlanEvidence     `json:"plan,omitempty"`
	Probe    *ProbeEvidence    `json:"probe,omitempty"`
}

// StaticEvidence points at the SQL that shows the issue.
type StaticEvidence struct {
	Span Span `json:"span"`
	// Text is the SQL the span covers.
	Text string `json:"text"`
}

// MetadataEvidence is one property of a database object, as the catalog reports it.
type MetadataEvidence struct {
	// Object is the object's name as schema.name.
	Object string `json:"object"`
	// Column is set when the property belongs to a column.
	Column   string `json:"column,omitempty"`
	Property string `json:"property"`
	Value    string `json:"value"`
}

// PlanEvidence is one value of a plan operator. Actual says whether it was measured (actual
// plan) or estimated; the two are always labelled.
type PlanEvidence struct {
	NodeID   int    `json:"nodeId"`
	Operator string `json:"operator"`
	Property string `json:"property"`
	Value    string `json:"value"`
	Actual   bool   `json:"actual"`
}

// ProbeEvidence is a probe query that ran and what it returned.
type ProbeEvidence struct {
	SQL    string `json:"sql"`
	Result string `json:"result"`
}

// Correction is a suggested change: Replacement instead of the text at Span. It is shown as a
// diff and never applied or executed by Qryvanta (D4).
type Correction struct {
	Description string `json:"description"`
	Span        Span   `json:"span"`
	Replacement string `json:"replacement"`
}

// Finding is one thing Qryvanta reports about a query.
type Finding struct {
	// RuleID is the stable ID of the rule that found it (for example QRY-NULL-001).
	RuleID         string         `json:"ruleId"`
	Title          string         `json:"title"`
	Explanation    string         `json:"explanation"`
	Severity       Severity       `json:"severity"`
	Classification Classification `json:"classification"`
	// Span is where the issue is in the analyzed SQL.
	Span       Span        `json:"span"`
	Evidence   []Evidence  `json:"evidence"`
	Correction *Correction `json:"correction,omitempty"`
	// Measurements describe probes that could confirm or dismiss the finding (D4). Rules fill
	// them from resolved metadata; internal/instrumentation turns them into SQL from fixed
	// templates. They stay on the server: the UI sees the generated probes instead.
	Measurements []Measurement `json:"-"`
}

// MeasurementKind is what a probe measures.
type MeasurementKind string

const (
	// MeasureFanOut: how many rows of Table share each value of Columns.
	MeasureFanOut MeasurementKind = "fanOut"
	// MeasureUnmatched: rows of Table whose Columns match no row of Other on OtherColumns.
	MeasureUnmatched MeasurementKind = "unmatched"
	// MeasureNulls: rows of Table where Columns[0] is NULL.
	MeasureNulls MeasurementKind = "nulls"
	// MeasureRangeEnd: rows of Table where Columns[0] is after Values[0] and before Values[1]
	// (the part of the last day that BETWEEN … AND Values[0] misses).
	MeasureRangeEnd MeasurementKind = "rangeEnd"
)

// TableRef names a resolved table: the names come from the catalog, not from the query text.
type TableRef struct {
	Database string
	Schema   string
	Name     string
}

// Measurement is what a probe would measure for a finding.
type Measurement struct {
	Kind         MeasurementKind
	Table        TableRef
	Columns      []string
	Other        *TableRef
	OtherColumns []string
	// Values are literals copied from the query; probes pass them as parameters.
	Values []string
}

// Validate checks f against the analyzed sql and returns every problem it finds, joined.
func Validate(f Finding, sql string) error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	size := utf16Len(sql)

	if f.RuleID == "" {
		fail("rule ID is empty")
	}
	if f.Title == "" {
		fail("title is empty")
	}
	if f.Explanation == "" {
		fail("explanation is empty")
	}
	if !f.Severity.Valid() {
		fail("unknown severity %q", f.Severity)
	}
	if !f.Classification.Valid() {
		fail("unknown classification %q", f.Classification)
	}
	if err := checkSpan(f.Span, size); err != nil {
		fail("span: %w", err)
	}
	if f.Classification == Confirmed && len(f.Evidence) == 0 {
		fail("a confirmed finding needs evidence that proves it")
	}
	for i, e := range f.Evidence {
		if err := validateEvidence(e, size); err != nil {
			fail("evidence %d: %w", i+1, err)
		}
	}
	if c := f.Correction; c != nil {
		if c.Description == "" {
			fail("correction: description is empty")
		}
		if err := checkSpan(c.Span, size); err != nil {
			fail("correction span: %w", err)
		}
	}
	return errors.Join(errs...)
}

func validateEvidence(e Evidence, size int) error {
	if !e.Kind.Valid() {
		return fmt.Errorf("unknown kind %q", e.Kind)
	}
	if e.Summary == "" {
		return errors.New("summary is empty")
	}
	set := 0
	for _, p := range []bool{e.Static != nil, e.Metadata != nil, e.Plan != nil, e.Probe != nil} {
		if p {
			set++
		}
	}
	if set != 1 {
		return fmt.Errorf("%s evidence must have exactly its own payload", e.Kind)
	}
	switch e.Kind {
	case EvidenceStatic:
		if e.Static == nil {
			return errors.New("static evidence without a static payload")
		}
		if err := checkSpan(e.Static.Span, size); err != nil {
			return fmt.Errorf("span: %w", err)
		}
	case EvidenceMetadata:
		if e.Metadata == nil || e.Metadata.Object == "" || e.Metadata.Property == "" {
			return errors.New("metadata evidence needs an object and a property")
		}
	case EvidencePlan:
		if e.Plan == nil || e.Plan.Operator == "" || e.Plan.Property == "" {
			return errors.New("plan evidence needs an operator and a property")
		}
	case EvidenceProbe:
		if e.Probe == nil || e.Probe.SQL == "" || e.Probe.Result == "" {
			return errors.New("probe evidence needs the probe's SQL and its result")
		}
	}
	return nil
}

// checkSpan reports whether s covers at least one code unit inside a text of size code units.
func checkSpan(s Span, size int) error {
	switch {
	case s.Offset < 0 || s.Length <= 0:
		return fmt.Errorf("offset %d, length %d is empty or negative", s.Offset, s.Length)
	case s.Offset+s.Length > size:
		return fmt.Errorf("offset %d, length %d ends after the SQL (%d code units)", s.Offset, s.Length, size)
	case s.Line < 1 || s.Column < 1:
		return fmt.Errorf("line %d, column %d is not 1-based", s.Line, s.Column)
	}
	return nil
}

// utf16Len is the length of s in UTF-16 code units.
func utf16Len(s string) int {
	n := 0
	for _, r := range s { // invalid bytes decode to U+FFFD: one unit
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}
