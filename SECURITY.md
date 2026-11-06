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
- Read-only execution is on by default. The documentation is explicit that application-level
  checks are best effort, and that **a read-only database login is the only reliable guarantee.**
- Generated query changes are never executed automatically.
- No telemetry. Nothing is sent anywhere except to the databases you connect to.
- The desktop webview runs under a strict Content-Security-Policy: no inline or external
  scripts, no eval, and no network connections other than to the app's own backend.

The full threat model will be published in `docs/security/` before the first release.
