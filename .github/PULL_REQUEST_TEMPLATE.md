## Summary

<!-- What does this change do, and why? -->

Closes #

## Changes

-

## How it was tested

<!-- Commands you ran and what you checked by hand. -->

- [ ] `go vet ./...` and `go test ./...`
- [ ] `pnpm -C frontend typecheck` and `pnpm -C frontend test`
- [ ] Integration tests against SQL Server (if this touches connectivity or execution)
- [ ] Checked in the desktop app (if this touches the UI)

## Checklist

- [ ] The branch name follows `<type>/<issue>-<short-description>` (see CONTRIBUTING.md)
- [ ] Core packages do not import Wails or the frontend (`internal/archtest` passes)
- [ ] No secrets can reach logs, errors, exports or JavaScript
- [ ] No fabricated statistics, row counts or plan values; findings classified as *Confirmed* have evidence
- [ ] Documentation and CHANGELOG updated where behavior changed

## Screenshots

<!-- For UI changes. Remove this section otherwise. -->
