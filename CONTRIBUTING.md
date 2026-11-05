# Contributing to Qryvanta

Thanks for your interest in Qryvanta! The project is in early development, so the best way to
help right now is to open an issue describing a SQL logic bug you hit in real life. Those cases
become diagnostic rules and test fixtures.

## Before you start

- Read the [architecture document](docs/architecture/overview.md), especially the dependency
  rules (section 4) and the evidence model (D3).
- For anything larger than a small fix, open an issue first so we can agree on the approach.

## Development

Setup instructions are in [docs/development/setup.md](docs/development/setup.md).
Before opening a pull request:

```bash
go vet ./...
```

```bash
go test ./...
```

```bash
pnpm -C frontend typecheck
```

```bash
pnpm -C frontend test
```

## Guidelines

- **Core stays UI-free.** Only `internal/desktop` and `cmd/` may import Wails. `internal/archtest` enforces this.
- **No fabricated data.** Never invent execution statistics, row counts or plan values. Show
  "unavailable" instead.
- **Findings need evidence.** A rule may classify a finding as *Confirmed* only when the evidence proves it.
- **Secrets never reach logs, errors, exports or JavaScript.** Use `internal/security` helpers.
- **Tests with every change.** Diagnostic rules need a fixture query that triggers them and one
  that must not trigger them.
- Keep dependencies minimal and Go code idiomatic (`gofmt`, wrapped errors, `context.Context` first).
- Keep TypeScript strict. Components call the backend only through `frontend/src/services`.
- Update the documentation in the same pull request when behavior or architecture changes.

## Commit messages

Use short, imperative subject lines (`Add JOIN fan-out probe`). Reference issues where relevant.

## License

By contributing, you agree that your contributions are licensed under the Apache License 2.0.
