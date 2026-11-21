// Package schema describes database metadata (databases, objects, columns) independently of
// any database product. Dialect packages implement Reader; the explorer and, later, the
// analyzer consume it.
package schema

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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
	// Table resolves a table or view as SQL Server would from database (schema may be empty: the
	// login's default schema, then dbo) and describes it for analysis. A missing or invisible
	// object is ErrObjectNotFound.
	Table(ctx context.Context, db *sql.DB, database, schema, name string) (Table, error)
}

// Table describes a table or view for query analysis: its columns with their types, and the
// column sets that are unique and the foreign keys it declares.
type Table struct {
	Schema  string        `json:"schema"`
	Name    string        `json:"name"`
	Kind    ObjectKind    `json:"kind"`
	Columns []TableColumn `json:"columns"`
	// Keys are the column sets that identify a row: the primary key, unique constraints and
	// unique indexes without a filter. A view has none.
	Keys        [][]string   `json:"keys"`
	ForeignKeys []ForeignKey `json:"foreignKeys"`
}

// TableColumn is a column of a Table.
type TableColumn struct {
	Name string `json:"name"`
	// Type is the base system type, lower-case (int, nvarchar, datetime2, ...).
	Type string `json:"type"`
	// DataType is the type as written, for messages (nvarchar(100), decimal(10,2)).
	DataType  string `json:"dataType"`
	Nullable  bool   `json:"nullable"`
	Collation string `json:"collation"`
}

// ForeignKey is a foreign key from Columns of the table to RefColumns of RefSchema.RefTable.
type ForeignKey struct {
	Name       string   `json:"name"`
	Columns    []string `json:"columns"`
	RefSchema  string   `json:"refSchema"`
	RefTable   string   `json:"refTable"`
	RefColumns []string `json:"refColumns"`
}

// Column returns the column with the given name (case-insensitive), or nil.
func (t *Table) Column(name string) *TableColumn {
	for i := range t.Columns {
		if strings.EqualFold(t.Columns[i].Name, name) {
			return &t.Columns[i]
		}
	}
	return nil
}

// FullName is schema.name.
func (t *Table) FullName() string { return t.Schema + "." + t.Name }

// ErrDatabaseNotFound is returned when a database does not exist or the login cannot open it.
var ErrDatabaseNotFound = errors.New("database not found or not accessible")

// ErrObjectNotFound is returned when an object does not exist or is not visible to the login.
var ErrObjectNotFound = errors.New("object not found or not visible")
