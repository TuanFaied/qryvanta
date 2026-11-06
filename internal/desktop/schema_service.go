package desktop

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/connection"
	"github.com/TuanFaied/qryvanta/internal/schema"
)

// metadataTimeout bounds each explorer request so a slow server cannot hang the tree.
const metadataTimeout = 30 * time.Second

// SchemaService exposes database metadata for the explorer. Every method works on a
// connected profile (ConnectionService.Connect) and never opens a connection itself.
type SchemaService struct {
	core *app.Core
}

// NewSchemaService creates the service.
func NewSchemaService(core *app.Core) *SchemaService {
	return &SchemaService{core: core}
}

// MetadataError is a failure the explorer shows on the node that failed to load.
type MetadataError struct {
	// Code is "not_connected", "not_found", "permission", "cancelled", "timeout" or "server".
	Code              string `json:"code"`
	ServerErrorNumber int32  `json:"serverErrorNumber"`
	Message           string `json:"message"`
}

// DatabasesResult is returned by Databases.
type DatabasesResult struct {
	Databases []schema.Database `json:"databases"`
	Error     *MetadataError    `json:"error"`
}

// ObjectsResult is returned by Objects.
type ObjectsResult struct {
	Objects []schema.Object `json:"objects"`
	Error   *MetadataError  `json:"error"`
}

// ColumnsResult is returned by Columns.
type ColumnsResult struct {
	Columns []schema.Column `json:"columns"`
	Error   *MetadataError  `json:"error"`
}

// Databases lists the databases on a connected server.
func (s *SchemaService) Databases(ctx context.Context, connectionID string) (DatabasesResult, error) {
	db, merr := s.db(connectionID)
	if merr != nil {
		return DatabasesResult{Databases: []schema.Database{}, Error: merr}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	dbs, err := s.core.Schema().Databases(ctx, db)
	if err != nil {
		return DatabasesResult{Databases: []schema.Database{}, Error: s.classify(err)}, nil
	}
	return DatabasesResult{Databases: dbs}, nil
}

// Objects lists tables, views, procedures and functions in one database.
func (s *SchemaService) Objects(ctx context.Context, connectionID, database string) (ObjectsResult, error) {
	db, merr := s.db(connectionID)
	if merr != nil {
		return ObjectsResult{Objects: []schema.Object{}, Error: merr}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	objs, err := s.core.Schema().Objects(ctx, db, database)
	if err != nil {
		return ObjectsResult{Objects: []schema.Object{}, Error: s.classify(err)}, nil
	}
	return ObjectsResult{Objects: objs}, nil
}

// Columns lists the columns of a table or view.
func (s *SchemaService) Columns(ctx context.Context, connectionID, database, schemaName, object string) (ColumnsResult, error) {
	db, merr := s.db(connectionID)
	if merr != nil {
		return ColumnsResult{Columns: []schema.Column{}, Error: merr}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	cols, err := s.core.Schema().Columns(ctx, db, database, schemaName, object)
	if err != nil {
		return ColumnsResult{Columns: []schema.Column{}, Error: s.classify(err)}, nil
	}
	return ColumnsResult{Columns: cols}, nil
}

func (s *SchemaService) db(connectionID string) (*sql.DB, *MetadataError) {
	db, err := s.core.Pool().DB(connectionID)
	if err != nil {
		return nil, &MetadataError{Code: "not_connected", Message: "Connect to this server first."}
	}
	return db, nil
}

// SQL Server permission errors: object/permission denied, and cannot open database.
var permissionErrorNumbers = map[int32]bool{229: true, 230: true, 262: true, 297: true, 300: true, 916: true, 4060: true}

func (s *SchemaService) classify(err error) *MetadataError {
	var se *connection.ServerError
	switch {
	case errors.Is(err, schema.ErrDatabaseNotFound), errors.Is(err, schema.ErrObjectNotFound):
		return &MetadataError{Code: "not_found", Message: err.Error()}
	case errors.Is(err, context.Canceled):
		return &MetadataError{Code: "cancelled", Message: "Loading was cancelled."}
	case errors.Is(err, context.DeadlineExceeded):
		return &MetadataError{Code: "timeout", Message: "The server did not answer within 30 seconds."}
	case errors.As(err, &se):
		code := "server"
		if permissionErrorNumbers[se.Number] {
			code = "permission"
		}
		return &MetadataError{Code: code, ServerErrorNumber: se.Number, Message: se.Message}
	default:
		s.core.Logger().Warn("metadata query failed", "err", err)
		return &MetadataError{Code: "server", Message: err.Error()}
	}
}
