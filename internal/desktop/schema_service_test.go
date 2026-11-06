package desktop

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/connection"
	"github.com/TuanFaied/qryvanta/internal/schema"
)

// fakeReader returns canned metadata and records the arguments it received.
type fakeReader struct {
	err       error
	gotDB     string
	gotSchema string
	gotObject string
	sawHandle bool
}

func (f *fakeReader) Databases(_ context.Context, db *sql.DB) ([]schema.Database, error) {
	f.sawHandle = db != nil
	if f.err != nil {
		return nil, f.err
	}
	return []schema.Database{{Name: "Sales", State: "ONLINE", Accessible: true}, {Name: "master", System: true, Accessible: true}}, nil
}

func (f *fakeReader) Objects(_ context.Context, _ *sql.DB, database string) ([]schema.Object, error) {
	f.gotDB = database
	if f.err != nil {
		return nil, f.err
	}
	return []schema.Object{{Schema: "dbo", Name: "Orders", Kind: schema.KindTable}}, nil
}

func (f *fakeReader) Columns(_ context.Context, _ *sql.DB, database, s, o string) ([]schema.Column, error) {
	f.gotDB, f.gotSchema, f.gotObject = database, s, o
	if f.err != nil {
		return nil, f.err
	}
	return []schema.Column{{Ordinal: 1, Name: "OrderID", DataType: "int", PrimaryKey: true}}, nil
}

func newSchemaService(t *testing.T) (*SchemaService, *ConnectionService, *fakeReader) {
	t.Helper()
	reader := &fakeReader{}
	core, err := app.New(app.Options{
		ConfigPath:     filepath.Join(t.TempDir(), "config.json"),
		DisableFileLog: true,
		Credentials:    connection.NewMemoryStore(),
		Driver:         &fakeDriver{},
		Schema:         reader,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = core.Close() })
	return NewSchemaService(core), NewConnectionService(core), reader
}

func TestSchemaRequiresConnectedProfile(t *testing.T) {
	s, _, _ := newSchemaService(t)
	res, err := s.Databases(context.Background(), "missing")
	if err != nil || res.Error == nil || res.Error.Code != "not_connected" || res.Databases == nil {
		t.Fatalf("Databases(not connected) = %+v, %v", res, err)
	}
}

func TestSchemaPassesThroughForConnectedProfile(t *testing.T) {
	s, conns, reader := newSchemaService(t)
	saved, _ := conns.Save(sqlInput())
	id := saved.Connection.Profile.ID
	if r, _ := conns.Connect(context.Background(), id); !r.OK {
		t.Fatal("connect failed")
	}

	dbs, _ := s.Databases(context.Background(), id)
	if dbs.Error != nil || len(dbs.Databases) != 2 || !reader.sawHandle {
		t.Fatalf("Databases = %+v", dbs)
	}
	objs, _ := s.Objects(context.Background(), id, "Sales")
	if objs.Error != nil || len(objs.Objects) != 1 || reader.gotDB != "Sales" {
		t.Fatalf("Objects = %+v (db %q)", objs, reader.gotDB)
	}
	cols, _ := s.Columns(context.Background(), id, "Sales", "dbo", "Orders")
	if cols.Error != nil || !cols.Columns[0].PrimaryKey || reader.gotSchema != "dbo" || reader.gotObject != "Orders" {
		t.Fatalf("Columns = %+v", cols)
	}
}

func TestSchemaClassifiesErrors(t *testing.T) {
	s, conns, reader := newSchemaService(t)
	saved, _ := conns.Save(sqlInput())
	id := saved.Connection.Profile.ID
	_, _ = conns.Connect(context.Background(), id)

	cases := []struct {
		err  error
		code string
	}{
		{schema.ErrDatabaseNotFound, "not_found"},
		{schema.ErrObjectNotFound, "not_found"},
		{&connection.ServerError{Number: 916, Message: "The server principal is not able to access the database"}, "permission"},
		{&connection.ServerError{Number: 8115, Message: "Arithmetic overflow"}, "server"},
		{context.DeadlineExceeded, "timeout"},
		{context.Canceled, "cancelled"},
		{errors.New("connection reset"), "server"},
	}
	for _, c := range cases {
		reader.err = c.err
		res, err := s.Objects(context.Background(), id, "Sales")
		if err != nil || res.Error == nil || res.Error.Code != c.code || res.Objects == nil {
			t.Errorf("%v: got %+v, %v; want code %s", c.err, res.Error, err, c.code)
		}
	}
}
