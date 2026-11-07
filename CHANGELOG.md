# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

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
  - Ctrl+Enter, Ctrl+Shift+Enter and Esc are reserved for execution, which arrives with the
    results grid.
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
