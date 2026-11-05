//go:build integration

package mssql

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/TuanFaied/qryvanta/internal/connection"
)

// Integration tests run against a real SQL Server:
//
//	go test -tags integration ./internal/dialect/mssql/
//
// Configure with environment variables (unset QRYVANTA_TEST_MSSQL_SERVER skips the tests):
//
//	QRYVANTA_TEST_MSSQL_SERVER    host, host\instance or IP
//	QRYVANTA_TEST_MSSQL_PORT      optional TCP port
//	QRYVANTA_TEST_MSSQL_USER      SQL login; empty uses Windows authentication
//	QRYVANTA_TEST_MSSQL_PASSWORD  password for the SQL login
//	QRYVANTA_TEST_MSSQL_TRUST     "true" to trust a self-signed certificate (local dev only)
func integrationProfile(t *testing.T) (connection.Profile, string) {
	t.Helper()
	server := os.Getenv("QRYVANTA_TEST_MSSQL_SERVER")
	if server == "" {
		t.Skip("QRYVANTA_TEST_MSSQL_SERVER not set")
	}
	p := connection.Profile{
		Name: "integration", Server: server, Encrypt: connection.EncryptMandatory,
		TrustServerCertificate: os.Getenv("QRYVANTA_TEST_MSSQL_TRUST") == "true",
		Auth:                   connection.AuthWindows,
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
	return p, os.Getenv("QRYVANTA_TEST_MSSQL_PASSWORD")
}

func TestIntegrationServerInfo(t *testing.T) {
	p, secret := integrationProfile(t)
	db, err := New().Open(p, secret, connection.OpenOptions{ConnectTimeout: 15 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	info, err := New().ServerInfo(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if info.ProductVersion == "" || info.Login == "" || info.Database == "" {
		t.Errorf("incomplete server info: %+v", info)
	}
	t.Logf("connected: %+v", info)

	var app string
	if err := db.QueryRowContext(ctx, "SELECT APP_NAME()").Scan(&app); err != nil {
		t.Fatal(err)
	}
	if app != "Qryvanta" {
		t.Errorf("APP_NAME() = %q, want Qryvanta", app)
	}
}

func TestIntegrationLoginFailureIsServerError(t *testing.T) {
	p, _ := integrationProfile(t)
	if p.Auth != connection.AuthSQL {
		t.Skip("needs SQL authentication")
	}
	db, err := New().Open(p, "definitely-wrong-password", connection.OpenOptions{ConnectTimeout: 15 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = New().ServerInfo(context.Background(), db)
	var se *connection.ServerError
	if !errors.As(err, &se) || se.Number != 18456 {
		t.Fatalf("err = %v, want SQL Server error 18456", err)
	}
}

func TestIntegrationCancellation(t *testing.T) {
	p, secret := integrationProfile(t)
	db, err := New().Open(p, secret, connection.OpenOptions{ConnectTimeout: 15 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = db.ExecContext(ctx, "WAITFOR DELAY '00:00:30'")
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Error("cancellation did not stop the query promptly")
	}
	// The pool must still be usable after a cancelled query.
	var one int
	if err := db.QueryRowContext(context.Background(), "SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Fatalf("pool unusable after cancel: %v", err)
	}
}
