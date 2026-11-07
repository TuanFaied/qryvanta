# Phase 1 implementation plan: Foundation

## Goal

A desktop app and a CLI that start, share one Go core, read and write real configuration,
log safely, and show the full application shell in dark and light themes.
No database features yet. Disabled controls say plainly that they are not available yet.

## Deliverables

### Repository

- `go.mod` (`github.com/TuanFaied/qryvanta`, Go 1.26, required by Wails v3.1)
- `.gitignore`, `.editorconfig`, `Taskfile.yml` (Wails v3 task runner), `build/` assets
- `README.md`, `LICENSE` (Apache-2.0), `CONTRIBUTING.md`, `SECURITY.md`, `CHANGELOG.md`
- `docs/architecture/overview.md`, `docs/development/{roadmap,phase-1-plan,setup}.md`

### Go core

| Package | Contents | Tests |
|---|---|---|
| `internal/version` | `Version`, `Commit`, `BuildDate` set through `-ldflags`; `Info()` | yes |
| `internal/config` | `Config` struct (appearance, execution limits, history retention, logging), `Default()`, `Validate()`, `Load(path)` (missing file uses defaults; unknown fields ignored), `Save(path)` (atomic temp file and rename), `DefaultPath()` | yes |
| `internal/logging` | `New(Options) (*slog.Logger, io.Closer, error)`, JSON file handler with startup rotation, `RedactingHandler` | yes |
| `internal/security` | `IsSensitiveKey`, `RedactString` (scrubs `password=…;` and similar) | yes |
| `internal/app` | `Core` (config path, config, logger), `New(Options)`, `UpdateConfig` | yes |
| `internal/archtest` | Fails if any core package imports `github.com/wailsapp/wails` | is a test |

### Desktop adapter

- `internal/desktop.AppService`: `Info()`, `GetConfig()`, `UpdateConfig(cfg)`,
  `OpenLogFolder()`. Returns DTOs only.
- `cmd/qryvanta/main.go`: builds `app.Core`, registers services, creates the window
  (1400×880, minimum 1024×640, dark background), embeds `frontend/dist`.

### CLI

- `cmd/qryvanta-cli`: `version`, `config path`, `config show` (JSON), using only the standard
  library `flag` package.

### Frontend

- React 19, TS strict, Vite, Tailwind v4 (`@tailwindcss/vite`), shadcn/ui
  (button, tooltip, dialog, select, switch, input, label, separator, resizable, scroll-area, tabs)
- Theme provider: dark (default), light, system. Applied through the `.dark` class and persisted in config.
- Layout:
  - **Header**: logo, connection indicator ("No connection"), database selector (disabled),
    Execute / Debug / Stop (disabled, with tooltips saying they arrive in Phase 3), theme menu,
    settings, version from the backend
  - **Left**: explorer with an empty state that points to Phase 2
  - **Center**: workspace with tab strip and editor area (empty state)
  - **Bottom**: Results / Messages / Diagnostics / Plan / Statistics tabs (empty states)
  - **Right**: Structure / Joins / Filters / Findings / Execution sections (empty states)
  - Resizable and collapsible panels; layout persisted per user in `localStorage` (a convenience only)
  - Status bar: config path, log level, version
- Settings dialog: theme, execution defaults (timeout, max rows, max result MB, read-only),
  history retention days. Saved through `UpdateConfig`, with validation errors shown inline.
- `src/services/backend.ts` wraps the generated bindings, so components never import them.

## Out of scope for Phase 1

Database connections, the Monaco editor, execution, parsing, diagnostics, plans, history.

## Status (2026-11-05)

Implemented and verified:

- `go vet ./...` is clean, `gofmt` is clean, and all Go test packages pass, including `internal/archtest`.
- Frontend: `tsc --noEmit` is clean, and 15 Vitest tests pass (theme, settings form, settings dialog, workbench).
- `wails3 build` produces `bin/qryvanta.exe`. The app starts, logs to `%LocalAppData%\Qryvanta\logs`,
  and serves the embedded frontend.
- `wails3 task build:cli` produces `bin/qryvanta-cli.exe`. `version`, `config path` and `config show`
  work, and unknown commands exit with code 2.

Known limitations:

- ~~The app icon is still the Wails default.~~ Resolved in #11: `tools/appicon` generates it.
- ~~No Content-Security-Policy yet.~~ Resolved in #12: see architecture decision D9a.
- ~~The Wails asset server logs every asset request at info level.~~ Resolved in #13: see architecture decision D8.
- ~~The JS bundle is 513 kB (a single chunk). Code-splitting is planned for Phase 3, when Monaco is added.~~
  Resolved in #29: Monaco is a separate chunk loaded with the first editor tab (see D9b).
- The module path is `github.com/TuanFaied/qryvanta`, matching the repository URL.

## Done when

- `go vet ./...` and `go test ./...` pass
- `pnpm -C frontend build` (tsc + vite) passes
- `wails3 build` produces `bin/qryvanta.exe`, and the window opens showing the shell
- `go build ./cmd/qryvanta-cli` works, and `qryvanta-cli version` and `config show` print real data
- Settings changes survive a restart (written to `config.json`)
