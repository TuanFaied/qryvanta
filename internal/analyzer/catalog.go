package analyzer

import (
	"strings"

	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/parser"
	"github.com/TuanFaied/qryvanta/internal/schema"
)

// Catalog holds the metadata of the tables and views a script reads, resolved before the
// analysis (rules never query the server). A nil Catalog means no connection: metadata rules
// report nothing and static rules are not upgraded.
type Catalog struct {
	tables map[string]TableLookup
}

// TableLookup is the metadata of one name as written in the script, or why it is missing.
type TableLookup struct {
	Table *schema.Table
	// Problem says why the table could not be checked (not found, no permission, linked server).
	Problem string
}

// NewCatalog creates an empty catalog.
func NewCatalog() *Catalog { return &Catalog{tables: map[string]TableLookup{}} }

// Set records the lookup of a name.
func (c *Catalog) Set(name parser.ObjectName, l TableLookup) { c.tables[CatalogKey(name)] = l }

// CatalogKey identifies a name as written: server.database.schema.name, lower-case.
func CatalogKey(n parser.ObjectName) string {
	return strings.ToLower(n.Server + "." + n.Database + "." + n.Schema + "." + n.Name)
}

// TableNames lists the distinct table and view names a parsed script reads or writes, for
// resolving before analysis. CTEs, temporary tables (#t) and table variables are left out:
// they have no catalog entry to read.
func TableNames(res *parser.Result) []parser.ObjectName {
	in := &Input{Result: res}
	seen := map[string]bool{}
	var out []parser.ObjectName
	in.Walk(Visitor{Source: func(s *parser.Source) {
		if s.Kind != parser.SourceTable || s.Cte || s.Name == nil || strings.HasPrefix(s.Name.Name, "#") {
			return
		}
		if k := CatalogKey(*s.Name); !seen[k] {
			seen[k] = true
			out = append(out, *s.Name)
		}
	}})
	return out
}

// tableOf returns the metadata of a FROM source, or nil when it is not a resolved table or view.
func (in *Input) tableOf(s *parser.Source) *schema.Table {
	if in.Catalog == nil || s == nil || s.Kind != parser.SourceTable || s.Cte || s.Name == nil {
		return nil
	}
	return in.Catalog.tables[CatalogKey(*s.Name)].Table
}

// leaves returns the sources of a FROM tree that are not joins.
func leaves(s *parser.Source) []*parser.Source {
	if s == nil {
		return nil
	}
	if s.Kind == parser.SourceJoin {
		return append(leaves(s.Left), leaves(s.Right)...)
	}
	return []*parser.Source{s}
}

// columnRef is a column reference resolved to its table.
type columnRef struct {
	expr   *parser.Expr
	source *parser.Source
	table  *schema.Table
	column *schema.TableColumn
}

// resolveColumn finds the table column a reference in a FROM clause names. A qualified
// reference names its source by alias or name; an unqualified one is resolved only when exactly
// one source can have it (the only source, or the only resolved table that has the column when
// every source is a resolved table).
func (in *Input) resolveColumn(from *parser.Source, col *parser.Expr) (columnRef, bool) {
	name := columnName(col)
	if name == "" || name == "*" {
		return columnRef{}, false
	}
	srcs := leaves(from)
	pick := func(s *parser.Source) (columnRef, bool) {
		t := in.tableOf(s)
		if t == nil {
			return columnRef{}, false
		}
		c := t.Column(name)
		if c == nil {
			return columnRef{}, false
		}
		return columnRef{expr: col, source: s, table: t, column: c}, true
	}
	if q, ok := qualifier(col); ok {
		for _, s := range srcs {
			if names := sourceNames(s); len(names) == 1 && names[0] == q {
				return pick(s)
			}
		}
		return columnRef{}, false
	}
	if len(col.Parts) != 1 {
		return columnRef{}, false
	}
	if len(srcs) == 1 {
		return pick(srcs[0])
	}
	var found columnRef
	n := 0
	for _, s := range srcs {
		if in.tableOf(s) == nil {
			return columnRef{}, false // another source could have the column
		}
		if r, ok := pick(s); ok {
			found = r
			n++
		}
	}
	return found, n == 1
}

// columnEvidence is metadata evidence about a column.
func columnEvidence(r columnRef, property, value, summary string) diagnostics.Evidence {
	return diagnostics.Evidence{
		Kind:    diagnostics.EvidenceMetadata,
		Summary: summary,
		Metadata: &diagnostics.MetadataEvidence{
			Object: r.table.FullName(), Column: r.column.Name, Property: property, Value: value,
		},
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// ref names a resolved table for a measurement.
func ref(t *schema.Table) diagnostics.TableRef {
	return diagnostics.TableRef{Database: t.Database, Schema: t.Schema, Name: t.Name}
}

// pairColumns lists the columns one side of join pairs uses.
func pairColumns(pairs []joinPair, left bool) []string {
	var out []string
	for _, p := range pairs {
		if left {
			out = append(out, p.left.column.Name)
		} else {
			out = append(out, p.right.column.Name)
		}
	}
	return out
}

func fanOut(t *schema.Table, columns []string) diagnostics.Measurement {
	return diagnostics.Measurement{Kind: diagnostics.MeasureFanOut, Table: ref(t), Columns: columns}
}

func unmatched(t *schema.Table, columns []string, other *schema.Table, otherColumns []string) diagnostics.Measurement {
	o := ref(other)
	return diagnostics.Measurement{Kind: diagnostics.MeasureUnmatched, Table: ref(t), Columns: columns, Other: &o, OtherColumns: otherColumns}
}

// dateTimeTypes have a time of day.
var dateTimeTypes = map[string]bool{"datetime": true, "datetime2": true, "smalldatetime": true, "datetimeoffset": true}
