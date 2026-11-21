package app

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/connection"
	"github.com/TuanFaied/qryvanta/internal/parser"
	"github.com/TuanFaied/qryvanta/internal/parser/parsertest"
	"github.com/TuanFaied/qryvanta/internal/schema"
)

// tableReader is a schema.Reader whose Table answers from a fixed list and records its calls.
type tableReader struct {
	mu    sync.Mutex
	calls []string
}

func (r *tableReader) Databases(context.Context, *sql.DB) ([]schema.Database, error) { return nil, nil }
func (r *tableReader) Objects(context.Context, *sql.DB, string) ([]schema.Object, error) {
	return nil, nil
}
func (r *tableReader) Columns(context.Context, *sql.DB, string, string, string) ([]schema.Column, error) {
	return nil, nil
}

func (r *tableReader) Table(_ context.Context, _ *sql.DB, database, schemaName, name string) (schema.Table, error) {
	r.mu.Lock()
	r.calls = append(r.calls, database+"|"+schemaName+"|"+name)
	r.mu.Unlock()
	switch strings.ToLower(name) {
	case "orders", "archive":
		return schema.Table{Schema: "dbo", Name: name, Kind: schema.KindTable}, nil
	case "flaky":
		return schema.Table{}, errors.New("connection reset")
	}
	return schema.Table{}, schema.ErrObjectNotFound
}

func (r *tableReader) count(call string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.calls {
		if c == call {
			n++
		}
	}
	return n
}

// crossJoined is one SELECT over the named sources, CROSS JOINed (which no rule reports).
func crossJoined(names ...parser.ObjectName) parsertest.Fake {
	return parsertest.Fake{Statements: func(string) []parser.Statement {
		var from *parser.Source
		for i := range names {
			src := &parser.Source{Kind: parser.SourceTable, Name: &names[i]}
			if from == nil {
				from = src
			} else {
				from = &parser.Source{Kind: parser.SourceJoin, JoinType: parser.JoinCross, Left: from, Right: src}
			}
		}
		return []parser.Statement{{Kind: parser.StmtSelect, Query: &parser.Query{Kind: parser.QuerySelect, From: from}}}
	}}
}

func metadataCore(t *testing.T, p parser.Parser, reader schema.Reader) (*Core, string) {
	t.Helper()
	c, err := New(Options{
		ConfigPath:     filepath.Join(t.TempDir(), "config.json"),
		DisableFileLog: true,
		Credentials:    connection.NewMemoryStore(),
		Driver:         nopDriver{},
		Access:         noAccess{},
		Schema:         reader,
		Parser:         p,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	prof, err := c.Connections().Create(connection.Profile{Name: "x", Server: "db", Auth: connection.AuthWindows}, "")
	if err != nil {
		t.Fatal(err)
	}
	return c, prof.ID
}

func unresolved(a Analysis) map[string]string {
	out := map[string]string{}
	for _, u := range a.Metadata.Unresolved {
		out[u.Name] = u.Reason
	}
	return out
}

func TestAnalyzeMetadataNeedsAConnection(t *testing.T) {
	reader := &tableReader{}
	c, id := metadataCore(t, crossJoined(parser.ObjectName{Schema: "dbo", Name: "Orders"}), reader)

	a, err := c.Analyze(context.Background(), "SELECT 1", AnalysisTarget{})
	if err != nil {
		t.Fatal(err)
	}
	if a.Metadata.Checked || !strings.Contains(a.Metadata.Reason, "No connection") || a.Metadata.Unresolved == nil {
		t.Fatalf("metadata = %+v", a.Metadata)
	}
	a, _ = c.Analyze(context.Background(), "SELECT 1", AnalysisTarget{ConnectionID: id, Database: "Sales"})
	if a.Metadata.Checked || !strings.Contains(a.Metadata.Reason, "Not connected") {
		t.Fatalf("metadata = %+v", a.Metadata)
	}
	if len(reader.calls) != 0 {
		t.Fatalf("the catalog was read without a connection: %v", reader.calls)
	}
}

func TestAnalyzeResolvesTablesAndReportsTheRest(t *testing.T) {
	reader := &tableReader{}
	c, id := metadataCore(t, crossJoined(
		parser.ObjectName{Schema: "dbo", Name: "Orders"},
		parser.ObjectName{Name: "Missing"},
		parser.ObjectName{Database: "History", Schema: "dbo", Name: "Archive"},
		parser.ObjectName{Server: "remote", Database: "db", Schema: "dbo", Name: "T"},
		parser.ObjectName{Name: "#scratch"},
		parser.ObjectName{Name: "Flaky"},
	), reader)
	if _, err := c.Pool().Connect(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	target := AnalysisTarget{ConnectionID: id, Database: "Sales"}
	a, err := c.Analyze(context.Background(), "SELECT 1", target)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Metadata.Checked {
		t.Fatalf("metadata = %+v", a.Metadata)
	}
	got := unresolved(a)
	want := map[string]string{
		"Missing":         "not found, or no permission to see it",
		"remote.db.dbo.T": "on a linked server; not checked",
		"Flaky":           "could not read the catalog: connection reset",
	}
	if len(got) != len(want) {
		t.Fatalf("unresolved = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("unresolved[%s] = %q, want %q", k, got[k], v)
		}
	}
	// A three-part name reads its own database; temporary tables are never looked up.
	if reader.count("Sales|dbo|Orders") != 1 || reader.count("History|dbo|Archive") != 1 || reader.count("Sales||#scratch") != 0 {
		t.Fatalf("calls = %v", reader.calls)
	}

	// Found and not-found results are cached; errors are retried.
	if _, err := c.Analyze(context.Background(), "SELECT 1", target); err != nil {
		t.Fatal(err)
	}
	if reader.count("Sales|dbo|Orders") != 1 || reader.count("Sales||Missing") != 1 || reader.count("Sales||Flaky") != 2 {
		t.Fatalf("calls after a second analysis = %v", reader.calls)
	}
	// Another database is another cache entry.
	if _, err := c.Analyze(context.Background(), "SELECT 1", AnalysisTarget{ConnectionID: id, Database: "Other"}); err != nil {
		t.Fatal(err)
	}
	if reader.count("Other|dbo|Orders") != 1 {
		t.Fatalf("calls = %v", reader.calls)
	}
	// Refreshing the explorer forgets the connection's entries.
	c.ForgetMetadata(id)
	if _, err := c.Analyze(context.Background(), "SELECT 1", target); err != nil {
		t.Fatal(err)
	}
	if reader.count("Sales|dbo|Orders") != 2 {
		t.Fatalf("calls after ForgetMetadata = %v", reader.calls)
	}
}

func TestAnalyzeWithoutADatabase(t *testing.T) {
	reader := &tableReader{}
	c, id := metadataCore(t, crossJoined(parser.ObjectName{Schema: "dbo", Name: "Orders"},
		parser.ObjectName{Database: "History", Schema: "dbo", Name: "Archive"}), reader)
	if _, err := c.Pool().Connect(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	a, err := c.Analyze(context.Background(), "SELECT 1", AnalysisTarget{ConnectionID: id})
	if err != nil {
		t.Fatal(err)
	}
	if got := unresolved(a); got["dbo.Orders"] != "no database is selected" || len(got) != 1 {
		t.Fatalf("unresolved = %v", got)
	}
}
