package mssql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-sql/sqlexp"
	mssqldb "github.com/microsoft/go-mssqldb"

	"github.com/TuanFaied/qryvanta/internal/executor"
	"github.com/TuanFaied/qryvanta/internal/security"
)

// Executor runs batches on SQL Server. It implements executor.Runner.
type Executor struct{}

var _ executor.Runner = Executor{}

// maxBinaryBytes caps how many bytes of a binary value are shown (as hex).
const maxBinaryBytes = 512

// maxSafeInteger is JavaScript's Number.MAX_SAFE_INTEGER; larger integers become strings.
const maxSafeInteger = 1<<53 - 1

// restoreTimeout bounds restoring the session's database after a run.
const restoreTimeout = 5 * time.Second

// Execute runs req on a dedicated connection from db and streams results to sink.
//
// The batch runs in req.Database (via a quoted USE) and the connection's original database is
// restored afterwards, even if the batch itself changed it, so the next user of the pooled
// connection never inherits a different context. If restoring fails the connection is
// discarded instead of being returned to the pool.
func (Executor) Execute(ctx context.Context, db *sql.DB, req executor.Request, sink executor.Sink) executor.Summary {
	start := time.Now()
	sum := executor.Summary{ResultSets: []executor.ResultSet{}, RowsAffected: []int64{}, Messages: []executor.Message{}}
	finish := func(o executor.Outcome) executor.Summary {
		sum.Outcome = o
		sum.ElapsedMs = time.Since(start).Milliseconds()
		return sum
	}

	if req.Limits.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Limits.Timeout)
		defer cancel()
	}
	// runCtx is also cancelled when a limit is reached, which makes the driver send an
	// attention signal so the server stops producing rows.
	runCtx, stop := context.WithCancel(ctx)
	defer stop()

	// outcomeFor classifies a failure: timeout and cancellation come from the caller's
	// context, anything else is a failure of the batch itself.
	outcomeFor := func(error) executor.Outcome {
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return executor.OutcomeTimedOut
		case errors.Is(ctx.Err(), context.Canceled):
			return executor.OutcomeCancelled
		default:
			return executor.OutcomeFailed
		}
	}

	conn, err := db.Conn(runCtx)
	if err != nil {
		sum.Error = clean(err)
		return finish(outcomeFor(err))
	}
	original, err := currentDatabase(runCtx, conn)
	if err != nil {
		_ = conn.Close()
		sum.Error = clean(err)
		return finish(outcomeFor(err))
	}
	defer releaseConn(conn, original)

	if req.Database != "" && req.Database != original {
		q, err := requireDatabase(runCtx, db, req.Database)
		if err == nil {
			_, err = conn.ExecContext(runCtx, "USE "+q)
		}
		if err != nil {
			sum.Error = clean(convertError(err))
			return finish(outcomeFor(err))
		}
	}

	retmsg := &sqlexp.ReturnMessage{}
	rows, err := conn.QueryContext(runCtx, req.SQL, retmsg)
	if err != nil {
		if m, ok := toMessage(err, "error", 0); ok {
			sum.Messages = append(sum.Messages, m)
			sum.ErrorCount++
			_ = sink.Message(m)
		}
		sum.Error = clean(convertError(err))
		return finish(outcomeFor(err))
	}
	defer rows.Close()

	budget := executor.NewBudget(req.Limits)
	var sinkErr error
	addMessage := func(m executor.Message) {
		sum.Messages = append(sum.Messages, m)
		if m.Kind == "error" {
			sum.ErrorCount++
		}
		if err := sink.Message(m); err != nil && sinkErr == nil {
			sinkErr = err
		}
	}

loop:
	for {
		if runCtx.Err() != nil || sinkErr != nil {
			break
		}
		switch m := retmsg.Message(runCtx).(type) {
		case sqlexp.MsgNotice:
			if msg, ok := toMessage(m.Message, "info", len(sum.ResultSets)); ok {
				addMessage(msg)
			}
		case sqlexp.MsgError:
			if msg, ok := toMessage(m.Error, "error", len(sum.ResultSets)); ok {
				addMessage(msg)
			}
		case sqlexp.MsgRowsAffected:
			sum.RowsAffected = append(sum.RowsAffected, m.Count)
		case sqlexp.MsgNext:
			truncatedBy, err := readResultSet(rows, &sum, budget, sink)
			if err != nil {
				sinkErr = err
				break loop
			}
			if truncatedBy != "" {
				sum.TruncatedBy = truncatedBy
				stop()
				break loop
			}
		case sqlexp.MsgNextResultSet:
			if !rows.NextResultSet() {
				break loop
			}
		case nil:
			break loop
		}
	}
	sum.TotalRows, sum.TotalBytes = budget.Rows, budget.Bytes

	switch {
	case sum.TruncatedBy != "":
		return finish(executor.OutcomeTruncated)
	case ctx.Err() != nil:
		return finish(outcomeFor(ctx.Err()))
	case sinkErr != nil:
		sum.Error = clean(sinkErr)
		return finish(executor.OutcomeFailed)
	}
	if err := rows.Err(); err != nil && !errors.Is(err, context.Canceled) {
		// Server errors were already reported as messages; anything else is a failure.
		var me mssqldb.Error
		if !errors.As(err, &me) {
			sum.Error = clean(err)
			return finish(executor.OutcomeFailed)
		}
	}
	return finish(executor.OutcomeSucceeded)
}

// readResultSet streams one result set. It returns the limit that stopped reading, if any.
func readResultSet(rows *sql.Rows, sum *executor.Summary, budget *executor.Budget, sink executor.Sink) (string, error) {
	types, err := rows.ColumnTypes()
	if err != nil {
		return "", err
	}
	cols := make([]executor.Column, len(types))
	for i, ct := range types {
		nullable, _ := ct.Nullable()
		cols[i] = executor.Column{Name: ct.Name(), Type: ct.DatabaseTypeName(), Nullable: nullable}
	}
	index := len(sum.ResultSets)
	sum.ResultSets = append(sum.ResultSets, executor.ResultSet{Index: index, Columns: cols})
	if err := sink.ResultSet(index, cols); err != nil {
		return "", err
	}

	raw := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range raw {
		ptrs[i] = &raw[i]
	}
	batch := make([][]executor.Value, 0, executor.RowBatchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		err := sink.Rows(index, batch)
		batch = make([][]executor.Value, 0, executor.RowBatchSize)
		return err
	}

	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return "", err
		}
		row := make([]executor.Value, len(cols))
		var size int64
		for i, v := range raw {
			row[i] = ConvertValue(cols[i].Type, v)
			size += executor.SizeOf(row[i])
		}
		if limit := budget.Add(size); limit != "" {
			return limit, flush()
		}
		sum.ResultSets[index].Rows++
		batch = append(batch, row)
		if len(batch) == executor.RowBatchSize {
			if err := flush(); err != nil {
				return "", err
			}
		}
	}
	return "", flush()
}

// ConvertValue turns a driver value into a display-safe executor.Value.
func ConvertValue(dbType string, v any) executor.Value {
	switch x := v.(type) {
	case nil:
		return nil
	case []byte:
		switch dbType {
		case "DECIMAL", "NUMERIC", "MONEY", "SMALLMONEY":
			return string(x)
		case "UNIQUEIDENTIFIER":
			var u mssqldb.UniqueIdentifier
			if err := u.Scan(x); err == nil {
				return u.String()
			}
		}
		return binaryString(x)
	case time.Time:
		switch dbType {
		case "DATE":
			return x.Format("2006-01-02")
		case "TIME":
			return x.Format("15:04:05.9999999")
		case "DATETIMEOFFSET":
			return x.Format(time.RFC3339Nano)
		default:
			return x.Format("2006-01-02T15:04:05.9999999")
		}
	case int64:
		if x > maxSafeInteger || x < -maxSafeInteger {
			return strconv.FormatInt(x, 10)
		}
		return x
	case float32:
		return float64(x)
	case string, bool, float64:
		return x
	default:
		return fmt.Sprint(x)
	}
}

func binaryString(b []byte) string {
	if len(b) > maxBinaryBytes {
		return "0x" + strings.ToUpper(hex.EncodeToString(b[:maxBinaryBytes])) + fmt.Sprintf("… (%d bytes)", len(b))
	}
	return "0x" + strings.ToUpper(hex.EncodeToString(b))
}

// toMessage converts a driver notice or error into an executor.Message.
func toMessage(v any, kind string, after int) (executor.Message, bool) {
	m := executor.Message{Kind: kind, AfterResultSet: after}
	var me mssqldb.Error
	switch x := v.(type) {
	case mssqldb.Error:
		me = x
	case error:
		if !errors.As(x, &me) {
			if errors.Is(x, context.Canceled) || errors.Is(x, context.DeadlineExceeded) {
				return m, false
			}
			m.Text = clean(x)
			return m, true
		}
	case fmt.Stringer:
		m.Text = x.String()
		return m, true
	default:
		return m, false
	}
	m.Number, m.Severity, m.State, m.Line, m.Procedure, m.Text = me.Number, me.Class, me.State, me.LineNo, me.ProcName, me.Message
	// Severity 11 and above is an error even when it arrives as a notice.
	if me.Class > 10 {
		m.Kind = "error"
	}
	return m, true
}

func clean(err error) string {
	if err == nil {
		return ""
	}
	return security.RedactString(err.Error())
}

func currentDatabase(ctx context.Context, conn *sql.Conn) (string, error) {
	var name string
	err := conn.QueryRowContext(ctx, "SELECT DB_NAME()").Scan(&name)
	return name, err
}

// releaseConn restores the original database and returns the connection to the pool, or
// discards it when the session state cannot be restored.
func releaseConn(conn *sql.Conn, original string) {
	ctx, cancel := context.WithTimeout(context.Background(), restoreTimeout)
	defer cancel()
	ok := false
	if q, err := QuoteIdent(original); err == nil {
		if now, err := currentDatabase(ctx, conn); err == nil {
			if now == original {
				ok = true
			} else if _, err := conn.ExecContext(ctx, "USE "+q); err == nil {
				ok = true
			}
		}
	}
	if !ok {
		// Returning driver.ErrBadConn makes database/sql close this connection.
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	}
	_ = conn.Close()
}
