package main

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/connection"
)

type fakeDriver struct{ gotSecret string }

func (f *fakeDriver) Open(_ connection.Profile, secret string, _ connection.OpenOptions) (*sql.DB, error) {
	f.gotSecret = secret
	return sql.OpenDB(nopConnector{}), nil
}

func (f *fakeDriver) ServerInfo(context.Context, *sql.DB) (connection.ServerInfo, error) {
	return connection.ServerInfo{ServerName: "DB1", ProductVersion: "16.0.1000.6", ProductLevel: "RTM",
		Edition: "Developer Edition (64-bit)", Database: "master", Login: "reader"}, nil
}

type nopConnector struct{}

func (nopConnector) Connect(context.Context) (driver.Conn, error) {
	return nil, errors.New("not dialled")
}
func (nopConnector) Driver() driver.Driver { return nil }

func testOpts(t *testing.T) (app.Options, *connection.MemoryStore, *fakeDriver) {
	t.Helper()
	creds, drv := connection.NewMemoryStore(), &fakeDriver{}
	return app.Options{
		ConfigPath:     filepath.Join(t.TempDir(), "config.json"),
		DisableFileLog: true,
		Credentials:    creds,
		Driver:         drv,
	}, creds, drv
}

func withStdin(t *testing.T, s string) {
	t.Helper()
	old := stdin
	stdin = strings.NewReader(s)
	t.Cleanup(func() { stdin = old })
}

func TestConnectionsAddTestRemove(t *testing.T) {
	opts, creds, drv := testOpts(t)
	withStdin(t, "pa$$ word\r\n")

	var out bytes.Buffer
	err := cmdConnections([]string{"add", "--name", "Dev", "--server", `localhost\SQLEXPRESS`,
		"--user", "reader", "--trust-server-certificate", "--password-stdin"}, opts, &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `Saved connection "Dev"`) || creds.Len() != 1 {
		t.Fatalf("add output %q, creds %d", out.String(), creds.Len())
	}

	out.Reset()
	if err := cmdConnections([]string{"test", "dev"}, opts, &out); err != nil {
		t.Fatal(err)
	}
	if drv.gotSecret != "pa$$ word" {
		t.Errorf("password from stdin = %q (CRLF must be trimmed, spaces kept)", drv.gotSecret)
	}
	for _, want := range []string{`Connected to "Dev"`, "16.0.1000.6 RTM", "Developer Edition", "reader"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("test output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "pa$$") {
		t.Fatal("test output contains the password")
	}

	out.Reset()
	if err := cmdConnections([]string{"remove", "--", "Dev"}, opts, &out); err != nil {
		t.Fatal(err)
	}
	if creds.Len() != 0 {
		t.Error("remove did not delete the stored password")
	}
}

func TestConnectionsAddReportsFieldErrors(t *testing.T) {
	opts, _, _ := testOpts(t)
	err := cmdConnections([]string{"add", "--name", "x", "--auth", "sql"}, opts, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	for _, want := range []string{"server is required", "username is required"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestConnectionsAddWithoutPasswordExplainsHowToAddOne(t *testing.T) {
	opts, _, _ := testOpts(t)
	var out bytes.Buffer
	if err := cmdConnections([]string{"add", "--name", "x", "--server", "db1", "--user", "u"}, opts, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no password stored") {
		t.Errorf("output = %q", out.String())
	}
	err := cmdConnections([]string{"test", "x"}, opts, &bytes.Buffer{})
	if !errors.Is(err, connection.ErrPasswordRequired) {
		t.Errorf("test without password = %v, want ErrPasswordRequired", err)
	}
}

func TestConnectionsTargetErrors(t *testing.T) {
	opts, _, _ := testOpts(t)
	if err := cmdConnections([]string{"test"}, opts, &bytes.Buffer{}); !errors.Is(err, errUsage) {
		t.Errorf("missing target = %v, want usage error", err)
	}
	if err := cmdConnections([]string{"remove", "ghost"}, opts, &bytes.Buffer{}); err == nil ||
		!strings.Contains(err.Error(), `no saved connection named "ghost"`) {
		t.Errorf("unknown target = %v", err)
	}
	withStdin(t, "")
	err := cmdConnections([]string{"add", "--name", "x", "--server", "db", "--user", "u", "--password-stdin"}, opts, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "stdin was empty") {
		t.Errorf("empty stdin = %v", err)
	}
}
