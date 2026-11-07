package desktop

import (
	"context"
	"errors"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/executor"
)

// EventExecution carries the results of a running query to the frontend.
const EventExecution = "qryvanta:execution"

func init() {
	application.RegisterEvent[ExecutionEvent](EventExecution)
}

// Execution event kinds, in the order a run produces them.
const (
	KindResultSet = "resultSet"
	KindRows      = "rows"
	KindMessage   = "message"
	KindDone      = "done"
)

// ExecutionEvent is one piece of a run's output. Events of one run arrive in server order,
// and "done" is always last. The frontend drops events whose RunID is not the run it is
// showing, so a cancelled or replaced run can never overwrite a newer one.
type ExecutionEvent struct {
	RunID string `json:"runId"`
	// Kind is resultSet, rows, message or done.
	Kind string `json:"kind"`
	// Index is the result set the event belongs to (resultSet and rows).
	Index   int                `json:"index"`
	Columns []executor.Column  `json:"columns"`
	Rows    [][]executor.Value `json:"rows"`
	Message *executor.Message  `json:"message"`
	Summary *executor.Summary  `json:"summary"`
}

// QueryService runs queries from the editor and streams their results as events.
type QueryService struct {
	core *app.Core
	emit func(ExecutionEvent)

	mu   sync.Mutex
	runs map[string]context.CancelFunc
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
	return &QueryService{core: core, emit: emit, runs: map[string]context.CancelFunc{}}
}

// MaxRunIDLength bounds the frontend-chosen run ID.
const MaxRunIDLength = 64

// Execute runs sql on the connection, in database (empty: the connection's current
// database), under the limits and read-only policy from the settings. Results are sent as
// "qryvanta:execution" events tagged with runID, which the caller chooses so it can match
// events that arrive before this call returns. The call returns the summary when the run has
// ended; cancelling the call (or calling Cancel) cancels the query on the server.
func (s *QueryService) Execute(ctx context.Context, runID, connectionID, database, sql string) (executor.Summary, error) {
	if runID == "" || len(runID) > MaxRunIDLength {
		return executor.Summary{}, errors.New("invalid run ID")
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

	req := executor.Request{SQL: sql, Database: database, Limits: s.core.ExecutionLimits()}
	sum := s.core.Execute(ctx, connectionID, req, &eventSink{runID: runID, emit: s.emit})
	s.emit(ExecutionEvent{RunID: runID, Kind: KindDone, Summary: &sum})
	return sum, nil
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
}

func (k *eventSink) ResultSet(index int, columns []executor.Column) error {
	k.emit(ExecutionEvent{RunID: k.runID, Kind: KindResultSet, Index: index, Columns: columns})
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
