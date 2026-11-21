# Qryvanta Architecture

Status: living document. Update it whenever a decision below changes.

## 1. Purpose

Qryvanta is a desktop SQL execution debugging tool. It helps developers understand
*why* a query that runs successfully returns the wrong rows: where rows were dropped,
where they were multiplied, and which predicates or NULLs caused it.

It is deterministic. Every finding is produced by a rule, backed by evidence that came from
the query text, the database's metadata, an execution plan, or a probe query the user
approved. Nothing is guessed, and no execution statistics are ever invented.

Initial dialect: Microsoft SQL Server (T-SQL).

### Non-goals for v1

- A general-purpose database administration tool (no table designers, no data editing).
- Running write statements. v1 is read-only debugging.
- AI features of any kind. The design leaves room for an optional AI module later (section 10).

## 2. System overview

```
┌────────────────────────────── Desktop process (Qryvanta.exe) ──────────────────────────────┐
│                                                                                            │
│  React + TypeScript UI  (frontend/)                                                        │
│     │  generated, typed bindings (frontend/bindings)                                       │
│     ▼                                                                                      │
│  Desktop adapter  (internal/desktop)      ◄── the only package that imports Wails          │
│     │  plain Go calls                                                                      │
│     ▼                                                                                      │
│  Composition root (internal/app)          builds the core from config                      │
│     │                                                                                      │
│     ▼                                                                                      │
│  Core engine (no UI, no Wails)                                                             │
│   config · logging · security · connection · executor · parser · analyzer · diagnostics   │
│   plan · compare · session · report · storage                                              │
│     │                 │                     │                    │                         │
└─────┼─────────────────┼─────────────────────┼────────────────────┼─────────────────────────┘
      ▼                 ▼                     ▼                    ▼
  SQL Server       T-SQL parser sidecar    SQLite (local        OS credential store
  (go-mssqldb)     (.NET, ScriptDom,       metadata, history)   (Windows Credential
                    stdin/stdout JSON)                           Manager via go-keyring)

┌──────── CLI process (qryvanta-cli) ────────┐
│  cmd/qryvanta-cli → internal/app → core    │   same core, no Wails, JSON/Markdown output
└────────────────────────────────────────────┘
```

The core is used by two front ends from day one: the desktop app and the CLI. Using it from
two places is what keeps it honestly independent of Wails. Later front ends (a VS Code
extension, a web app) are expected to drive the CLI or a small local API, using the JSON
report as the contract.

## 3. Repository layout

The layout follows the original spec, with the changes listed in section 3.1.

```
cmd/
  qryvanta/            Desktop entry point (Wails application)
  qryvanta-cli/        Command-line entry point
internal/
  app/                 Composition root: wires config, logging and core services
  desktop/             Wails adapter: App, Connection and Schema services (DTOs only)
  config/              Load, validate, and atomically save user configuration
  logging/             slog setup, file logging, secret redaction
  security/            Redaction helpers, execution policy (Phase 2+)
  version/             Build version information
  fsutil/              Atomic file writes shared by config and connections
  connection/          Connection profiles, keyring credentials; (Phase 2) pooling
  dialect/mssql/       SQL Server driver (DSN, server info, errors) and metadata reader
  schema/              Product-neutral metadata model and the schema.Reader interface
  executor/            Query execution model: requests, limits, streamed results, messages
  parser/              Parser interface and results; parser/sidecar is the ScriptDom client
                       (Phase 4: structured query model next)
  analyzer/            Rule interface, registry and engine that turn the QueryModel into findings
  diagnostics/         Finding, Evidence, Severity, Classification and their validation
  instrumentation/     (Phase 5/7) Probe query generation and approval workflow
  plan/                (Phase 6) Showplan XML parsing into an operator graph
  compare/             (Phase 7) Deterministic result comparison
  session/             (Phase 8) Debugging sessions and history
  report/              (Phase 8) JSON / Markdown / HTML report rendering
  storage/             (Phase 8) SQLite persistence and retention
sidecar/
  tsqlparser/          (Phase 4) .NET ScriptDom parser host
frontend/              React + Vite + Tailwind + shadcn/ui
docs/                  Architecture, features, development, security
tests/
  integration/         End-to-end tests against the fixture database (build tag `integration`)
  fixtures/mssql/      QryvantaSample: schema, problem data, read-only login, loader script
  sample_queries/      10 buggy queries with corrected versions and expected results
docker-compose.yml     SQL Server 2022 Developer + one-shot fixture loader
build/                 Wails build assets (icons, manifests, installers)
scripts/               Developer scripts
```

Packages are created when the phase that needs them starts. There are no empty
placeholder packages.

### 3.1 Changes from the original spec, and why

| Spec | Here | Reason |
|---|---|---|
| `internal/api` | `internal/desktop` | The package is a Wails adapter, not a general API. The name says which front end it serves. |
| `pkg/interfaces` | none | In Go, interfaces belong to the package that consumes them. A shared "interfaces" package creates import cycles and false abstraction. |
| `pkg/models` | none for now | Nothing outside this module imports Go types yet. The stable public contract is the **JSON report schema** (Phase 8). A `pkg/` package is added only when there is a real external Go consumer. |
| `internal/result` | inside `executor` | The result set types are owned by the executor that produces them. |
| — | `internal/compare` | Result comparison is a separate, pure, heavily tested algorithm. |
| — | `internal/logging`, `internal/version` | Logging needs redaction logic that deserves its own package. |
| — | `sidecar/tsqlparser` | The .NET parser host (decision D2). |

## 4. Dependency rules

1. Core packages (everything in `internal/` except `desktop`) **must not** import Wails or
   anything under `frontend/`. A test (`internal/archtest`) fails the build if they do.
2. `internal/desktop` may import the core. The core never imports `desktop`.
3. `internal/app` is the only place that constructs concrete implementations
   (SQL Server dialect, sidecar parser, SQLite store, keyring). Everything else receives
   them through small interfaces defined by the consumer.
4. The frontend contains no SQL execution logic, no credential handling, and no diagnostic
   rules. It renders what the core returns.
5. Stored credentials never cross into JavaScript. The frontend refers to a connection by ID.
   A password moves in one direction only: the connection dialog may send a new one
   (`desktop.ConnectionInput`, with a keep, set or clear action), but no bound method ever
   returns one. Responses carry `hasPassword` instead, and a test checks that no response
   contains a password field.

## 5. Key decisions

### D1. Wails v3

Wails v3 reached GA on 2026-10-26 (v3.0.0). v3.1.0 is used. v3 offers a service model for
bindings, multiple windows, and typed event bindings. Since the project has no deadline,
starting on v3 avoids a v2 → v3 migration later.
Risk: v3 is new as a stable release. Mitigation: the desktop adapter is thin, so the core is
unaffected by framework churn.

### D2. T-SQL parsing: .NET ScriptDom sidecar behind a Go interface

No pure-Go T-SQL parser is reliable enough for diagnostics we put our name on.
Microsoft's `Microsoft.SqlServer.TransactSql.ScriptDom` (MIT-licensed) is the parser SQL
Server tooling itself uses.

- `parser.Parser` is a Go interface: `Parse(ctx, sql) (*QueryModel, []ParseDiagnostic, error)`.
- The v1 implementation launches `sidecar/tsqlparser` (a self-contained, single-file .NET
  executable shipped next to the app) and talks to it over stdin/stdout with
  newline-delimited JSON. One long-lived process, request IDs, a timeout per request,
  restart on crash.
- The sidecar converts ScriptDom's AST into Qryvanta's own `QueryModel` JSON (tables, joins,
  predicates, grouping, CTEs, set operations, aggregates, windows). The Go side never sees
  ScriptDom types, so a future pure-Go parser can replace the sidecar behind the same interface.
- If the sidecar is missing or fails, query execution still works. Structural diagnostics
  report "parser unavailable" and are not guessed.

Regular expressions are never used as the parser.

**The sidecar (#39).** `sidecar/tsqlparser`:
- **Build:** .NET 10 (LTS until November 2028) and ScriptDom 180.x, published as a
  self-contained, trimmed single file of about 13 MB. Users need no .NET installation.
  - ScriptDom is kept whole during trimming, because its ANTLR token factory finds token
    classes by name.
  - The trimmed build gave identical output to the untrimmed one on the sample queries and
    other statement kinds. CI smoke-tests the published file.
- **Protocol 1:** UTF-8, one JSON object per line.
  - The sidecar first writes `hello` (protocol version, sidecar and ScriptDom versions,
    supported compatibility levels, size limit).
  - For each request `{id, op, sql?, compatLevel?}` it writes exactly one response, in order:
    `{id, ok, compatLevel, batches, diagnostics}` or `{id, ok: false, error: {code, message}}`.
  - Ops are `parse` and `ping`. Error codes are `bad_request`, `unknown_op`, `too_large`,
    `unsupported_compat_level` and `internal`. A request the sidecar cannot read is answered
    with `id: null`, and it keeps serving.
  - Offsets and lengths are UTF-16 code units, as in .NET and JavaScript strings (the Go client
    converts them). Lines and columns are 1-based.
  - Only protocol messages go to stdout; logs go to stderr.
- **Limits:** SQL up to 16 Mi characters; request lines up to 2 × that. Clients must not escape
  non-ASCII or HTML characters.
- **Compatibility levels:** 80–180, default 170 (SQL Server 2025).
- **Batches:** split on `GO` lines using ScriptDom's token stream, so a `GO` inside a string or
  comment is not a separator, and splitting works even when the script has syntax errors.
  - `GO n` is reported as `repeat: n`.
  - Empty batches (whitespace or comments only) are skipped.
- **Per-batch parsing:** each batch is parsed on its own, as SQL Server receives it. Diagnostic
  positions are mapped back to the whole script. Parsing per batch matters:
  - `GO n` is not T-SQL, and would otherwise be reported as a syntax error.
  - A procedure call without `EXEC` is valid only as the first statement of a batch.

**The QueryModel (#41).** A `parse` request with `model: true` gets, for each batch without
syntax errors, its `statements`. A batch with errors has none, because a broken tree is never
guessed at. The C# DTOs (`sidecar/tsqlparser/Model.cs`) and the Go types
(`internal/parser/model.go`) mirror each other.
- **Statements** carry a kind (select, insert, update, delete, merge, truncate, ddl, exec,
  declare, set, setOption, if, while, block, try, transaction, use, print, waitfor, return,
  other) and the ScriptDom class name.
  - CTEs, with recursion detected.
  - The SELECT query, the `INTO` target, and the DML target, FROM and WHERE.
  - IF/WHILE conditions, and nested bodies for IF, WHILE, BEGIN…END and TRY/CATCH, so a check
    can find an `EXEC` inside an `IF`.
- **Queries:**
  - `select` (DISTINCT, TOP, select list, FROM, WHERE, GROUP BY, HAVING) or `setOp` (UNION
    [ALL], EXCEPT, INTERSECT).
  - ORDER BY and OFFSET/FETCH belong to the whole query.
  - **Set operations follow SQL Server's precedence.** ScriptDom nests them strictly left to
    right, so `a UNION b INTERSECT c` would read as `(a UNION b) INTERSECT c`. The builder
    flattens each unparenthesized chain and rebuilds it with INTERSECT first, then UNION, UNION
    ALL and EXCEPT left to right, giving `a UNION (b INTERSECT c)`. The golden tests found this.
- **Sources:**
  - Kinds: table (flagged when it names a CTE in scope), derived table, join, table-valued
    function, table variable, VALUES.
  - Join types: inner, left, right, full, cross, crossApply, outerApply, and **comma**. An
    old-style comma list becomes left-deep `comma` joins with no `on`, so a missing join
    predicate is directly visible.
- **Expressions** are full trees: AND/OR/NOT, comparisons (including ANY/ALL), IN (list or
  subquery), BETWEEN, LIKE, IS [NOT] NULL, EXISTS, columns (identifier parts), literals,
  variables, functions (aggregates flagged; `OVER` with partition, order and frame),
  subqueries, arithmetic, CAST/CONVERT and CASE.
- **Rules:**
  - Every node has a script-absolute UTF-16 span (`parser.SpanText` slices the script in Go).
  - Parentheses are not nodes; the wrapped node gets `parenthesized`. So `a OR b AND c` reads
    as `OR(a, AND(b, c))`, and a consumer still knows whether the author wrote the grouping.
  - Identifiers are unquoted, and column references are left unresolved: binding names to
    metadata is Phase 5 work, in Go.
  - Anything not modelled is kind `unsupported` with its ScriptDom class, never dropped.
    Expressions nested more than 256 deep are cut off as `unsupported` (`TooDeep`) instead of
    risking the stack.
  - Empty lists, absent values and false flags are omitted from the JSON.
- **Versioning:** `modelVersion` (1) changes only when a field's meaning changes or a field
  is removed. Adding fields or kinds doesn't change it, and consumers ignore what they don't
  know. The Go client refuses a model version it doesn't know rather than misreading it.

**The Go client (#40).** `internal/parser` defines `Parser` (`Parse`, `Check`, `Close`) and the
result types. `internal/parser/sidecar` implements it:
- **Lifecycle:** one long-lived process, started on first use, and closed by `app.Core.Close`.
  If the app is killed, the sidecar sees end of input and exits too.
- **Location:** next to the executable (`wails3 build` puts it there), or at
  `QRYVANTA_PARSER_PATH` for development. On Windows it runs with `CREATE_NO_WINDOW`, so the GUI
  app never flashes a console.
- **Ordering:** requests are serialized through a semaphore that respects the caller's context.
  Responses must carry the request's ID.
- **Failure handling:**
  - **Timeout (5 s) or misbehaviour** (wrong ID, invalid JSON, a crash): the process is killed
    and the next request starts a new one.
  - **The caller gives up** (context cancelled): the process is kept. Its response is read and
    discarded in the background before the next request goes through, because the editor
    cancels parses constantly and a restart costs about 0.5 s.
  - **Error responses** (`too_large`, `unsupported_compat_level`, …) are returned as
    `ProtocolError` and do not restart anything.
  - **Repeated start failures** (missing file, no hello, wrong protocol): after three in a row,
    `Parse` fails fast with `parser.ErrUnavailable` for 30 s, then tries again.
- **Positions:** the client adds byte offsets (`ByteOffset`, `ByteLength`) next to the
  sidecar's UTF-16 offsets, so Go code can slice the script and the editor can use the UTF-16
  ones.
- **Logging:** the client logs lifecycle events, and the sidecar's stderr at debug level, never
  SQL text.
- **Visible state:** the desktop status bar shows "Parser unavailable", with the reason, when
  `Check` fails, and `qryvanta-cli parse` prints batches and syntax errors.

### D3. Evidence model

A **finding** has: rule ID, title, explanation, severity
(Critical/High/Medium/Low/Informational), classification
(Confirmed issue / Potential issue / Optimization suggestion), source location in the SQL,
a list of **evidence** items, and an optional suggested correction (text only, never
applied automatically).

Evidence types, from cheapest to most expensive:

1. **Static**: comes from the parsed query alone (`col = NULL`, `a OR b AND c`, a JOIN with no predicate).
2. **Metadata**: comes from catalog views (nullability, keys, unique indexes, data types of join columns).
3. **Plan**: comes from an estimated or actual showplan (operator row counts, warnings, implicit conversions).
4. **Probe**: comes from a generated read-only query (for example, matches per join key).

A finding may be **Confirmed** only when its evidence proves it (for example, `= NULL` is always
a confirmed issue, and a measured fan-out of 4.2× is confirmed row multiplication).
Otherwise it is **Potential**. This rule is enforced in code: a confirmed finding with
no evidence fails validation.

**Implementation (#66).**

- **Types:** `internal/diagnostics` defines the types:
  - `Finding{RuleID, Title, Explanation, Severity, Classification, Span, Evidence, Correction}`.
  - `Severity`: `critical`, `high`, `medium`, `low`, `informational`.
  - `Classification`: `confirmed`, `potential`, `optimization`.
  - `Evidence{Kind, Summary}` with exactly one payload for its kind: `StaticEvidence{Span, Text}`,
    `MetadataEvidence{Object, Column, Property, Value}`,
    `PlanEvidence{NodeID, Operator, Property, Value, Actual}` or `ProbeEvidence{SQL, Result}`.
  - `Correction{Description, Span, Replacement}`.
  - Spans are UTF-16 offsets into the analyzed SQL, like the parser's.
- **Validation:** `diagnostics.Validate` rejects:
  - a confirmed finding without evidence,
  - a span (of the finding, its static evidence or its correction) that is empty or not inside
    the SQL,
  - unknown severities, classifications or evidence kinds,
  - evidence without its payload.
- **Rules:** `internal/analyzer` defines `Rule` (`Info()`: ID, title, default severity, evidence
  kinds; `Check(*Input)`) and a `Registry` that requires IDs of the form `QRY-<AREA>-<NNN>` and
  unique. The rules are listed in [docs/features/rules.md](../features/rules.md), and a test fails
  when a built-in rule is missing there.
- **The engine:** `Engine.Analyze`:
  - It runs every rule and fills a finding's rule ID and default severity from its rule.
  - It validates each finding, and also rejects evidence of a kind the rule does not declare.
  - It drops invalid findings and turns them, errors and panics into `RuleFailure`s; the other
    rules still run.
  - It orders findings by offset (the enclosing one first), then severity, rule ID and title.
- **Callers:** `app.Core.Analyze` parses with the model (batches with syntax errors are not
  analyzed) and logs rule failures. `desktop.AnalysisService.Analyze` serves the analysis panel.
  It reports an unavailable parser separately from "no findings".
- **The panel:**
  - Analyze runs on what Execute would run: the selection or the whole tab.
  - Findings are grouped by severity, with their classification, evidence and the correction as
    a diff.
  - Clicking a finding selects its span in the editor.
  - A correction is never applied (D4).

### D4. Probes versus modifications

These are two different things with two different rules.

- A **modification** changes the user's query (a suggested fix). It is shown as a diff and
  **never executed** unless the user explicitly runs it themselves.
- A **probe** is a generated, read-only diagnostic query (`COUNT`, `COUNT(DISTINCT)`,
  `GROUP BY key HAVING COUNT(*) > 1`, and so on) whose only job is to collect evidence. Probes:
  - are produced only from a fixed set of templates in `internal/instrumentation`,
  - always run under the execution policy and the same limits as user queries, with their own
    shorter timeout,
  - are listed in full (SQL text included) before they run,
  - run only after the user approves the list, once per debugging run ("Run 3 probes"),
  - are recorded in the session and report with their SQL and results.

There is a "probe budget" setting (maximum probes per run, maximum estimated cost) so a
debugging run cannot turn into a heavy workload.

### D5. Execution policy (read-only by default)

Checking only the statement type is not enough: functions, `EXEC`, linked servers,
and `SELECT ... INTO` can all have side effects. The policy has several layers, and the
documentation states clearly which guarantees are hard and which are best effort.

1. **Statement allowlist** (implemented, hard): `security.ReadOnlyRefusals` decides from the
   parsed script (QueryModel) whether read-only mode may run it. `app.Core.Execute` applies it
   before anything is sent to the server, for the desktop app and the CLI alike, so no caller can
   skip it. A refused script ends with outcome `refused` and one `Refusal` per statement
   (position, statement kind, plain-language reason).
   - **Allowed:** `SELECT` (CTEs, set operations, `FOR XML/JSON`); `DECLARE` of variables and
     table variables, `SET @var`; `IF`, `WHILE`, `BEGIN...END`, `TRY...CATCH`, `PRINT`,
     `RAISERROR`, `THROW`, `RETURN`, `BREAK`, `CONTINUE`, `GOTO`; `WAITFOR DELAY/TIME`; SET options
     that change how queries are evaluated or reported (`NOCOUNT`, `ANSI_*` except
     `ANSI_DEFAULTS`, `QUOTED_IDENTIFIER`, `STATISTICS`, `DATEFORMAT`, `LANGUAGE`, isolation
     level, `ROWCOUNT`, ...).
   - **Temporary objects are allowed.** `INSERT`/`UPDATE`/`DELETE`/`MERGE`/`TRUNCATE` and
     `SELECT ... INTO` whose every target is a local temporary table (`#name`) or a table
     variable, and `CREATE TABLE`, `CREATE INDEX` and `DROP TABLE` on `#tables`. They live in the
     session's tempdb space and are rolled back with the guard, and debugging scripts need them
     (the `CREATE TABLE #t` / `GO` / `SELECT ... FROM #t` pattern). Global `##tables` are shared,
     so they are refused.
   - **Several statements in one batch are allowed** when each one is. The session guard
     (layer 2) still applies to all of them, so refusing them would only make scripts harder to
     run without adding protection.
   - **Refused:** writes to any other table, also when hidden in `OUTPUT ... INTO`, a CTE used
     as a DML target, or an alias; all other DDL; `EXEC` (procedures, `sp_executesql`, dynamic
     SQL, `INSERT ... EXEC`); transaction statements, which would end or nest the guard's
     transaction; `USE`, because the write-access check and the toolbar refer to the chosen
     database; cursors; `DBCC`, `BULK INSERT`, `GRANT` and every other statement not listed;
     `OPENROWSET`, `OPENQUERY` and `OPENDATASOURCE`; `WAITFOR` on a Service Broker queue; SET
     options that change transactions (`IMPLICIT_TRANSACTIONS`, `XACT_ABORT`, `ANSI_DEFAULTS`,
     ...), `LOCK_TIMEOUT` (it would replace the guard's), `SHOWPLAN_*`, `IDENTITY_INSERT` and
     `CONTEXT_INFO`.
   - **How it sees everything:** the sidecar adds `effects` to each statement (objects written,
     procedures or dynamic SQL executed, remote rowsets read), collected by a visitor over the
     full ScriptDom tree rather than from the QueryModel, so a write in a construct the model
     does not describe is still found. Keywords in comments and strings are never statements.
   - **Refuse rather than guess:** a script with syntax errors, a parse without a model, a
     statement the model marks unsupported, and an unknown effect kind are all refused. When the
     parser is unavailable, read-only mode runs nothing and says why; turning read-only mode off
     runs scripts without the check. Matching keywords in the SQL text is never used instead.
   - **Residual risks (layer 2 still applies):** scalar and table-valued functions can call
     `xp_` procedures or CLR code with side effects; four-part names read through linked servers,
     whose queries run outside the transaction; `NEXT VALUE FOR` consumes sequence values, which
     a rollback does not return.
   - There is no "run once without the check" button: the policy is not a caller choice (see
     the session guard below). The Messages tab offers "Turn off read-only mode…", which opens
     Settings and its confirmation, and the CLI suggests `--allow-writes`.
2. **Session guard** (implemented, best effort): `mssql.Executor` runs each read-only batch on a
   dedicated connection with `SET IMPLICIT_TRANSACTIONS OFF`, `SET LOCK_TIMEOUT`
   (`execution.lockTimeoutSeconds`, default 5 s), and `BEGIN TRANSACTION`. After the batch,
   including after errors, timeouts, cancellation and truncation, the transaction is rolled back
   and the lock timeout reset. If that fails, the connection is discarded instead of being
   returned to the pool.
   - **`XACT_ABORT` stays at the server default (OFF) on purpose.** With it ON, any runtime
     error would abort the rest of the batch, so read-only mode would behave differently from
     SSMS and from writes-allowed mode. The rollback already undoes everything.
   - **What it cannot prevent:** a `COMMIT` or `ROLLBACK` inside the batch ends the protective
     transaction early, and later statements run unprotected. After the batch the executor
     checks `@@TRANCOUNT` and reports this as `Summary.Guard.EndedByBatch`, and the app logs a
     warning. The statement allowlist (layer 1) now refuses transaction statements and `EXEC`
     before they run, so this check remains as a second line of defence.
   - **Scripts with `GO`:** one protective transaction spans all the batches of a script, so
     a temp table or `SET` option from one batch is visible to the next, as in SSMS (a
     transaction per batch would roll back `CREATE TABLE #t` before the batch that reads it).
     After each batch the executor checks `@@TRANCOUNT`. If the batch ended the transaction,
     `EndedByBatch` is reported and a new protective transaction starts before the next batch,
     so only the rest of that one batch ran unprotected.
   - **What it does not undo:** effects outside the transaction, such as linked servers, `xp_`
     procedures, CLR, Database Mail, consumed identity and sequence values, and statements that
     refuse to run in a user transaction (which fail instead).
   - **Not a caller choice:** `app.Core.Execute` always takes the policy from the settings.
     Turning read-only mode off in Settings requires a confirmation.
3. **Least-privilege guidance** (strongest): a read-only login is the only true guarantee, and
   the documentation says so. Qryvanta reports what the current login could change:
   - `connection.AccessChecker` (implemented by `mssql.Access`) runs a read-only permission
     query inside the target database through `EXEC [db].sys.sp_executesql`, so the pooled
     session's database never changes. It looks at, in order:
     - server role and permission (`sysadmin`, `CONTROL SERVER`)
     - the `db_owner`, `db_datawriter` and `db_ddladmin` roles
     - database permissions (`fn_my_permissions`: INSERT/UPDATE/DELETE/ALTER/CONTROL/CREATE …)
     - table-level grants (`HAS_PERMS_BY_NAME`)
     - EXECUTE on the database or on procedures
   - The result is `can_write` (any direct permission), `may_write` (only the ability to
     execute code that could write), `read_only`, or `unknown`. A failed check never fails the
     connection, because a login without permission to read its own permissions is still usable.
   - The result appears in the connection test (dialog and CLI) and in the status bar for the
     current database, and is rechecked whenever that database changes. Session reports
     (Phase 8) will include it.
   - It is guidance, not a guarantee. Permissions can change, and some paths (ownership
     chaining, signed modules, linked servers) are not visible to the login.
4. **Limits**: timeout, maximum rows, maximum result bytes, cancellation through `context.Context`.

### D6. Execution plans

- Estimated: `SET SHOWPLAN_XML ON` (the query is not executed).
- Actual: `SET STATISTICS XML ON` (the query **is** executed, under the full policy). Actual
  plans give actual rows per physical operator. This is real, measured evidence for
  "where were rows dropped", without rewriting the query.
- The UI always labels values as *estimated* or *actual*. A missing value is shown as
  unavailable, never filled in.
- Physical operator order is not logical clause order. Mapping operators back to clauses is
  done only where the plan's predicate and object references make it unambiguous.

### D7. Local storage and credentials

- Config: JSON at `%AppData%\Qryvanta\config.json` (the OS user config directory), written atomically.
- Metadata and history: SQLite through `modernc.org/sqlite` (pure Go, so the build stays
  `CGO_ENABLED=0`). By default it stores SQL text, timings, row counts, and findings, but
  **no result rows**. A retention policy prunes old sessions.
- Connection profiles: `connections.json` next to `config.json`, written atomically. Profiles
  have no password field.
- Secrets: Windows Credential Manager through `github.com/zalando/go-keyring`, as generic
  credentials named `Qryvanta:connection:<profile id>`. Passwords are never logged, and
  connection strings are never logged in full.
- `connection.Manager` keeps the two consistent. A failed save rolls back the credential
  change, and deleting a profile removes its credential first, so a secret is never orphaned.
  Windows-authentication profiles never store a password.

### D7a. Connecting to SQL Server

- `connection.Driver` is a small interface defined by its consumer, `connection.Pool`. It
  has two methods: `Open` and `ServerInfo`. `internal/dialect/mssql` implements it with
  `github.com/microsoft/go-mssqldb`. Later dialects add their own packages under
  `internal/dialect/`, and the interface grows only when a feature needs it. Metadata got its
  own consumer-side interface instead (`schema.Reader`, see D7b), and plans will do the same
  in Phase 6.
- The DSN is built with `net/url`, which escapes every component, and parsed by the driver's
  own `msdsn.Parse`. Values are never concatenated into a connection string. The unit tests
  check what the driver parses, including passwords containing `@:/?#&;=%'"`.
- Encryption is `mandatory` by default, with `strict` (TDS 8) and `optional` also available.
  There is no option that disables encryption, because the login packet carries the password.
  The server certificate is validated unless the profile sets *Trust server certificate*, and
  certificate errors explain that choice to the user.
- Windows authentication sends no user name, so go-mssqldb uses SSPI as the current OS user.
  TCP is tried first, then named pipes and shared memory, so default local SQL Server Express
  installs (TCP disabled) work.
- `connection.Pool` keeps one `*sql.DB` per connected profile (4 open, 2 idle, 5-minute
  idle timeout). Every open is bounded by the connect timeout (profile setting, else the
  application setting) and by the caller's context. A connection test opens a temporary
  handle, runs a server-info query that needs no extra permissions, and closes it.
- Errors leaving the pool are sanitized: the password and `key=value` secrets are scrubbed,
  SQL Server errors keep their number, state and class (`connection.ServerError`), and
  `context.Canceled`/`DeadlineExceeded` stay detectable with `errors.Is`.

### D7b. Reading metadata

- `internal/schema` holds a product-neutral model (`Database`, `Object`, `Column`) and the
  `schema.Reader` interface: `Databases`, `Objects(database)`, `Columns(database, schema, object)`.
  The explorer, and later the analyzer's metadata rules (Phase 5), depend only on this package.
  `mssql.Metadata` is the SQL Server implementation, chosen in `internal/app`.
- Readers work through an already open `*sql.DB` from `connection.Pool`. They never connect
  on their own, and they read only catalog views, so the results respect the login's metadata
  visibility. A `db_datareader`-only login, for example, does not see procedures unless it
  has `VIEW DEFINITION`.
- Other databases are read through three-part names, so one pooled connection serves the
  whole server and the session's database context is never changed (`USE` would leak into
  pooled connections). The database name is first checked against `sys.databases` with
  `HAS_DBACCESS`, then quoted by `mssql.QuoteIdent`, which follows QUOTENAME's rules: brackets,
  `]` doubled, at most 128 characters, no NUL. Object names are passed as parameters
  (`OBJECT_ID(@full)`). Integration tests send hostile names to confirm both.
- `desktop.SchemaService` gives each request a 30-second timeout and returns failures as data
  (`not_connected`, `not_found`, `permission`, `timeout`, `cancelled`, `server`), so the
  explorer can show them on the node that failed.
- The frontend loads each tree node only when it is expanded and caches it per connection.
  Refresh, disconnect, delete and switching connections drop that cache. The *current
  database* is a per-connection UI selection (default: the login's database). Query
  execution (Phase 3) will run against it.

### D7c. Executing queries

- `internal/executor` defines a product-neutral model (`Request`, `Limits`, `Column`,
  `Message`, `Summary`), a streaming `Sink`, and the `Runner` interface. `mssql.Executor` is the
  SQL Server runner, chosen in `internal/app`. `app.Core.Execute` runs a request on a connected
  profile.
- **Streaming:** results reach the sink as column metadata followed by row batches of 500, so
  a result is never held in memory twice. Messages (`PRINT`, informational `RAISERROR`) and
  errors (number, severity, state, line, procedure) arrive in server order through
  go-mssqldb's `sqlexp.ReturnMessage` stream. Each message records how many result sets
  preceded it, so the UI can interleave them. Row counts are reported per statement.
- **Limits:** timeout, maximum rows and maximum bytes (an estimate of the converted values)
  apply across the whole batch. When a limit is reached, the runner stops reading and cancels
  the query, which makes the driver send an attention signal so the server stops working. The
  outcome is `truncated`, with `rows` or `bytes` as the reason. Timeout and caller cancellation
  are reported as `timed_out` and `cancelled`.
- **Database context:** the batch runs on one dedicated pooled connection, switched to the
  requested database with a quoted `USE`. The connection's original database is always
  restored afterwards, even if the batch ran its own `USE`, so the next user of that pooled
  connection never inherits a different context. If restoring fails, the connection is
  discarded instead of returned to the pool.
- **Values are display-safe:**
  - Exact numerics (`decimal`, `money`) stay strings.
  - Integers outside JavaScript's safe range become strings.
  - Dates and times use ISO 8601, at the type's precision.
  - GUIDs use SQL Server's byte order.
  - Binary values become `0x…` hex, capped at 512 bytes.
  - NULL stays a distinct value.
- **Privacy:** result rows are never logged. SQL text is logged only at debug level when
  `logging.logSqlText` is on.
- **`GO` batches.** `GO` is a client-side `sqlcmd`/SSMS convention, so `app.Core.Execute`
  splits a script with the parser (D2) into `Request.Batches`, which ignores `GO` inside
  strings and comments. The runner sends them one at a time on the same connection, so
  temp tables, `SET` options and `USE` carry over.
  - `GO n` runs a batch n times. Batches that hold only comments are left out.
  - **An error does not stop the script**, as in SSMS and `sqlcmd` without `-b`: server
    errors are recorded and the next batch runs. A timeout, cancellation, truncation or a
    lost connection stops the whole script. There is no setting for this yet.
  - Messages, result sets and `Summary.Batches` (one `BatchRun` per execution, with its start
    line and error count) carry the batch index. `Message.ScriptLine` is the server's line
    mapped to the script (batch start line + line − 1); it is 0 inside a procedure.
  - Limits apply to the whole script, not to each batch.
  - **Without the parser**, a script that has a line holding only `GO` is refused with a clear
    message instead of being sent as one batch (which would fail at the first `GO`). A script
    without such a line runs as one batch.
- **Session guard:** in read-only mode the runner wraps the batch in the D5 session guard
  (`Request.Policy`, reported in `Summary.Guard`).

### D8. Logging

`log/slog` with JSON output to `%LocalAppData%\Qryvanta\logs\qryvanta.log`, with size-based
rotation at startup. A redacting handler masks attributes whose keys look sensitive
(`password`, `pwd`, `secret`, `token`, `connection_string`, ...) and scrubs
`password=...` fragments from string values. SQL text is logged only at debug level.
There is no telemetry and nothing is sent over the network except to the user's own databases.

Third-party components get their own logger through `logging.Logger.ForComponent` /
`app.Core.ComponentLogger`, so their routine chatter can't bury Qryvanta's entries:

- Records go to the same file, with redaction, tagged `component=<name>`.
- Only Warn and above are kept. When Qryvanta's own level is Debug, the floor drops to Info.
- The desktop app gives Wails such a logger and turns off the asset server's per-request logging
  (`AssetOptions.DisableLogging`). A normal session's log then contains Qryvanta's entries plus
  any Wails warnings or errors.

### D9. Frontend

React 19, TypeScript (strict), Vite, Tailwind CSS v4, shadcn/ui, and Zustand for UI state.
Monaco (Phase 3), TanStack Table (Phase 3), React Flow (Phases 6–7), and Recharts where
useful. The dark theme is the default, with a light theme available. All backend calls go
through generated bindings wrapped in `frontend/src/services`, so components never import
bindings directly.

### D9a. Webview hardening (Content-Security-Policy)

The webview loads only Qryvanta's own bundled assets, and it talks to Go through same-origin
requests (`/wails/runtime`, `/wails/stream/…`, the events WebSocket). So the production policy
can be strict:

```
default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:;
font-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none';
form-action 'none'; frame-ancestors 'none'
```

- **No inline or external scripts, and no eval.** Neither the app bundle nor `@wailsio/runtime`
  uses `eval` or `new Function`. Native calls such as `ExecJS` use the webview host API, which a
  page's CSP doesn't restrict.
- **No external connections.** Even if markup were injected, it could not send data anywhere,
  which matters for an app that displays query results.
- **`style-src 'unsafe-inline'`** is the one relaxation. React and Radix set `style` attributes
  (positioning, sizes). Inline styles can't execute code.
- **Single source:** the policy lives in `frontend/csp-production.txt`. The Go asset middleware
  (`desktop.SecurityHeaders`) sends it as a header on every response, together with
  `X-Content-Type-Options: nosniff` and `Referrer-Policy: no-referrer`. The Vite production build
  also writes it into `index.html` as a `<meta>` tag, without the header-only `frame-ancestors`,
  so it applies even if a webview ignored the header. Tests check both and the policy's key
  directives.
- **Development builds** (`wails3 dev`, no `production` build tag) use `desktop.DevelopmentCSP`.
  It adds `'unsafe-inline'` scripts (Vite's React Refresh preamble) and local
  `ws://`/`http://` connections (hot reload), and still forbids eval, plugins and non-local
  origins.
- **Monaco fits the policy unchanged** (#29). It is bundled from `monaco-editor`'s ESM build;
  nothing comes from a CDN. Its one web worker is a separate file Vite emits next to the bundle
  (`editor.worker-*.js`), started with `new Worker(url)`. With no `worker-src`, the browser falls back to
  `script-src 'self'`, which allows it. No blob: or data: workers are used. Its codicon font is a
  bundled `.ttf` (`font-src 'self'`), and the styles Monaco injects at runtime are covered by
  `style-src 'unsafe-inline'`. A manual check of the production build found no CSP violations
  in the console.

### D9b. SQL editor

- **Monaco, trimmed and lazy.** `src/features/editor/monaco.ts` imports the editor core, the
  editing contributions a SQL editor needs (find/replace, folding, multi-cursor, bracket
  matching, comments, suggestions) and the `sql` grammar, which is based on T-SQL. It does not
  load the ~90 other languages or the TypeScript/CSS/HTML/JSON language services that the
  package's main entry registers. `SqlEditor` is loaded with `React.lazy`, so Monaco
  (~3.7 MB, ~0.96 MB gzipped) downloads with the first editor tab, not on first paint.
- **One editor, one model per tab.** Undo history, cursor and scroll position survive tab
  switches. Models of closed tabs are disposed.
- **Tabs remember their connection and database** (`stores/editor-store.ts`). Changing either
  in the header updates the active tab. Switching to a tab reconnects to its connection (the
  workbench has one active connection at a time) and restores its database.
- **Formatting** uses `sql-formatter` with its `transactsql` dialect and the `formatting.*`
  settings. It formats the selection, or the whole document. It is applied as an editor edit,
  so Ctrl+Z undoes it. Text it cannot tokenize is reported, not changed.
- **Files** go through `desktop.FileService` and `internal/sqlfile`. The frontend never names a
  path itself. Save only writes to a path that came from the Open or Save As dialog in this
  run, so a compromised page cannot overwrite arbitrary files.
  - Encodings are detected (UTF-8, UTF-8 with BOM, UTF-16 LE/BE, and Windows-1252 for legacy
    files that are not valid UTF-8). Save writes the file back in the same encoding, so
    sqlcmd and deployment tools read it as before.
  - New files are UTF-8 with a BOM, which sqlcmd reads correctly without `-f 65001`.
  - Files over 16 MB are refused.
- **Unsaved work** is confirmed before a tab closes. The frontend reports unsaved state to
  `desktop.WindowService`, whose `WindowClosing` hook cancels the close and emits
  `qryvanta:close-requested`. The frontend then asks, and calls `WindowService.Close` to close
  anyway.

### D9c. Running queries from the editor

- **Streaming over events, ordered by run ID.** The frontend picks a run ID and calls
  `desktop.QueryService.Execute(runID, connection, database, sql)`, which runs
  `app.Core.Execute` (limits and the read-only policy come from the settings, D5).
  - Result sets, row batches (500 rows) and messages are emitted as `qryvanta:execution`
    events while they are read. A final `done` event carries the summary.
  - Wails delivers events in order through one mailbox, so `done` always comes after the run's
    rows.
  - The store keeps only each tab's latest run and drops events with any other run ID, so a
    cancelled or replaced run can never overwrite a newer one.
- **Stop** calls `QueryService.Cancel(runID)`. This cancels the run's context, and the driver
  cancels the query on the server. The run still ends with a `done` event (outcome
  `cancelled`), so the UI always learns how a run ended.
- **Selection and line numbers.** Execute runs the selection if there is one, otherwise the
  whole tab. SQL Server reports error lines relative to the batch; the backend maps them to the
  script (`scriptLine`, D7c), so the editor line is `scriptLine + selection start − 1`. Errors
  inside a procedure report the procedure's own lines and are not linked to the editor.
- **`GO` batches.** A `batch` event marks the start of each batch execution, and result sets
  carry their batch. With more than one batch, the Messages tab groups output under
  "Batch k · line L" headers (with "ran n times" for `GO n`) and result headings name the batch.
- **Syntax markers.** 300 ms after the active tab stops changing, its text is sent to
  `QueryService.CheckSyntax`, and the diagnostics become Monaco markers (squiggles with hover
  text) and a problems count in the status bar, which jumps to the first problem.
  - A newer check of the same tab aborts the older one, and a result is shown only while the
    tab still has the text it was computed for, so a slow parse never shows stale markers.
    Parsing never blocks typing.
  - When the parser is unavailable there are no markers (the status bar already warns), not
    "no problems".
  - **Running SQL with syntax errors asks first** ("Fix it first" / "Run anyway"). Only
    errors inside the selection count when a selection runs.
- **Results stay in memory.** Rows exist only in the frontend store for the tab's latest run.
  They are never logged (the core logs counts and timings; SQL text only with
  `logging.logSqlText`), and are never written to history. Result samples are a separate,
  off-by-default setting for Phase 8.
- **The grid is virtualized** (TanStack Table v9 for column sizing, TanStack Virtual for
  rows), so the DOM holds only the visible rows. 10,000 rows from the fixture server render
  in about a third of a second.
  - NULL is styled distinctly from the string `'NULL'`.
  - Copy produces TSV with CRLF lines and Excel-style quoting, and writes NULL as `NULL`.
- **Writes need a confirmation when read-only mode is off.** The first run on a connection
  asks first if the login can or may modify data, or its permissions are unknown. The answer
  holds until the app restarts. With read-only mode on, the session guard (D5) already rolls
  changes back, so there is no prompt.

### D9d. Autocomplete (query model, with a heuristic fallback)

- **The kind of completion comes from a small tokenizer** (`features/editor/completion/context.ts`).
  Text being typed rarely parses, so this stays token-based. It skips strings and comments,
  reads bracketed and quoted identifiers, and answers three questions:
  - Is the cursor after a qualifier (`o.`, `dbo.`, `Sales.dbo.`)?
  - Is it where a table goes (after FROM, JOIN, UPDATE, INTO or APPLY, or after a comma in a
    FROM list)?
  - Otherwise, it is a general position.
- **What names mean comes from the query model** (#45). On each completion request the editor
  sends the tab's text and the cursor offset to `QueryService.CompletionScope`.
  `app.Core.CompletionScope` parses with the model; when the batch at the cursor has syntax
  errors (`SELECT t.` never parses), it retries with a placeholder identifier at the cursor:
  with the rest of the text, then with `IS NULL` after it (in WHERE, ON and HAVING a bare name
  is not a condition), then without the text after the cursor. At most five parses, each a few
  milliseconds.
  - `parser.ScopeAt` finds the innermost query around the cursor and returns its sources, then
    those of the enclosing queries (correlated subqueries see outer aliases; inner names shadow
    outer ones), the UPDATE/DELETE target and FROM, and the statement's CTEs (usable before a
    FROM mentions them: `WITH t AS (...) SELECT t.`).
  - Each source has a kind (table, cte, derived, function, values, variable). CTEs, derived
    tables and VALUES lists carry their output columns: aliases and plain column names, with
    `*` and `t.*` expanded through other CTEs and derived tables, and to the tables they cover
    (whose columns come from metadata). Explicit column lists (`AS d (x, y)`) win. Tables and
    table-valued functions carry their name, and their columns come from metadata.
  - The editor does not use the debounced syntax-check parse (D9c) for this: it is usually
    older than the text at the cursor and has no model. Completion parses on demand instead,
    within its own budget.
- **Fallback:** when the parser is unavailable or nothing parses, and for a qualifier the
  scope does not know, `findTableRefs` reads the tables and aliases of the statement around
  the cursor from the tokens, as before #45. A wrong guess only means a less useful list.
- **Suggestions** (`suggestions.ts`):
  - After FROM/JOIN: the statement's CTEs, then the current database's tables, views and
    functions, schema-qualified, with `dbo` first.
  - After `alias.` or `table.`: that source's columns: metadata columns with type and PK/FK
    details as in the explorer, or the names a CTE, derived table or VALUES list defines.
  - After `schema.`: that schema's objects.
  - Elsewhere: the columns and names of every source in scope, the innermost query's first,
    then keywords and functions.
  - Identifiers that are not regular identifiers (spaces, a leading digit, reserved words) are
    inserted in brackets, with `]` escaped as in `QUOTENAME`.
- **Metadata comes from the schema store**, the same cache the explorer fills. Missing objects
  or columns are requested. One 400 ms budget covers the whole request: the scope is awaited
  for at most 400 ms (then the heuristic is used), and metadata for what remains, at least
  100 ms. After that it returns
  what it has, marked incomplete so Monaco asks again on the next keystroke. A slow server
  never freezes typing.
- Monaco's word-based suggestions are off, so the list contains only SQL-aware items.

## 6. Main flow: "Execute with debugging" (target design)

```
UI ──► desktop.Debug(connID, sql, options)
         │
         ▼
   parser.Parse ──► QueryModel ──► execution policy check ──► (reject with reason)
         │
         ├─► static rules ─────────────────────────────► findings (static evidence)
         ├─► dialect.Metadata(tables in model) ────────► findings (metadata evidence)
         ├─► plan.Estimated(sql) ──────────────────────► operator graph
         ├─► executor.Run(sql, limits, STATISTICS XML) ► rows + actual plan
         │                                              └► findings (plan evidence)
         └─► instrumentation.ProposeProbes(model, findings)
                    │  shown to the user, approved
                    ▼
               executor.Run(probe) × N ───────────────► findings upgraded or added
         │
         ▼
   session.Save(run) ──► report (JSON / MD / HTML)
```

Each step is cancellable through one `context.Context`. Results stream to the UI as events.

## 7. Concurrency

- One `*sql.DB` pool per connection profile, with bounded `SetMaxOpenConns`.
- Each user execution gets a context with timeout. The Stop button cancels it, and go-mssqldb
  sends an attention packet to SQL Server.
- Probes run with bounded parallelism (default 2) so they do not overload the server.
- The sidecar processes requests one at a time per process. Parse requests are short.

## 8. Error handling

Core functions return wrapped errors with `%w`. The desktop adapter maps them to a small
`UserError{Code, Message, Detail}` DTO. Driver errors keep the SQL Server error number and
line, but never the connection string.

## 9. Testing strategy

| Layer | Approach |
|---|---|
| config, logging, security, compare, plan parsing, analyzer rules | Pure unit tests, table-driven, run on every build |
| parser | Golden tests (`tests/parser`, 79 cases including every sample query): SQL file → expected batches, diagnostics and `QueryModel` JSON, run against the real sidecar in CI; plus sidecar unit tests and Go client tests with a fake sidecar |
| executor, connection, dialect | Integration tests against a real SQL Server (build tag `integration`, configured through `QRYVANTA_TEST_MSSQL_*` variables). CI runs them on Linux against `docker-compose.yml`, as the read-only `qryvanta_reader` login. |
| fixture data | `tests/integration` runs the 10 scenario queries in `tests/sample_queries` and checks the documented wrong and correct results |
| diagnostics end to end (Phase 5) | Each buggy scenario query must produce its finding, and each corrected query must not |
| architecture | `internal/archtest` checks that core packages do not import Wails |
| frontend | Vitest + Testing Library for components and stores |

## 10. Future extensions

- **More dialects:** `dialect.Dialect` covers metadata queries, plan retrieval, quoting, and
  session guards. PostgreSQL, MySQL, and SQLite are added as implementations, along with their
  own parsers.
- **Optional AI module:** a separate package that consumes the **analysis bundle**
  (QueryModel, schema metadata, plan graph, findings, statistics, the user's stated expectation)
  and returns additional findings marked as AI-generated. The deterministic engine never
  depends on it. v1 includes no AI code, SDKs, or keys.
- **Pure-Go parser:** replaces the sidecar behind `parser.Parser`. The golden tests serve as its
  acceptance suite.
