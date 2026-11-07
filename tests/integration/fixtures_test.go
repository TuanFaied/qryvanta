//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/TuanFaied/qryvanta/internal/connection"
	"github.com/TuanFaied/qryvanta/internal/dialect/mssql"
)

// manifest mirrors tests/sample_queries/manifest.json.
type manifest struct {
	Database  string     `json:"database"`
	Scenarios []scenario `json:"scenarios"`
}

type scenario struct {
	ID       string      `json:"id"`
	Title    string      `json:"title"`
	Question string      `json:"question"`
	Cause    string      `json:"cause"`
	Buggy    expectation `json:"buggy"`
	Correct  expectation `json:"correct"`
}

type expectation struct {
	File   string  `json:"file"`
	Rows   int     `json:"rows"`
	Checks []check `json:"checks"`
}

type check struct {
	Row    int    `json:"row"`
	Column string `json:"column"`
	Value  string `json:"value"`
}

const sampleDir = "../sample_queries"

func loadManifest(t *testing.T) manifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(sampleDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	return m
}

// openFixtureDB connects to QryvantaSample using the QRYVANTA_TEST_MSSQL_* variables.
func openFixtureDB(t *testing.T, database string) (*sql.DB, connection.Profile) {
	t.Helper()
	server := os.Getenv("QRYVANTA_TEST_MSSQL_SERVER")
	if server == "" {
		t.Skip("QRYVANTA_TEST_MSSQL_SERVER not set; start the fixture database with `docker compose run --rm fixtures`")
	}
	p := connection.Profile{
		Name: "fixtures", Server: server, Database: database, Auth: connection.AuthWindows,
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
	if err := p.Normalize().Validate(); err != nil {
		t.Fatalf("test profile: %v", err)
	}
	db, err := mssql.New().Open(p, os.Getenv("QRYVANTA_TEST_MSSQL_PASSWORD"), connection.OpenOptions{ConnectTimeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("connect to %s: %v (is the fixture database loaded?)", server, err)
	}
	return db, p
}

// TestSampleQueries runs every buggy and corrected query and checks the documented results.
// It guards the fixture data: diagnostic rules (Phase 5) rely on these exact outcomes.
func TestSampleQueries(t *testing.T) {
	m := loadManifest(t)
	if len(m.Scenarios) != 10 {
		t.Fatalf("manifest has %d scenarios, want 10", len(m.Scenarios))
	}
	db, _ := openFixtureDB(t, m.Database)

	for _, sc := range m.Scenarios {
		t.Run(sc.ID+"_"+sc.Title, func(t *testing.T) {
			t.Run("buggy", func(t *testing.T) { runExpectation(t, db, sc.Buggy) })
			t.Run("correct", func(t *testing.T) { runExpectation(t, db, sc.Correct) })
		})
	}
}

// readSample returns the text of a file in tests/sample_queries.
func readSample(t *testing.T, name string) string {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(sampleDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}

func runExpectation(t *testing.T, db *sql.DB, e expectation) {
	t.Helper()
	text := readSample(t, e.File)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cols, rows, err := query(ctx, db, string(text))
	if err != nil {
		t.Fatalf("%s: %v", e.File, err)
	}
	if len(rows) != e.Rows {
		t.Fatalf("%s returned %d rows, want %d", e.File, len(rows), e.Rows)
	}
	for _, c := range e.Checks {
		idx := indexOf(cols, c.Column)
		if idx < 0 {
			t.Fatalf("%s: no column %q (columns %v)", e.File, c.Column, cols)
		}
		if c.Row >= len(rows) {
			t.Fatalf("%s: check row %d out of range", e.File, c.Row)
		}
		if got := rows[c.Row][idx]; got != c.Value {
			t.Errorf("%s row %d %s = %q, want %q", e.File, c.Row, c.Column, got, c.Value)
		}
	}
}

// query returns all rows formatted as strings (NULL becomes "<NULL>").
func query(ctx context.Context, db *sql.DB, text string) ([]string, [][]string, error) {
	rs, err := db.QueryContext(ctx, text)
	if err != nil {
		return nil, nil, err
	}
	defer rs.Close()
	cols, err := rs.Columns()
	if err != nil {
		return nil, nil, err
	}
	var out [][]string
	for rs.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rs.Scan(ptrs...); err != nil {
			return nil, nil, err
		}
		row := make([]string, len(cols))
		for i, v := range vals {
			switch x := v.(type) {
			case nil:
				row[i] = "<NULL>"
			case []byte:
				row[i] = string(x) // decimal values arrive as text
			case time.Time:
				row[i] = x.Format(time.RFC3339)
			default:
				row[i] = fmt.Sprint(x)
			}
		}
		out = append(out, row)
	}
	return cols, out, rs.Err()
}

func indexOf(cols []string, name string) int {
	for i, c := range cols {
		if c == name {
			return i
		}
	}
	return -1
}

// TestReaderLoginCannotWrite proves the fixture's read-only login is enforced by the server.
// It runs only when the tests connect as qryvanta_reader.
func TestReaderLoginCannotWrite(t *testing.T) {
	if os.Getenv("QRYVANTA_TEST_MSSQL_USER") != "qryvanta_reader" {
		t.Skip("connect as qryvanta_reader to run this test")
	}
	db, _ := openFixtureDB(t, loadManifest(t).Database)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var n int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM dbo.Orders").Scan(&n); err != nil || n != 8 {
		t.Fatalf("reader cannot read Orders: n=%d err=%v", n, err)
	}

	_, err := db.ExecContext(ctx, "UPDATE dbo.Orders SET Status = 'Changed' WHERE OrderID = 101")
	if err == nil {
		t.Fatal("UPDATE succeeded for the read-only login")
	}
	if num, ok := serverErrorNumber(err); !ok || num != 229 {
		t.Fatalf("UPDATE error = %v, want SQL Server error 229 (permission denied)", err)
	}
}

// serverErrorNumber extracts the SQL Server error number from a go-mssqldb error without
// importing the driver into the test (the dialect package owns that mapping).
func serverErrorNumber(err error) (int32, bool) {
	var numbered interface{ SQLErrorNumber() int32 }
	if errors.As(err, &numbered) {
		return numbered.SQLErrorNumber(), true
	}
	return 0, false
}
