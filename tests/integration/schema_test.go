//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TuanFaied/qryvanta/internal/dialect/mssql"
	"github.com/TuanFaied/qryvanta/internal/schema"
)

// TestSchemaMetadata reads the fixture database through the SQL Server metadata reader,
// as the schema explorer does.
func TestSchemaMetadata(t *testing.T) {
	m := loadManifest(t)
	// Connect to master: the explorer reads other databases through three-part names.
	db, _ := openFixtureDB(t, "master")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	md := mssql.Metadata{}

	t.Run("databases", func(t *testing.T) {
		dbs, err := md.Databases(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		var found *schema.Database
		for i := range dbs {
			if dbs[i].Name == m.Database {
				found = &dbs[i]
			}
		}
		if found == nil || !found.Accessible || found.System || found.State != "ONLINE" {
			t.Fatalf("QryvantaSample = %+v in %+v", found, dbs)
		}
		if len(dbs) > 0 && dbs[0].System {
			t.Errorf("user databases must be listed before system databases: %+v", dbs)
		}
	})

	t.Run("objects", func(t *testing.T) {
		objs, err := md.Objects(ctx, db, m.Database)
		if err != nil {
			t.Fatal(err)
		}
		kinds := map[schema.ObjectKind][]string{}
		for _, o := range objs {
			kinds[o.Kind] = append(kinds[o.Kind], o.Schema+"."+o.Name)
		}
		if got := len(kinds[schema.KindTable]); got != 7 {
			t.Errorf("tables = %v, want 7", kinds[schema.KindTable])
		}
		for kind, want := range map[schema.ObjectKind]string{
			schema.KindView:      "dbo.vw_OrderTotals",
			schema.KindProcedure: "dbo.usp_CustomerOrders",
			schema.KindFunction:  "dbo.fn_OrderTotal",
		} {
			if len(kinds[kind]) != 1 || kinds[kind][0] != want {
				t.Errorf("%s = %v, want [%s]", kind, kinds[kind], want)
			}
		}
	})

	t.Run("columns", func(t *testing.T) {
		cols, err := md.Columns(ctx, db, m.Database, "dbo", "Orders")
		if err != nil {
			t.Fatal(err)
		}
		want := []struct {
			name, dataType, references string
			nullable, pk               bool
		}{
			{"OrderID", "int", "", false, true},
			{"CustomerID", "int", "dbo.Customers.CustomerID", true, false},
			{"OrderDate", "datetime2(0)", "", false, false},
			{"Status", "varchar(20)", "", false, false},
			{"ShipCountry", "char(2)", "", true, false},
		}
		if len(cols) != len(want) {
			t.Fatalf("columns = %+v", cols)
		}
		for i, w := range want {
			c := cols[i]
			if c.Name != w.name || c.DataType != w.dataType || c.Nullable != w.nullable || c.PrimaryKey != w.pk || c.References != w.references {
				t.Errorf("column %d = %+v, want %+v", i, c, w)
			}
		}

		view, err := md.Columns(ctx, db, m.Database, "dbo", "vw_OrderTotals")
		if err != nil || len(view) != 2 || view[1].Name != "OrderTotal" {
			t.Errorf("view columns = %+v, %v", view, err)
		}
	})

	t.Run("not found and hostile names", func(t *testing.T) {
		if _, err := md.Objects(ctx, db, "NoSuchDatabase"); !errors.Is(err, schema.ErrDatabaseNotFound) {
			t.Errorf("missing database: %v", err)
		}
		if _, err := md.Columns(ctx, db, m.Database, "dbo", "NoSuchTable"); !errors.Is(err, schema.ErrObjectNotFound) {
			t.Errorf("missing table: %v", err)
		}
		// A database name crafted to break out of the three-part name must be treated as a name.
		if _, err := md.Objects(ctx, db, "QryvantaSample].sys.objects; SELECT 1 --"); !errors.Is(err, schema.ErrDatabaseNotFound) {
			t.Errorf("hostile database name: %v", err)
		}
		if _, err := md.Columns(ctx, db, m.Database, "dbo", "Orders]; DROP TABLE dbo.Orders; --"); !errors.Is(err, schema.ErrObjectNotFound) {
			t.Errorf("hostile object name: %v", err)
		}
	})
}
