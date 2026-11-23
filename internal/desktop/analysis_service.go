package desktop

import (
	"context"

	"github.com/TuanFaied/qryvanta/internal/analyzer"
	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/instrumentation"
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
	// ID names the analysis for RunProbes.
	ID string `json:"id"`
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
	// Probes could confirm or dismiss findings. They are shown in full and run only when the
	// user approves them (RunProbes).
	Probes []instrumentation.Probe `json:"probes"`
}

// Analyze runs the rules over sql. connectionID and database select the catalog the metadata
// rules read; with an empty connectionID, or one that is not connected, they are skipped and
// Metadata says so. Lists are never null.
func (s *AnalysisService) Analyze(ctx context.Context, sql, connectionID, database string) Analysis {
	a, err := s.core.Analyze(ctx, sql, app.AnalysisTarget{ConnectionID: connectionID, Database: database})
	if err != nil {
		return Analysis{Reason: err.Error(), Findings: []diagnostics.Finding{}, Failures: []analyzer.RuleFailure{},
			Metadata: app.MetadataStatus{Unresolved: []app.UnresolvedTable{}}, Probes: []instrumentation.Probe{}}
	}
	return Analysis{
		ID:           a.ID,
		Available:    true,
		Findings:     a.Findings,
		Failures:     a.Failures,
		Rules:        a.Rules,
		Statements:   a.Statements,
		SyntaxErrors: a.SyntaxErrors,
		Metadata:     a.Metadata,
		Probes:       a.Probes,
	}
}

// ProbeRun is the result of running approved probes.
type ProbeRun struct {
	// Error is set when the probes could not run at all (unknown analysis, over budget, not
	// connected); each probe's own failure is in its result.
	Error     string                      `json:"error"`
	Results   []instrumentation.Result    `json:"results"`
	Findings  []diagnostics.Finding       `json:"findings"`
	Dismissed []instrumentation.Dismissal `json:"dismissed"`
}

// RunProbes runs the probes the user approved, by ID, for an analysis. The SQL comes from the
// analysis kept on the server, never from the caller. Cancelling the call cancels every probe.
func (s *AnalysisService) RunProbes(ctx context.Context, analysisID string, probeIDs []string) ProbeRun {
	run, err := s.core.RunProbes(ctx, analysisID, probeIDs)
	if err != nil {
		return ProbeRun{Error: err.Error(), Results: []instrumentation.Result{}, Findings: []diagnostics.Finding{},
			Dismissed: []instrumentation.Dismissal{}}
	}
	return ProbeRun{Results: run.Results, Findings: run.Findings, Dismissed: run.Dismissed}
}

// ForgetMetadata drops the cached catalog metadata of a connection, so the next analysis reads
// it again. The explorer calls it when it refreshes.
func (s *AnalysisService) ForgetMetadata(connectionID string) {
	s.core.ForgetMetadata(connectionID)
}
