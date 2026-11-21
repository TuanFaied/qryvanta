//go:build parser

package security

// The read-only allowlist against the real parser, so the decisions are tested on what
// ScriptDom actually produces. Run (after building the sidecar):
//
//	QRYVANTA_PARSER_PATH=bin/qryvanta-tsqlparser.exe go test -tags parser ./internal/security/

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/TuanFaied/qryvanta/internal/executor"
	"github.com/TuanFaied/qryvanta/internal/parser"
	"github.com/TuanFaied/qryvanta/internal/parser/sidecar"
)

func check(t *testing.T, c *sidecar.Client, sql string) []executor.Refusal {
	t.Helper()
	res, err := c.Parse(context.Background(), sql, parser.Options{Model: true})
	if err != nil {
		t.Fatalf("parse %q: %v", sql, err)
	}
	return ReadOnlyRefusals(res)
}

func realParser(t *testing.T) *sidecar.Client {
	t.Helper()
	path := os.Getenv("QRYVANTA_PARSER_PATH")
	if path == "" {
		t.Skip("QRYVANTA_PARSER_PATH not set; build the sidecar with `wails3 task common:build:sidecar`")
	}
	c := sidecar.New(sidecar.Options{Path: path, Timeout: 20 * time.Second})
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestReadOnlyAllows(t *testing.T) {
	c := realParser(t)
	for _, sql := range []string{
		"SELECT 1",
		"SELECT o.OrderID, c.Name FROM dbo.Orders o JOIN dbo.Customers c ON c.CustomerID = o.CustomerID WHERE o.Total > 10",
		"WITH t AS (SELECT 1 AS a) SELECT a FROM t",
		"SELECT 1 UNION ALL SELECT 2 EXCEPT SELECT 3",
		"SELECT name FROM sys.objects FOR JSON PATH",
		"SELECT name FROM sys.objects FOR XML AUTO",
		// Several statements in one batch are allowed when each one is.
		"SELECT 1; SELECT 2;",
		// Variables, control flow and messages.
		"DECLARE @n int = (SELECT COUNT(*) FROM sys.objects); SET @n += 1; IF @n > 0 PRINT @n ELSE RAISERROR('none', 10, 1);",
		"DECLARE @i int = 0; WHILE @i < 3 BEGIN SET @i += 1; IF @i = 2 CONTINUE; IF @i = 3 BREAK; END",
		"BEGIN TRY SELECT 1/0 END TRY BEGIN CATCH THROW; END CATCH",
		"WAITFOR DELAY '00:00:01'; RETURN;",
		// Harmless SET options.
		"SET NOCOUNT ON; SET ANSI_NULLS, QUOTED_IDENTIFIER ON; SET STATISTICS IO, TIME ON; SET DATEFORMAT ymd;",
		"SET TRANSACTION ISOLATION LEVEL READ UNCOMMITTED; SET ROWCOUNT 10; SET TEXTSIZE 1000; SET LANGUAGE us_english;",
		// Local temporary tables and table variables.
		"CREATE TABLE #t (a int); INSERT #t VALUES (1); UPDATE #t SET a = 2; DELETE FROM #t; TRUNCATE TABLE #t; DROP TABLE #t;",
		"SELECT * INTO #copy FROM sys.objects; CREATE INDEX ix ON #copy (name); DROP TABLE IF EXISTS #copy;",
		"DECLARE @t TABLE (a int); INSERT @t SELECT 1; UPDATE @t SET a = 2; DELETE FROM @t;",
		"UPDATE x SET a = 1 FROM #t AS x JOIN sys.objects o ON o.object_id = x.a",
		"MERGE #t AS t USING (SELECT 1 AS a) s ON t.a = s.a WHEN NOT MATCHED THEN INSERT (a) VALUES (s.a);",
		// Keywords inside comments and strings are not statements.
		"-- DELETE FROM dbo.Orders\n/* EXEC dbo.p; DROP TABLE dbo.t */\nSELECT 'UPDATE dbo.Orders SET x = 1; COMMIT' AS s",
		"-- only a comment",
	} {
		if r := check(t, c, sql); len(r) != 0 {
			t.Errorf("%q refused: %+v", sql, r)
		}
	}
}

func TestReadOnlyRefuses(t *testing.T) {
	c := realParser(t)
	for _, tc := range []struct {
		sql, statement, reason string
	}{
		{"SELECT * INTO dbo.Copy FROM dbo.Orders", "SELECT INTO", "Writes to dbo.Copy"},
		{"INSERT dbo.Orders (a) VALUES (1)", "INSERT", "Writes to dbo.Orders"},
		{"UPDATE dbo.Orders SET a = 1", "UPDATE", "Writes to dbo.Orders"},
		{"DELETE FROM dbo.Orders WHERE a = 1", "DELETE", "Writes to dbo.Orders"},
		{"MERGE dbo.T AS t USING #s AS s ON t.a = s.a WHEN MATCHED THEN DELETE;", "MERGE", "Writes to dbo.T"},
		{"TRUNCATE TABLE dbo.Orders", "TRUNCATE TABLE", "Writes to dbo.Orders"},
		{"UPDATE o SET a = 1 FROM dbo.Orders AS o", "UPDATE", "Writes to dbo.Orders"},
		// A CTE feeding DELETE deletes from the table behind it; the name is not a #table.
		{"WITH c AS (SELECT * FROM dbo.Orders) DELETE FROM c", "DELETE", "Writes to c"},
		// A write to a #table that sends its OUTPUT into a real table.
		{"DELETE FROM #t OUTPUT deleted.a INTO dbo.Audit", "DELETE", "Writes to dbo.Audit"},
		{"INSERT INTO ##shared VALUES (1)", "INSERT", "Writes to ##shared"},
		{"INSERT #t EXEC dbo.GetOrders", "EXEC", "procedure dbo.GetOrders"},
		{"EXEC dbo.GetOrders", "EXEC", "procedure dbo.GetOrders"},
		{"EXEC sp_executesql N'SELECT 1'", "EXEC", "procedure sp_executesql"},
		{"EXEC ('DELETE FROM dbo.Orders')", "EXEC", "dynamic SQL"},
		{"IF 1 = 1 EXEC dbo.p", "EXEC", "procedure dbo.p"},
		{"IF 1 = 1 BEGIN DROP TABLE dbo.X END", "DROP TABLE", "Writes to dbo.X"},
		{"CREATE TABLE dbo.New (a int)", "CREATE TABLE", "Writes to dbo.New"},
		{"CREATE VIEW dbo.v AS SELECT 1 AS a", "CREATE VIEW", "Changes the database schema"},
		{"ALTER TABLE #t ADD b int", "ALTER TABLE ADD TABLE ELEMENT", "Changes the database schema"},
		{"SELECT 1; COMMIT;", "COMMIT", "transaction"},
		{"BEGIN TRAN; SELECT 1; ROLLBACK;", "BEGIN TRANSACTION", "transaction"},
		{"USE master; SELECT 1", "USE", "Switches the database"},
		{"DECLARE c CURSOR FOR SELECT 1", "DECLARE", "Cursors"},
		{"DBCC CHECKDB", "DBCC", "not allowed"},
		{"BULK INSERT dbo.T FROM 'c:\\x.csv'", "BULK INSERT", "not allowed"},
		{"GRANT SELECT ON dbo.T TO public", "GRANT", "not allowed"},
		{"SELECT * FROM OPENQUERY(srv, 'SELECT 1')", "SELECT", "OPENQUERY"},
		{"SELECT * FROM OPENROWSET('SQLNCLI', 'Server=x;', 'SELECT 1')", "SELECT", "OPENROWSET"},
		{"SELECT * FROM OPENDATASOURCE('SQLNCLI', 'Data Source=x').db.dbo.t", "SELECT", "OPENDATASOURCE"},
		{"SELECT * FROM t WHERE a IN (SELECT a FROM OPENQUERY(srv, 'SELECT 1 AS a'))", "SELECT", "OPENQUERY"},
		{"WAITFOR (RECEIVE TOP (1) * FROM dbo.q), TIMEOUT 10", "WAITFOR", "Service Broker"},
		{"SET IMPLICIT_TRANSACTIONS ON", "SET", "IMPLICIT_TRANSACTIONS"},
		{"SET XACT_ABORT ON", "SET", "XACT_ABORT"},
		{"SET ANSI_DEFAULTS ON", "SET", "ANSI_DEFAULTS"},
		{"SET LOCK_TIMEOUT 0", "SET", "LOCK_TIMEOUT"},
		{"SET SHOWPLAN_XML ON", "SET", "SHOWPLAN_XML"},
		{"SET IDENTITY_INSERT dbo.T ON", "SET", "IDENTITY_INSERT"},
		{"SET CONTEXT_INFO 0x01", "SET", "CONTEXT_INFO"},
		{"SELECT FROM", "", "Syntax error"},
	} {
		got := check(t, c, tc.sql)
		if len(got) == 0 {
			t.Errorf("%q allowed, want refused (%s)", tc.sql, tc.reason)
			continue
		}
		if got[0].Statement != tc.statement || !strings.Contains(got[0].Reason, tc.reason) {
			t.Errorf("%q: got %q / %q, want %q / ...%s...", tc.sql, got[0].Statement, got[0].Reason, tc.statement, tc.reason)
		}
	}
}

// A refusal points at the statement (or the part of it) that is not allowed, with script
// positions, in every batch.
func TestReadOnlyRefusalPositions(t *testing.T) {
	c := realParser(t)
	sql := "SELECT 1;\nGO\nSELECT 2;\nUPDATE dbo.Orders SET a = 1;\nGO\nEXEC dbo.p;"
	got := check(t, c, sql)
	if len(got) != 2 {
		t.Fatalf("refusals = %+v", got)
	}
	if got[0].Batch != 1 || got[0].Line != 4 || parser.SpanText(sql, parser.Span{Offset: got[0].Offset, Length: got[0].Length}) != "dbo.Orders" {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Batch != 2 || got[1].Line != 6 || got[1].Statement != "EXEC" {
		t.Errorf("second = %+v", got[1])
	}
}
