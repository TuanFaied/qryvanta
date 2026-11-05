package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/connection"
)

// connectionView is the CLI's output shape. It reports whether a password is stored, never
// the password itself.
type connectionView struct {
	connection.Profile
	HasPassword bool `json:"hasPassword"`
}

// stdin is read by --password-stdin. Tests replace it.
var stdin io.Reader = os.Stdin

func cmdConnections(args []string, opts app.Options, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: connections needs a subcommand (list, add, test, remove)", errUsage)
	}
	switch args[0] {
	case "list":
		return cmdConnectionsList(args[1:], opts, stdout)
	case "add":
		return cmdConnectionsAdd(args[1:], opts, stdout)
	case "test":
		return cmdConnectionsTest(args[1:], opts, stdout)
	case "remove":
		return cmdConnectionsRemove(args[1:], opts, stdout)
	default:
		return fmt.Errorf("%w: unknown connections subcommand %q", errUsage, args[0])
	}
}

func cmdConnectionsAdd(args []string, opts app.Options, stdout io.Writer) error {
	fs := flag.NewFlagSet("connections add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var p connection.Profile
	var auth, encrypt string
	fs.StringVar(&p.Name, "name", "", "profile name")
	fs.StringVar(&p.Server, "server", "", `host, host\instance or IP address`)
	fs.IntVar(&p.Port, "port", 0, "TCP port (0 = default)")
	fs.StringVar(&p.Database, "database", "", "initial database")
	fs.StringVar(&auth, "auth", string(connection.AuthSQL), "sql or windows")
	fs.StringVar(&p.Username, "user", "", "SQL Server login")
	fs.StringVar(&encrypt, "encrypt", string(connection.EncryptMandatory), "mandatory, strict or optional")
	fs.BoolVar(&p.TrustServerCertificate, "trust-server-certificate", false, "skip certificate validation")
	fs.BoolVar(&p.ReadOnlyIntent, "read-only-intent", false, "ApplicationIntent=ReadOnly")
	fs.IntVar(&p.ConnectTimeoutSeconds, "timeout", 0, "connect timeout in seconds (0 = app default)")
	passwordStdin := fs.Bool("password-stdin", false, "read the password from the first line of stdin")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%w: unexpected argument %q", errUsage, fs.Arg(0))
	}
	p.Auth, p.Encrypt = connection.AuthMethod(auth), connection.EncryptMode(encrypt)

	password := ""
	if *passwordStdin {
		pw, err := readPassword(stdin)
		if err != nil {
			return err
		}
		password = pw
	}

	core, err := app.New(opts)
	if err != nil {
		return err
	}
	defer core.Close()

	saved, err := core.Connections().Create(p, password)
	if err != nil {
		return describeValidation(err)
	}
	note := ""
	if saved.UsesPassword() && password == "" {
		note = " (no password stored; add one with --password-stdin)"
	}
	_, err = fmt.Fprintf(stdout, "Saved connection %q (%s)%s\n", saved.Name, saved.ID, note)
	return err
}

func cmdConnectionsTest(args []string, opts app.Options, stdout io.Writer) error {
	fs := flag.NewFlagSet("connections test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "print JSON")
	target, err := parseWithTarget(fs, args)
	if err != nil {
		return err
	}

	core, err := app.New(opts)
	if err != nil {
		return err
	}
	defer core.Close()

	prof, err := resolveProfile(core.Connections(), target)
	if err != nil {
		return err
	}
	res, err := core.Pool().Test(context.Background(), prof.ID)
	if err != nil {
		return fmt.Errorf("connection %q failed: %w", prof.Name, err)
	}
	if *asJSON {
		return writeJSON(stdout, res)
	}
	s := res.Server
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Connected to %q in %d ms\n", prof.Name, res.LatencyMs)
	fmt.Fprintf(tw, "Server\t%s\n", s.ServerName)
	fmt.Fprintf(tw, "Version\t%s %s\n", s.ProductVersion, s.ProductLevel)
	fmt.Fprintf(tw, "Edition\t%s\n", s.Edition)
	fmt.Fprintf(tw, "Database\t%s\n", s.Database)
	fmt.Fprintf(tw, "Login\t%s\n", s.Login)
	return tw.Flush()
}

func cmdConnectionsRemove(args []string, opts app.Options, stdout io.Writer) error {
	fs := flag.NewFlagSet("connections remove", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	target, err := parseWithTarget(fs, args)
	if err != nil {
		return err
	}
	core, err := app.New(opts)
	if err != nil {
		return err
	}
	defer core.Close()

	prof, err := resolveProfile(core.Connections(), target)
	if err != nil {
		return err
	}
	if err := core.Connections().Delete(prof.ID); err != nil {
		return err
	}
	msg := fmt.Sprintf("Removed connection %q", prof.Name)
	if prof.UsesPassword() {
		msg += " and its stored password"
	}
	_, err = fmt.Fprintln(stdout, msg)
	return err
}

// parseWithTarget accepts the profile name or ID before or after the flags.
func parseWithTarget(fs *flag.FlagSet, args []string) (string, error) {
	var target string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		target, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return "", fmt.Errorf("%w: %v", errUsage, err)
	}
	if target == "" && fs.NArg() > 0 {
		target = fs.Arg(0)
	} else if fs.NArg() > 0 {
		return "", fmt.Errorf("%w: unexpected argument %q", errUsage, fs.Arg(0))
	}
	if target == "" {
		return "", fmt.Errorf("%w: %s needs a connection name or ID", errUsage, fs.Name())
	}
	return target, nil
}

// resolveProfile finds a profile by exact ID or case-insensitive name.
func resolveProfile(m *connection.Manager, target string) (connection.Profile, error) {
	profiles, err := m.List()
	if err != nil {
		return connection.Profile{}, err
	}
	for _, p := range profiles {
		if p.ID == target {
			return p, nil
		}
	}
	for _, p := range profiles {
		if strings.EqualFold(p.Name, target) {
			return p, nil
		}
	}
	return connection.Profile{}, fmt.Errorf("no saved connection named %q", target)
}

func readPassword(r io.Reader) (string, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read password from stdin: %w", err)
	}
	pw := strings.TrimRight(line, "\r\n")
	if pw == "" {
		return "", errors.New("--password-stdin was given but stdin was empty")
	}
	return pw, nil
}

// describeValidation formats per-field validation errors one per line.
func describeValidation(err error) error {
	var ve *connection.ValidationError
	if !errors.As(err, &ve) {
		return err
	}
	var b strings.Builder
	b.WriteString("invalid connection:")
	for _, k := range slices.Sorted(maps.Keys(ve.Fields)) {
		fmt.Fprintf(&b, "\n  %s %s", k, ve.Fields[k])
	}
	return errors.New(b.String())
}

func cmdConnectionsList(args []string, opts app.Options, stdout io.Writer) error {
	fs := flag.NewFlagSet("connections list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}

	core, err := app.New(opts)
	if err != nil {
		return err
	}
	defer core.Close()

	profiles, err := core.Connections().List()
	if err != nil {
		return err
	}
	views := make([]connectionView, 0, len(profiles))
	for _, p := range profiles {
		v := connectionView{Profile: p}
		if p.UsesPassword() {
			has, err := core.Connections().HasPassword(p.ID)
			if err != nil {
				return err
			}
			v.HasPassword = has
		}
		views = append(views, v)
	}

	if *asJSON {
		return writeJSON(stdout, views)
	}
	if len(views) == 0 {
		_, err := fmt.Fprintln(stdout, "No saved connections.")
		return err
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSERVER\tDATABASE\tAUTH\tPASSWORD\tID")
	for _, v := range views {
		server := v.Server
		if v.Port > 0 {
			server = fmt.Sprintf("%s,%d", v.Server, v.Port)
		}
		db := v.Database
		if db == "" {
			db = "(default)"
		}
		auth := "Windows"
		pw := "-"
		if v.Auth == connection.AuthSQL {
			auth = "SQL: " + v.Username
			pw = "missing"
			if v.HasPassword {
				pw = "stored"
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", v.Name, server, db, auth, pw, v.ID)
	}
	return tw.Flush()
}
