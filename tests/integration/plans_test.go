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

// planRun runs batches with a plan mode on a one-connection pool, so the next query reuses the
// same session and shows whether the plan setting was turned off.
func planRun(t *testing.T, db *sql.DB, mode executor.PlanMode, limits executor.Limits, batches ...executor.Batch) (executor.Summary, *executor.Collector) {
	t.Helper()
	if limits.Timeout == 0 {
		limits.Timeout = 30 * time.Second
	}
	var c executor.Collector
	sum := mssql.Executor{}.Execute(context.Background(), db, executor.Request{
		Batches: batches, Limits: limits, Policy: readOnly, Plan: mode,
	}, &c)
	return sum, &c
}

// assertNoPlanMode runs an ordinary query on the session and checks it returns its own result,
// not a plan: SHOWPLAN_XML and STATISTICS XML were turned off.
func assertNoPlanMode(t *testing.T, db *sql.DB) {
	t.Helper()
	sum, c := planRun(t, db, executor.PlanNone, executor.Limits{}, executor.Batch{SQL: "SELECT 42 AS answer;", Line: 1})
	if sum.Outcome != executor.OutcomeSucceeded || len(c.Columns) != 1 || c.Columns[0][0].Name != "answer" || len(c.Plans) != 0 || sum.Plan != nil {
		t.Fatalf("the session still returns plans: outcome %s, columns %v, plans %d", sum.Outcome, c.Columns, len(c.Plans))
	}
}

// onePool connects straight to the fixture database with a single connection. Requests then
// leave Database empty, so the runner needs no second connection to look it up.
func onePool(t *testing.T) *sql.DB {
	t.Helper()
	db, _ := openFixtureDB(t, loadManifest(t).Database)
	db.SetMaxOpenConns(1)
	return db
}

func TestEstimatedPlans(t *testing.T) {
	db := onePool(t)
	sum, c := planRun(t, db, executor.PlanEstimated, executor.Limits{},
		executor.Batch{SQL: "SELECT CustomerID FROM dbo.Customers;\nSELECT COUNT(*) FROM dbo.Orders WHERE Status = 'Shipped';", Line: 1},
		executor.Batch{SQL: "SELECT o.OrderID FROM dbo.Orders AS o JOIN dbo.OrderItems AS oi ON oi.OrderID = o.OrderID;", Line: 4},
	)
	if sum.Outcome != executor.OutcomeSucceeded || sum.Plan == nil || !sum.Plan.Complete {
		t.Fatalf("summary = %+v, plan %+v", sum, sum.Plan)
	}
	// Nothing ran: no result sets reach the sink, only plans.
	if len(c.Columns) != 0 || len(sum.ResultSets) != 0 {
		t.Fatalf("estimated plans produced result sets: %v", c.Columns)
	}
	if sum.Guard.Active {
		t.Error("an estimated plan needs no guard")
	}
	if len(c.Plans) < 2 || sum.Plan.Count != len(c.Plans) {
		t.Fatalf("plans = %d, summary %+v", len(c.Plans), sum.Plan)
	}
	batches := map[int]bool{}
	for _, p := range c.Plans {
		batches[p.Batch] = true
		if p.Mode != executor.PlanEstimated || p.Statement < 1 || !strings.Contains(p.XML, "<ShowPlanXML") {
			t.Fatalf("plan = %+v (%.80s)", p, p.XML)
		}
	}
	if !batches[0] || !batches[1] {
		t.Fatalf("plans do not cover both batches: %v", batches)
	}
	assertNoPlanMode(t, db)
}

func TestEstimatedPlanWithAnErrorPartWay(t *testing.T) {
	db := onePool(t)
	sum, c := planRun(t, db, executor.PlanEstimated, executor.Limits{},
		executor.Batch{SQL: "SELECT CustomerID FROM dbo.Customers;", Line: 1},
		executor.Batch{SQL: "SELECT nope FROM dbo.NoSuchTable;", Line: 3},
		executor.Batch{SQL: "SELECT OrderID FROM dbo.Orders;", Line: 5},
	)
	if sum.ErrorCount == 0 || len(c.Plans) < 2 {
		t.Fatalf("errors %d, plans %d (%+v)", sum.ErrorCount, len(c.Plans), sum.Messages)
	}
	for _, p := range c.Plans {
		if p.Batch == 1 {
			t.Fatalf("the failing batch has a plan: %+v", p)
		}
	}
	assertNoPlanMode(t, db)
}

func TestActualPlans(t *testing.T) {
	db := onePool(t)
	sum, c := planRun(t, db, executor.PlanActual, executor.Limits{},
		executor.Batch{SQL: "SELECT TOP (3) CustomerID, Name FROM dbo.Customers ORDER BY CustomerID;\nSELECT COUNT(*) AS n FROM dbo.Orders;", Line: 1},
		executor.Batch{SQL: "SELECT OrderID FROM dbo.Orders WHERE OrderID = 101;", Line: 4},
	)
	if sum.Outcome != executor.OutcomeSucceeded || sum.Plan == nil || !sum.Plan.Complete || !sum.Guard.Active {
		t.Fatalf("summary = %+v, plan %+v", sum, sum.Plan)
	}
	// The user's three result sets reach the sink; the plans never do.
	if len(c.Columns) != 3 || len(sum.ResultSets) != 3 {
		t.Fatalf("result sets = %d %v", len(c.Columns), c.Columns)
	}
	for _, cols := range c.Columns {
		if len(cols) == 1 && strings.Contains(cols[0].Name, "Showplan") {
			t.Fatal("a plan result set reached the result sink")
		}
	}
	if len(c.Data[0]) != 3 || len(c.Plans) != 3 {
		t.Fatalf("rows %d, plans %d", len(c.Data[0]), len(c.Plans))
	}
	if c.Plans[2].Batch != 1 || c.Plans[0].Statement != 1 || c.Plans[1].Statement != 2 ||
		!strings.Contains(c.Plans[0].XML, "RunTimeInformation") {
		t.Fatalf("plans = %+v", c.Plans)
	}
	assertNoPlanMode(t, db)
}

func TestActualPlansWithAnErrorAndTruncation(t *testing.T) {
	db := onePool(t)
	sum, c := planRun(t, db, executor.PlanActual, executor.Limits{},
		executor.Batch{SQL: "SELECT CustomerID FROM dbo.Customers;", Line: 1},
		executor.Batch{SQL: "SELECT 1/0 AS boom;", Line: 3},
		executor.Batch{SQL: "SELECT OrderID FROM dbo.Orders;", Line: 5},
	)
	if sum.ErrorCount == 0 || len(c.Plans) < 2 {
		t.Fatalf("errors %d, plans %d", sum.ErrorCount, len(c.Plans))
	}
	assertNoPlanMode(t, db)

	// A row limit stops the run: the plans so far are kept and marked incomplete.
	sum, c = planRun(t, db, executor.PlanActual, executor.Limits{MaxRows: 2},
		executor.Batch{SQL: "SELECT CustomerID FROM dbo.Customers;\nSELECT OrderID FROM dbo.Orders;", Line: 1})
	if sum.Outcome != executor.OutcomeTruncated || sum.Plan == nil || sum.Plan.Complete || !strings.Contains(sum.Plan.Reason, "truncated") {
		t.Fatalf("outcome %s, plan %+v", sum.Outcome, sum.Plan)
	}
	assertNoPlanMode(t, db)
	_ = c
}

func TestPlanTooLarge(t *testing.T) {
	db := onePool(t)
	sum, c := planRun(t, db, executor.PlanEstimated, executor.Limits{MaxPlanBytes: 100},
		executor.Batch{SQL: "SELECT CustomerID FROM dbo.Customers;", Line: 1})
	if len(c.Plans) != 0 || sum.Plan.TooLarge != 1 || sum.Plan.Complete || !strings.Contains(sum.Plan.Reason, "larger than") {
		t.Fatalf("plans %d, summary %+v", len(c.Plans), sum.Plan)
	}
	assertNoPlanMode(t, db)
}

func TestCancelledActualPlanRestoresTheSession(t *testing.T) {
	db := onePool(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	var c executor.Collector
	sum := mssql.Executor{}.Execute(ctx, db, executor.Request{
		SQL:    "SELECT CustomerID FROM dbo.Customers; WAITFOR DELAY '00:00:10'; SELECT 1;",
		Limits: executor.Limits{Timeout: 30 * time.Second}, Policy: readOnly, Plan: executor.PlanActual,
	}, &c)
	if sum.Outcome != executor.OutcomeCancelled || sum.Plan == nil || sum.Plan.Complete {
		t.Fatalf("outcome %s, plan %+v", sum.Outcome, sum.Plan)
	}
	assertNoPlanMode(t, db)
}
