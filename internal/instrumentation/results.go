package instrumentation

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/TuanFaied/qryvanta/internal/diagnostics"
)

// Result is what a probe returned.
type Result struct {
	ProbeID   string `json:"probeId"`
	ElapsedMs int64  `json:"elapsedMs"`
	// Error is set when the probe failed or was cancelled; then nothing is concluded from it.
	Error string `json:"error"`
	// Summary states the measurement in words.
	Summary string `json:"summary"`
	// ShowsProblem is true when the measurement confirms the findings, false when it dismisses them.
	ShowsProblem bool `json:"showsProblem"`
	// Values are the probe's single row, column name to value, for the record.
	Values map[string]string `json:"values"`
}

// number reads an executor value (int64, float64, a numeric string, or nil for NULL).
func number(v any) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	}
	return 0, false
}

// Interpret reads a probe's first row (column names and values) into a Result.
func Interpret(p Probe, columns []string, row []any) (Result, error) {
	if len(columns) != len(row) {
		return Result{}, fmt.Errorf("probe %s returned %d columns and %d values", p.ID, len(columns), len(row))
	}
	values := map[string]float64{}
	r := Result{ProbeID: p.ID, Values: map[string]string{}}
	for i, c := range columns {
		r.Values[c] = fmt.Sprint(row[i])
		if row[i] == nil {
			r.Values[c] = "NULL"
		}
		if n, ok := number(row[i]); ok {
			values[c] = n
		}
	}
	get := func(name string) (float64, error) {
		v, ok := values[name]
		if !ok {
			if _, present := r.Values[name]; present {
				return 0, nil // NULL: an aggregate over no rows
			}
			return 0, fmt.Errorf("probe %s returned no %s", p.ID, name)
		}
		return v, nil
	}
	m := p.measurement
	switch p.Kind {
	case diagnostics.MeasureFanOut:
		keys, err1 := get("key_values")
		repeated, err2 := get("repeated_values")
		maxRows, err3 := get("max_rows_per_value")
		avg, err4 := get("avg_rows_per_value")
		if err := firstError(err1, err2, err3, err4); err != nil {
			return Result{}, err
		}
		cols := strings.Join(m.Columns, ", ")
		if repeated > 0 {
			r.ShowsProblem = true
			r.Summary = fmt.Sprintf("%s of %s values of %s appear in several %s rows: up to %s rows per value, %.2f on average "+
				"(measured).", count(repeated), count(keys), cols, display(m.Table), count(maxRows), avg)
		} else {
			r.Summary = fmt.Sprintf("Each of the %s values of %s appears in one %s row: no fan-out today (measured; a key "+
				"or unique constraint would guarantee it).", count(keys), cols, display(m.Table))
		}
	case diagnostics.MeasureUnmatched:
		total, err1 := get("total_rows")
		unmatched, err2 := get("unmatched_rows")
		nulls, err3 := get("null_key_rows")
		if err := firstError(err1, err2, err3); err != nil {
			return Result{}, err
		}
		if unmatched > 0 {
			r.ShowsProblem = true
			r.Summary = fmt.Sprintf("%s of %s %s rows have no match in %s (%s of them with NULL in %s), so an inner join "+
				"drops them (measured).", count(unmatched), count(total), display(m.Table), display(*m.Other), count(nulls), strings.Join(m.Columns, ", "))
		} else {
			r.Summary = fmt.Sprintf("All %s %s rows have a match in %s: the join drops none today (measured).",
				count(total), display(m.Table), display(*m.Other))
		}
	case diagnostics.MeasureNulls:
		nulls, err := get("null_rows")
		if err != nil {
			return Result{}, err
		}
		if nulls > 0 {
			r.ShowsProblem = true
			r.Summary = fmt.Sprintf("%s.%s has %s, so NOT IN over it is never true and the query returns no rows "+
				"(measured).", display(m.Table), m.Columns[0], plural(nulls, "NULL", "NULLs"))
		} else {
			r.Summary = fmt.Sprintf("%s.%s has no NULLs today (measured), so NOT IN behaves as expected. The column still "+
				"allows NULL, so NOT EXISTS stays safer.", display(m.Table), m.Columns[0])
		}
	case diagnostics.MeasureRangeEnd:
		missed, err := get("missed_rows")
		if err != nil {
			return Result{}, err
		}
		if missed > 0 {
			r.ShowsProblem = true
			r.Summary = fmt.Sprintf("%s of %s %s after %s and before %s, so BETWEEN misses them (measured).",
				plural(missed, "1 row", "rows"), display(m.Table), verb(missed), m.Values[0], m.Values[1])
		} else {
			r.Summary = fmt.Sprintf("No %s rows fall after %s and before %s today (measured): BETWEEN returns the same "+
				"rows, though a half-open range stays correct when such rows appear.", display(m.Table), m.Values[0], m.Values[1])
		}
	default:
		return Result{}, fmt.Errorf("unknown probe kind %q", p.Kind)
	}
	return r, nil
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func count(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', 2, 64)
}

// Dismissal is a finding a probe showed to be no problem, with the reason.
type Dismissal struct {
	Finding diagnostics.Finding `json:"finding"`
	Reason  string              `json:"reason"`
}

// Apply updates findings with probe results. A finding whose probe shows the problem gets the
// measurement as probe evidence and becomes Confirmed. A finding whose probes all ran and none
// shows a problem is dismissed, with the reason. Failed probes leave their findings unchanged.
// The result is validated against sql; a finding that would become invalid keeps its old form.
func Apply(findings []diagnostics.Finding, probes []Probe, results []Result, sql string) ([]diagnostics.Finding, []Dismissal) {
	byProbe := map[string]Result{}
	for _, r := range results {
		byProbe[r.ProbeID] = r
	}
	type state struct {
		confirmed []Result
		cleared   []Result
		pending   bool
	}
	states := make([]state, len(findings))
	for _, p := range probes {
		r, ran := byProbe[p.ID]
		for _, fi := range p.Findings {
			if fi < 0 || fi >= len(findings) {
				continue
			}
			switch {
			case !ran || r.Error != "":
				states[fi].pending = true
			case r.ShowsProblem:
				states[fi].confirmed = append(states[fi].confirmed, r)
			default:
				states[fi].cleared = append(states[fi].cleared, r)
			}
		}
	}
	kept := make([]diagnostics.Finding, 0, len(findings))
	var dismissed []Dismissal
	for i, f := range findings {
		s := states[i]
		switch {
		case len(s.confirmed) > 0:
			updated := f
			updated.Evidence = append(append([]diagnostics.Evidence{}, f.Evidence...), probeEvidence(probes, s.confirmed)...)
			updated.Classification = diagnostics.Confirmed
			if diagnostics.Validate(updated, sql) == nil {
				f = updated
			}
			kept = append(kept, f)
		case len(s.cleared) > 0 && !s.pending:
			reason := s.cleared[0].Summary
			for _, r := range s.cleared[1:] {
				reason += " " + r.Summary
			}
			dismissed = append(dismissed, Dismissal{Finding: f, Reason: reason})
		default:
			kept = append(kept, f)
		}
	}
	if dismissed == nil {
		dismissed = []Dismissal{}
	}
	return kept, dismissed
}

func probeEvidence(probes []Probe, results []Result) []diagnostics.Evidence {
	sqlOf := map[string]string{}
	for _, p := range probes {
		sqlOf[p.ID] = p.SQL
	}
	out := make([]diagnostics.Evidence, 0, len(results))
	for _, r := range results {
		out = append(out, diagnostics.Evidence{
			Kind:    diagnostics.EvidenceProbe,
			Summary: r.Summary,
			Probe:   &diagnostics.ProbeEvidence{SQL: sqlOf[r.ProbeID], Result: formatValues(r.Values)},
		})
	}
	return out
}

func formatValues(v map[string]string) string {
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	s := ""
	for i, k := range keys {
		if i > 0 {
			s += ", "
		}
		s += k + " = " + v[k]
	}
	return s
}

// plural writes n with the singular or plural word ("1 NULL", "3 NULLs"); a one with its own
// count ("1 row") is used as is.
func plural(n float64, one, many string) string {
	if n == 1 {
		if strings.HasPrefix(one, "1 ") {
			return one
		}
		return "1 " + one
	}
	return count(n) + " " + many
}

func verb(n float64) string {
	if n == 1 {
		return "falls"
	}
	return "fall"
}
