// Package instrumentation generates probe queries: read-only queries that collect evidence for
// findings (architecture decision D4). Probes are built only from the fixed templates in this
// package, from measurements that rules describe with resolved catalog names, never from the
// user's SQL text. Identifiers are quoted; values copied from the query are parameters.
//
// Nothing here runs a probe. internal/app checks each probe against the read-only allowlist and
// runs it under the session guard, and only the probes the user approved.
package instrumentation

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/dialect/mssql"
)

// Param is a value a probe passes as @Name.
type Param struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Probe is one generated query, shown in full before it runs.
type Probe struct {
	ID   string                      `json:"id"`
	Kind diagnostics.MeasurementKind `json:"kind"`
	// Title says what the probe measures; Proves says what the result would show.
	Title  string  `json:"title"`
	Proves string  `json:"proves"`
	SQL    string  `json:"sql"`
	Params []Param `json:"params"`
	// Findings are the indexes of the findings the probe can confirm or dismiss.
	Findings []int `json:"findings"`
	// OverBudget probes are listed but cannot run: the run already has the maximum number.
	OverBudget bool `json:"overBudget"`

	measurement diagnostics.Measurement
}

// Plan generates the probes for findings, one per distinct measurement, in finding order. Probes
// after the first maxProbes are marked OverBudget. A measurement whose names cannot be quoted is
// skipped.
func Plan(findings []diagnostics.Finding, maxProbes int) []Probe {
	var out []Probe
	byKey := map[string]int{}
	for fi, f := range findings {
		for _, m := range f.Measurements {
			key := measurementKey(m)
			if i, ok := byKey[key]; ok {
				if !containsInt(out[i].Findings, fi) {
					out[i].Findings = append(out[i].Findings, fi)
				}
				continue
			}
			p, err := Build(m)
			if err != nil {
				continue
			}
			p.ID = "p" + strconv.Itoa(len(out)+1)
			p.Findings = []int{fi}
			p.OverBudget = len(out) >= maxProbes
			byKey[key] = len(out)
			out = append(out, p)
		}
	}
	return out
}

func containsInt(list []int, n int) bool {
	for _, x := range list {
		if x == n {
			return true
		}
	}
	return false
}

func measurementKey(m diagnostics.Measurement) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%s|%s|%s|%s", m.Kind, strings.ToLower(m.Table.Database), strings.ToLower(m.Table.Schema),
		strings.ToLower(m.Table.Name), strings.ToLower(strings.Join(m.Columns, ",")))
	if m.Other != nil {
		fmt.Fprintf(&b, "|%s.%s.%s|%s", strings.ToLower(m.Other.Database), strings.ToLower(m.Other.Schema),
			strings.ToLower(m.Other.Name), strings.ToLower(strings.Join(m.OtherColumns, ",")))
	}
	fmt.Fprintf(&b, "|%s", strings.Join(m.Values, "\x00"))
	return b.String()
}

// tableName is the quoted three-part name of a resolved table.
func tableName(t diagnostics.TableRef) (string, error) {
	var parts []string
	for _, p := range []string{t.Database, t.Schema, t.Name} {
		q, err := mssql.QuoteIdent(p)
		if err != nil {
			return "", fmt.Errorf("table %s.%s.%s: %w", t.Database, t.Schema, t.Name, err)
		}
		parts = append(parts, q)
	}
	return strings.Join(parts, "."), nil
}

func quoteAll(names []string) ([]string, error) {
	out := make([]string, len(names))
	for i, n := range names {
		q, err := mssql.QuoteIdent(n)
		if err != nil {
			return nil, fmt.Errorf("column %q: %w", n, err)
		}
		out[i] = q
	}
	return out, nil
}

func display(t diagnostics.TableRef) string { return t.Schema + "." + t.Name }

// Build turns a measurement into a probe from its template. It fails when the measurement is
// incomplete or a name cannot be quoted; it never falls back to unquoted text.
func Build(m diagnostics.Measurement) (Probe, error) {
	if len(m.Columns) == 0 {
		return Probe{}, fmt.Errorf("measurement %s has no columns", m.Kind)
	}
	table, err := tableName(m.Table)
	if err != nil {
		return Probe{}, err
	}
	cols, err := quoteAll(m.Columns)
	if err != nil {
		return Probe{}, err
	}
	p := Probe{Kind: m.Kind, Params: []Param{}, measurement: m}
	colList := strings.Join(m.Columns, ", ")
	switch m.Kind {
	case diagnostics.MeasureFanOut:
		var notNull []string
		for _, c := range cols {
			notNull = append(notNull, c+" IS NOT NULL")
		}
		p.Title = fmt.Sprintf("Rows per %s in %s", colList, display(m.Table))
		p.Proves = fmt.Sprintf("Whether a value of %s appears in more than one %s row, so a join on it multiplies rows, "+
			"and by how much.", colList, display(m.Table))
		p.SQL = "SELECT COUNT_BIG(*) AS key_values,\n" +
			"    SUM(CASE WHEN g.matches > 1 THEN 1 ELSE 0 END) AS repeated_values,\n" +
			"    MAX(g.matches) AS max_rows_per_value,\n" +
			"    AVG(CAST(g.matches AS float)) AS avg_rows_per_value\n" +
			"FROM (SELECT COUNT_BIG(*) AS matches\n" +
			"    FROM " + table + "\n" +
			"    WHERE " + strings.Join(notNull, " AND ") + "\n" +
			"    GROUP BY " + strings.Join(cols, ", ") + ") AS g;"
	case diagnostics.MeasureUnmatched:
		if m.Other == nil || len(m.OtherColumns) != len(m.Columns) {
			return Probe{}, fmt.Errorf("unmatched measurement needs the other table and as many columns")
		}
		other, err := tableName(*m.Other)
		if err != nil {
			return Probe{}, err
		}
		ocols, err := quoteAll(m.OtherColumns)
		if err != nil {
			return Probe{}, err
		}
		var match, isNull []string
		for i := range cols {
			match = append(match, "o."+ocols[i]+" = t."+cols[i])
			isNull = append(isNull, "t."+cols[i]+" IS NULL")
		}
		p.Title = fmt.Sprintf("%s rows without a match in %s", display(m.Table), display(*m.Other))
		p.Proves = fmt.Sprintf("How many %s rows have no %s row with the same %s, so an inner join drops them, and how "+
			"many of those have NULL in %s.", display(m.Table), display(*m.Other), strings.Join(m.OtherColumns, ", "), colList)
		p.SQL = "SELECT (SELECT COUNT_BIG(*) FROM " + table + ") AS total_rows,\n" +
			"    (SELECT COUNT_BIG(*) FROM " + table + " AS t\n" +
			"        WHERE NOT EXISTS (SELECT 1 FROM " + other + " AS o WHERE " + strings.Join(match, " AND ") + ")) AS unmatched_rows,\n" +
			"    (SELECT COUNT_BIG(*) FROM " + table + " AS t WHERE " + strings.Join(isNull, " OR ") + ") AS null_key_rows;"
	case diagnostics.MeasureNulls:
		p.Title = fmt.Sprintf("NULLs in %s.%s", display(m.Table), m.Columns[0])
		p.Proves = fmt.Sprintf("Whether %s.%s contains NULL, which makes NOT IN over it reject every row.",
			display(m.Table), m.Columns[0])
		p.SQL = "SELECT COUNT_BIG(*) AS null_rows\nFROM " + table + "\nWHERE " + cols[0] + " IS NULL;"
	case diagnostics.MeasureRangeEnd:
		if len(m.Values) != 2 {
			return Probe{}, fmt.Errorf("range-end measurement needs the end and the next day")
		}
		p.Title = fmt.Sprintf("%s rows after the start of %s", display(m.Table), m.Values[0])
		p.Proves = fmt.Sprintf("How many rows BETWEEN … AND '%s' misses: %s is later than %s but before %s.",
			m.Values[0], m.Columns[0], m.Values[0], m.Values[1])
		p.Params = []Param{{Name: "p1", Value: m.Values[0]}, {Name: "p2", Value: m.Values[1]}}
		p.SQL = "SELECT COUNT_BIG(*) AS missed_rows\nFROM " + table + "\nWHERE " + cols[0] + " > @p1 AND " + cols[0] + " < @p2;"
	default:
		return Probe{}, fmt.Errorf("unknown measurement %q", m.Kind)
	}
	return p, nil
}
