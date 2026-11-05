package security

import (
	"strings"
	"testing"
)

func TestIsSensitiveKey(t *testing.T) {
	sensitive := []string{"password", "Password", "db_password", "PWD", "connection_string",
		"connectionString", "ConnStr", "api-key", "apiKey", "authToken", "client_secret", "DSN", "credentialRef"}
	for _, k := range sensitive {
		if !IsSensitiveKey(k) {
			t.Errorf("IsSensitiveKey(%q) = false, want true", k)
		}
	}
	safe := []string{"server", "database", "user", "port", "timeout", "rows", "query_id", ""}
	for _, k := range safe {
		if IsSensitiveKey(k) {
			t.Errorf("IsSensitiveKey(%q) = true, want false", k)
		}
	}
}

func TestRedactString(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		secret string // must not appear in output
		keep   string // must still appear in output
	}{
		{"ado connection string", "server=db1;user id=sa;Password=S3cr3t!;database=app", "S3cr3t!", "database=app"},
		{"pwd spaced", "Server=db1; PWD = hunter2 ; Database=x", "hunter2", "Database=x"},
		{"quoted with separator", `server=db1;password='a;b=c';database=x`, "a;b=c", "database=x"},
		{"double quoted", `password="p w";user=u`, "p w", "user=u"},
		{"url query", "sqlserver://db1:1433?database=x&password=topsecret&encrypt=true", "topsecret", "encrypt=true"},
		{"url userinfo", "sqlserver://sa:MyPass@db1:1433?database=x", "MyPass", "db1:1433"},
		{"token colon", "token: abc.def.ghi", "abc.def.ghi", "token"},
		{"end of string", "user=sa;password=last", "last", "user=sa"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactString(tt.in)
			if strings.Contains(got, tt.secret) {
				t.Fatalf("RedactString(%q) = %q, still contains secret %q", tt.in, got, tt.secret)
			}
			if !strings.Contains(got, tt.keep) {
				t.Fatalf("RedactString(%q) = %q, lost non-secret part %q", tt.in, got, tt.keep)
			}
			if !strings.Contains(got, Mask) {
				t.Fatalf("RedactString(%q) = %q, missing mask", tt.in, got)
			}
		})
	}
}

func TestRedactStringLeavesPlainTextAlone(t *testing.T) {
	for _, s := range []string{"", "SELECT * FROM Orders WHERE Status = 'open'", "connected to db1 in 12ms"} {
		if got := RedactString(s); got != s {
			t.Errorf("RedactString(%q) = %q, want unchanged", s, got)
		}
	}
}
