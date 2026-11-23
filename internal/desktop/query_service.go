package desktop

import (
	"context"
	"errors"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/executor"
	"github.com/TuanFaied/qryvanta/internal/parser"
)

// EventExecution carries the results of a running query to the frontend.
const EventExecution = "qryvanta:execution"

func init() {
	application.RegisterEvent[ExecutionEvent](EventExecution)
}

// Execution event kinds, in the order a run produces them.
const (
	KindBatch     = "batch"
	KindResultSet = "resultSet"
	KindRows      = "rows"
	KindMessage   = "message"
	// KindPlan: a plan document arrived (Plan describes it; fetch its XML with PlanXML).
	KindPlan = "plan"
	KindDone = "done"
)

// keptPlanRuns is how many runs keep their plans in memory.
const keptPlanRuns = 20

// PlanInfo describes a plan of a run, without its XML.
type PlanInfo struct {
	// Index is the plan's position in the run, for PlanXML.
	Index     int               `json:"index"`
	Mode      executor.PlanMode `json:"mode"`
	Batch     int               `json:"batch"`
	Execution int               `json:"execution"`
	Statement int               `json:"statement"`
	Bytes     int               `json:"bytes"`
}

// ExecutionEvent is one piece of a run's output. Events of one run arrive in server order,
// and "done" is always last. The frontend drops events whose RunID is not the run it is
// showing, so a cancelled or replaced run can never overwrite a newer one.
type ExecutionEvent struct {
	RunID string `json:"runId"`
	// Kind is batch (a batch execution starts), resultSet, rows, message or done.
	Kind string `json:"kind"`
	// Index is the result set the event belongs to (resultSet and rows).
	Index int `json:"index"`
	// Batch is the batch a resultSet belongs to.
	Batch   int                `json:"batch"`
	Columns []executor.Column  `json:"columns"`
	Rows    [][]executor.Value `json:"rows"`
	Message *executor.Message  `json:"message"`
	Run     *executor.BatchRun `json:"run"`
	Summary *executor.Summary  `json:"summary"`
	// Plan describes a plan document (kind "plan").
	Plan *PlanInfo `json:"plan"`
}

// SyntaxCheck is the editor's view of a script: its syntax errors, or why it could not be
// checked.
type SyntaxCheck struct {
	// Available is false when the parser cannot run; Reason says why.
	Available   bool                `json:"available"`
	Reason      string              `json:"reason"`
	Batches     int                 `json:"batches"`
	Diagnostics []parser.Diagnostic `json:"diagnostics"`
}

// QueryService runs queries from the editor and streams their results as events.
type QueryService struct {
	core *app.Core
	emit func(ExecutionEvent)

	mu   sync.Mutex
	runs map[string]context.CancelFunc

	plansMu  sync.Mutex
	plans    map[string][]executor.Plan // plan documents of recent runs, by run ID
	planRuns []string                   // run IDs in arrival order, to forget the oldest
}

// NewQueryService creates the service. Events go to every window of the running app.
func NewQueryService(core *app.Core) *QueryService {
	return newQueryService(core, func(e ExecutionEvent) {
		if a := application.Get(); a != nil {
			a.Event.Emit(EventExecution, e)
		}
	})
}

func newQueryService(core *app.Core, emit func(ExecutionEvent)) *QueryService {
	return &QueryService{core: core, emit: emit, runs: map[string]context.CancelFunc{}, plans: map[string][]executor.Plan{}}
}

// MaxRunIDLength bounds the frontend-chosen run ID.
const MaxRunIDLength = 64

// Execute runs sql on the connection, in database (empty: the connection's current
// database), under the limits and read-only policy from the settings. Results are sent as
// "qryvanta:execution" events tagged with runID, which the caller chooses so it can match
// events that arrive before this call returns. The call returns the summary when the run has
// ended; cancelling the call (or calling Cancel) cancels the query on the server.
//
// plan asks for execution plans: "" (none), "estimated" (the script is compiled, not run) or
// "actual" (the script runs and each statement's plan follows its results). Plans never appear as
// result sets: they arrive as "plan" events and are kept in memory with the run (PlanXML).
func (s *QueryService) Execute(ctx context.Context, runID, connectionID, database, sql, plan string) (executor.Summary, error) {
	if runID == "" || len(runID) > MaxRunIDLength {
		return executor.Summary{}, errors.New("invalid run ID")
	}
	mode := executor.PlanMode(plan)
	if mode != executor.PlanNone && mode != executor.PlanEstimated && mode != executor.PlanActual {
		return executor.Summary{}, errors.New("plan must be empty, estimated or actual")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.mu.Lock()
	if _, busy := s.runs[runID]; busy {
		s.mu.Unlock()
		return executor.Summary{}, errors.New("a run with this ID is already in progress")
	}
	s.runs[runID] = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.runs, runID)
		s.mu.Unlock()
	}()

	req := executor.Request{SQL: sql, Database: database, Limits: s.core.ExecutionLimits(), Plan: mode}
	sum := s.core.Execute(ctx, connectionID, req, &eventSink{runID: runID, emit: s.emit, keep: s.keepPlan})
	s.emit(ExecutionEvent{RunID: runID, Kind: KindDone, Summary: &sum})
	return sum, nil
}

// CheckSyntax parses sql for the editor's syntax markers. Cancelling the call (the editor does
// when the text changes again) abandons the check without restarting the parser.
func (s *QueryService) CheckSyntax(ctx context.Context, sql string) SyntaxCheck {
	res, err := s.core.Parser().Parse(ctx, sql, parser.Options{})
	if err != nil {
		return SyntaxCheck{Reason: err.Error(), Diagnostics: []parser.Diagnostic{}}
	}
	diags := res.Diagnostics
	if diags == nil {
		diags = []parser.Diagnostic{}
	}
	return SyntaxCheck{Available: true, Batches: len(res.Batches), Diagnostics: diags}
}

// CompletionScope returns what the query at a UTF-16 offset of sql can refer to, for
// autocomplete. Found is false when the parser is unavailable or the text around the cursor
// does not parse even with a placeholder; the editor then uses its heuristic.
func (s *QueryService) CompletionScope(ctx context.Context, sql string, offset int) parser.Scope {
	scope, err := s.core.CompletionScope(ctx, sql, offset)
	if err != nil || !scope.Found {
		return parser.Scope{Sources: []parser.ScopeSource{}, Ctes: []parser.ScopeSource{}}
	}
	return scope
}

// Cancel stops a run. It reports whether the run was still in progress.
func (s *QueryService) Cancel(runID string) bool {
	s.mu.Lock()
	cancel, ok := s.runs[runID]
	s.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

// eventSink forwards results as events. Each batch slice is freshly allocated by the
// executor, so it can be handed to the asynchronous event dispatcher as it is.
type eventSink struct {
	runID string
	emit  func(ExecutionEvent)
	batch int
	// keep stores a plan document and returns its index.
	keep func(runID string, p executor.Plan) int
}

// Plan keeps the document with the run and announces it without its XML (plans can be large).
func (k *eventSink) Plan(p executor.Plan) error {
	index := k.keep(k.runID, p)
	k.emit(ExecutionEvent{RunID: k.runID, Kind: KindPlan, Plan: &PlanInfo{Index: index, Mode: p.Mode, Batch: p.Batch,
		Execution: p.Execution, Statement: p.Statement, Bytes: len(p.XML)}})
	return nil
}

func (k *eventSink) BatchStart(run executor.BatchRun) error {
	k.batch = run.Batch
	k.emit(ExecutionEvent{RunID: k.runID, Kind: KindBatch, Run: &run})
	return nil
}

func (k *eventSink) ResultSet(index int, columns []executor.Column) error {
	k.emit(ExecutionEvent{RunID: k.runID, Kind: KindResultSet, Index: index, Batch: k.batch, Columns: columns})
	return nil
}

func (k *eventSink) Rows(index int, rows [][]executor.Value) error {
	k.emit(ExecutionEvent{RunID: k.runID, Kind: KindRows, Index: index, Rows: rows})
	return nil
}

func (k *eventSink) Message(m executor.Message) error {
	k.emit(ExecutionEvent{RunID: k.runID, Kind: KindMessage, Message: &m})
	return nil
}

// keepPlan stores a plan document of a run and returns its index. The plans of the oldest runs
// are forgotten beyond keptPlanRuns.
func (s *QueryService) keepPlan(runID string, p executor.Plan) int {
	s.plansMu.Lock()
	defer s.plansMu.Unlock()
	if _, ok := s.plans[runID]; !ok {
		s.planRuns = append(s.planRuns, runID)
		if len(s.planRuns) > keptPlanRuns {
			delete(s.plans, s.planRuns[0])
			s.planRuns = s.planRuns[1:]
		}
	}
	s.plans[runID] = append(s.plans[runID], p)
	return len(s.plans[runID]) - 1
}

// PlanXML returns a plan document of a recent run: the showplan XML, which SSMS can open as a
// .sqlplan file.
func (s *QueryService) PlanXML(runID string, index int) (string, error) {
	s.plansMu.Lock()
	defer s.plansMu.Unlock()
	plans, ok := s.plans[runID]
	if !ok {
		return "", errors.New("the plans of this run are no longer kept; run it again")
	}
	if index < 0 || index >= len(plans) {
		return "", errors.New("no such plan")
	}
	return plans[index].XML, nil
}
