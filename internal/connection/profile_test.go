package connection

import (
	"errors"
	"strings"
	"testing"
)

func validSQLProfile() Profile {
	return Profile{Name: "Local dev", Server: `localhost\SQLEXPRESS`, Auth: AuthSQL, Username: "app_reader"}
}

func TestNormalizeAppliesDefaults(t *testing.T) {
	p := Profile{Name: "  Prod  ", Server: " db1 ", Auth: AuthWindows, Username: "ignored"}.Normalize()
	if p.Name != "Prod" || p.Server != "db1" {
		t.Errorf("whitespace not trimmed: %+v", p)
	}
	if p.Encrypt != EncryptMandatory {
		t.Errorf("Encrypt = %q, want mandatory by default", p.Encrypt)
	}
	if p.Username != "" {
		t.Error("Windows authentication must clear the username")
	}
}

func TestValidateAcceptsValidProfiles(t *testing.T) {
	cases := []Profile{
		validSQLProfile().Normalize(),
		{Name: "Win", Server: "db1.corp.local", Port: 14330, Auth: AuthWindows, Encrypt: EncryptStrict},
		{Name: "Azure", Server: "x.database.windows.net", Database: "app", Auth: AuthSQL, Username: "u",
			Encrypt: EncryptOptional, TrustServerCertificate: true, ConnectTimeoutSeconds: 30},
	}
	for _, p := range cases {
		if err := p.Validate(); err != nil {
			t.Errorf("Validate(%+v) = %v", p, err)
		}
	}
}

func TestValidateRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		field  string
		mutate func(*Profile)
	}{
		{"name", func(p *Profile) { p.Name = "" }},
		{"name", func(p *Profile) { p.Name = strings.Repeat("n", MaxNameLength+1) }},
		{"server", func(p *Profile) { p.Server = "" }},
		{"server", func(p *Profile) { p.Server = "db1;Password=x" }},
		{"server", func(p *Profile) { p.Server = "db 1" }},
		{"port", func(p *Profile) { p.Port = 70000 }},
		{"auth", func(p *Profile) { p.Auth = "kerberos" }},
		{"username", func(p *Profile) { p.Username = "" }},
		{"encrypt", func(p *Profile) { p.Encrypt = "disable" }},
		{"trustServerCertificate", func(p *Profile) { p.Encrypt = EncryptStrict; p.TrustServerCertificate = true }},
		{"connectTimeoutSeconds", func(p *Profile) { p.ConnectTimeoutSeconds = MaxConnectTimeout + 1 }},
		{"database", func(p *Profile) { p.Database = "bad\x00name" }},
	}
	for _, tt := range tests {
		p := validSQLProfile().Normalize()
		tt.mutate(&p)
		err := p.Validate()
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%s: Validate() = %v, want ValidationError", tt.field, err)
			continue
		}
		if _, ok := ve.Fields[tt.field]; !ok {
			t.Errorf("%s: missing error for field; got %v", tt.field, ve.Fields)
		}
	}
}

func TestWindowsAuthRejectsUsername(t *testing.T) {
	// Validate (without Normalize) must still catch an inconsistent profile.
	p := Profile{Name: "w", Server: "db1", Auth: AuthWindows, Username: "sa", Encrypt: EncryptMandatory}
	var ve *ValidationError
	if !errors.As(p.Validate(), &ve) || ve.Fields["username"] == "" {
		t.Fatalf("expected username error, got %v", p.Validate())
	}
}

func TestNewIDIsRandomHex(t *testing.T) {
	a, err := newID()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := newID()
	if len(a) != 32 || a == b {
		t.Errorf("unexpected ids %q %q", a, b)
	}
}
