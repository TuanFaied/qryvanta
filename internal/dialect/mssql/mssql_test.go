package mssql

import (
	"errors"
	"strings"
	"testing"
	"time"

	mssqldb "github.com/microsoft/go-mssqldb"
	"github.com/microsoft/go-mssqldb/msdsn"

	"github.com/TuanFaied/qryvanta/internal/connection"
)

func sqlProfile() connection.Profile {
	return connection.Profile{
		Name: "t", Server: "db1.corp.local", Auth: connection.AuthSQL, Username: "reader",
		Encrypt: connection.EncryptMandatory,
	}
}

// The tests check what the driver itself parses from the DSN Qryvanta builds, so a change
// in escaping or parameter names is caught here rather than at connect time.
func TestConfigBasicSQLAuth(t *testing.T) {
	p := sqlProfile()
	p.Port = 14333
	p.Database = "Sales"
	cfg, err := Config(p, "pw", connection.OpenOptions{ConnectTimeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "db1.corp.local" || cfg.Port != 14333 || cfg.Database != "Sales" {
		t.Errorf("host/port/db = %q/%d/%q", cfg.Host, cfg.Port, cfg.Database)
	}
	if cfg.User != "reader" || cfg.Password != "pw" {
		t.Errorf("user/password not passed through: %q", cfg.User)
	}
	if cfg.Encryption != msdsn.EncryptionRequired {
		t.Errorf("encryption = %v, want required", cfg.Encryption)
	}
	if cfg.TLSConfig == nil || cfg.TLSConfig.InsecureSkipVerify {
		t.Error("certificate must be validated by default")
	}
	if cfg.DialTimeout != 20*time.Second {
		t.Errorf("dial timeout = %v", cfg.DialTimeout)
	}
	if cfg.AppName != "Qryvanta" || cfg.ReadOnlyIntent {
		t.Errorf("app name/intent = %q/%v", cfg.AppName, cfg.ReadOnlyIntent)
	}
}

func TestConfigEscapesSpecialCharacters(t *testing.T) {
	p := sqlProfile()
	p.Username = `DOMAIN\user@x`
	p.Database = "My DB;x=1"
	const tricky = `p@ss:w/rd?#&;=%20'"`
	cfg, err := Config(p, tricky, connection.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Password != tricky || cfg.User != `DOMAIN\user@x` || cfg.Database != "My DB;x=1" {
		t.Errorf("values changed by escaping: user=%q db=%q pwOK=%v", cfg.User, cfg.Database, cfg.Password == tricky)
	}
	if cfg.Host != "db1.corp.local" {
		t.Errorf("host = %q; special characters leaked into the host", cfg.Host)
	}
}

func TestConfigNamedInstanceAndLocalAliases(t *testing.T) {
	p := sqlProfile()
	p.Server = `.\SQLEXPRESS`
	cfg, err := Config(p, "pw", connection.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "localhost" || cfg.Instance != "SQLEXPRESS" {
		t.Errorf("host/instance = %q/%q", cfg.Host, cfg.Instance)
	}
}

func TestConfigEncryptionModes(t *testing.T) {
	tests := []struct {
		mode  connection.EncryptMode
		trust bool
		want  msdsn.Encryption
		skip  bool
	}{
		{connection.EncryptMandatory, false, msdsn.EncryptionRequired, false},
		{connection.EncryptMandatory, true, msdsn.EncryptionRequired, true},
		{connection.EncryptStrict, false, msdsn.EncryptionStrict, false},
		{connection.EncryptOptional, false, msdsn.EncryptionOff, false},
	}
	for _, tt := range tests {
		p := sqlProfile()
		p.Encrypt, p.TrustServerCertificate = tt.mode, tt.trust
		cfg, err := Config(p, "pw", connection.OpenOptions{})
		if err != nil {
			t.Fatalf("%s: %v", tt.mode, err)
		}
		if cfg.Encryption != tt.want {
			t.Errorf("%s: encryption = %v, want %v", tt.mode, cfg.Encryption, tt.want)
		}
		if cfg.TLSConfig != nil && cfg.TLSConfig.InsecureSkipVerify != tt.skip {
			t.Errorf("%s trust=%v: InsecureSkipVerify = %v", tt.mode, tt.trust, cfg.TLSConfig.InsecureSkipVerify)
		}
		if cfg.Encryption == msdsn.EncryptionDisabled {
			t.Errorf("%s: encryption must never be fully disabled", tt.mode)
		}
	}
}

func TestConfigWindowsAuthHasNoCredentials(t *testing.T) {
	p := connection.Profile{Name: "w", Server: "db1", Database: "Sales", Auth: connection.AuthWindows,
		Encrypt: connection.EncryptMandatory, ReadOnlyIntent: true}
	cfg, err := Config(p, "ignored-secret", connection.OpenOptions{AppName: "Qryvanta test"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.User != "" || cfg.Password != "" {
		t.Errorf("Windows auth must not send a user or password: user=%q", cfg.User)
	}
	if !cfg.ReadOnlyIntent || cfg.AppName != "Qryvanta test" {
		t.Errorf("intent/app name = %v/%q", cfg.ReadOnlyIntent, cfg.AppName)
	}
}

func TestConvertErrorKeepsServerErrorNumber(t *testing.T) {
	err := convertError(mssqldb.Error{Number: 18456, State: 1, Class: 14, Message: "Login failed for user 'reader'."})
	var se *connection.ServerError
	if !errors.As(err, &se) || se.Number != 18456 || se.Class != 14 {
		t.Fatalf("convertError = %#v", err)
	}
	plain := errors.New("dial tcp: connection refused")
	if convertError(plain) != plain {
		t.Error("non-server errors must pass through")
	}
}

func TestConvertErrorNil(t *testing.T) {
	// Callers pass rows.Err() straight through, which is nil on success.
	if err := convertError(nil); err != nil {
		t.Fatalf("convertError(nil) = %v", err)
	}
}

func TestConvertErrorExplainsUntrustedCertificates(t *testing.T) {
	// go-mssqldb reports this as text, as seen against SQL Server Express with a self-signed cert.
	raw := errors.New("TLS Handshake failed: tls: failed to verify certificate: x509: certificate signed by unknown authority")
	err := convertError(raw)
	if !errors.Is(err, raw) || !strings.Contains(err.Error(), "Trust server certificate") {
		t.Fatalf("convertError = %v", err)
	}
}
