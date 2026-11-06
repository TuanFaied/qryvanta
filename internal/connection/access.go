package connection

import (
	"context"
	"database/sql"
)

// AccessLevel summarises what a login could change in a database.
type AccessLevel string

const (
	// AccessReadOnly: no permission found that lets the login change data or schema.
	AccessReadOnly AccessLevel = "read_only"
	// AccessMayWrite: no direct write permission, but the login can run code (for example
	// EXECUTE on procedures) that may change data on its behalf.
	AccessMayWrite AccessLevel = "may_write"
	// AccessCanWrite: the login has direct permission to change data or schema.
	AccessCanWrite AccessLevel = "can_write"
	// AccessUnknown: the check could not be completed.
	AccessUnknown AccessLevel = "unknown"
)

// AccessReason is one permission that contributes to the access level.
type AccessReason struct {
	// Kind is "direct" (changes data itself) or "indirect" (runs code that may change data).
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// WriteAccess is the result of a write-permission check for one database.
type WriteAccess struct {
	Database string         `json:"database"`
	Level    AccessLevel    `json:"level"`
	Reasons  []AccessReason `json:"reasons"`
	// Error explains why Level is AccessUnknown.
	Error string `json:"error"`
}

// MaxAccessReasons caps how many reasons are reported; one is enough to prove write access.
const MaxAccessReasons = 10

// AccessChecker reports whether the login behind db could modify data in database.
//
// This is guidance, not a guarantee: permissions can change at any time, and checks that are
// not visible to the login (for example through ownership chains or signed modules) cannot be
// seen. Architecture decision D5 explains why a read-only login matters.
type AccessChecker interface {
	WriteAccess(ctx context.Context, db *sql.DB, database string) (WriteAccess, error)
}

// LevelFromReasons derives the access level from the reasons found.
func LevelFromReasons(reasons []AccessReason) AccessLevel {
	level := AccessReadOnly
	for _, r := range reasons {
		switch r.Kind {
		case "direct":
			return AccessCanWrite
		case "indirect":
			level = AccessMayWrite
		}
	}
	return level
}
