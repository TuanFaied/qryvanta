//go:build parser

package analyzer

// Metadata rules and upgrades with the real parser and a fake catalog that mirrors the fixture
// database. Run (after building the sidecar):
//
//	QRYVANTA_PARSER_PATH=bin/qryvanta-tsqlparser.exe go test -tags parser ./internal/analyzer/

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/parser"
	"github.com/TuanFaied/qryvanta/internal/parser/sidecar"
	"github.com/TuanFaied/qryvanta/internal/schema"
)

func col(name, typ string, nullable bool) schema.TableColumn {
	return schema.TableColumn{Name: name, Type: typ, DataType: typ, Nullable: nullable}
}

func fk(name, column, refTable, refColumn string) schema.ForeignKey {
	return schema.ForeignKey{Name: name, Columns: []string{column}, RefSchema: "dbo", RefTable: refTable, RefColumns: []string{refColumn}}
}

// fixtureTables mirrors tests/fixtures/mssql/01_schema.sql, plus tables for type and key cases.
var fixtureTables = []schema.Table{
	{Schema: "dbo", Name: "Departments", Kind: schema.KindTable, Keys: [][]string{{"DepartmentID"}},
		Columns: []schema.TableColumn{col("DepartmentID", "int", false), col("Name", "nvarchar", false)}},
	{Schema: "dbo", Name: "Employees", Kind: schema.KindTable, Keys: [][]string{{"EmployeeID"}},
		Columns: []schema.TableColumn{col("EmployeeID", "int", false), col("Name", "nvarchar", false),
			col("DepartmentID", "int", true), col("HireDate", "date", false)}},
	{Schema: "dbo", Name: "Customers", Kind: schema.KindTable, Keys: [][]string{{"CustomerID"}},
		Columns: []schema.TableColumn{col("CustomerID", "int", false), col("Name", "nvarchar", false),
			col("Email", "nvarchar", true), col("Country", "char", false)}},
	{Schema: "dbo", Name: "Products", Kind: schema.KindTable, Keys: [][]string{{"ProductID"}},
		Columns: []schema.TableColumn{col("ProductID", "int", false), col("Name", "nvarchar", false),
			col("ListPrice", "decimal", false), col("LegacyID", "int", true)}},
	{Schema: "dbo", Name: "Orders", Kind: schema.KindTable, Keys: [][]string{{"OrderID"}},
		Columns: []schema.TableColumn{col("OrderID", "int", false), col("CustomerID", "int", true),
			col("OrderDate", "datetime2", false), col("Status", "varchar", false), col("ShipCountry", "char", true)},
		ForeignKeys: []schema.ForeignKey{fk("FK_Orders_Customers", "CustomerID", "Customers", "CustomerID")}},
	{Schema: "dbo", Name: "OrderItems", Kind: schema.KindTable, Keys: [][]string{{"OrderItemID"}},
		Columns: []schema.TableColumn{col("OrderItemID", "int", false), col("OrderID", "int", false),
			col("ProductID", "int", false), col("Quantity", "int", false), col("UnitPrice", "decimal", false)},
		ForeignKeys: []schema.ForeignKey{fk("FK_OrderItems_Orders", "OrderID", "Orders", "OrderID"),
			fk("FK_OrderItems_Products", "ProductID", "Products", "ProductID")}},
	{Schema: "dbo", Name: "Payments", Kind: schema.KindTable, Keys: [][]string{{"PaymentID"}},
		Columns: []schema.TableColumn{col("PaymentID", "int", false), col("OrderID", "int", false),
			col("Amount", "decimal", false), col("PaidAt", "datetime2", false)},
		ForeignKeys: []schema.ForeignKey{fk("FK_Payments_Orders", "OrderID", "Orders", "OrderID")}},
	{Schema: "dbo", Name: "OrderLines", Kind: schema.KindTable, Keys: [][]string{{"OrderID", "LineNumber"}},
		Columns: []schema.TableColumn{col("OrderID", "int", false), col("LineNumber", "int", false)}},
	{Schema: "dbo", Name: "Legacy", Kind: schema.KindTable, Keys: [][]string{{"ID"}},
		Columns: []schema.TableColumn{col("ID", "int", false), col("Code", "varchar", false), col("Big", "bigint", false)}},
	{Schema: "dbo", Name: "Codes", Kind: schema.KindTable, Keys: [][]string{{"Code"}},
		Columns: []schema.TableColumn{col("Code", "nvarchar", false)}},
	{Schema: "dbo", Name: "OrderSummary", Kind: schema.KindView,
		Columns: []schema.TableColumn{col("OrderID", "int", true), col("Total", "decimal", true)}},
}

// fakeCatalog resolves names as SQL Server would for a login whose default schema is dbo.
func fakeCatalog(res *parser.Result) *Catalog {
	c := NewCatalog()
	for _, n := range TableNames(res) {
		l := TableLookup{Problem: "not found, or no permission to see it"}
		if n.Server == "" && (n.Schema == "" || strings.EqualFold(n.Schema, "dbo")) {
			for i := range fixtureTables {
				if strings.EqualFold(fixtureTables[i].Name, n.Name) {
					l = TableLookup{Table: &fixtureTables[i]}
				}
			}
		}
		c.Set(n, l)
	}
	return c
}

type metaCase struct {
	name string
	sql  string
	want []string // span texts of the rule's findings
	fix  []string // corrections; nil skips the check
	// class of every finding; "" skips the check.
	class diagnostics.Classification
	// metadata: every finding has metadata evidence.
	metadata bool
	// noCatalog analyzes without metadata.
	noCatalog bool
}

func runMetaCases(t *testing.T, rule string, cases []metaCase) {
	t.Helper()
	path := os.Getenv("QRYVANTA_PARSER_PATH")
	if path == "" {
		t.Skip("QRYVANTA_PARSER_PATH not set; build the sidecar with `wails3 task common:build:sidecar`")
	}
	p := sidecar.New(sidecar.Options{Path: path, Timeout: 20 * time.Second})
	t.Cleanup(func() { _ = p.Close() })
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := p.Parse(context.Background(), tc.sql, parser.Options{Model: true})
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Diagnostics) > 0 {
				t.Fatalf("the case does not parse: %s", tc.sql)
			}
			in := &Input{SQL: tc.sql, Result: res}
			if !tc.noCatalog {
				in.Catalog = fakeCatalog(res)
			}
			rep, err := NewEngine(Default()).Analyze(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			if len(rep.Failures) > 0 {
				t.Fatalf("rule failures: %+v", rep.Failures)
			}
			var got, fixes []string
			for _, f := range rep.Findings {
				if f.RuleID != rule {
					continue
				}
				got = append(got, parser.SpanText(tc.sql, parser.Span{Offset: f.Span.Offset, Length: f.Span.Length}))
				fix := ""
				if f.Correction != nil {
					fix = f.Correction.Replacement
				}
				fixes = append(fixes, fix)
				if tc.class != "" && f.Classification != tc.class {
					t.Errorf("classification = %s, want %s", f.Classification, tc.class)
				}
				if tc.metadata {
					has := false
					for _, e := range f.Evidence {
						has = has || e.Kind == diagnostics.EvidenceMetadata
					}
					if !has {
						t.Errorf("finding %q has no metadata evidence: %+v", f.Title, f.Evidence)
					}
				}
			}
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("findings cover %q, want %q", got, tc.want)
			}
			if tc.fix != nil && strings.Join(fixes, "|") != strings.Join(tc.fix, "|") {
				t.Fatalf("corrections = %q, want %q", fixes, tc.fix)
			}
		})
	}
}

func sample(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("../../tests/sample_queries/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestJoinFanOut(t *testing.T) {
	runMetaCases(t, "QRY-JOIN-003", []metaCase{
		{name: "many to many", sql: "SELECT * FROM dbo.Orders AS o JOIN dbo.Payments AS p ON p.OrderID = o.CustomerID",
			want: []string{"p.OrderID = o.CustomerID"}, class: diagnostics.Potential, metadata: true},
		{name: "two one-to-many joins (sample 02)", sql: sample(t, "02_one_to_many_fanout.buggy.sql"),
			want: []string{"p.OrderID = o.OrderID"}, metadata: true},
		{name: "sample 02 corrected", sql: sample(t, "02_one_to_many_fanout.correct.sql")},
		{name: "one to many", sql: "SELECT * FROM Orders AS o JOIN OrderItems AS oi ON oi.OrderID = o.OrderID"},
		{name: "chain is not a chasm", sql: "SELECT * FROM Customers c JOIN Orders o ON o.CustomerID = c.CustomerID JOIN OrderItems oi ON oi.OrderID = o.OrderID"},
		{name: "composite key not covered", sql: "SELECT * FROM Payments AS p JOIN OrderLines AS l ON l.OrderID = p.OrderID",
			want: []string{"l.OrderID = p.OrderID"}},
		{name: "composite key covered by a constant", sql: "SELECT * FROM Payments AS p JOIN OrderLines AS l ON l.OrderID = p.OrderID AND l.LineNumber = 1"},
		{name: "view has no keys to judge", sql: "SELECT * FROM Payments AS p JOIN OrderSummary AS s ON s.OrderID = p.OrderID"},
		{name: "unresolved table", sql: "SELECT * FROM Payments AS p JOIN dbo.Missing AS m ON m.OrderID = p.OrderID"},
		{name: "cross join is not judged", sql: "SELECT * FROM Payments AS p CROSS JOIN Orders AS o"},
		{name: "no catalog", sql: "SELECT * FROM dbo.Orders AS o JOIN dbo.Payments AS p ON p.OrderID = o.CustomerID", noCatalog: true},
		{name: "in a subquery", sql: "SELECT * FROM Customers c WHERE EXISTS (SELECT 1 FROM Orders o JOIN Payments p ON p.OrderID = o.CustomerID WHERE o.CustomerID = c.CustomerID)",
			want: []string{"p.OrderID = o.CustomerID"}},
	})
}

func TestJoinTypeMismatch(t *testing.T) {
	runMetaCases(t, "QRY-JOIN-004", []metaCase{
		{name: "varchar and nvarchar", sql: "SELECT * FROM Legacy AS g JOIN Codes AS c ON c.Code = g.Code",
			want: []string{"c.Code = g.Code"}, class: diagnostics.Potential, metadata: true},
		{name: "int and varchar", sql: "SELECT * FROM Legacy AS g JOIN Orders AS o ON o.OrderID = g.Code", want: []string{"o.OrderID = g.Code"}},
		{name: "date and datetime2", sql: "SELECT * FROM Employees AS e JOIN Orders AS o ON o.OrderDate = e.HireDate", want: []string{"o.OrderDate = e.HireDate"}},
		{name: "int and bigint", sql: "SELECT * FROM Legacy AS g JOIN Orders AS o ON o.OrderID = g.Big"},
		{name: "same type", sql: "SELECT * FROM Orders AS o JOIN OrderItems AS oi ON oi.OrderID = o.OrderID"},
		{name: "unqualified but unique", sql: "SELECT * FROM Legacy JOIN Codes ON Codes.Code = Big", want: []string{"Codes.Code = Big"}},
	})
}

func TestNullableJoinKey(t *testing.T) {
	runMetaCases(t, "QRY-JOIN-005", []metaCase{
		{name: "sample 01", sql: sample(t, "01_employees_without_department.buggy.sql"),
			want: []string{"d.DepartmentID = e.DepartmentID"}, class: diagnostics.Potential, metadata: true},
		{name: "sample 01 corrected (LEFT JOIN)", sql: sample(t, "01_employees_without_department.correct.sql")},
		{name: "where requires a value", sql: "SELECT * FROM Employees e JOIN Departments d ON d.DepartmentID = e.DepartmentID WHERE e.DepartmentID IS NOT NULL"},
		{name: "not nullable", sql: "SELECT * FROM Orders o JOIN OrderItems oi ON oi.OrderID = o.OrderID"},
		{name: "right join keeps the right side", sql: "SELECT * FROM Departments d RIGHT JOIN Employees e ON d.DepartmentID = e.DepartmentID"},
		{name: "no catalog", sql: sample(t, "01_employees_without_department.buggy.sql"), noCatalog: true},
	})
}

func TestJoinIgnoresForeignKey(t *testing.T) {
	runMetaCases(t, "QRY-JOIN-006", []metaCase{
		{name: "sample 08", sql: sample(t, "08_wrong_join_key.buggy.sql"),
			want: []string{"p.ProductID = oi.OrderItemID"}, fix: []string{"p.ProductID = oi.ProductID"},
			class: diagnostics.Potential, metadata: true},
		{name: "sample 08 corrected", sql: sample(t, "08_wrong_join_key.correct.sql")},
		{name: "foreign key column against another parent column", sql: "SELECT * FROM OrderItems AS oi JOIN Products AS p ON oi.ProductID = p.LegacyID",
			want: []string{"oi.ProductID = p.LegacyID"}, fix: []string{"oi.ProductID = p.ProductID"}},
		{name: "unqualified wrong column has no rewrite", sql: "SELECT * FROM OrderItems JOIN Products ON Products.ProductID = OrderItemID",
			want: []string{"Products.ProductID = OrderItemID"}, fix: []string{""}},
		{name: "no foreign key between the tables", sql: "SELECT * FROM Employees e JOIN Departments d ON d.DepartmentID = e.EmployeeID"},
		{name: "follows another foreign key", sql: "SELECT * FROM OrderItems oi JOIN Orders o ON o.OrderID = oi.OrderID"},
	})
}

func TestMetadataUpgradesNotIn(t *testing.T) {
	runMetaCases(t, "QRY-NULL-002", []metaCase{
		{name: "nullable column stays potential (sample 04)", sql: sample(t, "04_not_in_with_null.buggy.sql"),
			want: []string{"c.CustomerID NOT IN (SELECT o.CustomerID FROM dbo.Orders AS o)"}, class: diagnostics.Potential, metadata: true},
		{name: "non-nullable column is dismissed", sql: "SELECT * FROM Customers c WHERE c.CustomerID NOT IN (SELECT o.OrderID FROM Orders o)"},
		{name: "unresolved stays potential", sql: "SELECT * FROM Customers c WHERE c.CustomerID NOT IN (SELECT m.ID FROM Missing m)",
			want: []string{"c.CustomerID NOT IN (SELECT m.ID FROM Missing m)"}},
	})
}

func TestMetadataUpgradesBetween(t *testing.T) {
	runMetaCases(t, "QRY-DATE-002", []metaCase{
		{name: "datetime2 is confirmed (sample 05)", sql: sample(t, "05_date_boundary.buggy.sql"),
			want: []string{"OrderDate BETWEEN '2026-01-01' AND '2026-01-31'"}, class: diagnostics.Confirmed, metadata: true},
		{name: "date column is dismissed", sql: "SELECT * FROM Employees WHERE HireDate BETWEEN '2026-01-01' AND '2026-01-31'"},
		{name: "without a catalog it stays potential", sql: sample(t, "05_date_boundary.buggy.sql"),
			want: []string{"OrderDate BETWEEN '2026-01-01' AND '2026-01-31'"}, class: diagnostics.Potential, noCatalog: true},
	})
}

func TestMetadataUpgradesCount(t *testing.T) {
	runMetaCases(t, "QRY-COUNT-001", []metaCase{
		{name: "nullable column", sql: "SELECT COUNT(Email) FROM Customers", want: []string{"COUNT(Email)"}, metadata: true},
		{name: "non-nullable column is dismissed", sql: "SELECT COUNT(Name) FROM Customers"},
		{name: "non-nullable on the optional side of a LEFT JOIN", sql: "SELECT COUNT(d.Name) FROM Employees e LEFT JOIN Departments d ON d.DepartmentID = e.DepartmentID",
			want: []string{"COUNT(d.Name)"}},
	})
}
