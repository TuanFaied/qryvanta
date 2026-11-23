package desktop

import (
	"context"
	"errors"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/TuanFaied/qryvanta/internal/analyzer"
	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/instrumentation"
)

// EventDiagnosis carries the step statuses of a running diagnosis to the frontend.
const EventDiagnosis = "qryvanta:diagnosis"

func init() {
	application.RegisterEvent[DiagnosisEvent](EventDiagnosis)
}

// DiagnosisEvent is one step status of a diagnosis ("Execute with debugging"). The frontend drops
// events whose RunID is not the diagnosis it shows.
type DiagnosisEvent struct {
	RunID  string         `json:"runId"`
	Step   app.Step       `json:"step"`
	Status app.StepStatus `json:"status"`
	Reason string         `json:"reason"`
	// Findings is the number of findings after the step, when it changed them (-1 otherwise).
	Findings int `json:"findings"`
}

// AnalysisService runs the diagnostic rules for the analysis panel. It executes nothing but
// probes the user approved; with a connection it reads catalog metadata for the metadata rules.
type AnalysisService struct {
	core *app.Core
	emit func(DiagnosisEvent)
}

// NewAnalysisService creates the service. Diagnosis events go to every window of the running app.
func NewAnalysisService(core *app.Core) *AnalysisService {
	return newAnalysisService(core, func(e DiagnosisEvent) {
		if a := application.Get(); a != nil {
			a.Event.Emit(EventDiagnosis, e)
		}
	})
}

func newAnalysisService(core *app.Core, emit func(DiagnosisEvent)) *AnalysisService {
	return &AnalysisService{core: core, emit: emit}
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
	// Steps is the final status of each step of a diagnosis (empty for Analyze).
	Steps []app.StepEvent `json:"steps"`
	// Stopped is true when the diagnosis was cancelled part-way; the findings are those found so far.
	Stopped bool `json:"stopped"`
}

// Analyze runs the rules over sql. connectionID and database select the catalog the metadata
// rules read; with an empty connectionID, or one that is not connected, they are skipped and
// Metadata says so. Lists are never null.
func (s *AnalysisService) Analyze(ctx context.Context, sql, connectionID, database string) Analysis {
	a, err := s.core.Analyze(ctx, sql, app.AnalysisTarget{ConnectionID: connectionID, Database: database})
	if err != nil {
		return Analysis{Reason: err.Error(), Findings: []diagnostics.Finding{}, Failures: []analyzer.RuleFailure{},
			Metadata: app.MetadataStatus{Unresolved: []app.UnresolvedTable{}}, Probes: []instrumentation.Probe{}, Steps: []app.StepEvent{}}
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
		Steps:        []app.StepEvent{},
	}
}

// Diagnose runs the debugging flow ("Execute with debugging"): parse and policy, static rules,
// metadata rules, and proposed probes, which wait for the user's approval (RunProbes). Each step's
// status is emitted as an EventDiagnosis with runID as it happens. Cancelling the call stops the
// flow; the steps not reached are reported as skipped and the findings so far are returned.
func (s *AnalysisService) Diagnose(ctx context.Context, runID, sql, connectionID, database string) Analysis {
	d, err := s.core.Diagnose(ctx, sql, app.DiagnoseOptions{
		Target: app.AnalysisTarget{ConnectionID: connectionID, Database: database},
		Emit: func(e app.StepEvent) {
			n := -1
			if e.Findings != nil {
				n = len(e.Findings)
			}
			s.emit(DiagnosisEvent{RunID: runID, Step: e.Step, Status: e.Status, Reason: e.Reason, Findings: n})
		},
	})
	stopped := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
	if err != nil && !stopped {
		return Analysis{Reason: err.Error(), Findings: []diagnostics.Finding{}, Failures: []analyzer.RuleFailure{},
			Metadata: app.MetadataStatus{Unresolved: []app.UnresolvedTable{}}, Probes: []instrumentation.Probe{}, Steps: d.Steps}
	}
	return Analysis{
		ID:           d.ID,
		Available:    true,
		Findings:     d.Findings,
		Failures:     d.Failures,
		Rules:        d.Rules,
		Statements:   d.Statements,
		SyntaxErrors: d.SyntaxErrors,
		Metadata:     d.Metadata,
		Probes:       d.Probes,
		Steps:        d.Steps,
		Stopped:      stopped,
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
