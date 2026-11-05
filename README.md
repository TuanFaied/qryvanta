# Qryvanta

**Understand why a SQL query returned the rows it did.**

Qryvanta is an open-source desktop tool for debugging SQL *logic*: the queries that run without
errors but return too many rows, too few rows, duplicates, or unexpected NULLs. It shows the
result, explains how it was produced, and backs every finding with evidence.

> **Status: early development (Phase 1 of 10).** The application shell, configuration, logging and
> CLI work today. Database connectivity is the next milestone. See the [roadmap](docs/development/roadmap.md).

<!-- Screenshots will be added once the editor and diagnostics are usable (Phase 5). -->

## Why

SQL tools show you the result set and maybe a plan. They don't tell you that an `INNER JOIN`
silently dropped 212 employees without a department, that `OrderItems` multiplied every order
by 4.2, or that `WHERE Status = NULL` can never be true. Qryvanta is built to find exactly those problems.

## Principles

- **Deterministic.** Findings come from rules plus evidence: the parsed query, catalog metadata,
  the execution plan, and read-only probe queries that you approve. No AI, no guessing.
- **Honest.** Estimated and measured values are always labelled. A finding is "Confirmed" only when
  the evidence proves it, and is otherwise reported as "Potential".
- **Safe by default.** Read-only execution, time/row/size limits, and cancellation. Generated
  query changes are never run automatically.
- **Local.** No telemetry. Queries and results never leave your machine except to your own database.
  Passwords are kept in the OS credential store.

## Planned features

| Area | Highlights |
|---|---|
| Connections | SQL Server and Windows authentication, credentials in Windows Credential Manager, schema explorer |
| Editor | Monaco, tabs, formatting, schema-aware completion, selection execution |
| Diagnostics | JOIN fan-out and row loss, NULL comparisons, AND/OR precedence, `NOT IN` with NULLs, COUNT vs COUNT DISTINCT, missing join predicates |
| Plans | Estimated and actual SQL Server plans, visual operator graph |
| Comparison | Key, column or full-row result diffs with correct NULL and duplicate handling |
| Reports | Session history, JSON / Markdown / HTML reports |
| CLI | The same engine from the command line, for scripts and CI |

## Supported databases

| Database | Status |
|---|---|
| Microsoft SQL Server 2016+ / Azure SQL | Planned for v1 |
| PostgreSQL, MySQL, SQLite | Planned after v1 |

## Technology

Go · [Wails v3](https://v3.wails.io) · React 19 · TypeScript · Vite · Tailwind CSS v4 · shadcn/ui.
T-SQL parsing uses Microsoft's ScriptDom in a small .NET sidecar process, behind a Go interface
that a pure-Go parser can replace later.

## Architecture

```
React UI ──bindings──► internal/desktop (Wails adapter) ──► internal/app ──► core engine
                                                                 ▲
qryvanta-cli ────────────────────────────────────────────────────┘
```

The core engine (config, logging, connections, executor, parser, analyzer, plans, reports) never
imports Wails, and a test enforces this. Read the full [architecture document](docs/architecture/overview.md).

## Install

There are no releases yet. Build from source as described below.

## Development

Requirements: Go 1.26+, Node.js 22+, pnpm 10+, the Wails v3 CLI, and WebView2 (preinstalled on Windows 11).

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
```

```bash
wails3 dev
```

```bash
wails3 build
```

```bash
go test ./...
```

```bash
pnpm -C frontend test
```

See [docs/development/setup.md](docs/development/setup.md) for details.

## Roadmap

See [docs/development/roadmap.md](docs/development/roadmap.md).

## Contributing

Contributions are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md) first. To report
vulnerabilities, see [SECURITY.md](SECURITY.md).

## License

[Apache License 2.0](LICENSE)
