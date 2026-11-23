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

// Execute runs req's batches, one after another, on a dedicated connection from db and streams
// results to sink.
//
// All batches share the session, as with GO in SSMS: temp tables, variables' absence and SET
// options carry over. A batch that reports errors does not stop the next one; cancellation,
// the timeout, a row or size limit, or a lost connection stop the script.
//
// The script runs in req.Database (via a quoted USE) and the connection's original database is
// restored afterwards, even if a batch changed it, so the next user of the pooled connection
// never inherits a different context. If restoring fails the connection is discarded instead
// of being returned to the pool.
//
// In read-only mode one guard transaction spans the whole script (rolling back after every
// batch would drop a temp table before the next batch can use it). If a batch's own COMMIT or
// ROLLBACK ends it, that is reported and a new guard transaction protects the batches after it.
func (Executor) Execute(ctx context.Context, db *sql.DB, req executor.Request, sink executor.Sink) executor.Summary {
	start := time.Now()
	sum := executor.Summary{ResultSets: []executor.ResultSet{}, RowsAffected: []int64{}, Messages: []executor.Message{},
		Batches: []executor.BatchRun{}, Refusals: []executor.Refusal{}}
	var (
		conn    *sql.Conn
		discard bool // the session could not be returned to a clean state
		planOn  executor.PlanMode
	)
	// finish turns plans off, ends the session guard and records the outcome. It is used on every
	// return path after the connection is acquired.
	finish := func(o executor.Outcome) executor.Summary {
		if planOn != executor.PlanNone && conn != nil {
			if !planOff(conn, planOn) {
				discard = true // the session would keep returning plans instead of results
			}
		}
		if sum.Plan != nil {
			completePlans(sum.Plan, o, req.Limits.MaxPlanBytes)
		}
		if sum.Guard.Active && conn != nil {
			if !endGuard(conn, &sum.Guard) {
				discard = true
			}
		}
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

	var err error
	conn, err = db.Conn(runCtx)
	if err != nil {
		sum.Error = clean(err)
		return finish(outcomeFor(err))
	}
	original, err := currentDatabase(runCtx, conn)
	if err != nil {
		_ = conn.Close()
		conn = nil
		sum.Error = clean(err)
		return finish(outcomeFor(err))
	}
	defer func() { releaseConn(conn, original, discard) }()

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

	// An estimated plan compiles the script without running it, so it needs no guard (the policy
	// check before the executor still applies).
	if req.Policy.ReadOnly && req.Plan != executor.PlanEstimated {
		if err := beginGuard(runCtx, conn, req.Policy.LockTimeout); err != nil {
			discard = true
			sum.Error = "could not start the read-only session guard: " + clean(convertError(err))
			return finish(outcomeFor(err))
		}
		sum.Guard.Active = true
	}

	if req.Plan != executor.PlanNone {
		if err := planOnFor(runCtx, conn, req.Plan); err != nil {
			sum.Error = "could not turn on execution plans: " + clean(convertError(err))
			return finish(outcomeFor(err))
		}
		planOn = req.Plan
		sum.Plan = &executor.PlanSummary{Mode: req.Plan}
	}

	budget := executor.NewBudget(req.Limits)
	batchSink, _ := sink.(executor.BatchSink)
	planSink, _ := sink.(executor.PlanSink)
	batches := req.BatchesOf()
	for bi, b := range batches {
		executions := max(b.Repeat, 1)
		for e := 1; e <= executions; e++ {
			run := executor.BatchRun{Batch: bi, Execution: e, Line: max(b.Line, 1)}
			if batchSink != nil {
				if err := batchSink.BatchStart(run); err != nil {
					sum.Error = clean(err)
					return finish(executor.OutcomeFailed)
				}
			}
			began, errorsBefore := time.Now(), sum.ErrorCount
			var plans *planCapture
			if planOn != executor.PlanNone {
				plans = &planCapture{mode: planOn, max: maxPlanBytes(req.Limits), batch: bi, execution: e, sink: planSink, sum: sum.Plan}
			}
			stopScript, outcome := runBatch(runCtx, ctx, conn, b, req.Args(), bi, &sum, budget, sink, stop, plans)
			run.ErrorCount = sum.ErrorCount - errorsBefore
			run.ElapsedMs = time.Since(began).Milliseconds()
			sum.Batches = append(sum.Batches, run)
			sum.TotalRows, sum.TotalBytes = budget.Rows, budget.Bytes
			if stopScript {
				if outcome == executor.OutcomeTimedOut || outcome == executor.OutcomeCancelled {
					outcome = outcomeFor(ctx.Err())
				}
				return finish(outcome)
			}
			last := bi == len(batches)-1 && e == executions
			if sum.Guard.Active && !last {
				// A COMMIT or ROLLBACK in the batch ended the protective transaction: record
				// it and protect the batches that follow with a new one.
				open, err := transactionOpen(runCtx, conn)
				if err != nil {
					discard = true
					sum.Error = "could not check the read-only session guard: " + clean(convertError(err))
					return finish(outcomeFor(err))
				}
				if !open {
					sum.Guard.EndedByBatch = true
					if err := beginGuard(runCtx, conn, req.Policy.LockTimeout); err != nil {
						discard = true
						sum.Error = "could not restart the read-only session guard: " + clean(convertError(err))
						return finish(outcomeFor(err))
					}
				}
			}
		}
	}
	return finish(executor.OutcomeSucceeded)
}

// runBatch runs one batch execution. It reports whether the script must stop, and how it ended
// in that case. Server errors are recorded as messages and do not stop the script.
func runBatch(runCtx, ctx context.Context, conn *sql.Conn, b executor.Batch, args []any, batchIndex int, sum *executor.Summary,
	budget *executor.Budget, sink executor.Sink, stop context.CancelFunc, plans *planCapture) (bool, executor.Outcome) {
	var sinkErr error
	addMessage := func(m executor.Message) {
		m.Batch = batchIndex
		if m.Line > 0 && m.Procedure == "" {
			m.ScriptLine = int32(max(b.Line, 1)-1) + m.Line
		}
		sum.Messages = append(sum.Messages, m)
		if m.Kind == "error" {
			sum.ErrorCount++
		}
		if err := sink.Message(m); err != nil && sinkErr == nil {
			sinkErr = err
		}
	}

	retmsg := &sqlexp.ReturnMessage{}
	rows, err := conn.QueryContext(runCtx, b.SQL, append([]any{retmsg}, args...)...)
	if err != nil {
		if ctx.Err() != nil {
			return true, executor.OutcomeCancelled // refined by the caller
		}
		if m, ok := toMessage(err, "error", len(sum.ResultSets)); ok {
			addMessage(m)
		}
		var me mssqldb.Error
		if errors.As(err, &me) && sinkErr == nil {
			return false, ""
		}
		sum.Error = clean(convertError(err))
		return true, executor.OutcomeFailed
	}
	defer rows.Close()

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
			truncatedBy, err := readResultSet(rows, sum, budget, sink, batchIndex, plans)
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

	switch {
	case sum.TruncatedBy != "":
		return true, executor.OutcomeTruncated
	case ctx.Err() != nil:
		return true, executor.OutcomeCancelled // refined by the caller
	case sinkErr != nil:
		sum.Error = clean(sinkErr)
		return true, executor.OutcomeFailed
	}
	if err := rows.Err(); err != nil && !errors.Is(err, context.Canceled) {
		// Server errors were already reported as messages; anything else (a lost connection)
		// stops the script.
		var me mssqldb.Error
		if !errors.As(err, &me) {
			sum.Error = clean(err)
			return true, executor.OutcomeFailed
		}
	}
	return false, ""
}

// transactionOpen reports whether the session still has an open transaction.
func transactionOpen(ctx context.Context, conn *sql.Conn) (bool, error) {
	var n int
	err := conn.QueryRowContext(ctx, "SELECT @@TRANCOUNT").Scan(&n)
	return n > 0, err
}

// readResultSet streams one result set. It returns the limit that stopped reading, if any.
func readResultSet(rows *sql.Rows, sum *executor.Summary, budget *executor.Budget, sink executor.Sink, batchIndex int,
	plans *planCapture) (string, error) {
	types, err := rows.ColumnTypes()
	if err != nil {
		return "", err
	}
	// A plan document: kept apart from the results, so the grid and the CLI never show it.
	if plans != nil && len(types) == 1 && types[0].Name() == showplanColumn {
		return "", plans.read(rows)
	}
	cols := make([]executor.Column, len(types))
	for i, ct := range types {
		nullable, _ := ct.Nullable()
		cols[i] = executor.Column{Name: ct.Name(), Type: ct.DatabaseTypeName(), Nullable: nullable}
	}
	index := len(sum.ResultSets)
	sum.ResultSets = append(sum.ResultSets, executor.ResultSet{Index: index, Batch: batchIndex, Columns: cols})
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

// beginGuard starts the read-only session guard: implicit transactions off, an optional lock
// timeout, and a transaction that endGuard always rolls back. XACT_ABORT is deliberately left
// at the server default (OFF): turning it on would make any runtime error abort the rest of
// the batch, so read-only mode would behave differently from SSMS and from writes-allowed
// mode, and the rollback already undoes everything the batch changed.
func beginGuard(ctx context.Context, conn *sql.Conn, lockTimeout time.Duration) error {
	stmt := "SET IMPLICIT_TRANSACTIONS OFF;"
	if lockTimeout > 0 {
		stmt += fmt.Sprintf(" SET LOCK_TIMEOUT %d;", lockTimeout.Milliseconds())
	}
	stmt += " BEGIN TRANSACTION;"
	_, err := conn.ExecContext(ctx, stmt)
	return err
}

// endGuard rolls back the protective transaction and resets the session's lock timeout. It
// uses a fresh context because the run's context may already be cancelled. It reports false
// when the session could not be returned to a clean state.
func endGuard(conn *sql.Conn, g *executor.Guard) bool {
	ctx, cancel := context.WithTimeout(context.Background(), restoreTimeout)
	defer cancel()
	var trancount int
	if err := conn.QueryRowContext(ctx, "SELECT @@TRANCOUNT").Scan(&trancount); err != nil {
		return false
	}
	if trancount == 0 {
		// The batch ran COMMIT or ROLLBACK: the guard ended before the batch did.
		g.EndedByBatch = true
	} else {
		if _, err := conn.ExecContext(ctx, "ROLLBACK TRANSACTION"); err != nil {
			return false
		}
		g.RolledBack = true
	}
	_, err := conn.ExecContext(ctx, "SET LOCK_TIMEOUT -1")
	return err == nil
}

// releaseConn restores the original database and returns the connection to the pool, or
// discards it when the session state cannot be restored (or discard is already set).
func releaseConn(conn *sql.Conn, original string, discard bool) {
	if conn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), restoreTimeout)
	defer cancel()
	ok := false
	if discard {
		// Skip restoring: the connection is closed below.
	} else if q, err := QuoteIdent(original); err == nil {
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

// showplanColumn names the single column of the result sets that carry showplan XML, with
// SHOWPLAN_XML and with STATISTICS XML alike.
const showplanColumn = "Microsoft SQL Server 2005 XML Showplan"

func maxPlanBytes(l executor.Limits) int64 {
	if l.MaxPlanBytes > 0 {
		return l.MaxPlanBytes
	}
	return executor.DefaultMaxPlanBytes
}

// planOnFor turns plans on for the session. SET SHOWPLAN_XML must be alone in its batch.
func planOnFor(ctx context.Context, conn *sql.Conn, mode executor.PlanMode) error {
	stmt := "SET STATISTICS XML ON"
	if mode == executor.PlanEstimated {
		stmt = "SET SHOWPLAN_XML ON"
	}
	_, err := conn.ExecContext(ctx, stmt)
	return err
}

// planOff turns plans off again with its own short deadline (the run may have been cancelled),
// and reports whether it succeeded.
func planOff(conn *sql.Conn, mode executor.PlanMode) bool {
	ctx, cancel := context.WithTimeout(context.Background(), restoreTimeout)
	defer cancel()
	stmt := "SET STATISTICS XML OFF"
	if mode == executor.PlanEstimated {
		stmt = "SET SHOWPLAN_XML OFF"
	}
	_, err := conn.ExecContext(ctx, stmt)
	return err == nil
}

// completePlans records whether the plans are complete, and why not.
func completePlans(p *executor.PlanSummary, o executor.Outcome, maxBytes int64) {
	p.Complete = o == executor.OutcomeSucceeded && p.TooLarge == 0
	switch {
	case o != executor.OutcomeSucceeded:
		p.Reason = fmt.Sprintf("The run ended early (%s): these are the plans received before it stopped.", o)
	case p.TooLarge > 0:
		limit := maxBytes
		if limit <= 0 {
			limit = executor.DefaultMaxPlanBytes
		}
		p.Reason = fmt.Sprintf("%d plan(s) were larger than the %d MB plan limit and were not kept.", p.TooLarge, limit>>20)
	}
}

// planCapture receives the plan documents of one batch execution.
type planCapture struct {
	mode      executor.PlanMode
	max       int64
	batch     int
	execution int
	statement int
	sink      executor.PlanSink
	sum       *executor.PlanSummary
}

// read takes the plan documents of a showplan result set. Plans are never logged.
func (c *planCapture) read(rows *sql.Rows) error {
	for rows.Next() {
		var raw any
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		var xml string
		switch v := raw.(type) {
		case string:
			xml = v
		case []byte:
			xml = string(v)
		default:
			continue
		}
		c.statement++
		if int64(len(xml)) > c.max {
			c.sum.TooLarge++
			continue
		}
		c.sum.Count++
		if c.sink != nil {
			if err := c.sink.Plan(executor.Plan{Mode: c.mode, Batch: c.batch, Execution: c.execution, Statement: c.statement, XML: xml}); err != nil {
				return err
			}
		}
	}
	return nil
}
