package main

import (
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/config"
	"github.com/TuanFaied/qryvanta/internal/connection"
	"github.com/TuanFaied/qryvanta/internal/executor"
	"github.com/TuanFaied/qryvanta/internal/sqlfile"
)

// Exit codes of "run", documented in the usage text.
const (
	exitQueryError = 1 // the batch failed or reported errors
	exitStopped    = 3 // timed out or cancelled (Ctrl+C)
)

// exitError carries a specific exit code. Its message has already been printed when silent.
type exitError struct {
	code   int
	err    error
	silent bool
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// maxCellWidth caps table-format columns so one long value does not wreck the layout.
const maxCellWidth = 40

type runOptions struct {
	conn, database, file, query, format string
	maxRows, timeout                    int
	allowWrites                         bool
}

func parseRunFlags(args []string) (runOptions, error) {
	var o runOptions
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.conn, "conn", "", "connection name or ID")
	fs.StringVar(&o.database, "database", "", "database to run in")
	fs.StringVar(&o.file, "file", "", "read the batch from a .sql file")
	fs.StringVar(&o.query, "query", "", "the batch to run")
	fs.StringVar(&o.format, "format", "table", "table, json or csv")
	fs.IntVar(&o.maxRows, "max-rows", 0, "row limit (default: the configured limit)")
	fs.IntVar(&o.timeout, "timeout", 0, "timeout in seconds (default: the configured timeout)")
	fs.BoolVar(&o.allowWrites, "allow-writes", false, "turn read-only mode off for this run")
	if err := fs.Parse(args); err != nil {
		return o, fmt.Errorf("%w: %v", errUsage, err)
	}
	if fs.NArg() > 0 {
		return o, fmt.Errorf("%w: unexpected argument %q (pass SQL with --query, --file or stdin)", errUsage, fs.Arg(0))
	}
	switch o.format {
	case "table", "json", "csv":
	default:
		return o, fmt.Errorf("%w: --format must be table, json or csv", errUsage)
	}
	if o.file != "" && o.query != "" {
		return o, fmt.Errorf("%w: use either --file or --query, not both", errUsage)
	}
	if o.maxRows != 0 && (o.maxRows < config.MinMaxRows || o.maxRows > config.MaxMaxRows) {
		return o, fmt.Errorf("%w: --max-rows must be between %d and %d", errUsage, config.MinMaxRows, config.MaxMaxRows)
	}
	if o.timeout != 0 && (o.timeout < config.MinTimeoutSeconds || o.timeout > config.MaxTimeoutSeconds) {
		return o, fmt.Errorf("%w: --timeout must be between %d and %d seconds", errUsage, config.MinTimeoutSeconds, config.MaxTimeoutSeconds)
	}
	return o, nil
}

// readBatch returns the SQL from --query, --file (any encoding sqlfile detects) or stdin.
func readBatch(o runOptions) (string, error) {
	var text string
	switch {
	case o.query != "":
		text = o.query
	case o.file != "":
		doc, err := sqlfile.Read(o.file)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", o.file, err)
		}
		text = doc.Text
	default:
		data, err := io.ReadAll(io.LimitReader(stdin, sqlfile.MaxSize+1))
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		if len(data) > sqlfile.MaxSize {
			return "", sqlfile.ErrTooLarge
		}
		// PowerShell pipes text with a byte-order mark (UTF-8, or UTF-16 for some redirections).
		doc, err := sqlfile.Decode(data)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		text = doc.Text
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("%w: no SQL to run (use --query, --file or pipe it to stdin)", errUsage)
	}
	return text, nil
}

// pickConnection resolves --conn, or the only saved connection when it is omitted.
func pickConnection(m *connection.Manager, target string) (connection.Profile, error) {
	if target != "" {
		return resolveProfile(m, target)
	}
	profiles, err := m.List()
	if err != nil {
		return connection.Profile{}, err
	}
	switch len(profiles) {
	case 0:
		return connection.Profile{}, errors.New("no saved connections; add one with 'qryvanta-cli connections add'")
	case 1:
		return profiles[0], nil
	default:
		return connection.Profile{}, fmt.Errorf("%w: several connections are saved; choose one with --conn", errUsage)
	}
}

func cmdRun(ctx context.Context, args []string, opts app.Options, stdout, stderr io.Writer) error {
	o, err := parseRunFlags(args)
	if err != nil {
		return err
	}
	sql, err := readBatch(o)
	if err != nil {
		return err
	}

	opts.AllowWrites = o.allowWrites
	core, err := app.New(opts)
	if err != nil {
		return err
	}
	defer core.Close()

	prof, err := pickConnection(core.Connections(), o.conn)
	if err != nil {
		return err
	}
	server, err := core.Pool().Connect(ctx, prof.ID)
	if err != nil {
		return fmt.Errorf("connection %q failed: %w", prof.Name, err)
	}
	database := o.database
	if database == "" {
		database = server.Database
	}

	policy := core.ExecutionPolicy()
	if o.allowWrites {
		fmt.Fprintln(stderr, "qryvanta-cli: WARNING: --allow-writes turns read-only mode off; changes made by this batch will be committed.")
	}
	// Tell the user when the login could change data, as the desktop status bar does.
	if access, err := core.Pool().CheckAccessFor(ctx, prof.ID, database); err == nil && access != nil {
		if access.Level == connection.AccessCanWrite || access.Level == connection.AccessMayWrite {
			msg := describeAccess(access)
			if len(access.Reasons) > 0 {
				msg += " (" + access.Reasons[0].Text + ")"
			}
			fmt.Fprintf(stderr, "qryvanta-cli: write access in %s: %s\n", database, msg)
		}
	}

	limits := core.ExecutionLimits()
	if o.maxRows > 0 {
		limits.MaxRows = o.maxRows
	}
	if o.timeout > 0 {
		limits.Timeout = time.Duration(o.timeout) * time.Second
	}

	col := &executor.Collector{}
	sum := core.Execute(ctx, prof.ID, executor.Request{SQL: sql, Database: database, Limits: limits}, col)
	res := buildResult(prof.Name, database, policy.ReadOnly, limits, sum, col)

	switch o.format {
	case "json":
		if err := writeJSON(stdout, res); err != nil {
			return err
		}
	case "csv":
		if len(res.ResultSets) > 1 {
			return fmt.Errorf("%w: CSV output needs a batch with one result set; this one returned %d (use --format json)",
				errUsage, len(res.ResultSets))
		}
		if len(res.ResultSets) == 1 {
			if err := writeCSV(stdout, res.ResultSets[0]); err != nil {
				return err
			}
		}
		writeMessages(stderr, res)
	default:
		if err := writeTables(stdout, res); err != nil {
			return err
		}
		writeMessages(stderr, res)
	}
	return exitFor(res)
}

// runResult is the JSON output of "run" (also the model the other formats print from).
type runResult struct {
	Connection   string             `json:"connection"`
	Database     string             `json:"database"`
	ReadOnly     bool               `json:"readOnly"`
	Outcome      executor.Outcome   `json:"outcome"`
	Truncated    bool               `json:"truncated"`
	TruncatedBy  string             `json:"truncatedBy,omitempty"`
	MaxRows      int                `json:"maxRows"`
	TimeoutSecs  int                `json:"timeoutSeconds"`
	ElapsedMs    int64              `json:"elapsedMs"`
	TotalRows    int                `json:"totalRows"`
	RowsAffected []int64            `json:"rowsAffected"`
	ErrorCount   int                `json:"errorCount"`
	Error        string             `json:"error,omitempty"`
	Guard        executor.Guard     `json:"guard"`
	ResultSets   []resultSetOut     `json:"resultSets"`
	Messages     []executor.Message `json:"messages"`
}

type resultSetOut struct {
	Columns []executor.Column  `json:"columns"`
	Rows    [][]executor.Value `json:"rows"`
}

func buildResult(conn, database string, readOnly bool, l executor.Limits, sum executor.Summary, col *executor.Collector) runResult {
	sets := make([]resultSetOut, len(col.Columns))
	for i := range col.Columns {
		rows := col.Data[i]
		if rows == nil {
			rows = [][]executor.Value{}
		}
		sets[i] = resultSetOut{Columns: col.Columns[i], Rows: rows}
	}
	msgs := col.Messages
	if msgs == nil {
		msgs = []executor.Message{}
	}
	affected := sum.RowsAffected
	if affected == nil {
		affected = []int64{}
	}
	return runResult{
		Connection:   conn,
		Database:     database,
		ReadOnly:     readOnly,
		Outcome:      sum.Outcome,
		Truncated:    sum.Outcome == executor.OutcomeTruncated,
		TruncatedBy:  sum.TruncatedBy,
		MaxRows:      l.MaxRows,
		TimeoutSecs:  int(l.Timeout / time.Second),
		ElapsedMs:    sum.ElapsedMs,
		TotalRows:    sum.TotalRows,
		RowsAffected: affected,
		ErrorCount:   sum.ErrorCount,
		Error:        sum.Error,
		Guard:        sum.Guard,
		ResultSets:   sets,
		Messages:     msgs,
	}
}

// exitFor maps the outcome to the documented exit code. Messages were already printed.
func exitFor(r runResult) error {
	switch {
	case r.Outcome == executor.OutcomeTimedOut:
		return &exitError{code: exitStopped, silent: true, err: fmt.Errorf("the query timed out after %d s", r.TimeoutSecs)}
	case r.Outcome == executor.OutcomeCancelled:
		return &exitError{code: exitStopped, silent: true, err: errors.New("the query was cancelled")}
	case r.Outcome == executor.OutcomeFailed:
		return &exitError{code: exitQueryError, silent: true, err: errors.New(r.Error)}
	case r.ErrorCount > 0:
		return &exitError{code: exitQueryError, silent: true, err: fmt.Errorf("the batch reported %d error(s)", r.ErrorCount)}
	}
	return nil
}

// cellText renders a value for the table and CSV formats.
func cellText(v executor.Value, null string) string {
	switch x := v.(type) {
	case nil:
		return null
	case bool:
		if x {
			return "1"
		}
		return "0"
	default:
		return fmt.Sprint(x)
	}
}

// fit shortens s to width runes on one line, marking the cut with an ellipsis.
func fit(s string, width int) string {
	s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(s)
	if utf8.RuneCountInString(s) <= width {
		return s
	}
	r := []rune(s)
	return string(r[:width-1]) + "…"
}

func writeTables(w io.Writer, r runResult) error {
	for i, rs := range r.ResultSets {
		if i > 0 {
			fmt.Fprintln(w)
		}
		widths := make([]int, len(rs.Columns))
		header := make([]string, len(rs.Columns))
		for c, column := range rs.Columns {
			header[c] = fit(column.Name, maxCellWidth)
			widths[c] = utf8.RuneCountInString(header[c])
		}
		cells := make([][]string, len(rs.Rows))
		for ri, row := range rs.Rows {
			cells[ri] = make([]string, len(rs.Columns))
			for c := range rs.Columns {
				var v executor.Value
				if c < len(row) {
					v = row[c]
				}
				cells[ri][c] = fit(cellText(v, "NULL"), maxCellWidth)
				widths[c] = max(widths[c], utf8.RuneCountInString(cells[ri][c]))
			}
		}
		line := func(vals []string) {
			var b strings.Builder
			for c, v := range vals {
				if c > 0 {
					b.WriteString("  ")
				}
				b.WriteString(v)
				if c < len(vals)-1 {
					b.WriteString(strings.Repeat(" ", widths[c]-utf8.RuneCountInString(v)))
				}
			}
			fmt.Fprintln(w, b.String())
		}
		line(header)
		dashes := make([]string, len(widths))
		for c, n := range widths {
			dashes[c] = strings.Repeat("-", n)
		}
		line(dashes)
		for _, row := range cells {
			line(row)
		}
		noun := "rows"
		if len(rs.Rows) == 1 {
			noun = "row"
		}
		if _, err := fmt.Fprintf(w, "(%d %s)\n", len(rs.Rows), noun); err != nil {
			return err
		}
	}
	return nil
}

// writeCSV writes one result set as RFC 4180 CSV with a header line. NULL is an empty field,
// the usual convention for CSV consumers; use --format json to tell NULL from an empty string.
func writeCSV(w io.Writer, rs resultSetOut) error {
	cw := csv.NewWriter(w)
	header := make([]string, len(rs.Columns))
	for i, c := range rs.Columns {
		header[i] = c.Name
	}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, row := range rs.Rows {
		rec := make([]string, len(rs.Columns))
		for i := range rec {
			if i < len(row) {
				rec[i] = cellText(row[i], "")
			}
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// writeMessages prints server messages, errors and the outcome to stderr, keeping stdout for data.
func writeMessages(w io.Writer, r runResult) {
	for _, m := range r.Messages {
		if m.Kind == "error" {
			head := fmt.Sprintf("Msg %d, Level %d, State %d", m.Number, m.Severity, m.State)
			if m.Procedure != "" {
				head += ", Procedure " + m.Procedure
			}
			if m.Line > 0 {
				head += fmt.Sprintf(", Line %d", m.Line)
			}
			fmt.Fprintf(w, "%s\n%s\n", head, m.Text)
		} else {
			fmt.Fprintln(w, m.Text)
		}
	}
	for _, n := range r.RowsAffected {
		noun := "rows"
		if n == 1 {
			noun = "row"
		}
		fmt.Fprintf(w, "(%d %s affected)\n", n, noun)
	}
	switch r.Outcome {
	case executor.OutcomeTruncated:
		limit := fmt.Sprintf("the %d-row limit", r.MaxRows)
		if r.TruncatedBy == "bytes" {
			limit = "the result size limit"
		}
		fmt.Fprintf(w, "Stopped reading at %s; the query was cancelled. Showing %d rows (raise it with --max-rows).\n", limit, r.TotalRows)
	case executor.OutcomeTimedOut:
		fmt.Fprintf(w, "The query timed out after %d s.\n", r.TimeoutSecs)
	case executor.OutcomeCancelled:
		fmt.Fprintln(w, "The query was cancelled.")
	case executor.OutcomeFailed:
		fmt.Fprintln(w, r.Error)
	}
	if r.Guard.EndedByBatch {
		fmt.Fprintln(w, "WARNING: a COMMIT or ROLLBACK in the batch ended read-only protection; later changes may have been committed.")
	}
}
