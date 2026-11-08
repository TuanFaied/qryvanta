# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- T-SQL parser sidecar (`sidecar/tsqlparser`, .NET 10 with Microsoft ScriptDom):
  - It reads requests as newline-delimited JSON on stdin and writes responses to stdout.
  - It splits scripts into batches on `GO` (including `GO n`, and ignoring `GO` inside strings
    and comments), and reports syntax errors with their script line and column.
  - It is published as a self-contained single file (about 13 MB) next to `qryvanta.exe` by
    `wails3 build`, so users don't need .NET installed.
- Go client for the parser (`internal/parser`):
  - One long-lived sidecar process, started on first use, with one request at a time.
  - A timeout or crash restarts it. A cancelled request does not, so typing never restarts the
    parser.
  - After repeated start failures it reports "unavailable" for 30 s instead of retrying on
    every request.
  - Results carry byte offsets as well as editor (UTF-16) offsets.
- The status bar warns "Parser unavailable", with the reason, when the parser cannot run.
  Queries still run without it.
- `qryvanta-cli parse` prints a script's `GO` batches and syntax errors (with `--json` and
  `--compat`), and exits 1 when there are errors.
- Connection profiles (`internal/connection`): validation, `connections.json` store, passwords in
  Windows Credential Manager, and a manager that keeps profiles and credentials consistent.
- `qryvanta-cli connections list [--json]`.
- SQL Server driver (`internal/dialect/mssql`, go-mssqldb): SQL Server and Windows authentication,
  strict/mandatory/optional encryption with certificate validation, and TCP, named pipes and
  shared memory, so local SQL Server Express works out of the box.
- Connection pool (`connection.Pool`): connection test, connect/disconnect, per-profile limits,
  timeouts, cancellation, and sanitized errors that keep the SQL Server error number.
- `qryvanta-cli connections add|test|remove`, with passwords read only through `--password-stdin`.
- Integration tests (`-tags integration`) configured through `QRYVANTA_TEST_MSSQL_*` variables.
- `docker-compose.yml` with SQL Server 2022 Developer edition and a one-shot loader for the
  `QryvantaSample` fixture database, plus a read-only `qryvanta_reader` login.
- Ten sample queries (`tests/sample_queries`), each buggy and corrected, with the expected
  results in `manifest.json`, checked by `tests/integration` and a Linux CI job.
- Connection management in the desktop app:
  - A connection dialog to create, edit and test connections, with cancellable tests and
    specific guidance for login, certificate, timeout and network failures.
  - Delete with confirmation.
  - An explorer list with search, a context menu and keyboard actions (Enter, F2, Del).
  - A header connection selector, and connection details in the status bar.
  - Passwords are write-only from the UI: the frontend can set, replace or remove one but never
    receives it.
- `QRYVANTA_CONFIG` points the desktop app at another config (and connections) file for
  development and manual testing.
- Schema explorer:
  - A lazily loaded tree of databases (system databases in their own folder), tables, views,
    stored procedures, functions and columns.
  - Columns show their T-SQL type, nullability, identity, primary key and foreign-key target.
  - Search across loaded objects and keyboard expansion.
  - Context menus to copy a name or a `SELECT TOP (100)` statement, refresh a node, or make a
    database the current one.
  - Errors are shown on the node that failed, with a retry.
- Current-database selection: a header selector lists the databases the login can open. It
  defaults to the login's database, and the status bar shows it.
- `internal/schema` (database, object and column model, `Reader` interface) with a SQL Server
  implementation that reads other databases through safely quoted three-part names.
- The fixture database gains a view, a procedure and a function, and `qryvanta_reader` gets
  `VIEW DEFINITION`, so every object kind appears in the explorer.
- `qryvanta-cli run` runs a batch from `--query`, `--file` or stdin with the desktop app's
  engine, limits and read-only session guard.
  - Output formats: a readable table, JSON (result sets, messages, timings, guard, truncation)
    or CSV.
  - `--max-rows` and `--timeout` override the limits for one run.
  - `--allow-writes` turns read-only mode off for one run, with a warning.
  - Messages go to stderr, along with a warning when the login can modify data.
  - Exit codes: 0, 1 query error, 2 usage, 3 timeout or cancel. Ctrl+C cancels on the server.
- Schema-aware autocomplete in the editor:
  - After FROM or JOIN it suggests the current database's tables, views and functions.
  - After `alias.` it suggests that table's columns, with their type and PK/FK.
  - Elsewhere it suggests columns of the tables in the statement, keywords and functions.
  - Names that need it are inserted in brackets.
  - Metadata comes from the explorer's cache and is loaded on demand. Completion never waits
    more than 400 ms on the server.
- Running queries from the editor:
  - Execute (Ctrl+Enter or the header button) runs the selection, or the whole tab, on the
    tab's connection and database. Stop (Esc or the header button) cancels it on the server.
  - Results stream in while the query runs, into a virtualized grid per result set.
    - Column types show on hover, and NULL is shown distinctly.
    - Columns can be resized.
    - Cells, rows, ranges or everything can be copied as TSV, with or without headers.
    - A banner explains when a row or size limit stopped reading.
  - The Messages tab lists messages and errors in order (`Msg 207, Level 16, State 1, Line 3`).
    Clicking an error moves the editor to its line, even when only a selection was run.
  - Row counts, timeouts, cancellation, the read-only rollback, and a batch's own COMMIT
    ending protection are reported.
  - Status shows a running indicator, elapsed time and row counts, and running tabs are marked.
  - With read-only mode off, the first run on a connection whose login may modify data asks for
    confirmation.
  - Result rows are kept in memory only, and never logged or stored.
- SQL editor (Monaco, bundled locally and loaded with the first tab):
  - Tabs that can be created, renamed (double-click or F2) and closed (Ctrl+W, middle click).
    They show unsaved changes, and each tab remembers its connection and database.
  - T-SQL highlighting, find and replace, folding, bracket matching and multiple cursors. The
    editor follows the app theme and font size.
  - Format Document (Shift+Alt+F, or the toolbar) with sql-formatter's T-SQL dialect. It formats
    the selection or the whole document. New settings choose keyword case, indent size and tabs.
  - Open and save `.sql` files (Ctrl+O, Ctrl+S, Ctrl+Shift+S) through native dialogs. Files keep
    their encoding (UTF-8, UTF-8 with BOM, UTF-16, Windows-1252), and new files are UTF-8 with BOM.
  - Closing a tab or the window with unsaved changes asks first.
- Read-only session guard:
  - In read-only mode, every batch runs inside a transaction that is always rolled back (also
    after errors, timeouts and cancellation), with a configurable lock wait limit (default 5 s).
  - A `COMMIT` or `ROLLBACK` inside the batch that ends the protection is detected and reported.
  - Turning read-only mode off in Settings asks for confirmation, and the policy always comes
    from the settings.
- Query executor (`internal/executor`, `mssql.Executor`, `app.Core.Execute`), not yet wired
  into the UI or CLI:
  - Streams result sets and messages in server order.
  - Enforces the timeout, row and size limits, with server-side cancellation.
  - Runs in the chosen database without leaking that context to other pooled sessions.
  - Converts values without losing precision.
- Content-Security-Policy for the desktop webview:
  - The policy blocks inline and external scripts, eval, and connections other than the app's
    own backend.
  - It is sent as a header with `X-Content-Type-Options` and `Referrer-Policy`, and written into
    the production `index.html`.
  - Development builds use a relaxed policy for Vite hot reload.
- Login write-access check:
  - Qryvanta reports whether the current login could modify data in the current database:
    *read-only login*, *may modify via procedures*, *login can modify data*, or *unknown*.
  - Reasons are listed most significant first, for example sysadmin, `db_owner`, or an
    INSERT grant on a table.
  - Shown in the status bar (rechecked when the database changes), in the connection
    dialog's test result, and in `qryvanta-cli connections test`.
- Architecture document, roadmap, and Phase 1 plan.
- Wails v3 desktop application and `qryvanta-cli`, sharing one Go core.
- Configuration with validation and atomic saves. An invalid file is moved aside at desktop startup.
- Structured JSON logging with secret redaction and size-based rotation.
- Application shell: header, explorer, workspace, output panel, analysis panel, and status bar.
  Panels are resizable and collapsible, with dark and light themes.
- Settings dialog backed by the real configuration, with per-field validation errors.
- Architecture test that keeps the core free of Wails.
- GitHub issue forms (bug, feature, SQL logic case, task), PR template, CODEOWNERS, Dependabot,
  Code of Conduct, and a CI workflow (Go, frontend, and the Wails desktop build on Windows).

### Changed

- Quieter logs:
  - Wails runtime messages are recorded only at Warn and above (Info as well while Qryvanta's
    log level is Debug), tagged `component=wails`.
  - The asset server no longer logs every file request.
- Qryvanta has its own application icon (Windows `.ico`, macOS `.icns`, Icon Composer source) and
  macOS disk-image artwork in place of the Wails defaults. They are generated by `tools/appicon`
  from the same geometry as the in-app logo.
- The status bar's execution-policy label reads "Read-only mode", so it is not confused with the
  login check's "Read-only login".
- Profiles with read-only application intent must name a database (a SQL Server requirement).
- The explorer and analysis panels keep their pixel width when the window is resized, and start
  at 260 px and 300 px, so they no longer collapse on narrow windows.

### Fixed

- The Settings dialog no longer overflows its frame on short windows. It now scrolls inside the
  dialog, with the footer kept visible.
- `--password-stdin` ignores the UTF-8 byte-order mark that Windows PowerShell 5.1 puts in front
  of piped text. Before this fix, passwords piped from PowerShell were stored with three
  invisible extra bytes and the login failed.
