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
// analysisCore is a core connected to the fixture database, as the desktop app would be.
func analysisCore(t *testing.T) (*app.Core, app.AnalysisTarget, context.Context) {
	t.Helper()
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
	t.Cleanup(cancel)
	if _, err := core.Pool().Connect(ctx, prof.ID); err != nil {
		t.Fatal(err)
	}
	return core, app.AnalysisTarget{ConnectionID: prof.ID, Database: m.Database}, ctx
}

func TestSampleQueriesWithMetadata(t *testing.T) {
	core, target, ctx := analysisCore(t)

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

// TestProbesMeasureSampleScenarios analyzes buggy samples, runs every planned probe (as the user
// approving them would) and checks the measured evidence against the fixture data.
func TestProbesMeasureSampleScenarios(t *testing.T) {
	core, target, ctx := analysisCore(t)
	cases := []struct {
		file, rule string
		// summary is a fragment of the probe result that confirms the finding.
		summary string
	}{
		// Each order has several items and several payments: the two joins multiply.
		{"02_one_to_many_fanout.buggy.sql", "QRY-JOIN-003", "rows per value"},
		// Two employees have no department and one an unknown one: the INNER JOIN drops 3 of 12.
		{"01_employees_without_department.buggy.sql", "QRY-JOIN-005", "3 of 12 dbo.Employees rows have no match in dbo.Departments (2 of them with NULL in DepartmentID)"},
		// A guest order has a NULL CustomerID.
		{"04_not_in_with_null.buggy.sql", "QRY-NULL-002", "dbo.Orders.CustomerID has 1 NULL,"},
		// One order was placed on 31 January at 16:45.
		{"05_date_boundary.buggy.sql", "QRY-DATE-002", "1 row of dbo.Orders falls after 2026-01-31"},
		// The join on OrderItemID drops a line (#08).
		{"08_wrong_join_key.buggy.sql", "QRY-JOIN-006", "have no match in dbo.Products"},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			a, err := core.Analyze(ctx, readSample(t, c.file), target)
			if err != nil {
				t.Fatal(err)
			}
			if len(a.Probes) == 0 {
				t.Fatalf("no probes were planned: %+v", a.Findings)
			}
			var ids []string
			for _, p := range a.Probes {
				if !p.OverBudget {
					ids = append(ids, p.ID)
				}
			}
			run, err := core.RunProbes(ctx, a.ID, ids)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range run.Results {
				if r.Error != "" {
					t.Fatalf("probe %s failed: %s", r.ProbeID, r.Error)
				}
			}
			for _, f := range run.Findings {
				if f.RuleID != c.rule {
					continue
				}
				if f.Classification != "confirmed" {
					t.Fatalf("%s = %s, want confirmed by the probe", f.RuleID, f.Classification)
				}
				for _, e := range f.Evidence {
					if e.Kind == "probe" && strings.Contains(e.Summary, c.summary) {
						return
					}
				}
				t.Fatalf("no probe evidence containing %q in %+v", c.summary, f.Evidence)
			}
			t.Fatalf("no %s finding after the probes: %+v (dismissed %+v)", c.rule, run.Findings, run.Dismissed)
		})
	}
}
