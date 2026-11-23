package mssql

import (
	"context"
	"database/sql"
	"errors"
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

// Table resolves a table or view the way SQL Server resolves a name in a query run from
// database: with an empty schema, OBJECT_ID uses the login's default schema and then dbo. It
// reads the columns, the unique column sets (primary key, unique constraints, unique indexes
// without a filter) and the foreign keys. Only catalog views are read.
func (Metadata) Table(ctx context.Context, db *sql.DB, database, schemaName, name string) (schema.Table, error) {
	q, err := requireDatabase(ctx, db, database)
	if err != nil {
		return schema.Table{}, err
	}
	qo, err := QuoteIdent(name)
	if err != nil {
		return schema.Table{}, fmt.Errorf("%w: %v", schema.ErrObjectNotFound, err)
	}
	full := q + ".." + qo
	if schemaName != "" {
		qs, err := QuoteIdent(schemaName)
		if err != nil {
			return schema.Table{}, fmt.Errorf("%w: %v", schema.ErrObjectNotFound, err)
		}
		full = q + "." + qs + "." + qo
	}

	var (
		objectID sql.NullInt64
		t        schema.Table
		kind     string
	)
	if err := db.QueryRowContext(ctx, `SELECT OBJECT_ID(@full)`, sql.Named("full", full)).Scan(&objectID); err != nil {
		return schema.Table{}, convertError(err)
	}
	if !objectID.Valid {
		return schema.Table{}, schema.ErrObjectNotFound
	}
	id := sql.Named("id", objectID.Int64)
	err = db.QueryRowContext(ctx, `SELECT s.name, o.name, RTRIM(o.type)
FROM `+q+`.sys.objects AS o JOIN `+q+`.sys.schemas AS s ON s.schema_id = o.schema_id
WHERE o.object_id = @id`, id).Scan(&t.Schema, &t.Name, &kind)
	if errors.Is(err, sql.ErrNoRows) {
		return schema.Table{}, schema.ErrObjectNotFound
	}
	if err != nil {
		return schema.Table{}, convertError(err)
	}
	t.Database = database
	switch kind {
	case "U":
		t.Kind = schema.KindTable
	case "V":
		t.Kind = schema.KindView
	default:
		return schema.Table{}, fmt.Errorf("%w: %s is not a table or view", schema.ErrObjectNotFound, name)
	}

	rows, err := db.QueryContext(ctx, `SELECT c.name, TYPE_NAME(c.system_type_id), t.name, c.max_length, c.precision, c.scale,
	c.is_nullable, ISNULL(c.collation_name, N'')
FROM `+q+`.sys.columns AS c
JOIN `+q+`.sys.types AS t ON t.user_type_id = c.user_type_id
WHERE c.object_id = @id
ORDER BY c.column_id`, id)
	if err != nil {
		return schema.Table{}, convertError(err)
	}
	t.Columns = []schema.TableColumn{}
	for rows.Next() {
		var (
			c                      schema.TableColumn
			typeName               string
			maxLength, prec, scale int
		)
		if err := rows.Scan(&c.Name, &c.Type, &typeName, &maxLength, &prec, &scale, &c.Nullable, &c.Collation); err != nil {
			rows.Close()
			return schema.Table{}, convertError(err)
		}
		c.Type = strings.ToLower(c.Type)
		c.DataType = FormatType(typeName, maxLength, prec, scale)
		t.Columns = append(t.Columns, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return schema.Table{}, convertError(err)
	}

	t.Keys = [][]string{}
	rows, err = db.QueryContext(ctx, `SELECT i.index_id, c.name
FROM `+q+`.sys.indexes AS i
JOIN `+q+`.sys.index_columns AS ic ON ic.object_id = i.object_id AND ic.index_id = i.index_id
JOIN `+q+`.sys.columns AS c ON c.object_id = ic.object_id AND c.column_id = ic.column_id
WHERE i.object_id = @id AND i.is_unique = 1 AND i.has_filter = 0 AND i.is_disabled = 0 AND ic.is_included_column = 0
ORDER BY i.index_id, ic.key_ordinal`, id)
	if err != nil {
		return schema.Table{}, convertError(err)
	}
	last := -1
	for rows.Next() {
		var (
			index  int
			column string
		)
		if err := rows.Scan(&index, &column); err != nil {
			rows.Close()
			return schema.Table{}, convertError(err)
		}
		if index != last {
			t.Keys = append(t.Keys, nil)
			last = index
		}
		t.Keys[len(t.Keys)-1] = append(t.Keys[len(t.Keys)-1], column)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return schema.Table{}, convertError(err)
	}

	t.ForeignKeys = []schema.ForeignKey{}
	rows, err = db.QueryContext(ctx, `SELECT fk.name, pc.name, rs.name, rt.name, rc.name
FROM `+q+`.sys.foreign_keys AS fk
JOIN `+q+`.sys.foreign_key_columns AS fkc ON fkc.constraint_object_id = fk.object_id
JOIN `+q+`.sys.columns AS pc ON pc.object_id = fkc.parent_object_id AND pc.column_id = fkc.parent_column_id
JOIN `+q+`.sys.objects AS rt ON rt.object_id = fkc.referenced_object_id
JOIN `+q+`.sys.schemas AS rs ON rs.schema_id = rt.schema_id
JOIN `+q+`.sys.columns AS rc ON rc.object_id = fkc.referenced_object_id AND rc.column_id = fkc.referenced_column_id
WHERE fk.parent_object_id = @id
ORDER BY fk.name, fkc.constraint_column_id`, id)
	if err != nil {
		return schema.Table{}, convertError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var fkName, col, refSchema, refTable, refCol string
		if err := rows.Scan(&fkName, &col, &refSchema, &refTable, &refCol); err != nil {
			return schema.Table{}, convertError(err)
		}
		n := len(t.ForeignKeys)
		if n == 0 || t.ForeignKeys[n-1].Name != fkName {
			t.ForeignKeys = append(t.ForeignKeys, schema.ForeignKey{Name: fkName, RefSchema: refSchema, RefTable: refTable})
			n++
		}
		fk := &t.ForeignKeys[n-1]
		fk.Columns = append(fk.Columns, col)
		fk.RefColumns = append(fk.RefColumns, refCol)
	}
	return t, convertError(rows.Err())
}
