// Package executor runs SQL against an open database and streams the results.
//
// It defines a product-neutral model (requests, limits, columns, messages, summaries) and
// the Runner interface. Dialect packages implement Runner (internal/dialect/mssql). Callers
// receive results through a Sink as they are read, so large results never have to be held in
// memory twice, and limits stop reading as soon as they are reached.
package executor

import (
	"context"
	"database/sql"
	"time"
)

// Limits bound a single execution. Zero values mean "no limit" except Timeout, which callers
// always set from the user's configuration.
type Limits struct {
	Timeout  time.Duration
	MaxRows  int   // across all result sets
	MaxBytes int64 // approximate size of the converted values, across all result sets
}

// Request is one batch to execute.
type Request struct {
	SQL string
	// Database is the database to run in. Empty uses the connection's current database.
	Database string
	Limits   Limits
}

// Column describes a result-set column.
type Column struct {
	Name string `json:"name"`
	// Type is the database type name, for example NVARCHAR or DECIMAL.
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
}

// Value is a display-safe cell value: nil (SQL NULL), string, int64 (within JavaScript's safe
// integer range), float64 or bool. Exact numerics, large integers, dates, GUIDs and binary
// values are strings so nothing loses precision on the way to the UI.
type Value = any

// Message is a server message or error, in the order the server sent it.
type Message struct {
	// Kind is "info" (PRINT, informational RAISERROR) or "error".
	Kind      string `json:"kind"`
	Number    int32  `json:"number"`
	Severity  uint8  `json:"severity"`
	State     uint8  `json:"state"`
	Line      int32  `json:"line"`
	Procedure string `json:"procedure"`
	Text      string `json:"text"`
	// AfterResultSet is the number of result sets returned before this message, so the UI
	// can interleave messages and results in order.
	AfterResultSet int `json:"afterResultSet"`
}

// ResultSet summarises one result set.
type ResultSet struct {
	Index   int      `json:"index"`
	Columns []Column `json:"columns"`
	Rows    int      `json:"rows"`
}

// Outcome is how an execution ended.
type Outcome string

const (
	OutcomeSucceeded Outcome = "succeeded" // ran to completion, possibly with errors reported as messages
	OutcomeFailed    Outcome = "failed"    // the batch could not run (connection, database not found, ...)
	OutcomeTruncated Outcome = "truncated" // a row or size limit stopped reading; the query was cancelled
	OutcomeTimedOut  Outcome = "timed_out"
	OutcomeCancelled Outcome = "cancelled"
)

// Summary describes a finished execution. It never contains result rows.
type Summary struct {
	Outcome      Outcome     `json:"outcome"`
	ResultSets   []ResultSet `json:"resultSets"`
	RowsAffected []int64     `json:"rowsAffected"`
	Messages     []Message   `json:"messages"`
	// ErrorCount counts error messages (Kind "error").
	ErrorCount int `json:"errorCount"`
	// TruncatedBy is "rows" or "bytes" when Outcome is truncated.
	TruncatedBy string `json:"truncatedBy"`
	TotalRows   int    `json:"totalRows"`
	TotalBytes  int64  `json:"totalBytes"`
	// ElapsedMs is measured wall-clock time from sending the batch to the end of reading.
	ElapsedMs int64 `json:"elapsedMs"`
	// Error explains OutcomeFailed. Server errors during the batch are in Messages instead.
	Error string `json:"error"`
}

// Sink receives results as they are read. Methods are called from the executing goroutine,
// in server order. Returning an error stops the execution (the query is cancelled).
type Sink interface {
	ResultSet(index int, columns []Column) error
	Rows(index int, rows [][]Value) error
	Message(m Message) error
}

// Runner executes a request on a database handle.
type Runner interface {
	Execute(ctx context.Context, db *sql.DB, req Request, sink Sink) Summary
}

// RowBatchSize is how many rows are delivered to the sink at once.
const RowBatchSize = 500

// Budget tracks rows and bytes against the limits.
type Budget struct {
	limits Limits
	Rows   int
	Bytes  int64
}

// NewBudget starts tracking against limits.
func NewBudget(l Limits) *Budget { return &Budget{limits: l} }

// Add records one row of the given size and reports which limit, if any, the row exceeds.
// A row that would exceed a limit is not counted and must not be delivered.
func (b *Budget) Add(rowBytes int64) (exceeded string) {
	if b.limits.MaxRows > 0 && b.Rows+1 > b.limits.MaxRows {
		return "rows"
	}
	if b.limits.MaxBytes > 0 && b.Bytes+rowBytes > b.limits.MaxBytes {
		return "bytes"
	}
	b.Rows++
	b.Bytes += rowBytes
	return ""
}

// SizeOf approximates the memory a converted value occupies, for the byte limit.
func SizeOf(v Value) int64 {
	switch x := v.(type) {
	case nil:
		return 1
	case string:
		return int64(len(x)) + 2
	case bool:
		return 1
	default:
		return 8
	}
}

// Collector is a Sink that keeps everything in memory. It is meant for tests and small
// command-line results, not for the desktop UI.
type Collector struct {
	Columns  [][]Column
	Data     [][][]Value
	Messages []Message
}

func (c *Collector) ResultSet(index int, columns []Column) error {
	c.Columns = append(c.Columns, columns)
	c.Data = append(c.Data, nil)
	return nil
}

func (c *Collector) Rows(index int, rows [][]Value) error {
	c.Data[index] = append(c.Data[index], rows...)
	return nil
}

func (c *Collector) Message(m Message) error {
	c.Messages = append(c.Messages, m)
	return nil
}
