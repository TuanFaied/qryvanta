package analyzer

import (
	"fmt"
	"slices"
	"strings"

	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/parser"
	"github.com/TuanFaied/qryvanta/internal/schema"
)

// Rules that need the catalog: they report nothing without a connection, and skip any join
// whose tables could not be resolved (the analysis lists those).

// joinPair is one `left = right` condition of a join's ON, with each column resolved and on its
// side of the join.
type joinPair struct {
	expr        *parser.Expr
	left, right columnRef
}

// joinPairs returns the column = column conditions of j's ON that compare a column of each side.
func (in *Input) joinPairs(from, j *parser.Source) []joinPair {
	if in.Catalog == nil || j.On == nil {
		return nil
	}
	leftLeaves, rightLeaves := leaves(j.Left), leaves(j.Right)
	var out []joinPair
	for _, c := range conjuncts(j.On) {
		if c.Kind != parser.ExprCompare || c.Op != "=" || c.Quantifier != "" {
			continue
		}
		a, okA := in.resolveColumn(from, c.Left)
		b, okB := in.resolveColumn(from, c.Right)
		if !okA || !okB {
			continue
		}
		switch {
		case containsSource(leftLeaves, a.source) && containsSource(rightLeaves, b.source):
			out = append(out, joinPair{c, a, b})
		case containsSource(leftLeaves, b.source) && containsSource(rightLeaves, a.source):
			out = append(out, joinPair{c, b, a})
		}
	}
	return out
}

// boundColumns returns, per source, the lower-case columns j's ON fixes: those compared with a
// column of the other side, and those compared with a value (b.Type = 1).
func (in *Input) boundColumns(from, j *parser.Source, pairs []joinPair) map[*parser.Source]map[string]bool {
	out := map[*parser.Source]map[string]bool{}
	add := func(r columnRef) {
		if out[r.source] == nil {
			out[r.source] = map[string]bool{}
		}
		out[r.source][strings.ToLower(r.column.Name)] = true
	}
	for _, p := range pairs {
		add(p.left)
		add(p.right)
	}
	for _, c := range conjuncts(j.On) {
		if c.Kind != parser.ExprCompare || c.Op != "=" {
			continue
		}
		for _, side := range [][2]*parser.Expr{{c.Left, c.Right}, {c.Right, c.Left}} {
			if columnFree(side[1]) {
				if r, ok := in.resolveColumn(from, side[0]); ok {
					add(r)
				}
			}
		}
	}
	return out
}

func containsSource(list []*parser.Source, s *parser.Source) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// uniqueOn reports whether some key of t is within cols (lower-case names).
func uniqueOn(t *schema.Table, cols map[string]bool) bool {
	for _, key := range t.Keys {
		all := len(key) > 0
		for _, k := range key {
			if !cols[strings.ToLower(k)] {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

func describeKeys(t *schema.Table) string {
	if len(t.Keys) == 0 {
		return "none"
	}
	parts := make([]string, len(t.Keys))
	for i, k := range t.Keys {
		parts[i] = "(" + strings.Join(k, ", ") + ")"
	}
	return strings.Join(parts, ", ")
}

func tableEvidence(t *schema.Table, summary string) diagnostics.Evidence {
	return diagnostics.Evidence{
		Kind:     diagnostics.EvidenceMetadata,
		Summary:  summary,
		Metadata: &diagnostics.MetadataEvidence{Object: t.FullName(), Property: "unique keys", Value: describeKeys(t)},
	}
}

// joinSide is the one source a side of a join's conditions refers to.
func joinSide(pairs []joinPair, left bool) *columnRef {
	var src *parser.Source
	var ref *columnRef
	for i := range pairs {
		r := &pairs[i].right
		if left {
			r = &pairs[i].left
		}
		if src != nil && r.source != src {
			return nil // conditions on several sources of one side: not analyzed
		}
		src, ref = r.source, r
	}
	return ref
}

func columnList(pairs []joinPair, left bool) string {
	var names []string
	for _, p := range pairs {
		r := p.right
		if left {
			r = p.left
		}
		names = append(names, r.column.Name)
	}
	return strings.Join(names, ", ")
}

// joinFanOut is QRY-JOIN-003: a join that can multiply rows, judged by the keys of the joined
// tables. Two shapes: neither side of the join's equality conditions covers a key (many-to-many),
// or one table is joined one-to-many to two different tables, so their rows multiply each other.
type joinFanOut struct{}

func (joinFanOut) Info() Info {
	return Info{
		ID:       "QRY-JOIN-003",
		Title:    "Join on non-key columns (fan-out risk)",
		Severity: diagnostics.SeverityMedium,
		Evidence: []diagnostics.EvidenceKind{diagnostics.EvidenceMetadata, diagnostics.EvidenceProbe},
	}
}

type oneToMany struct {
	join          *parser.Source
	parent, child *columnRef
}

func (joinFanOut) Check(in *Input) ([]diagnostics.Finding, error) {
	var out []diagnostics.Finding
	for _, fc := range in.fromClauses() {
		children := map[*parser.Source][]oneToMany{}
		var parents []*parser.Source
		for _, j := range joins(fc.from) {
			switch j.JoinType {
			case parser.JoinInner, parser.JoinLeft, parser.JoinRight, parser.JoinFull:
			default:
				continue
			}
			pairs := in.joinPairs(fc.from, j)
			l, r := joinSide(pairs, true), joinSide(pairs, false)
			if len(pairs) == 0 || l == nil || r == nil || l.table.Kind != schema.KindTable || r.table.Kind != schema.KindTable {
				continue
			}
			bound := in.boundColumns(fc.from, j, pairs)
			lu, ru := uniqueOn(l.table, bound[l.source]), uniqueOn(r.table, bound[r.source])
			switch {
			case !lu && !ru:
				span := SpanOf(j.On.Span)
				out = append(out, diagnostics.Finding{
					Title: fmt.Sprintf("Neither %s (%s) nor %s (%s) is unique, so rows can multiply",
						l.table.FullName(), columnList(pairs, true), r.table.FullName(), columnList(pairs, false)),
					Explanation: fmt.Sprintf("The join compares %s with %s, but neither column set is a primary key or unique on "+
						"its table. A row on either side can match several rows on the other, so rows, and SUM or COUNT "+
						"totals, can multiply. Join on a key, or aggregate one side first. The fan-out probe can measure it.",
						columnList(pairs, true), columnList(pairs, false)),
					Classification: diagnostics.Potential,
					Span:           span,
					Evidence: []diagnostics.Evidence{
						tableEvidence(l.table, fmt.Sprintf("No key of %s is within (%s).", l.table.FullName(), columnList(pairs, true))),
						tableEvidence(r.table, fmt.Sprintf("No key of %s is within (%s).", r.table.FullName(), columnList(pairs, false))),
					},
					Measurements: []diagnostics.Measurement{
						fanOut(l.table, pairColumns(pairs, true)), fanOut(r.table, pairColumns(pairs, false)),
					},
				})
			case lu && !ru:
				if children[l.source] == nil {
					parents = append(parents, l.source)
				}
				children[l.source] = append(children[l.source], oneToMany{j, l, r})
			case ru && !lu:
				if children[r.source] == nil {
					parents = append(parents, r.source)
				}
				children[r.source] = append(children[r.source], oneToMany{j, r, l})
			}
		}
		for _, p := range parents {
			// In text order: the finding goes on the second one-to-many join as written.
			edges := children[p]
			slices.SortFunc(edges, func(x, y oneToMany) int { return x.join.On.Span.Offset - y.join.On.Span.Offset })
			if len(edges) < 2 || edges[0].child.source == edges[1].child.source {
				continue
			}
			a, b := edges[0], edges[1]
			parent := a.parent.table.FullName()
			span := SpanOf(b.join.On.Span)
			out = append(out, diagnostics.Finding{
				Title: fmt.Sprintf("%s is joined one-to-many to both %s and %s, so their rows multiply",
					parent, a.child.table.FullName(), b.child.table.FullName()),
				Explanation: fmt.Sprintf("Each %s row is repeated once per matching %s row and again per matching %s row, so "+
					"the result has (%s rows) × (%s rows) per %s row, and SUM or COUNT over either table is inflated. "+
					"Aggregate each one-to-many relationship separately (in a CTE or derived table), then join the totals.",
					parent, a.child.table.FullName(), b.child.table.FullName(), a.child.table.Name, b.child.table.Name, parent),
				Classification: diagnostics.Potential,
				Span:           span,
				Evidence: []diagnostics.Evidence{
					tableEvidence(a.parent.table, fmt.Sprintf("The join conditions cover a key of %s.", parent)),
					tableEvidence(a.child.table, fmt.Sprintf("No key of %s is within (%s), so several rows can match.", a.child.table.FullName(), a.child.column.Name)),
					tableEvidence(b.child.table, fmt.Sprintf("No key of %s is within (%s), so several rows can match.", b.child.table.FullName(), b.child.column.Name)),
				},
				Measurements: []diagnostics.Measurement{
					fanOut(a.child.table, []string{a.child.column.Name}), fanOut(b.child.table, []string{b.child.column.Name}),
				},
			})
		}
	}
	return out, nil
}

// typeFamily groups SQL Server types whose comparison needs no conversion that changes matches.
func typeFamily(t string) string {
	switch t {
	case "char", "varchar", "text":
		return "ansi string"
	case "nchar", "nvarchar", "ntext", "sysname":
		return "unicode string"
	case "tinyint", "smallint", "int", "bigint", "decimal", "numeric", "money", "smallmoney", "float", "real", "bit":
		return "number"
	case "date":
		return "date"
	case "datetime", "datetime2", "smalldatetime", "datetimeoffset":
		return "datetime"
	case "binary", "varbinary", "image":
		return "binary"
	}
	return t
}

func isString(family string) bool { return family == "ansi string" || family == "unicode string" }

// joinTypeMismatch is QRY-JOIN-004: a join that compares columns of different type families.
// SQL Server converts one side for every row (by data type precedence): matches can differ from
// how the values look, the conversion can fail, and an index on the converted column cannot be
// sought. Different collations are an error SQL Server reports itself, so they are not checked.
type joinTypeMismatch struct{}

func (joinTypeMismatch) Info() Info {
	return Info{
		ID:       "QRY-JOIN-004",
		Title:    "Join columns of different types",
		Severity: diagnostics.SeverityMedium,
		Evidence: []diagnostics.EvidenceKind{diagnostics.EvidenceMetadata},
	}
}

func (joinTypeMismatch) Check(in *Input) ([]diagnostics.Finding, error) {
	var out []diagnostics.Finding
	for _, fc := range in.fromClauses() {
		for _, j := range joins(fc.from) {
			for _, p := range in.joinPairs(fc.from, j) {
				fa, fb := typeFamily(p.left.column.Type), typeFamily(p.right.column.Type)
				if fa == fb {
					continue
				}
				why := "SQL Server converts one column to the other's type for every row, so the comparison can match " +
					"differently than the values look (for example '01' = 1), fail for values that do not convert, " +
					"and cannot use an index on the converted column for a seek."
				switch {
				case isString(fa) && isString(fb):
					why = "SQL Server converts the varchar side to nvarchar for every row, which can prevent an index seek " +
						"on it and compares by the Unicode collation's rules."
				case (fa == "date" && fb == "datetime") || (fa == "datetime" && fb == "date"):
					why = "SQL Server converts the date to a datetime at midnight, so only rows whose time is exactly " +
						"midnight match."
				}
				span := SpanOf(p.expr.Span)
				out = append(out, diagnostics.Finding{
					Title: fmt.Sprintf("%s (%s) is compared with %s (%s)", in.Text(p.left.expr.Span), p.left.column.DataType,
						in.Text(p.right.expr.Span), p.right.column.DataType),
					Explanation:    why + " Join columns of the same type, or convert the value side explicitly.",
					Classification: diagnostics.Potential,
					Span:           span,
					Evidence: []diagnostics.Evidence{
						columnEvidence(p.left, "type", p.left.column.DataType, fmt.Sprintf("%s.%s is %s.", p.left.table.FullName(), p.left.column.Name, p.left.column.DataType)),
						columnEvidence(p.right, "type", p.right.column.DataType, fmt.Sprintf("%s.%s is %s.", p.right.table.FullName(), p.right.column.Name, p.right.column.DataType)),
					},
				})
			}
		}
	}
	return out, nil
}

// nullableJoinKey is QRY-JOIN-005: an INNER JOIN on a nullable column. NULL equals nothing, so
// rows with NULL in the column have no match and are dropped without a sign.
type nullableJoinKey struct{}

func (nullableJoinKey) Info() Info {
	return Info{
		ID:       "QRY-JOIN-005",
		Title:    "INNER JOIN on a nullable column",
		Severity: diagnostics.SeverityMedium,
		Evidence: []diagnostics.EvidenceKind{diagnostics.EvidenceMetadata, diagnostics.EvidenceProbe},
	}
}

func (nullableJoinKey) Check(in *Input) ([]diagnostics.Finding, error) {
	var out []diagnostics.Finding
	for _, fc := range in.fromClauses() {
		for _, j := range joins(fc.from) {
			if j.JoinType != parser.JoinInner {
				continue
			}
			for _, p := range in.joinPairs(fc.from, j) {
				for i, r := range []columnRef{p.left, p.right} {
					other := p.right
					if i == 1 {
						other = p.left
					}
					if !r.column.Nullable || outerJoined(fc.from, r.source) || whereRequiresValue(fc.where, columnKey(r.expr)) {
						continue
					}
					name := r.table.FullName()
					out = append(out, diagnostics.Finding{
						Title: fmt.Sprintf("INNER JOIN drops %s rows where %s is NULL", name, r.column.Name),
						Explanation: fmt.Sprintf("%s.%s allows NULL. NULL equals nothing, so %s rows with NULL in %s have no "+
							"match and the INNER JOIN removes them without a sign. If they belong in the result, use a "+
							"LEFT JOIN from %s.", name, r.column.Name, name, r.column.Name, name),
						Classification: diagnostics.Potential,
						Span:           SpanOf(p.expr.Span),
						Evidence: []diagnostics.Evidence{columnEvidence(r, "nullable", "yes",
							fmt.Sprintf("%s.%s allows NULL.", name, r.column.Name))},
						Measurements: []diagnostics.Measurement{
							unmatched(r.table, []string{r.column.Name}, other.table, []string{other.column.Name}),
						},
					})
				}
			}
		}
	}
	return out, nil
}

// joinIgnoresForeignKey is QRY-JOIN-006: a join between two tables linked by a single-column
// foreign key that uses the key on one side but another column on the other (oi.OrderItemID =
// p.ProductID while OrderItems.ProductID references Products.ProductID).
type joinIgnoresForeignKey struct{}

func (joinIgnoresForeignKey) Info() Info {
	return Info{
		ID:       "QRY-JOIN-006",
		Title:    "Join does not follow the foreign key",
		Severity: diagnostics.SeverityHigh,
		Evidence: []diagnostics.EvidenceKind{diagnostics.EvidenceMetadata, diagnostics.EvidenceProbe},
	}
}

func (joinIgnoresForeignKey) Check(in *Input) ([]diagnostics.Finding, error) {
	var out []diagnostics.Finding
	for _, fc := range in.fromClauses() {
		for _, j := range joins(fc.from) {
			for _, p := range in.joinPairs(fc.from, j) {
				for _, dir := range [][2]columnRef{{p.left, p.right}, {p.right, p.left}} {
					if f, ok := in.foreignKeyMismatch(p.expr, dir[0], dir[1]); ok {
						out = append(out, f)
						break
					}
				}
			}
		}
	}
	return out, nil
}

// foreignKeyMismatch checks child = parent where child's table has foreign keys to parent's.
func (in *Input) foreignKeyMismatch(pair *parser.Expr, child, parent columnRef) (diagnostics.Finding, bool) {
	if child.table == parent.table || strings.EqualFold(child.table.FullName(), parent.table.FullName()) {
		return diagnostics.Finding{}, false // self-joins are ambiguous
	}
	var fks []schema.ForeignKey
	for _, fk := range child.table.ForeignKeys {
		if len(fk.Columns) == 1 && strings.EqualFold(fk.RefSchema+"."+fk.RefTable, parent.table.FullName()) {
			if strings.EqualFold(fk.Columns[0], child.column.Name) && strings.EqualFold(fk.RefColumns[0], parent.column.Name) {
				return diagnostics.Finding{}, false // the join follows a foreign key
			}
			fks = append(fks, fk)
		}
	}
	for _, fk := range fks {
		var wrong columnRef
		var want string
		switch {
		case strings.EqualFold(fk.RefColumns[0], parent.column.Name):
			wrong, want = child, fk.Columns[0] // the referenced key against another child column
		case strings.EqualFold(fk.Columns[0], child.column.Name):
			wrong, want = parent, fk.RefColumns[0] // the foreign key column against another parent column
		default:
			continue
		}
		link := fmt.Sprintf("%s.%s references %s.%s", child.table.FullName(), fk.Columns[0], parent.table.FullName(), fk.RefColumns[0])
		f := diagnostics.Finding{
			Title: fmt.Sprintf("The join uses %s, but %s", in.Text(wrong.expr.Span), link),
			Explanation: fmt.Sprintf("The foreign key %s links the two tables through %s.%s and %s.%s. This join compares "+
				"%s instead, which matches unrelated rows when the values happen to be equal and drops the rest, "+
				"without an error.", fk.Name, child.table.Name, fk.Columns[0], parent.table.Name, fk.RefColumns[0],
				in.Text(wrong.expr.Span)),
			Classification: diagnostics.Potential,
			Span:           SpanOf(pair.Span),
			Evidence: []diagnostics.Evidence{{
				Kind:    diagnostics.EvidenceMetadata,
				Summary: fmt.Sprintf("Foreign key %s: %s.", fk.Name, link),
				Metadata: &diagnostics.MetadataEvidence{Object: child.table.FullName(), Column: fk.Columns[0],
					Property: "foreign key " + fk.Name, Value: parent.table.FullName() + "." + fk.RefColumns[0]},
			}},
			// The rows of the foreign key's table that the join as written drops.
			Measurements: []diagnostics.Measurement{
				unmatched(child.table, []string{child.column.Name}, parent.table, []string{parent.column.Name}),
			},
		}
		if replacement, ok := in.replaceColumn(pair, wrong.expr, want); ok {
			f.Correction = &diagnostics.Correction{
				Description: "Join on the foreign key.",
				Span:        SpanOf(pair.Span),
				Replacement: replacement,
			}
		}
		return f, true
	}
	return diagnostics.Finding{}, false
}

// replaceColumn returns the text of expr with the column reference col renamed to name, keeping
// its qualifier as written.
func (in *Input) replaceColumn(expr, col *parser.Expr, name string) (string, bool) {
	start, end := col.Span.Offset, col.Span.Offset+col.Span.Length
	if start < expr.Span.Offset || end > expr.Span.Offset+expr.Span.Length || len(col.Parts) < 2 {
		return "", false // an unqualified name could become ambiguous between the two tables
	}
	text := in.Text(col.Span)
	i := strings.LastIndex(text, ".")
	if i < 0 {
		return "", false
	}
	renamed := text[:i+1] + name
	before := in.Text(parser.Span{Offset: expr.Span.Offset, Length: start - expr.Span.Offset})
	after := in.Text(parser.Span{Offset: end, Length: expr.Span.Offset + expr.Span.Length - end})
	return before + renamed + after, true
}
