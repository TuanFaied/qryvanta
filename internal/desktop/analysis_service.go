package desktop

import (
	"context"

	"github.com/TuanFaied/qryvanta/internal/analyzer"
	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/diagnostics"
)

// AnalysisService runs the diagnostic rules for the analysis panel. It executes nothing; with a
// connection it reads catalog metadata for the metadata rules.
type AnalysisService struct {
	core *app.Core
}

// NewAnalysisService creates the service.
func NewAnalysisService(core *app.Core) *AnalysisService {
	return &AnalysisService{core: core}
}

// Analysis is the analysis panel's view of a script.
type Analysis struct {
	// Available is false when the parser cannot run; Reason says why. Then there are no
	// findings, which is not the same as "no findings".
	Available bool                  `json:"available"`
	Reason    string                `json:"reason"`
	Findings  []diagnostics.Finding `json:"findings"`
	// Failures are rules that failed; the other rules' findings are still listed.
	Failures []analyzer.RuleFailure `json:"failures"`
	// Rules is how many rules ran.
	Rules int `json:"rules"`
	// Statements is how many top-level statements were analyzed.
	Statements int `json:"statements"`
	// SyntaxErrors counts syntax errors; the batches that have them were not analyzed.
	SyntaxErrors int `json:"syntaxErrors"`
	// Metadata says whether keys, types and nullability were checked, and which tables could not be.
	Metadata app.MetadataStatus `json:"metadata"`
}

// Analyze runs the rules over sql. connectionID and database select the catalog the metadata
// rules read; with an empty connectionID, or one that is not connected, they are skipped and
// Metadata says so. Lists are never null.
func (s *AnalysisService) Analyze(ctx context.Context, sql, connectionID, database string) Analysis {
	a, err := s.core.Analyze(ctx, sql, app.AnalysisTarget{ConnectionID: connectionID, Database: database})
	if err != nil {
		return Analysis{Reason: err.Error(), Findings: []diagnostics.Finding{}, Failures: []analyzer.RuleFailure{},
			Metadata: app.MetadataStatus{Unresolved: []app.UnresolvedTable{}}}
	}
	return Analysis{
		Available:    true,
		Findings:     a.Findings,
		Failures:     a.Failures,
		Rules:        a.Rules,
		Statements:   a.Statements,
		SyntaxErrors: a.SyntaxErrors,
		Metadata:     a.Metadata,
	}
}

// ForgetMetadata drops the cached catalog metadata of a connection, so the next analysis reads
// it again. The explorer calls it when it refreshes.
func (s *AnalysisService) ForgetMetadata(connectionID string) {
	s.core.ForgetMetadata(connectionID)
}
