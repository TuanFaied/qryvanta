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

- Profiles with read-only application intent must name a database (a SQL Server requirement).
- The explorer and analysis panels keep their pixel width when the window is resized, and start
  at 260 px and 300 px, so they no longer collapse on narrow windows.

### Fixed

- The Settings dialog no longer overflows its frame on short windows. It now scrolls inside the
  dialog, with the footer kept visible.

- `--password-stdin` ignores the UTF-8 byte-order mark that Windows PowerShell 5.1 puts in front
  of piped text. Before this fix, passwords piped from PowerShell were stored with three
  invisible extra bytes and the login failed.
