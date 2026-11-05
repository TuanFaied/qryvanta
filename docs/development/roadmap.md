# Roadmap

There is no fixed date. A phase is finished only when its features work against a real
SQL Server, its tests pass, and the desktop app and CLI both build.

Legend: ✅ done · 🚧 in progress · ⬜ not started

## Phase 1: Foundation ✅

- Architecture document, roadmap, and repository structure
- Wails v3 desktop app (`cmd/qryvanta`) and CLI (`cmd/qryvanta-cli`) sharing one core
- Configuration (load, validate, atomic save) and structured logging with secret redaction
- React + TypeScript + Tailwind v4 + shadcn/ui frontend
- Typed frontend ↔ backend bindings
- Application shell: header, explorer, workspace, bottom panel, analysis panel; resizable
  and collapsible; dark and light themes; settings dialog backed by the real config
- Architecture test that keeps the core free of Wails

Details: [phase-1-plan.md](phase-1-plan.md)

## Phase 2: Database connectivity 🚧

- ✅ Connection profile model, file store, and credential storage in Windows Credential Manager (#5)
- ✅ SQL Server driver (go-mssqldb): connection test, connect/disconnect, per-profile pool (#6)
- ✅ SQL Server authentication and Windows integrated authentication; TCP, named pipes, shared memory (#6)
- Connection management UI (#8)
- Database selection; schema explorer (schemas, tables, views, procedures, columns) (#9)
- Permission check: report whether the login can write (#10)
- Docker Compose with SQL Server 2022 and the **fixture database** (10 problem scenarios) (#7)
- ✅ CLI: `qryvanta-cli connections list` (#5); `connections add|test|remove` (#6)

## Phase 3: SQL editor and execution ⬜

- Monaco editor: tabs, T-SQL highlighting, formatting (sql-formatter, T-SQL dialect),
  find/replace, shortcuts (Ctrl+Enter, Ctrl+Shift+Enter, Esc), selection execution
- Executor: timeout, max rows, max bytes, cancellation, streamed results, messages, errors
- Execution policy layers 2 and 4 (session guard, limits). Layer 1 arrives with the parser in Phase 4,
  and until then read-only mode refuses non-SELECT text that it cannot verify.
- TanStack Table result grid (virtualized), Messages tab
- Schema-aware autocomplete (tables, columns)
- CLI: `qryvanta-cli run query.sql --conn X --format json`

## Phase 4: SQL parsing ⬜

- `sidecar/tsqlparser` (.NET 8, ScriptDom), published as a self-contained single file
- JSON protocol, Go client with restart, timeout, and request IDs
- `QueryModel`: statements, sources, joins and predicates, WHERE/HAVING expression trees, GROUP BY,
  ORDER BY, CTEs, subqueries, set operations, aggregates, window functions, source positions
- Parser diagnostics are shown as editor markers
- Execution policy layer 1 (statement allowlist) on top of the parse tree
- Golden test suite

## Phase 5: Diagnostic engine ⬜

- Finding / Evidence / Severity / Classification model, with validation (D3)
- Static rules: `= NULL`/`<> NULL`, AND/OR precedence, `NOT IN` with a nullable subquery,
  JOIN without a predicate / cartesian product, LEFT JOIN cancelled by a WHERE on the right table,
  contradictory or redundant predicates (where provable), non-sargable date filters,
  `BETWEEN` on datetime end boundaries, COUNT(col) versus COUNT(*) versus COUNT(DISTINCT)
- Metadata rules: join on non-key columns (fan-out risk), join type mismatch, nullable join keys
- Probes (D4): fan-out per join, unmatched rows per INNER JOIN, rows excluded per predicate
- CLI: `qryvanta-cli diagnose query.sql --conn X`

## Phase 6: Execution plans ⬜

- Estimated (`SHOWPLAN_XML`) and actual (`STATISTICS XML`) plans
- Showplan XML parser into an operator graph (operators, estimated and actual rows, costs, warnings)
- React Flow plan view; estimated and actual values always labelled
- Plan-based findings: large estimate/actual gaps, implicit conversions, spills, missing-join warnings

## Phase 7: Debugging visualization and comparison ⬜

- Query structure graph (sources → joins → filters → aggregation → output)
- Row-count overlay from actual plans and probes, marked "unavailable" where not measurable
- Result comparison: key-based, selected columns, or full row (multiset). Added, removed,
  changed, and duplicate-count differences, with correct handling of NULLs and types
- Side-by-side viewer

## Phase 8: Sessions, history, reports ⬜

- SQLite store (modernc), sessions, history, retention policy
- Reports: JSON (versioned schema, the public contract), Markdown, HTML
- CLI: `qryvanta-cli report <session> --format md`

## Phase 9: Security and quality ⬜

- Security review against `docs/security/threat-model.md`
- Full integration suite in CI with SQL Server in Docker
- Error handling and resource usage pass

## Phase 10: Open-source release ⬜

- CODE_OF_CONDUCT, issue and PR templates, GitHub Actions (build, test, lint, release)
- NSIS installer bundling the parser sidecar, and code signing if a certificate is available
- Documentation pass and screenshots, then v1.0.0

## Later

- PostgreSQL, MySQL, SQLite dialects
- Pure-Go T-SQL parser replacing the sidecar
- Optional AI module consuming the analysis bundle
