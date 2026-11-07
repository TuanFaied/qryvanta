package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/connection"
	"github.com/TuanFaied/qryvanta/internal/executor"
	"github.com/TuanFaied/qryvanta/internal/sqlfile"
)

// fakeRunner returns a scripted result and remembers the request.
type fakeRunner struct {
	got     executor.Request
	sets    [][]executor.Column
	rows    [][][]executor.Value
	msgs    []executor.Message
	summary executor.Summary
}

func (f *fakeRunner) Execute(_ context.Context, _ *sql.DB, req executor.Request, sink executor.Sink) executor.Summary {
	f.got = req
	for _, m := range f.msgs {
		_ = sink.Message(m)
	}
	for i, cols := range f.sets {
		_ = sink.ResultSet(i, cols)
		_ = sink.Rows(i, f.rows[i])
	}
	s := f.summary
	if s.Outcome == "" {
		s.Outcome = executor.OutcomeSucceeded
	}
	return s
}

type fakeAccess struct{ level connection.AccessLevel }

func (f fakeAccess) WriteAccess(_ context.Context, _ *sql.DB, database string) (connection.WriteAccess, error) {
	return connection.WriteAccess{Database: database, Level: f.level,
		Reasons: []connection.AccessReason{{Text: "member of db_owner"}}}, nil
}

func ordersRunner() *fakeRunner {
	return &fakeRunner{
		sets: [][]executor.Column{{{Name: "OrderID", Type: "INT"}, {Name: "Customer", Type: "NVARCHAR", Nullable: true}}},
		rows: [][][]executor.Value{{
			{int64(101), "Nimal Perera"},
			{int64(102), nil},
			{int64(103), strings.Repeat("x", 60)},
		}},
		summary: executor.Summary{TotalRows: 3, ElapsedMs: 7, RowsAffected: []int64{}},
	}
}

// runSetup saves one connection and returns options using the fake runner.
func runSetup(t *testing.T, runner executor.Runner, access connection.AccessLevel, names ...string) app.Options {
	t.Helper()
	opts, _, _ := testOpts(t)
	opts.Runner = runner
	opts.Access = fakeAccess{level: access}
	if len(names) == 0 {
		names = []string{"Fixture"}
	}
	core, err := app.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if _, err := core.Connections().Create(connection.Profile{Name: n, Server: "db", Auth: connection.AuthWindows}, ""); err != nil {
			t.Fatal(err)
		}
	}
	_ = core.Close()
	return opts
}

func runCmd(t *testing.T, opts app.Options, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := cmdRun(context.Background(), args, opts, &out, &errOut)
	code = report(err, &bytes.Buffer{}, &errOut)
	return out.String(), errOut.String(), code
}

func TestRunTableFormat(t *testing.T) {
	runner := ordersRunner()
	runner.msgs = []executor.Message{{Kind: "info", Text: "hello from PRINT"}}
	opts := runSetup(t, runner, connection.AccessReadOnly)

	out, errOut, code := runCmd(t, opts, "--query", "SELECT OrderID, Customer FROM dbo.Orders")
	if code != 0 {
		t.Fatalf("exit %d, stderr %s", code, errOut)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	want := []string{
		"OrderID  Customer",
		"-------  ----------------------------------------",
		"101      Nimal Perera",
		"102      NULL",
		"103      " + strings.Repeat("x", 39) + "…",
		"(3 rows)",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("table output:\n%s\nwant:\n%s", out, strings.Join(want, "\n"))
	}
	if !strings.Contains(errOut, "hello from PRINT") || strings.Contains(out, "PRINT") {
		t.Errorf("messages belong on stderr: stdout %q, stderr %q", out, errOut)
	}
	// Read-only mode and the configured limits are used by default.
	if runner.got.Limits.MaxRows != 10_000 || runner.got.Limits.Timeout != 30*time.Second {
		t.Errorf("limits = %+v", runner.got.Limits)
	}
}

func TestRunJSONFormat(t *testing.T) {
	runner := ordersRunner()
	runner.msgs = []executor.Message{{Kind: "error", Number: 207, Severity: 16, State: 1, Line: 3, Text: "Invalid column name 'x'."}}
	runner.summary.ErrorCount = 1
	opts := runSetup(t, runner, connection.AccessReadOnly)

	out, _, code := runCmd(t, opts, "--query", "SELECT 1", "--format", "json", "--database", "Sales")
	if code != exitQueryError {
		t.Errorf("exit = %d, want %d for a batch with errors", code, exitQueryError)
	}
	var res runResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if res.Connection != "Fixture" || res.Database != "Sales" || !res.ReadOnly || res.Outcome != executor.OutcomeSucceeded {
		t.Errorf("result = %+v", res)
	}
	if len(res.ResultSets) != 1 || len(res.ResultSets[0].Rows) != 3 || res.ResultSets[0].Rows[1][1] != nil {
		t.Errorf("result sets = %+v", res.ResultSets)
	}
	if res.ErrorCount != 1 || res.Messages[0].Line != 3 || res.MaxRows != 10_000 || res.TimeoutSecs != 30 {
		t.Errorf("messages/limits = %+v", res)
	}
	if runner.got.Database != "Sales" {
		t.Errorf("database = %q", runner.got.Database)
	}
}

func TestRunCSVFormat(t *testing.T) {
	runner := ordersRunner()
	runner.rows[0][2][1] = `say "hi", then go`
	opts := runSetup(t, runner, connection.AccessReadOnly)
	out, _, code := runCmd(t, opts, "--query", "SELECT 1", "--format", "csv")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	want := "OrderID,Customer\n101,Nimal Perera\n102,\n103,\"say \"\"hi\"\", then go\"\n"
	if out != want {
		t.Errorf("csv = %q, want %q", out, want)
	}

	two := ordersRunner()
	two.sets = append(two.sets, two.sets[0])
	two.rows = append(two.rows, two.rows[0])
	_, errOut, code := runCmd(t, runSetup(t, two, connection.AccessReadOnly), "--query", "SELECT 1; SELECT 2", "--format", "csv")
	if code != 2 || !strings.Contains(errOut, "one result set") {
		t.Errorf("csv with two result sets: exit %d, %s", code, errOut)
	}
}

func TestRunExitCodes(t *testing.T) {
	cases := []struct {
		outcome executor.Outcome
		code    int
		stderr  string
	}{
		{executor.OutcomeSucceeded, 0, ""},
		{executor.OutcomeTruncated, 0, "Stopped reading at the 5-row limit"},
		{executor.OutcomeFailed, exitQueryError, "Cannot open database"},
		{executor.OutcomeTimedOut, exitStopped, "timed out after 2 s"},
		{executor.OutcomeCancelled, exitStopped, "cancelled"},
	}
	for _, c := range cases {
		t.Run(string(c.outcome), func(t *testing.T) {
			runner := ordersRunner()
			runner.summary.Outcome = c.outcome
			if c.outcome == executor.OutcomeFailed {
				runner.summary.Error = "Cannot open database \"Nope\"."
			}
			_, errOut, code := runCmd(t, runSetup(t, runner, connection.AccessReadOnly), "--query", "SELECT 1", "--max-rows", "5", "--timeout", "2")
			if code != c.code || !strings.Contains(errOut, c.stderr) {
				t.Errorf("exit %d (want %d), stderr %q (want %q)", code, c.code, errOut, c.stderr)
			}
			if runner.got.Limits.MaxRows != 5 || runner.got.Limits.Timeout != 2*time.Second {
				t.Errorf("limits = %+v", runner.got.Limits)
			}
		})
	}
}

func TestRunUsageErrors(t *testing.T) {
	opts := runSetup(t, ordersRunner(), connection.AccessReadOnly)
	for _, args := range [][]string{
		{"--format", "xml", "--query", "SELECT 1"},
		{"--query", "SELECT 1", "--file", "a.sql"},
		{"--query", "   "},
		{"--query", "SELECT 1", "--max-rows", "-1"},
		{"--query", "SELECT 1", "--timeout", "99999"},
		{"SELECT 1"},
	} {
		if _, _, code := runCmd(t, opts, args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}

func TestRunChoosesConnection(t *testing.T) {
	opts := runSetup(t, ordersRunner(), connection.AccessReadOnly, "A", "B")
	if _, errOut, code := runCmd(t, opts, "--query", "SELECT 1"); code != 2 || !strings.Contains(errOut, "--conn") {
		t.Errorf("two connections without --conn: exit %d, %s", code, errOut)
	}
	if _, _, code := runCmd(t, opts, "--query", "SELECT 1", "--conn", "b"); code != 0 {
		t.Errorf("--conn b: exit %d", code)
	}
	if _, errOut, code := runCmd(t, opts, "--query", "SELECT 1", "--conn", "C"); code != 1 || !strings.Contains(errOut, `"C"`) {
		t.Errorf("unknown connection: exit %d, %s", code, errOut)
	}
}

func TestRunReadsFileAndStdin(t *testing.T) {
	runner := ordersRunner()
	opts := runSetup(t, runner, connection.AccessReadOnly)

	path := filepath.Join(t.TempDir(), "q.sql")
	if err := sqlfile.Write(path, "SELECT N'東京';", sqlfile.UTF16LE); err != nil {
		t.Fatal(err)
	}
	if _, _, code := runCmd(t, opts, "--file", path); code != 0 || runner.got.SQL != "SELECT N'東京';" {
		t.Errorf("--file: exit %d, sql %q", code, runner.got.SQL)
	}

	// PowerShell pipes text with a byte-order mark.
	withStdin(t, "\xef\xbb\xbfSELECT 42;\r\n")
	if _, _, code := runCmd(t, opts); code != 0 || runner.got.SQL != "SELECT 42;\r\n" {
		t.Errorf("stdin: exit %d, sql %q", code, runner.got.SQL)
	}

	if _, _, code := runCmd(t, opts, "--file", filepath.Join(t.TempDir(), "missing.sql")); code != 1 {
		t.Errorf("missing file: exit %d", code)
	}
	_ = os.Remove(path)
}

func TestRunWritesPolicyAndWarnings(t *testing.T) {
	runner := ordersRunner()
	opts := runSetup(t, runner, connection.AccessCanWrite)

	// The verdict about the login goes to stderr.
	_, errOut, _ := runCmd(t, opts, "--query", "SELECT 1")
	if !strings.Contains(errOut, "CAN MODIFY DATA") || !strings.Contains(errOut, "db_owner") {
		t.Errorf("stderr = %q, want the write-access verdict", errOut)
	}

	var out bytes.Buffer
	var stderr bytes.Buffer
	if err := cmdRun(context.Background(), []string{"--query", "UPDATE t SET x = 1", "--allow-writes"}, opts, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "WARNING: --allow-writes") {
		t.Errorf("stderr = %q, want the --allow-writes warning", stderr.String())
	}
}

func TestRunPolicyFollowsSettings(t *testing.T) {
	// The policy is applied by the core; check it reaches the runner through the CLI path.
	policyRunner := &policyRecorder{}
	opts := runSetup(t, policyRunner, connection.AccessReadOnly)
	_, _, _ = runCmd(t, opts, "--query", "SELECT 1")
	if !policyRunner.got.ReadOnly {
		t.Error("read-only mode should be on by default")
	}
	_, _, _ = runCmd(t, opts, "--query", "SELECT 1", "--allow-writes")
	if policyRunner.got.ReadOnly {
		t.Error("--allow-writes should turn read-only mode off")
	}
}

type policyRecorder struct{ got executor.Policy }

func (p *policyRecorder) Execute(_ context.Context, _ *sql.DB, req executor.Request, _ executor.Sink) executor.Summary {
	p.got = req.Policy
	return executor.Summary{Outcome: executor.OutcomeSucceeded}
}

func TestFit(t *testing.T) {
	if got := fit("a\tb\r\nc", 10); got != "a b c" {
		t.Errorf("fit = %q", got)
	}
	if got := fit("東京東京東京", 4); got != "東京東…" {
		t.Errorf("fit = %q", got)
	}
	if !errors.Is(&exitError{err: errUsage}, errUsage) {
		t.Error("exitError should unwrap")
	}
}
