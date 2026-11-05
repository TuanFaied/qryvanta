// Package connection manages saved database connection profiles.
//
// A Profile describes how to reach a server. It never contains a password: secrets are kept
// in a CredentialStore (the OS credential manager in production) and looked up by profile ID.
package connection

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode"
)

// AuthMethod selects how the connection authenticates.
type AuthMethod string

const (
	// AuthSQL uses a SQL Server login and password.
	AuthSQL AuthMethod = "sql"
	// AuthWindows uses integrated Windows authentication as the current OS user.
	AuthWindows AuthMethod = "windows"
)

// EncryptMode controls TLS for the connection.
//
// There is deliberately no mode that disables encryption entirely, because that would send
// the login packet, including the password, in clear text.
type EncryptMode string

const (
	// EncryptMandatory encrypts the connection (TDS 7.x "encrypt=true"). The default.
	EncryptMandatory EncryptMode = "mandatory"
	// EncryptStrict uses TDS 8.0 strict encryption; the certificate is always validated.
	EncryptStrict EncryptMode = "strict"
	// EncryptOptional encrypts only the login unless the server requires full encryption.
	EncryptOptional EncryptMode = "optional"
)

// Profile is a saved connection. It is safe to persist and to send to the UI.
type Profile struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Server   string     `json:"server"`
	Port     int        `json:"port"` // 0 = driver default (1433, or SQL Browser for named instances)
	Database string     `json:"database"`
	Auth     AuthMethod `json:"auth"`
	Username string     `json:"username"` // SQL authentication only

	Encrypt                EncryptMode `json:"encrypt"`
	TrustServerCertificate bool        `json:"trustServerCertificate"`
	// ConnectTimeoutSeconds overrides the application default when greater than zero.
	ConnectTimeoutSeconds int `json:"connectTimeoutSeconds"`
	// ReadOnlyIntent sets ApplicationIntent=ReadOnly, which routes to readable secondaries in
	// availability groups. It is not a security control on its own.
	ReadOnlyIntent bool `json:"readOnlyIntent"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Field limits. SQL Server identifiers are at most 128 characters.
const (
	MaxNameLength     = 100
	MaxServerLength   = 255
	MaxIdentLength    = 128
	MaxConnectTimeout = 300
)

// ValidationError lists every invalid field by its JSON name.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Fields))
	for _, k := range slices.Sorted(maps.Keys(e.Fields)) {
		parts = append(parts, k+": "+e.Fields[k])
	}
	return "invalid connection profile: " + strings.Join(parts, "; ")
}

// Normalize trims whitespace and fills defaults. Validate should be called afterwards.
func (p Profile) Normalize() Profile {
	p.Name = strings.TrimSpace(p.Name)
	p.Server = strings.TrimSpace(p.Server)
	p.Database = strings.TrimSpace(p.Database)
	p.Username = strings.TrimSpace(p.Username)
	if p.Encrypt == "" {
		p.Encrypt = EncryptMandatory
	}
	if p.Auth == AuthWindows {
		p.Username = ""
	}
	return p
}

// Validate checks the profile and returns a *ValidationError listing every invalid field.
func (p Profile) Validate() error {
	errs := map[string]string{}

	switch {
	case p.Name == "":
		errs["name"] = "is required"
	case len([]rune(p.Name)) > MaxNameLength:
		errs["name"] = fmt.Sprintf("must be at most %d characters", MaxNameLength)
	case hasControl(p.Name):
		errs["name"] = "must not contain control characters"
	}

	switch {
	case p.Server == "":
		errs["server"] = "is required"
	case len(p.Server) > MaxServerLength:
		errs["server"] = fmt.Sprintf("must be at most %d characters", MaxServerLength)
	case strings.ContainsAny(p.Server, ";=") || strings.ContainsFunc(p.Server, unicode.IsSpace) || hasControl(p.Server):
		errs["server"] = `must be a host name, address or host\instance, without spaces, ";" or "="`
	}

	if p.Port < 0 || p.Port > 65535 {
		errs["port"] = "must be between 1 and 65535, or 0 for the default"
	}

	if len([]rune(p.Database)) > MaxIdentLength || hasControl(p.Database) {
		errs["database"] = fmt.Sprintf("must be at most %d characters without control characters", MaxIdentLength)
	}

	switch p.Auth {
	case AuthSQL:
		switch {
		case p.Username == "":
			errs["username"] = "is required for SQL Server authentication"
		case len([]rune(p.Username)) > MaxIdentLength || hasControl(p.Username):
			errs["username"] = fmt.Sprintf("must be at most %d characters without control characters", MaxIdentLength)
		}
	case AuthWindows:
		if p.Username != "" {
			errs["username"] = "must be empty for Windows authentication"
		}
	default:
		errs["auth"] = `must be "sql" or "windows"`
	}

	switch p.Encrypt {
	case EncryptMandatory, EncryptOptional:
	case EncryptStrict:
		if p.TrustServerCertificate {
			errs["trustServerCertificate"] = "cannot be used with strict encryption, which always validates the certificate"
		}
	default:
		errs["encrypt"] = `must be "mandatory", "strict" or "optional"`
	}

	if p.ConnectTimeoutSeconds < 0 || p.ConnectTimeoutSeconds > MaxConnectTimeout {
		errs["connectTimeoutSeconds"] = fmt.Sprintf("must be between 1 and %d, or 0 for the default", MaxConnectTimeout)
	}

	if len(errs) > 0 {
		return &ValidationError{Fields: errs}
	}
	return nil
}

// UsesPassword reports whether the profile authenticates with a stored password.
func (p Profile) UsesPassword() bool { return p.Auth == AuthSQL }

func hasControl(s string) bool { return strings.ContainsFunc(s, unicode.IsControl) }

// newID returns a random 128-bit hex identifier.
func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate profile id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
