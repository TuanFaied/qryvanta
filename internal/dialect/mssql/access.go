package mssql

import (
	"context"
	"database/sql"

	"github.com/TuanFaied/qryvanta/internal/connection"
)

// Access checks SQL Server write permissions. It implements connection.AccessChecker.
type Access struct{}

var _ connection.AccessChecker = Access{}

// writeAccessQuery runs inside the target database (through sp_executesql) and lists the
// permissions that let the current login change data or schema there. It only reads
// catalog views and permission functions; it never tries a write.
//
// Kinds: "direct" permissions change data themselves; "indirect" ones run code (stored
// procedures, functions) that may change data on the caller's behalf.
const writeAccessQuery = `
SET NOCOUNT ON;
DECLARE @reasons TABLE (priority int NOT NULL, kind varchar(10) NOT NULL, reason nvarchar(400) NOT NULL);

IF IS_SRVROLEMEMBER('sysadmin') = 1
    INSERT @reasons VALUES (1, 'direct', N'member of the sysadmin server role');
IF HAS_PERMS_BY_NAME(NULL, NULL, 'CONTROL SERVER') = 1 AND IS_SRVROLEMEMBER('sysadmin') = 0
    INSERT @reasons VALUES (2, 'direct', N'CONTROL SERVER permission');

INSERT @reasons
SELECT 3, 'direct', N'member of the ' + r.name + N' database role'
FROM (VALUES (N'db_owner'), (N'db_datawriter'), (N'db_ddladmin')) AS r(name)
WHERE IS_ROLEMEMBER(r.name) = 1;

INSERT @reasons
SELECT 4, 'direct', p.permission_name + N' permission on the database'
FROM fn_my_permissions(NULL, 'DATABASE') AS p
WHERE p.permission_name IN (N'CONTROL', N'ALTER', N'INSERT', N'UPDATE', N'DELETE', N'CREATE TABLE',
    N'CREATE PROCEDURE', N'CREATE FUNCTION', N'CREATE VIEW', N'ALTER ANY SCHEMA', N'TAKE OWNERSHIP');

-- Object-level grants are not visible at database scope; probe user tables (first matches only).
IF NOT EXISTS (SELECT 1 FROM @reasons WHERE kind = 'direct')
    INSERT @reasons
    SELECT TOP (5) 5, 'direct', p.perm + N' permission on ' + s.name + N'.' + t.name
    FROM sys.tables AS t
    JOIN sys.schemas AS s ON s.schema_id = t.schema_id
    CROSS JOIN (VALUES (N'INSERT'), (N'UPDATE'), (N'DELETE'), (N'ALTER')) AS p(perm)
    WHERE t.is_ms_shipped = 0
      AND HAS_PERMS_BY_NAME(QUOTENAME(s.name) + N'.' + QUOTENAME(t.name), 'OBJECT', p.perm) = 1;

INSERT @reasons
SELECT 6, 'indirect', N'EXECUTE permission on the database'
FROM fn_my_permissions(NULL, 'DATABASE') AS p
WHERE p.permission_name = N'EXECUTE';

IF NOT EXISTS (SELECT 1 FROM @reasons)
    INSERT @reasons
    SELECT TOP (5) 7, 'indirect', N'EXECUTE permission on ' + s.name + N'.' + o.name
    FROM sys.objects AS o
    JOIN sys.schemas AS s ON s.schema_id = o.schema_id
    WHERE o.type IN ('P', 'PC', 'X') AND o.is_ms_shipped = 0
      AND HAS_PERMS_BY_NAME(QUOTENAME(s.name) + N'.' + QUOTENAME(o.name), 'OBJECT', 'EXECUTE') = 1;

SELECT kind, reason FROM (SELECT kind, reason, MIN(priority) AS priority FROM @reasons GROUP BY kind, reason) AS r
ORDER BY priority, reason;`

// WriteAccess reports what the login could change in database.
func (Access) WriteAccess(ctx context.Context, db *sql.DB, database string) (connection.WriteAccess, error) {
	out := connection.WriteAccess{Database: database, Reasons: []connection.AccessReason{}}
	q, err := requireDatabase(ctx, db, database)
	if err != nil {
		return out, err
	}
	// EXEC <db>.sys.sp_executesql runs the batch in that database's context without changing
	// the pooled session's database. q comes from QuoteIdent.
	rows, err := db.QueryContext(ctx, "EXEC "+q+".sys.sp_executesql @stmt", sql.Named("stmt", writeAccessQuery))
	if err != nil {
		return out, convertError(err)
	}
	defer rows.Close()

	var direct, indirect []connection.AccessReason
	for rows.Next() {
		var r connection.AccessReason
		if err := rows.Scan(&r.Kind, &r.Text); err != nil {
			return out, convertError(err)
		}
		if r.Kind == "direct" {
			direct = append(direct, r)
		} else {
			indirect = append(indirect, r)
		}
	}
	if err := rows.Err(); err != nil {
		return out, convertError(err)
	}
	// Direct reasons first: they decide the level and are what the user needs to see.
	for _, r := range append(direct, indirect...) {
		if len(out.Reasons) == connection.MaxAccessReasons {
			break
		}
		out.Reasons = append(out.Reasons, r)
	}
	out.Level = connection.LevelFromReasons(out.Reasons)
	return out, nil
}
