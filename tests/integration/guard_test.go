//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/TuanFaied/qryvanta/internal/dialect/mssql"
	"github.com/TuanFaied/qryvanta/internal/executor"
)

// openAdminDB connects as sa to the fixture database. The guard can only be proven with a
// login that is allowed to write.
func openAdminDB(t *testing.T) *sql.DB {
	t.Helper()
	pw := os.Getenv("QRYVANTA_TEST_MSSQL_ADMIN_PASSWORD")
	if pw == "" {
		t.Skip("QRYVANTA_TEST_MSSQL_ADMIN_PASSWORD not set")
	}
	t.Setenv("QRYVANTA_TEST_MSSQL_USER", "sa")
	t.Setenv("QRYVANTA_TEST_MSSQL_PASSWORD", pw)
	db, _ := openFixtureDB(t, "master")
	return db
}

var readOnly = executor.Policy{ReadOnly: true, LockTimeout: 5 * time.Second}

func guarded(t *testing.T, db *sql.DB, sqlText string, p executor.Policy) (executor.Summary, *executor.Collector) {
	t.Helper()
	var c executor.Collector
	sum := mssql.Executor{}.Execute(context.Background(), db, executor.Request{
		SQL: sqlText, Database: loadManifest(t).Database, Limits: executor.Limits{Timeout: 30 * time.Second}, Policy: p,
	}, &c)
	return sum, &c
}

func scalar(t *testing.T, db *sql.DB, q string) any {
	t.Helper()
	var v any
	if err := db.QueryRow(q).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestGuardRollsBackWrites(t *testing.T) {
	db := openAdminDB(t)
	probe := fmt.Sprintf("guard_probe_%d", time.Now().UnixNano())

	sum, c := guarded(t, db, `
UPDATE dbo.Orders SET Status = 'Changed' WHERE OrderID = 101;
SELECT Status FROM dbo.Orders WHERE OrderID = 101;
CREATE TABLE dbo.`+probe+` (id int);
INSERT INTO dbo.Customers (CustomerID, Name, Email, Country) VALUES (999, N'Guard', NULL, 'LK');`, readOnly)

	if sum.Outcome != executor.OutcomeSucceeded || sum.ErrorCount != 0 {
		t.Fatalf("summary = %+v", sum)
	}
	if c.Data[0][0][0] != "Changed" {
		t.Errorf("inside the batch the update must be visible, got %v", c.Data[0][0][0])
	}
	if !sum.Guard.Active || !sum.Guard.RolledBack || sum.Guard.EndedByBatch {
		t.Errorf("guard = %+v", sum.Guard)
	}

	if got := scalar(t, db, "SELECT Status FROM QryvantaSample.dbo.Orders WHERE OrderID = 101"); got != "Delivered" {
		t.Errorf("update survived the guard: Status = %v", got)
	}
	if got := scalar(t, db, "SELECT OBJECT_ID(N'QryvantaSample.dbo."+probe+"')"); got != nil {
		t.Errorf("CREATE TABLE survived the guard")
	}
	if got := scalar(t, db, "SELECT COUNT(*) FROM QryvantaSample.dbo.Customers WHERE CustomerID = 999"); got != int64(0) {
		t.Errorf("INSERT survived the guard")
	}
}

func TestGuardRollsBackAfterErrorsAndCancellation(t *testing.T) {
	db := openAdminDB(t)

	// A statement-level error leaves the transaction open; the guard still rolls it back.
	sum, _ := guarded(t, db, "UPDATE dbo.Orders SET Status = 'Changed' WHERE OrderID = 102; SELECT 1/0;", readOnly)
	if sum.ErrorCount != 1 || !sum.Guard.RolledBack {
		t.Fatalf("error case: %+v", sum)
	}

	// Cancellation (attention) also leaves the transaction open for the guard to roll back.
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(500 * time.Millisecond); cancel() }()
	var c executor.Collector
	sum = mssql.Executor{}.Execute(ctx, db, executor.Request{
		SQL:      "UPDATE dbo.Orders SET Status = 'Changed' WHERE OrderID = 103; WAITFOR DELAY '00:00:30';",
		Database: loadManifest(t).Database, Limits: executor.Limits{Timeout: 30 * time.Second}, Policy: readOnly,
	}, &c)
	if sum.Outcome != executor.OutcomeCancelled || !sum.Guard.RolledBack {
		t.Fatalf("cancel case: %+v", sum)
	}

	got := scalar(t, db, "SELECT STRING_AGG(Status, ',') WITHIN GROUP (ORDER BY OrderID) FROM QryvantaSample.dbo.Orders WHERE OrderID IN (102, 103)")
	if got != "Shipped,Delivered" {
		t.Errorf("statuses after rollback = %v, want unchanged", got)
	}
}

func TestGuardReportsCommitInsideBatch(t *testing.T) {
	db := openAdminDB(t)
	probe := fmt.Sprintf("guard_commit_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = db.Exec("IF OBJECT_ID(N'QryvantaSample.dbo." + probe + "') IS NOT NULL DROP TABLE QryvantaSample.dbo." + probe)
	})

	sum, _ := guarded(t, db, "CREATE TABLE dbo."+probe+" (id int); COMMIT;", readOnly)
	if !sum.Guard.Active || !sum.Guard.EndedByBatch || sum.Guard.RolledBack {
		t.Fatalf("guard = %+v, want EndedByBatch", sum.Guard)
	}
	// This is exactly the gap the report exists for: the commit made the change permanent.
	if got := scalar(t, db, "SELECT OBJECT_ID(N'QryvantaSample.dbo."+probe+"')"); got == nil {
		t.Error("expected the committed table to exist (the guard cannot prevent a COMMIT inside the batch)")
	}
}

func TestGuardLockTimeout(t *testing.T) {
	admin := openAdminDB(t)
	// Session A holds an exclusive lock on order 101.
	conn, err := admin.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), "BEGIN TRANSACTION; UPDATE QryvantaSample.dbo.Orders SET Status = Status WHERE OrderID = 101;"); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(context.Background(), "IF @@TRANCOUNT > 0 ROLLBACK TRANSACTION") //nolint:errcheck

	reader, _ := openFixtureDB(t, "master")
	start := time.Now()
	sum, _ := guarded(t, reader, "SELECT Status FROM dbo.Orders WHERE OrderID = 101;", executor.Policy{ReadOnly: true, LockTimeout: time.Second})
	if time.Since(start) > 10*time.Second {
		t.Fatalf("blocked query was not stopped by the lock timeout (%s)", time.Since(start))
	}
	if sum.ErrorCount != 1 || sum.Messages[0].Number != 1222 {
		t.Fatalf("want SQL Server error 1222 (lock request time out), got %+v", sum.Messages)
	}
}

func TestGuardLeavesPooledSessionsClean(t *testing.T) {
	db, _ := openFixtureDB(t, "master")
	for i := 0; i < 3; i++ {
		guarded(t, db, "SELECT 1; BEGIN TRANSACTION;", readOnly) // even an unbalanced BEGIN is rolled back
	}
	for i := 0; i < 3; i++ {
		var trancount, lockTimeout int
		if err := db.QueryRow("SELECT @@TRANCOUNT, @@LOCK_TIMEOUT").Scan(&trancount, &lockTimeout); err != nil {
			t.Fatal(err)
		}
		if trancount != 0 || lockTimeout != -1 {
			t.Fatalf("pooled session left dirty: trancount=%d lock_timeout=%d", trancount, lockTimeout)
		}
	}
	assertDatabase(t, db, "master")
}

func TestWritesAllowedModeHasNoGuard(t *testing.T) {
	db := openAdminDB(t)
	t.Cleanup(func() {
		_, _ = db.Exec("UPDATE QryvantaSample.dbo.Orders SET Status = 'Delivered' WHERE OrderID = 101")
	})
	sum, _ := guarded(t, db, "UPDATE dbo.Orders SET Status = 'Changed' WHERE OrderID = 101;", executor.Policy{ReadOnly: false})
	if sum.Guard.Active || sum.Guard.RolledBack {
		t.Fatalf("guard = %+v, want inactive", sum.Guard)
	}
	if got := scalar(t, db, "SELECT Status FROM QryvantaSample.dbo.Orders WHERE OrderID = 101"); got != "Changed" {
		t.Errorf("with read-only mode off the update must persist, got %v", got)
	}
}
