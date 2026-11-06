// Package schema describes database metadata (databases, objects, columns) independently of
// any database product. Dialect packages implement Reader; the explorer and, later, the
// analyzer consume it.
package schema

import (
	"context"
	"database/sql"
	"errors"
)

// Database is a database on the connected server.
type Database struct {
	Name string `json:"name"`
	// State is the server's state description, for example ONLINE, RESTORING or OFFLINE.
	State    string `json:"state"`
	System   bool   `json:"system"`
	ReadOnly bool   `json:"readOnly"`
	// Accessible is false when the login cannot open the database (no user mapped, or it is
	// not ONLINE). Inaccessible databases are listed but cannot be expanded.
	Accessible bool `json:"accessible"`
}

// ObjectKind classifies schema objects.
type ObjectKind string

const (
	KindTable     ObjectKind = "table"
	KindView      ObjectKind = "view"
	KindProcedure ObjectKind = "procedure"
	KindFunction  ObjectKind = "function"
)

// Object is a table, view, stored procedure or function.
type Object struct {
	Schema string     `json:"schema"`
	Name   string     `json:"name"`
	Kind   ObjectKind `json:"kind"`
}

// Column is a column of a table or view.
type Column struct {
	Ordinal  int    `json:"ordinal"`
	Name     string `json:"name"`
	DataType string `json:"dataType"` // display form, for example nvarchar(100) or decimal(10,2)
	Nullable bool   `json:"nullable"`
	Identity bool   `json:"identity"`
	Computed bool   `json:"computed"`
	// PrimaryKey is true when the column is part of the primary key.
	PrimaryKey bool `json:"primaryKey"`
	// References is "schema.table.column" when the column is part of a foreign key.
	References string `json:"references"`
}

// Reader reads metadata through an open database handle. Implementations must only read
// catalog views the login can see; results respect the login's metadata visibility.
type Reader interface {
	Databases(ctx context.Context, db *sql.DB) ([]Database, error)
	Objects(ctx context.Context, db *sql.DB, database string) ([]Object, error)
	Columns(ctx context.Context, db *sql.DB, database, schema, object string) ([]Column, error)
}

// ErrDatabaseNotFound is returned when a database does not exist or the login cannot open it.
var ErrDatabaseNotFound = errors.New("database not found or not accessible")

// ErrObjectNotFound is returned when an object does not exist or is not visible to the login.
var ErrObjectNotFound = errors.New("object not found or not visible")
