package desktop

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/connection"
	"github.com/TuanFaied/qryvanta/internal/executor"
)

// scriptedRunner streams one result set and a message, or waits for cancellation.
type scriptedRunner struct {
	block   bool
	started chan struct{}
	got     executor.Request
}

func (r *scriptedRunner) Execute(ctx context.Context, _ *sql.DB, req executor.Request, sink executor.Sink) executor.Summary {
	r.got = req
	if r.block {
		close(r.started)
		<-ctx.Done()
		return executor.Summary{Outcome: executor.OutcomeCancelled}
	}
	_ = sink.ResultSet(0, []executor.Column{{Name: "id", Type: "INT"}})
	_ = sink.Rows(0, [][]executor.Value{{int64(1)}, {nil}})
	_ = sink.Message(executor.Message{Kind: "info", Text: "(2 rows affected)", AfterResultSet: 1})
	return executor.Summary{Outcome: executor.OutcomeSucceeded, TotalRows: 2}
}

type eventLog struct {
	mu     sync.Mutex
	events []ExecutionEvent
}

func (l *eventLog) add(e ExecutionEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}

func newQueryTestService(t *testing.T, runner executor.Runner) (*QueryService, *eventLog, string) {
	t.Helper()
	core, err := app.New(app.Options{
		ConfigPath:     filepath.Join(t.TempDir(), "config.json"),
		DisableFileLog: true,
		Credentials:    connection.NewMemoryStore(),
		Driver:         &fakeDriver{},
		Runner:         runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = core.Close() })
	conns := NewConnectionService(core)
	saved, _ := conns.Save(sqlInput())
	id := saved.Connection.Profile.ID
	if r, _ := conns.Connect(context.Background(), id); !r.OK {
		t.Fatal("connect failed")
	}
	log := &eventLog{}
	return newQueryService(core, log.add), log, id
}

func TestQueryServiceStreamsEventsInOrder(t *testing.T) {
	runner := &scriptedRunner{}
	s, log, id := newQueryTestService(t, runner)

	sum, err := s.Execute(context.Background(), "run-1", id, "Sales", "SELECT id FROM t")
	if err != nil || sum.Outcome != executor.OutcomeSucceeded {
		t.Fatalf("Execute = %+v, %v", sum, err)
	}
	kinds := []string{}
	for _, e := range log.events {
		if e.RunID != "run-1" {
			t.Fatalf("event with run ID %q", e.RunID)
		}
		kinds = append(kinds, e.Kind)
	}
	want := []string{KindResultSet, KindRows, KindMessage, KindDone}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", kinds, want)
		}
	}
	if len(log.events[1].Rows) != 2 || log.events[3].Summary.TotalRows != 2 {
		t.Errorf("rows event %+v, done %+v", log.events[1], log.events[3].Summary)
	}
	// The request carries the database and the configured limits; the policy is set by the core.
	if runner.got.Database != "Sales" || runner.got.Limits.MaxRows != 10_000 || !runner.got.Policy.ReadOnly {
		t.Errorf("request = %+v", runner.got)
	}
}

func TestQueryServiceCancel(t *testing.T) {
	runner := &scriptedRunner{block: true, started: make(chan struct{})}
	s, log, id := newQueryTestService(t, runner)

	done := make(chan executor.Summary)
	go func() {
		sum, _ := s.Execute(context.Background(), "run-2", id, "", "WAITFOR DELAY '00:00:30'")
		done <- sum
	}()
	<-runner.started

	if _, err := s.Execute(context.Background(), "run-2", id, "", "SELECT 1"); err == nil {
		t.Error("a second run with the same ID should be rejected")
	}
	if !s.Cancel("run-2") {
		t.Fatal("Cancel reported no such run")
	}
	select {
	case sum := <-done:
		if sum.Outcome != executor.OutcomeCancelled {
			t.Fatalf("outcome = %s", sum.Outcome)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not stop after Cancel")
	}
	if last := log.events[len(log.events)-1]; last.Kind != KindDone {
		t.Errorf("last event = %+v, want done", last)
	}
	if s.Cancel("run-2") {
		t.Error("Cancel after the run ended should report false")
	}
}

func TestQueryServiceRejectsBadRunIDsAndUnknownConnections(t *testing.T) {
	s, log, _ := newQueryTestService(t, &scriptedRunner{})
	if _, err := s.Execute(context.Background(), "", "x", "", "SELECT 1"); err == nil {
		t.Error("empty run ID accepted")
	}
	sum, err := s.Execute(context.Background(), "run-3", "not-connected", "", "SELECT 1")
	if err != nil || sum.Outcome != executor.OutcomeFailed {
		t.Fatalf("Execute(not connected) = %+v, %v", sum, err)
	}
	if last := log.events[len(log.events)-1]; last.Kind != KindDone || last.Summary.Error == "" {
		t.Errorf("done event = %+v", last)
	}
}
