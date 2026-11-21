package security

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/TuanFaied/qryvanta/internal/executor"
	"github.com/TuanFaied/qryvanta/internal/parser"
)

// ReadOnlyRefusals is execution policy layer 1 (architecture D5): it decides from the parsed
// script whether read-only mode may run it, and returns every reason it may not. An empty
// result means the script may run (the session guard, layer 2, still applies).
//
// res must come from a parse with parser.Options{Model: true}. The check refuses what it cannot
// verify instead of guessing: a script with syntax errors, a result without a model, statements
// the model does not describe, and effect kinds it does not know are all refused.
//
// What it allows:
//   - SELECT (CTEs, set operations, FOR XML/JSON), and SELECT ... INTO a local temporary table
//   - INSERT, UPDATE, DELETE, MERGE and TRUNCATE whose every target is a local temporary table
//     (#name) or a table variable; CREATE TABLE, CREATE INDEX and DROP TABLE on #tables
//   - DECLARE (not cursors), SET @variable, harmless SET options, IF, WHILE, BEGIN...END,
//     TRY...CATCH, PRINT, RAISERROR, THROW, RETURN, BREAK, CONTINUE, GOTO, WAITFOR DELAY/TIME
//
// Everything else is refused: writes to real tables (including through OUTPUT INTO and CTEs),
// DDL, EXEC and dynamic SQL, transaction statements, USE, cursors, DBCC, BULK INSERT,
// OPENROWSET/OPENQUERY/OPENDATASOURCE, WAITFOR on a queue, and SET options that change
// transaction behaviour or the guard's lock timeout. Several statements in one batch are
// allowed when each one is.
func ReadOnlyRefusals(res *parser.Result) []executor.Refusal {
	c := &checker{seen: map[[2]int]bool{}}
	if res == nil || res.ModelVersion == 0 {
		return []executor.Refusal{{Line: 1, Column: 1,
			Reason: "The parser did not describe the script, so read-only mode cannot check it."}}
	}
	for _, d := range res.Diagnostics {
		c.out = append(c.out, executor.Refusal{Line: d.Line, Column: d.Column, Offset: d.Offset,
			Reason: fmt.Sprintf("Syntax error: %s Read-only mode refuses a script it cannot parse.", strings.TrimSpace(d.Message))})
	}
	if len(c.out) > 0 {
		return c.out
	}
	for _, b := range res.Batches {
		c.batch = b.Index
		for _, st := range b.Statements {
			c.statement(st)
		}
	}
	return c.out
}

type checker struct {
	batch int
	out   []executor.Refusal
	// seen drops a second refusal at the same place: effects are reported by every statement
	// that contains them, so a write inside IF is found at the IF and again at the write.
	seen map[[2]int]bool
}

func (c *checker) refuse(span parser.Span, statement, reason string) {
	key := [2]int{span.Offset, span.Length}
	if c.seen[key] {
		return
	}
	c.seen[key] = true
	c.out = append(c.out, executor.Refusal{Batch: c.batch, Line: span.Line, Column: span.Column,
		Offset: span.Offset, Length: span.Length, Statement: statement, Reason: reason})
}

func (c *checker) statement(st parser.Statement) {
	label := statementLabel(st)
	if reason := kindReason(st); reason != "" {
		c.refuse(st.Span, label, reason)
		return // its effects are inside it; one refusal per statement is enough
	}
	// Nested statements first, so a refusal names the innermost statement (DROP TABLE, not the
	// IF around it); the container's own effects then add only what no nested statement covers.
	for _, s := range st.Body {
		c.statement(s)
	}
	for _, s := range st.Else {
		c.statement(s)
	}
	for _, e := range st.Effects {
		c.effect(e, label)
	}
}

func (c *checker) effect(e parser.Effect, label string) {
	switch e.Kind {
	case parser.EffectWrite:
		if e.Variable != "" || isLocalTemp(e.Object) {
			return
		}
		if e.Object == nil {
			c.refuse(e.Span, label, "Writes to a target that read-only mode cannot identify. "+writesAllowed)
			return
		}
		c.refuse(e.Span, label, fmt.Sprintf("Writes to %s. %s", objectName(e.Object), writesAllowed))
	case parser.EffectExec:
		c.refuse(e.Span, "EXEC", execReason(e.Object))
	case parser.EffectRemote:
		c.refuse(e.Span, label, fmt.Sprintf("Reads through %s, which runs a query on another server or provider, "+
			"outside the protective transaction.", remoteName(e.Type)))
	default:
		c.refuse(e.Span, label, "Uses a construct that read-only mode does not recognise, so it refuses it.")
	}
}

const writesAllowed = "Read-only mode allows writes only to local temporary tables (#name) and table variables."

// kindReason returns why a statement of this kind is refused, or "" when its kind is allowed
// (its effects are checked separately).
func kindReason(st parser.Statement) string {
	switch st.Kind {
	case parser.StmtSelect, parser.StmtInsert, parser.StmtUpdate, parser.StmtDelete, parser.StmtMerge,
		parser.StmtTruncate, parser.StmtSet, parser.StmtIf, parser.StmtWhile, parser.StmtBlock,
		parser.StmtTry, parser.StmtPrint, parser.StmtReturn:
		return ""
	case parser.StmtDeclare:
		if st.Type == "DeclareCursorStatement" {
			return "Cursors are not supported in read-only mode."
		}
		return ""
	case parser.StmtDDL:
		if tempDDL[st.Type] && hasWrite(st) {
			return "" // the effects check that every object is a #table
		}
		return "Changes the database schema. Read-only mode allows only CREATE TABLE, CREATE INDEX and DROP TABLE on local temporary tables (#name)."
	case parser.StmtExec:
		return execReason(st.Procedure)
	case parser.StmtSetOption:
		return setOptionReason(st)
	case parser.StmtTransaction:
		return "Read-only mode runs the script in a transaction that it rolls back afterwards; " +
			"transaction statements in the script would end or change it."
	case parser.StmtUse:
		return "Switches the database. Read-only mode checks what the login can change in the database you chose; " +
			"choose the database in the toolbar (or with --database) instead."
	case parser.StmtWaitFor:
		if st.Action == "delay" || st.Action == "time" {
			return ""
		}
		return "WAITFOR on a Service Broker queue (RECEIVE or GET CONVERSATION GROUP) removes messages from it."
	case parser.StmtOther:
		if allowedOther[st.Type] {
			return ""
		}
		return friendlyType(st.Type) + " is not allowed in read-only mode."
	default:
		return "Read-only mode cannot check this statement, so it refuses it."
	}
}

// tempDDL are the DDL statements allowed on local temporary tables.
var tempDDL = map[string]bool{"CreateTableStatement": true, "CreateIndexStatement": true, "DropTableStatement": true}

// allowedOther are statements the model does not give a kind of their own that cannot change data.
var allowedOther = map[string]bool{
	"RaiseErrorStatement": true, "ThrowStatement": true, "BreakStatement": true,
	"ContinueStatement": true, "GoToStatement": true, "LabelStatement": true,
}

func hasWrite(st parser.Statement) bool {
	for _, e := range st.Effects {
		if e.Kind == parser.EffectWrite {
			return true
		}
	}
	return false
}

func isLocalTemp(o *parser.ObjectName) bool {
	return o != nil && strings.HasPrefix(o.Name, "#") && !strings.HasPrefix(o.Name, "##")
}

func execReason(proc *parser.ObjectName) string {
	if proc == nil {
		return "Runs dynamic SQL, which read-only mode cannot check."
	}
	return fmt.Sprintf("Runs the procedure %s, which read-only mode cannot check: a procedure can change data "+
		"or end the protective transaction.", objectName(proc))
}

// SET options, by the names ScriptDom gives them.
var (
	// predicateSetAllowed: options that change how queries are evaluated or reported, not data
	// or transactions. ANSI_DEFAULTS is not here: it turns IMPLICIT_TRANSACTIONS on.
	predicateSetAllowed = map[string]bool{
		"NoCount": true, "QuotedIdentifier": true, "ConcatNullYieldsNull": true, "ArithAbort": true,
		"ArithIgnore": true, "NumericRoundAbort": true, "AnsiNulls": true, "AnsiPadding": true,
		"AnsiWarnings": true, "AnsiNullDfltOn": true, "AnsiNullDfltOff": true, "FmtOnly": true,
		"NoExec": true, "ParseOnly": true, "ForcePlan": true, "NoBrowsetable": true,
	}
	transactionOptions = map[string]bool{
		"ImplicitTransactions": true, "XactAbort": true, "CursorCloseOnCommit": true,
		"RemoteProcTransactions": true, "AnsiDefaults": true,
	}
	setCommandAllowed = map[string]bool{
		"Language": true, "DateFormat": true, "DateFirst": true, "DeadlockPriority": true, "QueryGovernorCostLimit": true,
	}
	optionSQLNames = map[string]string{
		"QuotedIdentifier": "QUOTED_IDENTIFIER", "ConcatNullYieldsNull": "CONCAT_NULL_YIELDS_NULL",
		"CursorCloseOnCommit": "CURSOR_CLOSE_ON_COMMIT", "ArithAbort": "ARITHABORT", "ArithIgnore": "ARITHIGNORE",
		"FmtOnly": "FMTONLY", "NoCount": "NOCOUNT", "NoExec": "NOEXEC", "NumericRoundAbort": "NUMERIC_ROUNDABORT",
		"ParseOnly": "PARSEONLY", "AnsiDefaults": "ANSI_DEFAULTS", "AnsiNullDfltOff": "ANSI_NULL_DFLT_OFF",
		"AnsiNullDfltOn": "ANSI_NULL_DFLT_ON", "AnsiNulls": "ANSI_NULLS", "AnsiPadding": "ANSI_PADDING",
		"AnsiWarnings": "ANSI_WARNINGS", "ForcePlan": "FORCEPLAN", "ShowPlanAll": "SHOWPLAN_ALL",
		"ShowPlanText": "SHOWPLAN_TEXT", "ShowPlanXml": "SHOWPLAN_XML", "ImplicitTransactions": "IMPLICIT_TRANSACTIONS",
		"RemoteProcTransactions": "REMOTE_PROC_TRANSACTIONS", "XactAbort": "XACT_ABORT",
		"DisableDefCnstChk": "DISABLE_DEF_CNST_CHK", "NoBrowsetable": "NO_BROWSETABLE",
		"Language": "LANGUAGE", "DateFormat": "DATEFORMAT", "DateFirst": "DATEFIRST",
		"DeadlockPriority": "DEADLOCK_PRIORITY", "LockTimeout": "LOCK_TIMEOUT",
		"QueryGovernorCostLimit": "QUERY_GOVERNOR_COST_LIMIT", "ContextInfo": "CONTEXT_INFO",
	}
)

func optionName(o string) string {
	if n, ok := optionSQLNames[o]; ok {
		return n
	}
	return strings.ToUpper(o)
}

func setOptionReason(st parser.Statement) string {
	switch st.Type {
	case "SetStatisticsStatement", "SetTransactionIsolationLevelStatement", "SetRowCountStatement", "SetTextSizeStatement":
		return ""
	case "PredicateSetStatement":
		for _, o := range st.Options {
			switch {
			case predicateSetAllowed[o]:
			case transactionOptions[o]:
				return fmt.Sprintf("SET %s changes how transactions behave, which would weaken the transaction "+
					"read-only mode rolls back.", optionName(o))
			case strings.HasPrefix(o, "ShowPlan"):
				return fmt.Sprintf("SET %s returns plans instead of running the script; execution plans are not supported yet.", optionName(o))
			default:
				return fmt.Sprintf("SET %s is not allowed in read-only mode.", optionName(o))
			}
		}
		if len(st.Options) == 0 {
			return "Read-only mode cannot tell which option this SET changes, so it refuses it."
		}
		return ""
	case "SetCommandStatement":
		for _, o := range st.Options {
			switch {
			case setCommandAllowed[o]:
			case o == "LockTimeout":
				return "SET LOCK_TIMEOUT would replace the lock timeout read-only mode sets (the lock timeout in Settings)."
			default:
				return fmt.Sprintf("SET %s is not allowed in read-only mode.", optionName(o))
			}
		}
		if len(st.Options) == 0 {
			return "Read-only mode cannot tell which option this SET changes, so it refuses it."
		}
		return ""
	case "SetIdentityInsertStatement":
		return "SET IDENTITY_INSERT is for inserting explicit values into a table's identity column; read-only mode does not allow writes to tables."
	default:
		return friendlyType(st.Type) + " is not allowed in read-only mode."
	}
}

func statementLabel(st parser.Statement) string {
	switch st.Kind {
	case parser.StmtSelect:
		if st.Into != nil {
			return "SELECT INTO"
		}
		return "SELECT"
	case parser.StmtTruncate:
		return "TRUNCATE TABLE"
	case parser.StmtBlock:
		return "BEGIN...END"
	case parser.StmtTry:
		return "TRY...CATCH"
	case parser.StmtSetOption:
		return "SET"
	case parser.StmtTransaction:
		switch st.Action {
		case "begin":
			return "BEGIN TRANSACTION"
		case "commit":
			return "COMMIT"
		case "rollback":
			return "ROLLBACK"
		case "save":
			return "SAVE TRANSACTION"
		}
		return "TRANSACTION"
	case parser.StmtDDL, parser.StmtOther, parser.KindUnsupported:
		return friendlyType(st.Type)
	}
	return strings.ToUpper(st.Kind)
}

// friendlyType turns a ScriptDom class name into SQL words: BulkInsertStatement -> BULK INSERT.
func friendlyType(t string) string {
	t = strings.TrimSuffix(t, "Statement")
	if t == "" {
		return "This statement"
	}
	var b strings.Builder
	for i, r := range t {
		if i > 0 && unicode.IsUpper(r) && !unicode.IsUpper(rune(t[i-1])) {
			b.WriteByte(' ')
		}
		b.WriteRune(unicode.ToUpper(r))
	}
	return b.String()
}

func remoteName(t string) string {
	switch t {
	case "OpenQueryTableReference":
		return "OPENQUERY"
	case "AdHocTableReference":
		return "OPENDATASOURCE"
	default:
		return "OPENROWSET"
	}
}

func objectName(o *parser.ObjectName) string {
	parts := make([]string, 0, 4)
	for _, p := range []string{o.Server, o.Database, o.Schema, o.Name} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ".")
}
