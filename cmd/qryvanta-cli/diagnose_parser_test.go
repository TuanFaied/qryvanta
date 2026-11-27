//go:build parser

package main

// "diagnose" end to end with the real parser, a fake catalog and a fake runner that answers
// probes. Run (after building the sidecar):
//
//	QRYVANTA_PARSER_PATH=bin/qryvanta-tsqlparser.exe go test -tags parser ./cmd/qryvanta-cli/

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/connection"
	"github.com/TuanFaied/qryvanta/internal/executor"
	"github.com/TuanFaied/qryvanta/internal/schema"
)

// catalogReader knows the two tables of sample 01.
type catalogReader struct{}

func (catalogReader) Databases(context.Context, *sql.DB) ([]schema.Database, error) { return nil, nil }
func (catalogReader) Objects(context.Context, *sql.DB, string) ([]schema.Object, error) {
	return nil, nil
}
func (catalogReader) Columns(context.Context, *sql.DB, string, string, string) ([]schema.Column, error) {
	return nil, nil
}
func (catalogReader) Table(_ context.Context, _ *sql.DB, database, _, name string) (schema.Table, error) {
	col := func(n, typ string, nullable bool) schema.TableColumn {
		return schema.TableColumn{Name: n, Type: typ, DataType: typ, Nullable: nullable}
	}
	switch strings.ToLower(name) {
	case "employees":
		return schema.Table{Database: database, Schema: "dbo", Name: "Employees", Kind: schema.KindTable, Keys: [][]string{{"EmployeeID"}},
			Columns: []schema.TableColumn{col("EmployeeID", "int", false), col("Name", "nvarchar", false), col("DepartmentID", "int", true)}}, nil
	case "departments":
		return schema.Table{Database: database, Schema: "dbo", Name: "Departments", Kind: schema.KindTable, Keys: [][]string{{"DepartmentID"}},
			Columns: []schema.TableColumn{col("DepartmentID", "int", false), col("Name", "nvarchar", false)}}, nil
	}
	return schema.Table{}, schema.ErrObjectNotFound
}

// unmatchedRunner answers the unmatched-rows probe: 3 of 12 rows dropped, 2 with NULL.
type unmatchedRunner struct {
	mu   sync.Mutex
	runs int
}

func (r *unmatchedRunner) Execute(_ context.Context, _ *sql.DB, req executor.Request, sink executor.Sink) executor.Summary {
	r.mu.Lock()
	r.runs++
	r.mu.Unlock()
	if !req.Policy.ReadOnly {
		return executor.Summary{Outcome: executor.OutcomeFailed, Error: "a probe ran without the read-only guard"}
	}
	_ = sink.ResultSet(0, []executor.Column{{Name: "total_rows"}, {Name: "unmatched_rows"}, {Name: "null_key_rows"}})
	_ = sink.Rows(0, [][]executor.Value{{int64(12), int64(3), int64(2)}})
	return executor.Summary{Outcome: executor.OutcomeSucceeded}
}

func probeOpts(t *testing.T, budget int) (app.Options, *unmatchedRunner) {
	t.Helper()
	if os.Getenv("QRYVANTA_PARSER_PATH") == "" {
		t.Skip("QRYVANTA_PARSER_PATH not set; build the sidecar with `wails3 task common:build:sidecar`")
	}
	opts, _, _ := testOpts(t)
	opts.Parser = nil // the real sidecar
	runner := &unmatchedRunner{}
	opts.Runner = runner
	opts.Schema = catalogReader{}
	opts.Access = fakeAccess{level: connection.AccessReadOnly}
	core, err := app.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.Connections().Create(connection.Profile{Name: "Fixture", Server: "db", Database: "QryvantaSample",
		Auth: connection.AuthWindows}, ""); err != nil {
		t.Fatal(err)
	}
	cfg := core.Config()
	cfg.Execution.MaxProbesPerRun = budget
	if err := core.UpdateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	_ = core.Close()
	return opts, runner
}

var sample01 = filepath.Join("..", "..", "tests", "sample_queries", "01_employees_without_department.buggy.sql")

func diagnoseJSON(t *testing.T, opts app.Options, args ...string) (diagnoseResult, int) {
	t.Helper()
	stdout, stderr, code := diagnoseCmd(t, context.Background(), opts, append(args, "--format", "json")...)
	var res diagnoseResult
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("invalid JSON (exit %d, stderr %q): %v\n%s", code, stderr, err, stdout)
	}
	return res, code
}

func join005(t *testing.T, res diagnoseResult) string {
	t.Helper()
	for _, f := range res.Findings {
		if f.RuleID == "QRY-JOIN-005" {
			return string(f.Classification)
		}
	}
	t.Fatalf("no QRY-JOIN-005 in %+v", res.Findings)
	return ""
}

func TestDiagnoseProbeModes(t *testing.T) {
	terminal(t, false)

	opts, runner := probeOpts(t, 10)
	res, code := diagnoseJSON(t, opts, "--conn", "Fixture", "--plan", "none", "--file", sample01, "--probes", "none", "--fail-on", "medium")
	if code != exitFindings || join005(t, res) != "potential" || runner.runs != 0 || len(res.Probes) != 1 || res.Probes[0].Result != nil {
		t.Fatalf("none: exit %d, runs %d, result %+v", code, runner.runs, res)
	}

	res, _ = diagnoseJSON(t, opts, "--conn", "Fixture", "--plan", "none", "--file", sample01, "--probes", "all")
	if join005(t, res) != "confirmed" || runner.runs != 1 || res.Probes[0].Result == nil ||
		!strings.Contains(res.Probes[0].Result.Summary, "3 of 12 dbo.Employees rows have no match") {
		t.Fatalf("all: runs %d, result %+v", runner.runs, res)
	}

	withStdin(t, "y\n")
	stdout, stderr, _ := diagnoseCmd(t, context.Background(), opts, "--conn", "Fixture", "--plan", "none", "--file", sample01, "--probes", "ask")
	if runner.runs != 2 || !strings.Contains(stderr, "Run 1 probe(s)? [y/N]") || !strings.Contains(stderr, "NOT EXISTS") ||
		!strings.Contains(stdout, "Confirmed issue · QRY-JOIN-005") {
		t.Fatalf("ask y: runs %d\nstderr %s\nstdout %s", runner.runs, stderr, stdout)
	}

	withStdin(t, "n\n")
	stdout, _, _ = diagnoseCmd(t, context.Background(), opts, "--conn", "Fixture", "--plan", "none", "--file", sample01, "--probes", "ask")
	if runner.runs != 2 || !strings.Contains(stdout, "Potential issue · QRY-JOIN-005") {
		t.Fatalf("ask n: runs %d\n%s", runner.runs, stdout)
	}
}

func TestDiagnoseProbesRespectTheBudget(t *testing.T) {
	terminal(t, false)
	opts, runner := probeOpts(t, 0)
	res, _ := diagnoseJSON(t, opts, "--conn", "Fixture", "--plan", "none", "--file", sample01, "--probes", "all")
	if runner.runs != 0 || len(res.Probes) != 1 || !res.Probes[0].OverBudget || res.Steps[5].Status != app.StepSkipped {
		t.Fatalf("runs %d, result %+v", runner.runs, res)
	}
}

func TestDiagnoseStaticFindingsAndFailOn(t *testing.T) {
	terminal(t, false)
	opts, _ := probeOpts(t, 10)
	query := "SELECT * FROM t WHERE a = NULL"
	res, code := diagnoseJSON(t, opts, "--query", query)
	if code != exitFindings || len(res.Findings) != 1 || res.Findings[0].RuleID != "QRY-NULL-001" {
		t.Fatalf("exit %d, findings %+v", code, res.Findings)
	}
	if _, code = diagnoseJSON(t, opts, "--query", query, "--fail-on", "critical"); code != 0 {
		t.Fatalf("--fail-on critical: exit %d, want 0", code)
	}
	if _, code = diagnoseJSON(t, opts, "--query", "SELECT a FROM t WHERE a IS NULL"); code != 0 {
		t.Fatalf("no findings: exit %d", code)
	}
}
