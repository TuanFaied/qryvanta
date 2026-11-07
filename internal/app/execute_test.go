package app

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/config"
	"github.com/TuanFaied/qryvanta/internal/connection"
	"github.com/TuanFaied/qryvanta/internal/executor"
)

type nopDriver struct{}

func (nopDriver) Open(connection.Profile, string, connection.OpenOptions) (*sql.DB, error) {
	return sql.OpenDB(nopConnector{}), nil
}
func (nopDriver) ServerInfo(context.Context, *sql.DB) (connection.ServerInfo, error) {
	return connection.ServerInfo{Database: "master"}, nil
}

type nopConnector struct{}

func (nopConnector) Connect(context.Context) (driver.Conn, error) {
	return nil, errors.New("not dialled")
}
func (nopConnector) Driver() driver.Driver { return nil }

// fakeRunner returns a canned summary and remembers the request.
type fakeRunner struct{ got executor.Request }

func (f *fakeRunner) Execute(_ context.Context, _ *sql.DB, req executor.Request, sink executor.Sink) executor.Summary {
	f.got = req
	_ = sink.ResultSet(0, []executor.Column{{Name: "secret_column"}})
	_ = sink.Rows(0, [][]executor.Value{{"row-value-must-not-be-logged"}})
	return executor.Summary{Outcome: executor.OutcomeSucceeded, TotalRows: 1, ResultSets: []executor.ResultSet{{Rows: 1}}}
}

func TestExecuteUsesLimitsAndNeverLogsRows(t *testing.T) {
	var logs bytes.Buffer
	runner := &fakeRunner{}
	c, err := New(Options{
		ConfigPath:     filepath.Join(t.TempDir(), "config.json"),
		DisableFileLog: true,
		LogExtra:       &logs,
		Credentials:    connection.NewMemoryStore(),
		Driver:         nopDriver{},
		Access:         noAccess{},
		Runner:         runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	cfg := c.Config()
	cfg.Logging.Level = config.LogDebug
	if err := c.UpdateConfig(cfg); err != nil {
		t.Fatal(err)
	}

	if sum := c.Execute(context.Background(), "nope", executor.Request{SQL: "SELECT 1"}, &executor.Collector{}); sum.Outcome != executor.OutcomeFailed {
		t.Fatalf("not connected: %+v", sum)
	}

	p, _ := c.Connections().Create(connection.Profile{Name: "x", Server: "db", Auth: connection.AuthWindows}, "")
	if _, err := c.Pool().Connect(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	req := executor.Request{SQL: "SELECT secret_sql_text", Database: "Sales", Limits: c.ExecutionLimits()}
	sum := c.Execute(context.Background(), p.ID, req, &executor.Collector{})
	if sum.Outcome != executor.OutcomeSucceeded || runner.got.Limits.MaxRows != 10_000 || runner.got.Limits.MaxBytes != 64<<20 {
		t.Fatalf("summary %+v, limits %+v", sum, runner.got.Limits)
	}
	if !runner.got.Policy.ReadOnly || runner.got.Policy.LockTimeout.Seconds() != 5 {
		t.Errorf("policy = %+v, want read-only with a 5 s lock timeout from the defaults", runner.got.Policy)
	}

	// A caller cannot bypass the configured policy by passing its own.
	req.Policy = executor.Policy{ReadOnly: false}
	_ = c.Execute(context.Background(), p.ID, req, &executor.Collector{})
	if !runner.got.Policy.ReadOnly {
		t.Error("caller-supplied policy overrode read-only mode")
	}
	cfg = c.Config()
	cfg.Execution.ReadOnly = false
	if err := c.UpdateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	_ = c.Execute(context.Background(), p.ID, req, &executor.Collector{})
	if runner.got.Policy.ReadOnly {
		t.Error("turning read-only mode off in settings had no effect")
	}
	out := logs.String()
	if !strings.Contains(out, "query executed") {
		t.Fatalf("expected a debug log entry:\n%s", out)
	}
	for _, leak := range []string{"row-value-must-not-be-logged", "secret_sql_text"} {
		if strings.Contains(out, leak) {
			t.Errorf("log contains %q (SQL text logging is off by default)", leak)
		}
	}
}

func TestAllowWritesOptionTurnsReadOnlyOff(t *testing.T) {
	for _, allow := range []bool{false, true} {
		c, err := New(Options{
			ConfigPath:     filepath.Join(t.TempDir(), "config.json"),
			DisableFileLog: true,
			Credentials:    connection.NewMemoryStore(),
			AllowWrites:    allow,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := c.ExecutionPolicy().ReadOnly; got == allow {
			t.Errorf("AllowWrites=%v: ReadOnly=%v", allow, got)
		}
		_ = c.Close()
	}
}

type noAccess struct{}

func (noAccess) WriteAccess(_ context.Context, _ *sql.DB, database string) (connection.WriteAccess, error) {
	return connection.WriteAccess{Database: database, Level: connection.AccessReadOnly}, nil
}
