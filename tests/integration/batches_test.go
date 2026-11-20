//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/TuanFaied/qryvanta/internal/dialect/mssql"
	"github.com/TuanFaied/qryvanta/internal/executor"
)

func script(t *testing.T, db *sql.DB, p executor.Policy, timeout time.Duration, batches ...executor.Batch) (executor.Summary, *executor.Collector) {
	t.Helper()
	var c executor.Collector
	sum := mssql.Executor{}.Execute(context.Background(), db, executor.Request{
		Batches: batches, Database: loadManifest(t).Database, Limits: executor.Limits{Timeout: timeout}, Policy: p,
	}, &c)
	return sum, &c
}

func TestBatchesShareTheSessionUnderTheGuard(t *testing.T) {
	db, _ := openFixtureDB(t, "master")
	sum, c := script(t, db, readOnly, 30*time.Second,
		executor.Batch{SQL: "CREATE TABLE #t (a int);", Line: 1},
		executor.Batch{SQL: "INSERT INTO #t VALUES (1);", Line: 3, Repeat: 3},
		executor.Batch{SQL: "SELECT COUNT(*) AS n FROM #t;", Line: 5},
	)
	if sum.Outcome != executor.OutcomeSucceeded || sum.ErrorCount != 0 {
		t.Fatalf("summary = %+v", sum)
	}
	// The temp table survives from batch to batch: one guard transaction spans the script.
	if c.Data[0][0][0] != int64(3) {
		t.Fatalf("count = %v, want 3 (GO 3 runs the insert three times)", c.Data[0][0][0])
	}
	if len(sum.Batches) != 5 || sum.Batches[3].Execution != 3 || sum.Batches[4].Line != 5 {
		t.Errorf("batch runs = %+v", sum.Batches)
	}
	if sum.ResultSets[0].Batch != 2 {
		t.Errorf("result set batch = %d, want 2", sum.ResultSets[0].Batch)
	}
	if !sum.Guard.Active || !sum.Guard.RolledBack || sum.Guard.EndedByBatch {
		t.Errorf("guard = %+v", sum.Guard)
	}
}

func TestErrorsDoNotStopLaterBatchesAndMapToScriptLines(t *testing.T) {
	db, _ := openFixtureDB(t, "master")
	sum, c := script(t, db, readOnly, 30*time.Second,
		executor.Batch{SQL: "SELECT 1 AS ok;", Line: 1},
		// Compile error on line 2 of a batch that starts on script line 4.
		executor.Batch{SQL: "SELECT 2;\nSELECT nmae FROM dbo.Customers;", Line: 4},
		executor.Batch{SQL: "SELECT 3 AS still_runs;", Line: 7},
	)
	if sum.Outcome != executor.OutcomeSucceeded || sum.ErrorCount != 1 {
		t.Fatalf("summary = %+v", sum)
	}
	m := sum.Messages[0]
	if m.Number != 207 || m.Batch != 1 || m.Line != 2 || m.ScriptLine != 5 {
		t.Fatalf("message = %+v, want error 207 in batch 1 at batch line 2 = script line 5", m)
	}
	if len(c.Columns) != 2 || c.Data[1][0][0] != int64(3) {
		t.Fatalf("result sets = %v %v (the third batch must still run)", c.Columns, c.Data)
	}
	if sum.Batches[1].ErrorCount != 1 || sum.Batches[2].ErrorCount != 0 {
		t.Errorf("per-batch errors = %+v", sum.Batches)
	}
}

func TestCommitInABatchRestartsTheGuardForLaterBatches(t *testing.T) {
	db := openAdminDB(t)
	kept := fmt.Sprintf("batch_kept_%d", time.Now().UnixNano())
	undone := kept + "_undone"
	t.Cleanup(func() {
		for _, n := range []string{kept, undone} {
			_, _ = db.Exec("IF OBJECT_ID(N'QryvantaSample.dbo." + n + "') IS NOT NULL DROP TABLE QryvantaSample.dbo." + n)
		}
	})
	sum, _ := script(t, db, readOnly, 30*time.Second,
		executor.Batch{SQL: "CREATE TABLE dbo." + kept + " (id int); COMMIT;", Line: 1},
		executor.Batch{SQL: "CREATE TABLE dbo." + undone + " (id int);", Line: 3},
	)
	if !sum.Guard.EndedByBatch || !sum.Guard.RolledBack {
		t.Fatalf("guard = %+v, want EndedByBatch and a final rollback", sum.Guard)
	}
	if scalar(t, db, "SELECT OBJECT_ID(N'QryvantaSample.dbo."+kept+"')") == nil {
		t.Error("the committed table should exist: the guard cannot undo a COMMIT inside a batch")
	}
	if scalar(t, db, "SELECT OBJECT_ID(N'QryvantaSample.dbo."+undone+"')") != nil {
		t.Error("the batch after the COMMIT must be protected again, but its table was kept")
	}
}

func TestTimeoutStopsTheScript(t *testing.T) {
	db, _ := openFixtureDB(t, "master")
	sum, c := script(t, db, readOnly, time.Second,
		executor.Batch{SQL: "WAITFOR DELAY '00:00:10';", Line: 1},
		executor.Batch{SQL: "SELECT 'must not run';", Line: 3},
	)
	if sum.Outcome != executor.OutcomeTimedOut || len(sum.Batches) != 1 || len(c.Columns) != 0 {
		t.Fatalf("summary = %+v, result sets %d", sum, len(c.Columns))
	}
}

func TestUseInABatchCarriesOverAndIsRestored(t *testing.T) {
	db, _ := openFixtureDB(t, "master")
	sum, c := script(t, db, executor.Policy{}, 30*time.Second,
		executor.Batch{SQL: "USE master;", Line: 1},
		executor.Batch{SQL: "SELECT DB_NAME() AS db;", Line: 3},
	)
	if sum.Outcome != executor.OutcomeSucceeded || c.Data[0][0][0] != "master" {
		t.Fatalf("summary = %+v, data %v", sum, c.Data)
	}
	// The pooled session goes back to the database it had.
	if got := scalar(t, db, "SELECT DB_NAME()"); got != "master" {
		t.Errorf("pooled session database = %v", got)
	}
}
