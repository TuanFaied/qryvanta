package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/connection"
	"github.com/TuanFaied/qryvanta/internal/executor"
)

// planFakeRunner returns a result set with a secret value (as an actual-plan run would) and the
// plans, the way the SQL Server runner delivers them.
type planFakeRunner struct{ got executor.Request }

func (r *planFakeRunner) Execute(_ context.Context, _ *sql.DB, req executor.Request, sink executor.Sink) executor.Summary {
	r.got = req
	if req.Plan == executor.PlanActual {
		_ = sink.ResultSet(0, []executor.Column{{Name: "Customer", Type: "NVARCHAR"}})
		_ = sink.Rows(0, [][]executor.Value{{"secret customer name"}})
	}
	ps := sink.(executor.PlanSink)
	_ = ps.Plan(executor.Plan{Mode: req.Plan, Batch: 0, Execution: 1, Statement: 1, XML: `<ShowPlanXML a="1"/>`})
	_ = ps.Plan(executor.Plan{Mode: req.Plan, Batch: 1, Execution: 1, Statement: 1, XML: `<ShowPlanXML a="2"/>`})
	return executor.Summary{Outcome: executor.OutcomeSucceeded, Plan: &executor.PlanSummary{Mode: req.Plan, Count: 2, Complete: true}}
}

func planCmd(t *testing.T, opts app.Options, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := cmdPlan(context.Background(), args, opts, &out, &errOut)
	code = report(err, &bytes.Buffer{}, &errOut)
	return out.String(), errOut.String(), code
}

func TestPlanFlags(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--query", "SELECT 1"}, "choose --estimated or --actual"},
		{[]string{"--estimated", "--actual", "--query", "SELECT 1"}, "choose --estimated or --actual"},
		{[]string{"--estimated", "--format", "csv", "--query", "SELECT 1"}, "--format must be xml or json"},
		{[]string{"--estimated", "--file", "a.sql", "--query", "SELECT 1"}, "either --file or --query"},
		{[]string{"--estimated", "--query", "SELECT 1", "--out", filepath.Join(t.TempDir(), "missing")}, "--out must be an existing directory"},
	} {
		_, stderr, code := planCmd(t, app.Options{}, c.args...)
		if code != 2 || !strings.Contains(stderr, c.want) {
			t.Errorf("%v: exit %d, stderr %q; want 2 and %q", c.args, code, stderr, c.want)
		}
	}
}

func TestPlanActualPrintsOnlyPlans(t *testing.T) {
	runner := &planFakeRunner{}
	opts := runSetup(t, runner, connection.AccessReadOnly)
	stdout, stderr, code := planCmd(t, opts, "--actual", "--query", "SELECT Name FROM dbo.Customers")
	if code != 0 || runner.got.Plan != executor.PlanActual {
		t.Fatalf("exit %d, request %+v, stderr %s", code, runner.got, stderr)
	}
	if strings.Contains(stdout+stderr, "secret customer name") {
		t.Fatal("the query's own results reached the output")
	}
	if !strings.Contains(stdout, `<ShowPlanXML a="1"/>`) || !strings.Contains(stdout, `<ShowPlanXML a="2"/>`) {
		t.Fatalf("stdout = %q", stdout)
	}
	if !strings.Contains(stderr, "use --out DIR") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestPlanEstimatedJSONAndFiles(t *testing.T) {
	runner := &planFakeRunner{}
	opts := runSetup(t, runner, connection.AccessReadOnly)
	stdout, _, code := planCmd(t, opts, "--estimated", "--query", "SELECT 1", "--format", "json")
	if code != 0 || runner.got.Plan != executor.PlanEstimated {
		t.Fatalf("exit %d, request %+v", code, runner.got)
	}
	var res planResult
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}
	if res.Mode != executor.PlanEstimated || !res.Complete || len(res.Plans) != 2 || res.Plans[1].Batch != 2 ||
		res.Plans[0].Bytes != len(`<ShowPlanXML a="1"/>`) || res.Plans[0].XML == "" {
		t.Fatalf("result = %+v", res)
	}

	dir := t.TempDir()
	stdout, _, code = planCmd(t, opts, "--estimated", "--query", "SELECT 1", "--out", dir)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	data, err := os.ReadFile(filepath.Join(dir, "plan-2-1-1.sqlplan"))
	if err != nil || string(data) != `<ShowPlanXML a="2"/>` || !strings.Contains(stdout, "plan-1-1-1.sqlplan") {
		t.Fatalf("file = %q, %v; stdout %q", data, err, stdout)
	}
}
