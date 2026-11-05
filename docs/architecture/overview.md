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
  desktop/             Wails adapter: bound services, events, DTO mapping
  config/              Load, validate, and atomically save user configuration
  logging/             slog setup, file logging, secret redaction
  security/            Redaction helpers, execution policy (Phase 2+)
  version/             Build version information
  fsutil/              Atomic file writes shared by config and connections
  connection/          Connection profiles, keyring credentials; (Phase 2) pooling
  dialect/mssql/       SQL Server driver: DSN building, server info, error mapping
  executor/            (Phase 3) Query execution, limits, cancellation, result streaming
  parser/              (Phase 4) Parser interface, sidecar client, structured query model
  analyzer/            (Phase 5) Rules that turn query structure + evidence into findings
  diagnostics/         (Phase 5) Finding, Evidence, Severity, Classification types
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
  integration/         (Phase 2+) Tests against a real SQL Server in Docker
  fixtures/            Sample database with intentionally problematic data
  sample_queries/      Buggy queries matching the fixture scenarios
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
5. Credentials never cross into JavaScript. The frontend refers to a connection by ID only.

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

1. **Statement allowlist** (hard): from the parsed batch, only `SELECT`, CTE-led `SELECT`, and set
   operations are allowed. `INTO`, DML, DDL, `EXEC`, `DECLARE`/`SET` side effects, and
   multi-statement batches are rejected in read-only mode. If the parser is unavailable,
   read-only mode refuses to execute instead of guessing.
2. **Session guard** (best effort): queries run in an explicit transaction that is always
   rolled back, with `SET XACT_ABORT ON`, `SET IMPLICIT_TRANSACTIONS OFF`, and a lock timeout.
   This does not undo effects outside the transaction (linked servers, `xp_` procedures, CLR).
3. **Least-privilege guidance** (strongest): the connection dialog recommends, and the
   permission check (Phase 2) reports, whether the login can write. A read-only login is the only
   true guarantee, and the documentation says so.
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
  `internal/dialect/`, and the interface grows only when a feature needs it (metadata in #9,
  plans in Phase 6).
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

### D8. Logging

`log/slog` with JSON output to `%LocalAppData%\Qryvanta\logs\qryvanta.log`, with size-based
rotation at startup. A redacting handler masks attributes whose keys look sensitive
(`password`, `pwd`, `secret`, `token`, `connection_string`, ...) and scrubs
`password=...` fragments from string values. SQL text is logged only at debug level.
There is no telemetry and nothing is sent over the network except to the user's own databases.

### D9. Frontend

React 19, TypeScript (strict), Vite, Tailwind CSS v4, shadcn/ui, and Zustand for UI state.
Monaco (Phase 3), TanStack Table (Phase 3), React Flow (Phases 6–7), and Recharts where
useful. The dark theme is the default, with a light theme available. All backend calls go
through generated bindings wrapped in `frontend/src/services`, so components never import
bindings directly.

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
| parser | Golden tests: SQL file → expected `QueryModel` JSON, run against the real sidecar |
| executor, connection, dialect | Integration tests against a real SQL Server (build tag `integration`, configured through `QRYVANTA_TEST_MSSQL_*` variables; Docker setup in #7) |
| diagnostics end to end | The fixture database's 10 scenario queries must produce the expected findings |
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
