# Security policy

## Reporting a vulnerability

Please **do not** open a public issue for security problems. Report them privately through
GitHub's "Report a vulnerability" feature on this repository (Security → Advisories).
You will get an acknowledgement within 7 days.

## Supported versions

Qryvanta has not had a release yet. Once it does, security fixes will go to the latest minor release.

## Security model (summary)

Qryvanta connects to your databases with your credentials, so it is designed to keep them safe:

- Passwords are stored in the operating system's credential store (Windows Credential Manager),
  never in configuration files, and are never sent to the UI layer.
- Logs mask password-like fields and scrub secrets from connection strings and driver errors.
- Read-only mode is on by default:
  - **Statement check (hard):** before anything is sent to the server, every statement is parsed
    and checked against an allowlist. Queries are allowed, and so are writes to temporary tables
    (`#name`) and table variables. Writes to tables (including through `OUTPUT INTO`, CTEs and
    `SELECT INTO`), DDL, `EXEC` and dynamic SQL, transaction statements, `USE`,
    `OPENROWSET`/`OPENQUERY`, and SET options that change transactions are refused with the
    reason. A script the parser cannot read, or any script when the parser is unavailable, is
    refused rather than guessed at.
  - **Rolled-back transaction (best effort):** what runs is inside a transaction that is always
    rolled back, with a lock wait limit. Effects outside the transaction are not undone: functions
    with side effects (CLR, `xp_` procedures called from functions), linked servers reached through
    four-part names, consumed identity and sequence values, e-mail.
  - Turning read-only mode off requires a confirmation.
  - Qryvanta also tells you whether your login could modify data. **A read-only database login
    is the only reliable guarantee.**
- Generated query changes are never executed automatically.
- No telemetry. Nothing is sent anywhere except to the databases you connect to.
- The desktop webview runs under a strict Content-Security-Policy: no inline or external
  scripts, no eval, and no network connections other than to the app's own backend.

The full threat model will be published in `docs/security/` before the first release.
