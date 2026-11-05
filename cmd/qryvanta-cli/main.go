// Command qryvanta-cli is the Qryvanta command-line interface.
//
// It uses the same core as the desktop application. Output is human-readable by default;
// commands that produce data also support --json for scripting.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

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
	err := dispatch(args, stdout)
	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp):
		fmt.Fprint(stdout, usage)
		return 0
	case errors.Is(err, errUsage):
		fmt.Fprintf(stderr, "qryvanta-cli: %v\n\n%s", err, usage)
		return 2
	default:
		fmt.Fprintf(stderr, "qryvanta-cli: %v\n", err)
		return 1
	}
}

func dispatch(args []string, stdout io.Writer) error {
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
