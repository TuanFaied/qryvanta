// Package analyzer runs diagnostic rules over a parsed query and returns validated findings
// (architecture decision D3).
//
// A Rule describes what it looks for and returns diagnostics.Finding values. The Engine runs every
// rule of a Registry, isolates a rule that fails or panics, validates each finding against the
// analyzed SQL and orders the findings by position. Rules never execute anything: a suggested
// correction is text (D4). Rule IDs are stable and documented in docs/features/rules.md.
package analyzer

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/parser"
)

// Input is what rules analyze: a script and its parse with the QueryModel. Batches with syntax
// errors have no statements, so rules never see them.
type Input struct {
	SQL    string
	Result *parser.Result
	// Catalog is the metadata of the tables the script reads, or nil without a connection.
	Catalog *Catalog
}

// Text returns the SQL a span covers.
func (in *Input) Text(s parser.Span) string { return parser.SpanText(in.SQL, s) }

// SpanOf converts a parser span to a finding span.
func SpanOf(s parser.Span) diagnostics.Span {
	return diagnostics.Span{Offset: s.Offset, Length: s.Length, Line: s.Line, Column: s.Column}
}

// Info describes a rule.
type Info struct {
	// ID is stable once published: QRY-<AREA>-<NNN>, for example QRY-NULL-001.
	ID    string
	Title string
	// Severity is the default for the rule's findings; a finding may set its own.
	Severity diagnostics.Severity
	// Evidence lists the kinds of evidence the rule uses. Its findings may attach only these.
	// Rules that need metadata, plans or probes run only when that evidence is available
	// (later phases).
	Evidence []diagnostics.EvidenceKind
}

// Rule looks for one kind of problem.
type Rule interface {
	Info() Info
	// Check returns the rule's findings. RuleID and Severity may be left empty; the engine
	// fills them from Info. An error (or a panic) is reported and does not stop other rules.
	Check(in *Input) ([]diagnostics.Finding, error)
}

var ruleID = regexp.MustCompile(`^QRY-[A-Z]+-[0-9]{3}$`)

func validateInfo(i Info) error {
	var errs []error
	if !ruleID.MatchString(i.ID) {
		errs = append(errs, fmt.Errorf("rule ID %q does not look like QRY-AREA-001", i.ID))
	}
	if i.Title == "" {
		errs = append(errs, fmt.Errorf("rule %s has no title", i.ID))
	}
	if !i.Severity.Valid() {
		errs = append(errs, fmt.Errorf("rule %s has unknown severity %q", i.ID, i.Severity))
	}
	if len(i.Evidence) == 0 {
		errs = append(errs, fmt.Errorf("rule %s declares no evidence kinds", i.ID))
	}
	for _, k := range i.Evidence {
		if !k.Valid() {
			errs = append(errs, fmt.Errorf("rule %s declares unknown evidence kind %q", i.ID, k))
		}
	}
	return errors.Join(errs...)
}

// Registry is a set of rules with unique IDs, kept in ID order.
type Registry struct {
	rules []Rule
}

// NewRegistry checks and registers rules: IDs must be well formed and unique.
func NewRegistry(rules ...Rule) (*Registry, error) {
	r := &Registry{}
	seen := map[string]bool{}
	var errs []error
	for _, rule := range rules {
		info := rule.Info()
		if err := validateInfo(info); err != nil {
			errs = append(errs, err)
			continue
		}
		if seen[info.ID] {
			errs = append(errs, fmt.Errorf("rule ID %s is registered twice", info.ID))
			continue
		}
		seen[info.ID] = true
		r.rules = append(r.rules, rule)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	slices.SortFunc(r.rules, func(a, b Rule) int { return strings.Compare(a.Info().ID, b.Info().ID) })
	return r, nil
}

// Rules returns the registered rules in ID order.
func (r *Registry) Rules() []Rule { return slices.Clone(r.rules) }

// Builtin returns Qryvanta's rules. Adding a rule here also means documenting it in
// docs/features/rules.md (a test checks that every ID is there).
func Builtin() []Rule {
	return []Rule{
		andOrPrecedence{},
		betweenDates{},
		contradiction{},
		countAtJoinGrain{},
		countSkipsNulls{},
		leftJoinCancelled{},
		missingJoinPredicate{},
		notInNull{},
		nonSargableDate{},
		nullComparison{},
		joinFanOut{},
		joinTypeMismatch{},
		nullableJoinKey{},
		joinIgnoresForeignKey{},
		redundancy{},
	}
}

// Default is the registry of the built-in rules.
func Default() *Registry {
	r, err := NewRegistry(Builtin()...)
	if err != nil {
		panic("analyzer: built-in rules: " + err.Error()) // covered by TestBuiltinRules
	}
	return r
}
