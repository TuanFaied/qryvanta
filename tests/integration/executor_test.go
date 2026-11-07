//go:build integration

package integration

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/TuanFaied/qryvanta/internal/dialect/mssql"
	"github.com/TuanFaied/qryvanta/internal/executor"
)

func runQuery(t *testing.T, db *sql.DB, ctx context.Context, sqlText string, limits executor.Limits) (executor.Summary, *executor.Collector) {
	t.Helper()
	if limits.Timeout == 0 {
		limits.Timeout = 30 * time.Second
	}
	var c executor.Collector
	sum := mssql.Executor{}.Execute(ctx, db, executor.Request{SQL: sqlText, Database: loadManifest(t).Database, Limits: limits}, &c)
	return sum, &c
}

// assertDatabase checks that pooled sessions are back in their original database.
func assertDatabase(t *testing.T, db *sql.DB, want string) {
	t.Helper()
	for i := 0; i < 3; i++ { // the pool holds several connections; check a few
		var name string
		if err := db.QueryRow("SELECT DB_NAME()").Scan(&name); err != nil {
			t.Fatal(err)
		}
		if name != want {
			t.Fatalf("pooled session is in %q, want %q: the executor leaked its database context", name, want)
		}
	}
}

func TestExecutorResultsAndMessages(t *testing.T) {
	db, _ := openFixtureDB(t, "master")
	sum, c := runQuery(t, db, context.Background(), `
SELECT COUNT(*) AS Orders FROM dbo.Orders;
PRINT N'between the result sets';
SELECT TOP (2) OrderID, Status FROM dbo.Orders ORDER BY OrderID;`, executor.Limits{})

	if sum.Outcome != executor.OutcomeSucceeded || len(sum.ResultSets) != 2 || sum.ErrorCount != 0 {
		t.Fatalf("summary = %+v", sum)
	}
	if c.Data[0][0][0] != int64(8) || len(c.Data[1]) != 2 || c.Data[1][1][0] != int64(102) || c.Data[1][1][1] != "Shipped" {
		t.Errorf("rows = %v", c.Data)
	}
	if c.Columns[1][1].Name != "Status" || c.Columns[1][1].Type != "VARCHAR" {
		t.Errorf("columns = %+v", c.Columns[1])
	}
	if len(sum.Messages) != 1 || sum.Messages[0].Text != "between the result sets" || sum.Messages[0].AfterResultSet != 1 || sum.Messages[0].Kind != "info" {
		t.Errorf("messages = %+v", sum.Messages)
	}
	if sum.ElapsedMs < 0 || sum.TotalRows != 3 {
		t.Errorf("totals = %+v", sum)
	}
	assertDatabase(t, db, "master")
}

func TestExecutorErrorsKeepNumberAndLine(t *testing.T) {
	db, _ := openFixtureDB(t, "master")
	sum, c := runQuery(t, db, context.Background(), "SELECT 1 AS one;\nSELECT 1/0 AS boom;\nSELECT 2 AS two;", executor.Limits{})

	if sum.ErrorCount != 1 {
		t.Fatalf("summary = %+v", sum)
	}
	var e executor.Message
	for _, m := range sum.Messages {
		if m.Kind == "error" {
			e = m
		}
	}
	if e.Number != 8134 || e.Line != 2 || e.Severity != 16 || !strings.Contains(strings.ToLower(e.Text), "divide by zero") {
		t.Errorf("error = %+v", e)
	}
	// SQL Server continues after a statement-level error: the third SELECT still runs.
	if len(c.Data) == 0 || c.Data[len(c.Data)-1][0][0] != int64(2) {
		t.Errorf("later statements did not run: %v", c.Data)
	}

	sum, _ = runQuery(t, db, context.Background(), "SELECT * FROM dbo.NoSuchTable;", executor.Limits{})
	if sum.ErrorCount != 1 || sum.Messages[0].Number != 208 {
		t.Errorf("invalid object: %+v", sum)
	}
}

func TestExecutorLimits(t *testing.T) {
	db, _ := openFixtureDB(t, "master")
	const big = "SELECT a.object_id, a.name FROM sys.all_objects AS a CROSS JOIN sys.all_objects AS b"

	sum, c := runQuery(t, db, context.Background(), big, executor.Limits{MaxRows: 1200})
	if sum.Outcome != executor.OutcomeTruncated || sum.TruncatedBy != "rows" || sum.TotalRows != 1200 || len(c.Data[0]) != 1200 {
		t.Fatalf("max rows: %+v (collected %d)", sum, len(c.Data[0]))
	}

	sum, _ = runQuery(t, db, context.Background(), big, executor.Limits{MaxBytes: 64 << 10})
	if sum.Outcome != executor.OutcomeTruncated || sum.TruncatedBy != "bytes" || sum.TotalBytes > 64<<10 {
		t.Fatalf("max bytes: %+v", sum)
	}
	// Truncation cancels the query on the server; the pool must still work.
	assertDatabase(t, db, "master")
}

func TestExecutorTimeoutAndCancellation(t *testing.T) {
	db, _ := openFixtureDB(t, "master")

	start := time.Now()
	sum, _ := runQuery(t, db, context.Background(), "WAITFOR DELAY '00:00:30'; SELECT 1;", executor.Limits{Timeout: time.Second})
	if sum.Outcome != executor.OutcomeTimedOut || time.Since(start) > 10*time.Second {
		t.Fatalf("timeout: %+v after %s", sum, time.Since(start))
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()
	start = time.Now()
	sum, _ = runQuery(t, db, ctx, "WAITFOR DELAY '00:00:30'; SELECT 1;", executor.Limits{})
	if sum.Outcome != executor.OutcomeCancelled || time.Since(start) > 10*time.Second {
		t.Fatalf("cancel: %+v after %s", sum, time.Since(start))
	}
	assertDatabase(t, db, "master")
}

func TestExecutorRestoresDatabaseContext(t *testing.T) {
	db, _ := openFixtureDB(t, "master")
	sum, c := runQuery(t, db, context.Background(), "SELECT DB_NAME() AS db; USE tempdb; SELECT DB_NAME() AS db;", executor.Limits{})
	if sum.Outcome != executor.OutcomeSucceeded || c.Data[0][0][0] != "QryvantaSample" || c.Data[1][0][0] != "tempdb" {
		t.Fatalf("summary %+v data %v", sum, c.Data)
	}
	assertDatabase(t, db, "master")
}

func TestExecutorValueConversion(t *testing.T) {
	db, _ := openFixtureDB(t, "master")
	sum, c := runQuery(t, db, context.Background(), `SELECT
	CAST(9007199254740993 AS bigint) AS big,
	CAST(42 AS bigint) AS small,
	CAST(1.25 AS decimal(5,2)) AS dec,
	CAST('2026-01-31T16:45:00.1234567' AS datetime2(7)) AS dt,
	CAST('2026-01-31' AS date) AS d,
	CAST(0xDEADBEEF AS varbinary(4)) AS bin,
	CAST('12345678-1234-5678-1234-56789ABCDEF0' AS uniqueidentifier) AS g,
	CAST(NULL AS int) AS n,
	CAST(1 AS bit) AS flag`, executor.Limits{})
	if sum.Outcome != executor.OutcomeSucceeded {
		t.Fatalf("summary = %+v", sum)
	}
	want := []any{"9007199254740993", int64(42), "1.25", "2026-01-31T16:45:00.1234567", "2026-01-31", "0xDEADBEEF",
		"12345678-1234-5678-1234-56789ABCDEF0", nil, true}
	for i, w := range want {
		if got := c.Data[0][0][i]; got != w {
			t.Errorf("%s = %#v, want %#v", c.Columns[0][i].Name, got, w)
		}
	}
}

// TestExecutorMatchesSampleQueries runs the documented sample queries through the executor,
// so the desktop app and CLI see the same row counts as the fixture manifest.
func TestExecutorMatchesSampleQueries(t *testing.T) {
	m := loadManifest(t)
	db, _ := openFixtureDB(t, "master")
	for _, sc := range m.Scenarios {
		for _, e := range []expectation{sc.Buggy, sc.Correct} {
			text := readSample(t, e.File)
			sum, c := runQuery(t, db, context.Background(), text, executor.Limits{})
			if sum.Outcome != executor.OutcomeSucceeded || sum.ErrorCount != 0 || len(c.Data) != 1 || len(c.Data[0]) != e.Rows {
				t.Errorf("%s: outcome %s, errors %d, rows %v, want %d", e.File, sum.Outcome, sum.ErrorCount, rowCounts(c), e.Rows)
			}
		}
	}
}

func rowCounts(c *executor.Collector) []int {
	var out []int
	for _, d := range c.Data {
		out = append(out, len(d))
	}
	return out
}
