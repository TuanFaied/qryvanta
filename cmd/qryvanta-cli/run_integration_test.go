//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/connection"
	"github.com/TuanFaied/qryvanta/internal/executor"
)

// fixtureOptions saves a profile for the fixture database (QRYVANTA_TEST_MSSQL_* variables)
// in a temporary config, with an in-memory credential store so CI needs no OS keyring.
func fixtureOptions(t *testing.T) app.Options {
	t.Helper()
	server := os.Getenv("QRYVANTA_TEST_MSSQL_SERVER")
	if server == "" {
		t.Skip("QRYVANTA_TEST_MSSQL_SERVER not set; start the fixture database with `docker compose run --rm fixtures`")
	}
	p := connection.Profile{
		Name: "fixtures", Server: server, Database: "QryvantaSample", Auth: connection.AuthWindows,
		Encrypt:                connection.EncryptMandatory,
		TrustServerCertificate: os.Getenv("QRYVANTA_TEST_MSSQL_TRUST") == "true",
	}
	if port := os.Getenv("QRYVANTA_TEST_MSSQL_PORT"); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil {
			t.Fatalf("QRYVANTA_TEST_MSSQL_PORT: %v", err)
		}
		p.Port = n
	}
	if user := os.Getenv("QRYVANTA_TEST_MSSQL_USER"); user != "" {
		p.Auth, p.Username = connection.AuthSQL, user
	}
	opts := app.Options{
		ConfigPath:     filepath.Join(t.TempDir(), "config.json"),
		DisableFileLog: true,
		Credentials:    connection.NewMemoryStore(),
	}
	core, err := app.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.Connections().Create(p, os.Getenv("QRYVANTA_TEST_MSSQL_PASSWORD")); err != nil {
		t.Fatal(err)
	}
	_ = core.Close()
	return opts
}

// TestRunSampleQueryJSON runs a documented sample query through "run --format json".
func TestRunSampleQueryJSON(t *testing.T) {
	opts := fixtureOptions(t)
	file := filepath.Join("..", "..", "tests", "sample_queries", "01_employees_without_department.correct.sql")

	var out, errOut bytes.Buffer
	err := cmdRun(context.Background(), []string{"--conn", "fixtures", "--file", file, "--format", "json"}, opts, &out, &errOut)
	if code := report(err, &bytes.Buffer{}, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var res runResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if res.Outcome != executor.OutcomeSucceeded || !res.ReadOnly || !res.Guard.Active || res.Database != "QryvantaSample" {
		t.Errorf("result = %+v", res)
	}
	// manifest.json: 12 rows; row 9 has "(no department)", row 11 is EmployeeID 12.
	if len(res.ResultSets) != 1 || len(res.ResultSets[0].Rows) != 12 {
		t.Fatalf("result sets = %+v", res.ResultSets)
	}
	rows := res.ResultSets[0].Rows
	if rows[9][2] != "(no department)" || rows[11][0] != float64(12) {
		t.Errorf("rows[9] = %v, rows[11] = %v", rows[9], rows[11])
	}
}

// TestRunErrorsAndLimitsAgainstFixture checks exit codes, error lines and limits on a real server.
func TestRunErrorsAndLimitsAgainstFixture(t *testing.T) {
	opts := fixtureOptions(t)
	run := func(args ...string) (string, string, int) {
		var out, errOut bytes.Buffer
		err := cmdRun(context.Background(), append([]string{"--conn", "fixtures"}, args...), opts, &out, &errOut)
		return out.String(), errOut.String(), report(err, &bytes.Buffer{}, &errOut)
	}

	_, errOut, code := run("--query", "SELECT 1 AS ok;\nSELECT nmae FROM dbo.Customers;")
	if code != exitQueryError || !strings.Contains(errOut, "Msg 207, Level 16, State 1, Line 2") {
		t.Errorf("invalid column: exit %d, stderr %q", code, errOut)
	}

	_, errOut, code = run("--query", "WAITFOR DELAY '00:00:05';", "--timeout", "1")
	if code != exitStopped || !strings.Contains(errOut, "timed out") {
		t.Errorf("timeout: exit %d, stderr %q", code, errOut)
	}

	out, _, code := run("--query", "SELECT TOP (3) name FROM sys.all_objects ORDER BY name;", "--format", "csv", "--max-rows", "2")
	if code != 0 || strings.Count(out, "\n") != 3 {
		t.Errorf("csv with --max-rows 2: exit %d, output %q", code, out)
	}
}
