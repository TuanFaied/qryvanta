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

## Workflow

1. Every change starts from an issue. Use the issue templates.
2. Create a branch from `main` named after the issue:

   ```
   <type>/<issue-number>-<short-kebab-description>
   ```

   | Type | Use for |
   |---|---|
   | `feat` | New functionality |
   | `fix` | Bug fixes |
   | `docs` | Documentation only |
   | `refactor` | Code changes that do not change behavior |
   | `test` | Tests only |
   | `chore` | Build, dependencies, tooling |
   | `ci` | GitHub Actions and release automation |

   Examples: `feat/3-connection-profiles`, `fix/12-settings-timeout-validation`, `docs/7-threat-model`.
3. Open a pull request against `main` using the template, and link the issue with `Closes #<n>`.
4. CI (Go vet/test, frontend typecheck/test/build, and the Wails desktop build) must pass.
5. Pull requests are squash-merged, so the PR title becomes the commit on `main`.

## Commit messages and PR titles

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(connection): add connection profile store
fix(settings): reject zero query timeout
docs: describe probe approval flow
```

Scopes match package or feature names: `config`, `logging`, `connection`, `executor`, `parser`,
`analyzer`, `plan`, `ui`, `cli`, `ci`, and so on.

## Labels

| Prefix | Meaning |
|---|---|
| `type:` | bug, feature, task, diagnostic-rule, docs, chore |
| `area:` | core, connectivity, execution, parser, diagnostics, plans, ui, cli, security, ci, deps |
| `phase:` | Roadmap phase the work belongs to |
| `status:` | triage, blocked |
| `good first issue`, `help wanted` | Contributor-friendly issues |

## License

By contributing, you agree that your contributions are licensed under the Apache License 2.0.
