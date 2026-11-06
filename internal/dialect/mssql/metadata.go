package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/TuanFaied/qryvanta/internal/schema"
)

// Metadata reads SQL Server catalog views. It implements schema.Reader.
type Metadata struct{}

var _ schema.Reader = Metadata{}

// QuoteIdent quotes a SQL Server identifier the way QUOTENAME does: it wraps the name in
// brackets and doubles any closing bracket, so the result is always a single identifier.
// Identifiers longer than 128 characters are rejected, as SQL Server itself does.
func QuoteIdent(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty identifier")
	}
	if len([]rune(name)) > 128 {
		return "", fmt.Errorf("identifier longer than 128 characters")
	}
	if strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("identifier contains a NUL character")
	}
	return "[" + strings.ReplaceAll(name, "]", "]]") + "]", nil
}

const databasesQuery = `SELECT
	d.name,
	d.state_desc,
	CAST(CASE WHEN d.database_id <= 4 THEN 1 ELSE 0 END AS bit),
	d.is_read_only,
	CAST(CASE WHEN d.state = 0 AND HAS_DBACCESS(d.name) = 1 THEN 1 ELSE 0 END AS bit)
FROM sys.databases AS d
ORDER BY CASE WHEN d.database_id <= 4 THEN 1 ELSE 0 END, d.name`

// Databases lists the databases on the server, user databases first.
func (Metadata) Databases(ctx context.Context, db *sql.DB) ([]schema.Database, error) {
	rows, err := db.QueryContext(ctx, databasesQuery)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()
	out := []schema.Database{}
	for rows.Next() {
		var d schema.Database
		if err := rows.Scan(&d.Name, &d.State, &d.System, &d.ReadOnly, &d.Accessible); err != nil {
			return nil, convertError(err)
		}
		out = append(out, d)
	}
	return out, convertError(rows.Err())
}

// requireDatabase checks that the database exists and the login can open it, and returns
// its quoted name for use in three-part names.
func requireDatabase(ctx context.Context, db *sql.DB, database string) (string, error) {
	quoted, err := QuoteIdent(database)
	if err != nil {
		return "", fmt.Errorf("%w: %v", schema.ErrDatabaseNotFound, err)
	}
	var ok int
	err = db.QueryRowContext(ctx,
		`SELECT CASE WHEN EXISTS (SELECT 1 FROM sys.databases WHERE name = @name AND state = 0 AND HAS_DBACCESS(name) = 1) THEN 1 ELSE 0 END`,
		sql.Named("name", database)).Scan(&ok)
	if err != nil {
		return "", convertError(err)
	}
	if ok != 1 {
		return "", schema.ErrDatabaseNotFound
	}
	return quoted, nil
}

// Objects lists user tables, views, stored procedures and functions in a database.
func (Metadata) Objects(ctx context.Context, db *sql.DB, database string) ([]schema.Object, error) {
	q, err := requireDatabase(ctx, db, database)
	if err != nil {
		return nil, err
	}
	// q is a bracket-quoted identifier produced by QuoteIdent, so it cannot break out of the
	// three-part name.
	query := `SELECT s.name, o.name, RTRIM(o.type)
FROM ` + q + `.sys.objects AS o
JOIN ` + q + `.sys.schemas AS s ON s.schema_id = o.schema_id
WHERE o.type IN ('U', 'V', 'P', 'FN', 'IF', 'TF') AND o.is_ms_shipped = 0
ORDER BY s.name, o.name`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()
	out := []schema.Object{}
	for rows.Next() {
		var o schema.Object
		var typ string
		if err := rows.Scan(&o.Schema, &o.Name, &typ); err != nil {
			return nil, convertError(err)
		}
		switch typ {
		case "U":
			o.Kind = schema.KindTable
		case "V":
			o.Kind = schema.KindView
		case "P":
			o.Kind = schema.KindProcedure
		default:
			o.Kind = schema.KindFunction
		}
		out = append(out, o)
	}
	return out, convertError(rows.Err())
}

// Columns lists the columns of a table or view with key information.
func (Metadata) Columns(ctx context.Context, db *sql.DB, database, schemaName, object string) ([]schema.Column, error) {
	q, err := requireDatabase(ctx, db, database)
	if err != nil {
		return nil, err
	}
	qs, err := QuoteIdent(schemaName)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", schema.ErrObjectNotFound, err)
	}
	qo, err := QuoteIdent(object)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", schema.ErrObjectNotFound, err)
	}
	full := q + "." + qs + "." + qo

	var objectID sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT OBJECT_ID(@full)`, sql.Named("full", full)).Scan(&objectID); err != nil {
		return nil, convertError(err)
	}
	if !objectID.Valid {
		return nil, schema.ErrObjectNotFound
	}

	query := `SELECT
	c.column_id,
	c.name,
	t.name,
	c.max_length,
	c.precision,
	c.scale,
	c.is_nullable,
	c.is_identity,
	c.is_computed,
	CAST(CASE WHEN EXISTS (
		SELECT 1 FROM ` + q + `.sys.indexes AS i
		JOIN ` + q + `.sys.index_columns AS ic ON ic.object_id = i.object_id AND ic.index_id = i.index_id
		WHERE i.is_primary_key = 1 AND i.object_id = c.object_id AND ic.column_id = c.column_id
	) THEN 1 ELSE 0 END AS bit),
	(SELECT TOP (1) rs.name + N'.' + rt.name + N'.' + rc.name
		FROM ` + q + `.sys.foreign_key_columns AS fkc
		JOIN ` + q + `.sys.objects AS rt ON rt.object_id = fkc.referenced_object_id
		JOIN ` + q + `.sys.schemas AS rs ON rs.schema_id = rt.schema_id
		JOIN ` + q + `.sys.columns AS rc ON rc.object_id = fkc.referenced_object_id AND rc.column_id = fkc.referenced_column_id
		WHERE fkc.parent_object_id = c.object_id AND fkc.parent_column_id = c.column_id
		ORDER BY fkc.constraint_object_id)
FROM ` + q + `.sys.columns AS c
JOIN ` + q + `.sys.types AS t ON t.user_type_id = c.user_type_id
WHERE c.object_id = @id
ORDER BY c.column_id`
	rows, err := db.QueryContext(ctx, query, sql.Named("id", objectID.Int64))
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()
	out := []schema.Column{}
	for rows.Next() {
		var (
			c                schema.Column
			typeName         string
			maxLength        int
			precision, scale int
			references       sql.NullString
		)
		if err := rows.Scan(&c.Ordinal, &c.Name, &typeName, &maxLength, &precision, &scale,
			&c.Nullable, &c.Identity, &c.Computed, &c.PrimaryKey, &references); err != nil {
			return nil, convertError(err)
		}
		c.DataType = FormatType(typeName, maxLength, precision, scale)
		c.References = references.String
		out = append(out, c)
	}
	return out, convertError(rows.Err())
}

// FormatType renders a column type the way it is written in T-SQL, from sys.columns values.
func FormatType(name string, maxLength, precision, scale int) string {
	length := func(n int) string {
		if n == -1 {
			return "max"
		}
		return strconv.Itoa(n)
	}
	switch strings.ToLower(name) {
	case "varchar", "char", "varbinary", "binary":
		return fmt.Sprintf("%s(%s)", name, length(maxLength))
	case "nvarchar", "nchar":
		if maxLength == -1 {
			return name + "(max)"
		}
		return fmt.Sprintf("%s(%d)", name, maxLength/2)
	case "decimal", "numeric":
		return fmt.Sprintf("%s(%d,%d)", name, precision, scale)
	case "datetime2", "time", "datetimeoffset":
		return fmt.Sprintf("%s(%d)", name, scale)
	default:
		return name
	}
}
