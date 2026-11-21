package analyzer

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/TuanFaied/qryvanta/internal/diagnostics"
)

// Report is the result of one analysis.
type Report struct {
	// Findings are valid findings ordered by position (see Analyze).
	Findings []diagnostics.Finding `json:"findings"`
	// Failures are rules that returned an error, panicked or returned an invalid finding. Their
	// other findings are still reported.
	Failures []RuleFailure `json:"failures"`
	// Rules is how many rules ran.
	Rules int `json:"rules"`
}

// RuleFailure says why a rule did not report (all of) its findings.
type RuleFailure struct {
	RuleID  string `json:"ruleId"`
	Message string `json:"message"`
}

// Engine runs the rules of a registry.
type Engine struct {
	registry *Registry
}

// NewEngine creates an engine for registry.
func NewEngine(registry *Registry) *Engine { return &Engine{registry: registry} }

// Analyze runs every rule over in. Each finding is completed from its rule's Info, then
// validated (diagnostics.Validate, and its evidence must be of a kind the rule declares); an
// invalid finding is dropped and reported as a failure. Findings are ordered by start offset,
// then the enclosing one first, then severity, rule ID and title, so the order is stable. The
// only error is ctx's, checked between rules.
func (e *Engine) Analyze(ctx context.Context, in *Input) (Report, error) {
	rep := Report{Findings: []diagnostics.Finding{}, Failures: []RuleFailure{}}
	for _, rule := range e.registry.rules {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		info := rule.Info()
		rep.Rules++
		findings, err := runRule(rule, in)
		if err != nil {
			rep.Failures = append(rep.Failures, RuleFailure{RuleID: info.ID, Message: err.Error()})
		}
		for _, f := range findings {
			if f.RuleID == "" {
				f.RuleID = info.ID
			}
			if f.Severity == "" {
				f.Severity = info.Severity
			}
			if err := checkFinding(f, info, in.SQL); err != nil {
				rep.Failures = append(rep.Failures, RuleFailure{RuleID: info.ID, Message: "invalid finding: " + err.Error()})
				continue
			}
			if f.Evidence == nil {
				f.Evidence = []diagnostics.Evidence{}
			}
			rep.Findings = append(rep.Findings, f)
		}
	}
	slices.SortStableFunc(rep.Findings, compareFindings)
	return rep, nil
}

// runRule calls the rule and turns a panic into an error.
func runRule(rule Rule, in *Input) (findings []diagnostics.Finding, err error) {
	defer func() {
		if r := recover(); r != nil {
			findings, err = nil, fmt.Errorf("panicked: %v", r)
		}
	}()
	return rule.Check(in)
}

func checkFinding(f diagnostics.Finding, info Info, sql string) error {
	if f.RuleID != info.ID {
		return fmt.Errorf("rule ID %q does not match the rule %s", f.RuleID, info.ID)
	}
	for _, ev := range f.Evidence {
		if !slices.Contains(info.Evidence, ev.Kind) {
			return fmt.Errorf("%s evidence is not declared by the rule", ev.Kind)
		}
	}
	return diagnostics.Validate(f, sql)
}

func compareFindings(a, b diagnostics.Finding) int {
	if c := a.Span.Offset - b.Span.Offset; c != 0 {
		return c
	}
	if c := b.Span.Length - a.Span.Length; c != 0 {
		return c
	}
	if c := a.Severity.Rank() - b.Severity.Rank(); c != 0 {
		return c
	}
	if c := strings.Compare(a.RuleID, b.RuleID); c != 0 {
		return c
	}
	return strings.Compare(a.Title, b.Title)
}
