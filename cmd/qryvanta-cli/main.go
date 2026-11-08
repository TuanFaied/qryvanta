// Command qryvanta-cli is the Qryvanta command-line interface.
//
// It uses the same core as the desktop application. Output is human-readable by default;
// commands that produce data also support --json for scripting.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/version"
)

const usage = `Qryvanta command-line interface

Usage:
  qryvanta-cli <command> [flags]

Commands:
  version        Print version information
  config path    Print the configuration file path
  config show    Print the effective configuration (JSON)
  connections list [--json]
                 List saved connection profiles (passwords are never shown)
  connections add --name N --server S [--port P] [--database D]
                  [--auth sql|windows] [--user U] [--password-stdin]
                  [--encrypt mandatory|strict|optional] [--trust-server-certificate]
                  [--read-only-intent] [--timeout SECONDS]
                 Save a profile. The password is read from stdin, never from a flag,
                 and stored in the OS credential manager.
  connections test NAME|ID [--json]
                 Connect, report the server version and login, and disconnect
  connections remove NAME|ID
                 Delete a profile and its stored password
  run [--conn NAME|ID] [--database DB] [--file query.sql | --query SQL]
      [--format table|json|csv] [--max-rows N] [--timeout SECONDS] [--allow-writes]
                 Run a batch with the desktop app's engine and protections. SQL comes
                 from --query, --file or stdin. Read-only mode follows the settings;
                 --allow-writes turns it off for this run. Results go to stdout,
                 messages to stderr. Exit codes: 0 success, 1 query error, 2 usage
                 error, 3 timed out or cancelled (Ctrl+C cancels on the server).
  parse [--file query.sql | --query SQL] [--compat LEVEL] [--json]
                 Split a script into GO batches and report syntax errors with the
                 T-SQL parser (qryvanta-tsqlparser next to this program, or at
                 QRYVANTA_PARSER_PATH). Exit codes: 0 no errors, 1 syntax errors or
                 parser unavailable, 2 usage error.

Global flags:
  --config PATH  Use this configuration file instead of the default
  -h, --help     Show help
`

// errUsage marks errors caused by invalid command-line usage (exit code 2).
var errUsage = errors.New("usage error")

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	// Ctrl+C cancels the context; a running query is then cancelled on the server.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return report(dispatch(ctx, args, stdout, stderr), stdout, stderr)
}

// report prints err as appropriate and returns the process exit code.
func report(err error, stdout, stderr io.Writer) int {
	var ee *exitError
	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp):
		fmt.Fprint(stdout, usage)
		return 0
	case errors.Is(err, errUsage):
		fmt.Fprintf(stderr, "qryvanta-cli: %v\n\n%s", err, usage)
		return 2
	case errors.As(err, &ee):
		if !ee.silent {
			fmt.Fprintf(stderr, "qryvanta-cli: %v\n", err)
		}
		return ee.code
	default:
		fmt.Fprintf(stderr, "qryvanta-cli: %v\n", err)
		return 1
	}
}

func dispatch(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("qryvanta-cli", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", "", "configuration file path")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return flag.ErrHelp
	}

	switch rest[0] {
	case "version":
		return cmdVersion(rest[1:], stdout)
	case "config":
		return cmdConfig(rest[1:], *configPath, stdout)
	case "connections":
		return cmdConnections(rest[1:], app.Options{ConfigPath: *configPath, DisableFileLog: true}, stdout)
	case "run":
		return cmdRun(ctx, rest[1:], app.Options{ConfigPath: *configPath, DisableFileLog: true}, stdout, stderr)
	case "parse":
		return cmdParse(ctx, rest[1:], app.Options{ConfigPath: *configPath, DisableFileLog: true}, stdout, stderr)
	case "help":
		return flag.ErrHelp
	default:
		return fmt.Errorf("%w: unknown command %q", errUsage, rest[0])
	}
}

func cmdVersion(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	v := version.Get()
	if *asJSON {
		return writeJSON(stdout, v)
	}
	_, err := fmt.Fprintf(stdout, "qryvanta %s (commit %s, %s, %s)\n", v.Version, v.Commit, v.GoVersion, v.Platform)
	return err
}

func cmdConfig(args []string, configPath string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: config needs a subcommand (path, show)", errUsage)
	}
	core, err := app.New(app.Options{ConfigPath: configPath, DisableFileLog: true})
	if err != nil {
		return err
	}
	defer core.Close()

	switch args[0] {
	case "path":
		_, err := fmt.Fprintln(stdout, core.ConfigPath())
		return err
	case "show":
		return writeJSON(stdout, core.Config())
	default:
		return fmt.Errorf("%w: unknown config subcommand %q", errUsage, args[0])
	}
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
