// Package integration holds end-to-end tests that need a real SQL Server loaded with the
// QryvantaSample fixture database (docker compose run --rm fixtures).
//
// The tests use the `integration` build tag and are skipped when QRYVANTA_TEST_MSSQL_SERVER
// is not set. See docs/development/setup.md.
package integration
