//go:build integration

package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/connection"
	"github.com/TuanFaied/qryvanta/internal/dialect/mssql"
	"github.com/TuanFaied/qryvanta/internal/schema"
)

// TestTableMetadata reads tables and views as the metadata rules do (#69).
func TestTableMetadata(t *testing.T) {
	m := loadManifest(t)
	db, _ := openFixtureDB(t, "master")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	md := mssql.Metadata{}

	orders, err := md.Table(ctx, db, m.Database, "dbo", "Orders")
	if err != nil {
		t.Fatal(err)
	}
	if orders.Kind != schema.KindTable || orders.FullName() != "dbo.Orders" {
		t.Fatalf("Orders = %+v", orders)
	}
	if len(orders.Keys) != 1 || !slices.Equal(orders.Keys[0], []string{"OrderID"}) {
		t.Errorf("keys = %v, want [[OrderID]]", orders.Keys)
	}
	c := orders.Column("customerid")
	if c == nil || !c.Nullable || c.Type != "int" {
		t.Errorf("CustomerID = %+v, want a nullable int", c)
	}
	if d := orders.Column("OrderDate"); d == nil || d.Type != "datetime2" || d.DataType != "datetime2(0)" {
		t.Errorf("OrderDate = %+v", d)
	}
	if s := orders.Column("Status"); s == nil || s.Type != "varchar" || s.Collation == "" {
		t.Errorf("Status = %+v, want varchar with a collation", s)
	}
	if len(orders.ForeignKeys) != 1 || orders.ForeignKeys[0].RefTable != "Customers" ||
		!slices.Equal(orders.ForeignKeys[0].Columns, []string{"CustomerID"}) ||
		!slices.Equal(orders.ForeignKeys[0].RefColumns, []string{"CustomerID"}) {
		t.Errorf("foreign keys = %+v", orders.ForeignKeys)
	}

	// Without a schema, the name resolves as SQL Server would (the login's default schema, then dbo).
	items, err := md.Table(ctx, db, m.Database, "", "orderitems")
	if err != nil || items.Name != "OrderItems" || len(items.ForeignKeys) != 2 {
		t.Fatalf("OrderItems = %+v, %v", items, err)
	}
	view, err := md.Table(ctx, db, m.Database, "dbo", "vw_OrderTotals")
	if err != nil || view.Kind != schema.KindView || len(view.Keys) != 0 {
		t.Fatalf("view = %+v, %v", view, err)
	}
	for _, name := range []string{"Missing", "usp_CustomerOrders"} {
		if _, err := md.Table(ctx, db, m.Database, "dbo", name); !errors.Is(err, schema.ErrObjectNotFound) {
			t.Errorf("%s: err = %v, want ErrObjectNotFound", name, err)
		}
	}
	if _, err := md.Table(ctx, db, "NoSuchDatabase", "dbo", "Orders"); !errors.Is(err, schema.ErrDatabaseNotFound) {
		t.Errorf("missing database: err = %v", err)
	}
}

// TestSampleQueriesWithMetadata analyzes every sample query on the fixture database and checks
// the exact rules each one triggers, metadata rules included.
func TestSampleQueriesWithMetadata(t *testing.T) {
	if os.Getenv("QRYVANTA_PARSER_PATH") == "" {
		t.Skip("QRYVANTA_PARSER_PATH not set; analysis needs the parser sidecar")
	}
	m := loadManifest(t)
	_, p := openFixtureDB(t, m.Database)
	core, err := app.New(app.Options{
		ConfigPath:     filepath.Join(t.TempDir(), "config.json"),
		DisableFileLog: true,
		Credentials:    connection.NewMemoryStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = core.Close() })
	prof, err := core.Connections().Create(p, os.Getenv("QRYVANTA_TEST_MSSQL_PASSWORD"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := core.Pool().Connect(ctx, prof.ID); err != nil {
		t.Fatal(err)
	}
	target := app.AnalysisTarget{ConnectionID: prof.ID, Database: m.Database}

	// The rules each sample triggers, in order of position. Samples not listed trigger none.
	want := map[string][]string{
		"01_employees_without_department.buggy.sql": {"QRY-JOIN-005"},
		"02_one_to_many_fanout.buggy.sql":           {"QRY-JOIN-005", "QRY-JOIN-003"},
		"04_not_in_with_null.buggy.sql":             {"QRY-NULL-002"},
		"05_date_boundary.buggy.sql":                {"QRY-DATE-002"},
		"06_and_or_precedence.buggy.sql":            {"QRY-LOGIC-001"},
		"07_count_vs_count_distinct.buggy.sql":      {"QRY-COUNT-002"},
		"08_wrong_join_key.buggy.sql":               {"QRY-JOIN-006"},
		"10_missing_join_predicate.buggy.sql":       {"QRY-JOIN-001"},
		// Guest orders have no customer: the corrected query drops them, which may be intended.
		"10_missing_join_predicate.correct.sql": {"QRY-JOIN-005"},
	}
	files, err := filepath.Glob(filepath.Join(sampleDir, "*.sql"))
	if err != nil || len(files) < 20 {
		t.Fatalf("sample queries: %v (%d files)", err, len(files))
	}
	for _, path := range files {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			a, err := core.Analyze(ctx, readSample(t, name), target)
			if err != nil {
				t.Fatal(err)
			}
			if !a.Metadata.Checked || len(a.Metadata.Unresolved) > 0 || len(a.Failures) > 0 {
				t.Fatalf("metadata = %+v, failures = %+v", a.Metadata, a.Failures)
			}
			var got []string
			for _, f := range a.Findings {
				got = append(got, f.RuleID)
			}
			if !slices.Equal(got, want[name]) {
				var titles []string
				for _, f := range a.Findings {
					titles = append(titles, f.RuleID+": "+f.Title)
				}
				t.Fatalf("rules = %v, want %v\n%s", got, want[name], strings.Join(titles, "\n"))
			}
			// Sample 05's column is datetime2, so the BETWEEN finding is confirmed by metadata.
			if name == "05_date_boundary.buggy.sql" && a.Findings[0].Classification != "confirmed" {
				t.Errorf("sample 05 = %s, want confirmed", a.Findings[0].Classification)
			}
		})
	}
}
