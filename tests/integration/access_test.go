//go:build integration

package integration

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/TuanFaied/qryvanta/internal/connection"
	"github.com/TuanFaied/qryvanta/internal/dialect/mssql"
)

// TestWriteAccessReader: the fixture's qryvanta_reader has only db_datareader, SHOWPLAN and
// VIEW DEFINITION, so it must be reported as read-only.
func TestWriteAccessReader(t *testing.T) {
	if os.Getenv("QRYVANTA_TEST_MSSQL_USER") != "qryvanta_reader" {
		t.Skip("connect as qryvanta_reader to run this test")
	}
	m := loadManifest(t)
	db, _ := openFixtureDB(t, m.Database)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := mssql.Access{}.WriteAccess(ctx, db, m.Database)
	if err != nil {
		t.Fatal(err)
	}
	if res.Level != connection.AccessReadOnly || len(res.Reasons) != 0 {
		t.Fatalf("reader access = %+v, want read_only with no reasons", res)
	}
}

// TestWriteAccessAdmin: sa can change everything. Runs when the admin password is provided.
func TestWriteAccessAdmin(t *testing.T) {
	pw := os.Getenv("QRYVANTA_TEST_MSSQL_ADMIN_PASSWORD")
	if pw == "" {
		t.Skip("QRYVANTA_TEST_MSSQL_ADMIN_PASSWORD not set")
	}
	t.Setenv("QRYVANTA_TEST_MSSQL_USER", "sa")
	t.Setenv("QRYVANTA_TEST_MSSQL_PASSWORD", pw)
	m := loadManifest(t)
	db, _ := openFixtureDB(t, "master")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := mssql.Access{}.WriteAccess(ctx, db, m.Database)
	if err != nil {
		t.Fatal(err)
	}
	if res.Level != connection.AccessCanWrite || len(res.Reasons) == 0 || !strings.Contains(res.Reasons[0].Text, "sysadmin") {
		t.Fatalf("admin access = %+v, want can_write because of sysadmin", res)
	}
	if res.Database != m.Database {
		t.Errorf("checked database %q, want %q", res.Database, m.Database)
	}
}
