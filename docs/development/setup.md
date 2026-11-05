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
| .NET SDK | 8+ | Needed from Phase 4 for the parser sidecar |

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
| `wails3 build` | Production desktop build → `bin/qryvanta.exe` |
| `wails3 task build:cli` | CLI build → `bin/qryvanta-cli.exe` |
| `wails3 task test` | `go vet`, Go tests, and frontend tests |
| `wails3 task common:generate:bindings` | Regenerates TypeScript bindings after changing a bound Go service |
| `wails3 package` | NSIS installer (requires NSIS, Phase 10) |

## Project layout

See [the architecture document](../architecture/overview.md#3-repository-layout).

## Integration tests against SQL Server

Integration tests use the `integration` build tag and are skipped unless a server is configured.
Any SQL Server 2016+ works, including a local SQL Server Express (shared memory and named pipes
are supported, so TCP does not need to be enabled).

| Variable | Meaning |
|---|---|
| `QRYVANTA_TEST_MSSQL_SERVER` | `host`, `host\instance` or IP. If unset, the tests are skipped. |
| `QRYVANTA_TEST_MSSQL_PORT` | Optional TCP port |
| `QRYVANTA_TEST_MSSQL_USER` / `QRYVANTA_TEST_MSSQL_PASSWORD` | SQL login. If no user is set, Windows authentication is used. |
| `QRYVANTA_TEST_MSSQL_TRUST` | `true` to accept a self-signed certificate (local development only) |

PowerShell example for a local Express instance:

```powershell
$env:QRYVANTA_TEST_MSSQL_SERVER = 'localhost\SQLEXPRESS'; $env:QRYVANTA_TEST_MSSQL_TRUST = 'true'
```

```bash
go test -tags integration ./internal/dialect/mssql/
```

The tests only run read-only queries (`SERVERPROPERTY`, `APP_NAME()`, `WAITFOR DELAY`).
A Docker SQL Server with the fixture database is tracked in #7.

## Trying connections from the CLI

```bash
qryvanta-cli connections add --name "Local Express" --server 'localhost\SQLEXPRESS' --auth windows --trust-server-certificate
```

```bash
qryvanta-cli connections test "Local Express"
```

For SQL authentication, pipe the password on stdin so it never appears in shell history:
`... --auth sql --user reader --password-stdin`.

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
