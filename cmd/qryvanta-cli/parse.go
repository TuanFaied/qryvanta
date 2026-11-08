package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/parser"
)

// cmdParse prints the batches and syntax errors of a script, using the same parser as the
// desktop app. It exits 1 when the script has syntax errors, so it can be used as a check.
func cmdParse(ctx context.Context, args []string, opts app.Options, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("parse", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var o runOptions
	fs.StringVar(&o.file, "file", "", "read the script from a .sql file")
	fs.StringVar(&o.query, "query", "", "the script to parse")
	asJSON := fs.Bool("json", false, "print JSON")
	compat := fs.Int("compat", 0, "SQL Server compatibility level (80-180; default: the parser's default)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%w: unexpected argument %q (pass SQL with --query, --file or stdin)", errUsage, fs.Arg(0))
	}
	if o.file != "" && o.query != "" {
		return fmt.Errorf("%w: use either --file or --query, not both", errUsage)
	}
	sql, err := readBatch(o)
	if err != nil {
		return err
	}

	core, err := app.New(opts)
	if err != nil {
		return err
	}
	defer core.Close()

	res, err := core.Parser().Parse(ctx, sql, parser.Options{CompatLevel: *compat})
	if err != nil {
		return err
	}
	if *asJSON {
		if err := writeJSON(stdout, res); err != nil {
			return err
		}
	} else {
		for _, b := range res.Batches {
			repeat := ""
			if b.Repeat > 1 {
				repeat = fmt.Sprintf(", runs %d times (GO %d)", b.Repeat, b.Repeat)
			}
			fmt.Fprintf(stdout, "Batch %d: line %d, %d characters%s\n", b.Index+1, b.Line, b.Length, repeat)
		}
		if len(res.Batches) == 0 {
			fmt.Fprintln(stdout, "No batches (the script is empty or only comments).")
		}
		for _, d := range res.Diagnostics {
			fmt.Fprintf(stderr, "Line %d, column %d: %s (parser error %d)\n", d.Line, d.Column, d.Message, d.Number)
		}
	}
	if n := len(res.Diagnostics); n > 0 {
		return &exitError{code: exitQueryError, silent: !*asJSON, err: fmt.Errorf("the script has %d syntax error(s)", n)}
	}
	return nil
}
