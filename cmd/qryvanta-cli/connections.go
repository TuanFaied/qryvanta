package main

import (
	"flag"
	"fmt"
	"io"
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

func cmdConnections(args []string, opts app.Options, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: connections needs a subcommand (list)", errUsage)
	}
	switch args[0] {
	case "list":
		return cmdConnectionsList(args[1:], opts, stdout)
	default:
		return fmt.Errorf("%w: unknown connections subcommand %q", errUsage, args[0])
	}
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
