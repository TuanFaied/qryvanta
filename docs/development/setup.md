# Development setup

## Requirements

| Tool | Version | Notes |
|---|---|---|
| Go | 1.26+ | Wails v3.1 requires Go 1.26. `go` downloads the toolchain automatically if needed. |
| Node.js | 22+ | |
| pnpm | 10+ | The default package manager (`PACKAGE_MANAGER=npm` also works). |
| Wails CLI | v3.1.0 | `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0` |
| WebView2 | any current | Preinstalled on Windows 11 |
| Docker | optional | Needed from Phase 2 for the SQL Server integration database |
| .NET SDK | 10.0 | For the parser sidecar; `wails3 build` builds it. `winget install Microsoft.DotNet.SDK.10`. Users of the app do not need .NET. |

Check your environment with:

```bash
wails3 doctor
```

## Common tasks

All tasks are defined in `Taskfile.yml` and run through `wails3 task <name>` (or `task <name>`
if you have go-task installed).

| Command | What it does |
|---|---|
| `wails3 dev` | Runs the desktop app with hot reload (Vite on port 9245) |
| `wails3 build` | Production desktop build → `bin/qryvanta.exe`, plus the parser sidecar |
| `wails3 task build:cli` | CLI build → `bin/qryvanta-cli.exe` |
| `wails3 task common:build:sidecar` | Parser sidecar only → `bin/qryvanta-tsqlparser.exe` |
| `wails3 task test` | `go vet`, Go tests, and frontend tests |
| `wails3 task common:test:sidecar` | The sidecar's .NET unit tests |
| `wails3 task common:generate:bindings` | Regenerates TypeScript bindings after changing a bound Go service |
| `wails3 package` | NSIS installer (requires NSIS, Phase 10) |

## Testing the UI without touching your settings

Set `QRYVANTA_CONFIG` to a scratch file before starting the app. Settings and saved connections
then go there instead of `%AppData%\Qryvanta`. Saved passwords still go to Windows Credential
Manager, so prefer Windows-authentication profiles, or delete test connections from the app
(that removes their credentials too).

To drive the real app from a browser (for example with browser automation), build it in Wails
server mode. The frontend and the Go services are then served over HTTP on localhost:

```powershell
pnpm -C frontend build; go build -tags server -o bin/qryvanta-server.exe ./cmd/qryvanta
```

```powershell
$env:QRYVANTA_CONFIG = "$env:TEMP\qryvanta-test\config.json"; $env:WAILS_SERVER_PORT = '18080'; .\bin\qryvanta-server.exe
```

Then open `http://localhost:18080`. Server mode is a development aid only: it listens on
localhost without authentication and must not be shipped.

## App icon

The icon is generated, not drawn by hand. `tools/appicon` renders it from the same geometry as
the logo mark in the app header (`frontend/src/components/brand/Logo.tsx`), so the two always
match. It is a separate Go module, so its image library is not a dependency of Qryvanta.

```bash
cd tools/appicon && go run . -o ../../build/appicon.png -dmg ../../build/darwin -preview "$TMPDIR/icon"
```

```bash
wails3 task common:generate:icons
```

```bash
wails3 generate icons -input build/darwin/dmg-file-icon.png -macfilename build/darwin/dmg-file-icon.icns -windowsfilename "$TMPDIR/unused.ico"
```

- The first command writes `build/appicon.png`, the macOS disk-image background and volume icon,
  and 16–256 px previews for checking legibility at small sizes.
- The second regenerates `build/windows/icon.ico` and `build/darwin/icons.icns`.
- The third turns the disk-image volume icon PNG into `.icns`.
- On macOS, the second command also compiles `build/appicon.icon` (Icon Composer, which uses
  `Assets/qryvanta_glyph.svg`) into `build/darwin/Assets.car`. That file is not committed, because
  it can only be built on macOS. Without it, macOS uses `icons.icns`.

## Project layout

See [the architecture document](../architecture/overview.md#3-repository-layout).

## T-SQL parser sidecar

`sidecar/tsqlparser` is a .NET 10 program that parses T-SQL with Microsoft's ScriptDom
(architecture decision D2). The Go side talks to it over stdin and stdout; you rarely run it
by hand, but it is useful for checking what the parser sees:

```bash
wails3 task common:build:sidecar
```

```bash
printf '%s\n' '{"id":"1","op":"parse","sql":"SELECT 1\nGO\nSELECT FROM"}' | bin/qryvanta-tsqlparser.exe
```

The first line of output is a `hello` message (protocol and versions); then one response per
request. The protocol is documented in `sidecar/tsqlparser/Protocol.cs`.

- `dotnet publish -r <rid>` makes a self-contained, trimmed single file (about 13 MB), so users
  do not need .NET installed.
- Package versions are pinned in `packages.lock.json`; CI restores in locked mode. After
  changing a package reference, run `dotnet restore sidecar/Sidecar.slnx --force-evaluate`.
- Tests: `dotnet test sidecar/Sidecar.slnx` (or `wails3 task common:test:sidecar`).

## Fixture database (Docker)

`docker-compose.yml` runs SQL Server 2022 Developer edition on `127.0.0.1:14333` and loads
`QryvantaSample`, a small database whose data contains the problems Qryvanta diagnoses (see
[tests/sample_queries](../../tests/sample_queries/README.md)).

```bash
docker compose run --rm fixtures
```

That command starts SQL Server, waits until it is healthy, recreates `QryvantaSample` and the
`qryvanta_reader` login, and exits. SQL Server keeps running in the background. Run it again
at any time to reset the data. To stop the server and delete its data:

```bash
docker compose down -v
```

| Login | Password (default) | Rights |
|---|---|---|
| `sa` | `Qryvanta_Dev_2026!` | Administrator (used only by the loader) |
| `qryvanta_reader` | `Sample_R3adOnly!` | `db_datareader` and `SHOWPLAN` on `QryvantaSample`. Writes fail with error 229. |

These defaults are for a server that listens on localhost only. Override them with
`QRYVANTA_SA_PASSWORD`, `QRYVANTA_READER_PASSWORD` and `QRYVANTA_SQL_PORT`, for example in a
`.env` file next to `docker-compose.yml`, which git ignores. The SQL Server image is
linux/amd64; on Apple silicon it runs under emulation.

## Integration tests against SQL Server

Integration tests use the `integration` build tag and are skipped unless a server is configured.
Use the Docker fixture database above, or any SQL Server 2016+ (including a local SQL Server
Express: shared memory and named pipes are supported, so TCP does not need to be enabled).
`tests/integration` needs `QryvantaSample`. The connection tests in `internal/dialect/mssql`
work against any server.

| Variable | Meaning |
|---|---|
| `QRYVANTA_TEST_MSSQL_SERVER` | `host`, `host\instance` or IP. If unset, the tests are skipped. |
| `QRYVANTA_TEST_MSSQL_PORT` | Optional TCP port |
| `QRYVANTA_TEST_MSSQL_USER` / `QRYVANTA_TEST_MSSQL_PASSWORD` | SQL login. If no user is set, Windows authentication is used. |
| `QRYVANTA_TEST_MSSQL_TRUST` | `true` to accept a self-signed certificate (local development only) |

PowerShell example for the Docker fixture database (this is what CI runs, on Linux):

```powershell
$env:QRYVANTA_TEST_MSSQL_SERVER = 'localhost'; $env:QRYVANTA_TEST_MSSQL_PORT = '14333'; $env:QRYVANTA_TEST_MSSQL_USER = 'qryvanta_reader'; $env:QRYVANTA_TEST_MSSQL_PASSWORD = 'Sample_R3adOnly!'; $env:QRYVANTA_TEST_MSSQL_TRUST = 'true'
```

```bash
go test -tags integration ./internal/dialect/mssql/ ./tests/integration/ ./cmd/qryvanta-cli/
```

For a local Express instance with Windows authentication, set only
`QRYVANTA_TEST_MSSQL_SERVER = 'localhost\SQLEXPRESS'` and `QRYVANTA_TEST_MSSQL_TRUST = 'true'`.

The tests do not change data. There are two exceptions:
- an `UPDATE` sent as `qryvanta_reader`, which checks that the server rejects it
- the read-only session guard tests, which write as `sa` (`QRYVANTA_TEST_MSSQL_ADMIN_PASSWORD`).
  Their writes are rolled back, or restored by the test.

## Trying connections from the CLI

```bash
qryvanta-cli connections add --name "Local Express" --server 'localhost\SQLEXPRESS' --auth windows --trust-server-certificate
```

```bash
qryvanta-cli connections test "Local Express"
```

For SQL authentication, pipe the password on stdin so it never appears in shell history:
`... --auth sql --user reader --password-stdin`.

The CLI always uses your real configuration and Windows Credential Manager. Pass
`--config <path>` (before the command) to keep test connections in another file;
`QRYVANTA_CONFIG` only affects the desktop app.

## Running queries from the CLI

`qryvanta-cli run` uses the desktop app's engine: the same limits, the same read-only session
guard, and the same write-access check.

```bash
qryvanta-cli run --conn "Local Express" --query "SELECT TOP (5) name FROM sys.objects"
```

```bash
qryvanta-cli run --conn fixtures --file tests/sample_queries/01_employees_without_department.correct.sql --format json
```

```bash
cat report.sql | qryvanta-cli run --conn fixtures --database QryvantaSample --format csv > report.csv
```

| Flag | Meaning |
|---|---|
| `--conn NAME\|ID` | Saved connection. It can be omitted when only one connection is saved. |
| `--database DB` | Database to run in. The default is the connection's database. |
| `--query SQL` / `--file PATH` / stdin | The batch. Files can be UTF-8 (with or without BOM), UTF-16 or Windows-1252. |
| `--format table\|json\|csv` | `table` (default) caps columns at 40 characters. `json` includes result sets, messages, timings, the guard and truncation. `csv` takes a single result set, and writes NULL as an empty field. |
| `--max-rows N`, `--timeout S` | Override the configured limits for this run. |
| `--allow-writes` | Turns read-only mode off for this run, and prints a warning. |

- Results go to stdout. Messages, errors (`Msg 207, Level 16, State 1, Line 3`), row counts and
  warnings go to stderr, so output can be piped safely.
- If the login can modify data, a warning saying so is printed to stderr.
- Exit codes: `0` success, `1` query error or failed batch, `2` usage error, `3` timed out or
  cancelled. Ctrl+C cancels the query on the server.

## Notes

- `frontend/dist/.gitkeep` is committed so that `go build` and `go test ./...` work on a fresh clone
  (Go's `//go:embed` needs at least one file). The Vite build recreates it.
- `frontend/bindings` is generated but committed, so the frontend type-checks without the Wails CLI.
  Regenerate it whenever a bound service in `internal/desktop` changes.
- User data locations on Windows:
  - Config: `%AppData%\Qryvanta\config.json`
  - Connection profiles: `%AppData%\Qryvanta\connections.json` (no passwords)
  - Passwords: Windows Credential Manager, generic credentials named `Qryvanta:connection:<id>`
  - Logs: `%LocalAppData%\Qryvanta\logs\qryvanta.log` (rotated at 5 MiB, 3 backups)
