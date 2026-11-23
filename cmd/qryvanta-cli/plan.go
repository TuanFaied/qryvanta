package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/config"
	"github.com/TuanFaied/qryvanta/internal/executor"
)

type planOptions struct {
	conn, database, file, query, format, out string
	estimated, actual                        bool
	timeout                                  int
}

func parsePlanFlags(args []string) (planOptions, error) {
	var o planOptions
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.conn, "conn", "", "connection name or ID")
	fs.StringVar(&o.database, "database", "", "database to run in")
	fs.StringVar(&o.file, "file", "", "read the batch from a .sql file")
	fs.StringVar(&o.query, "query", "", "the batch")
	fs.StringVar(&o.format, "format", "xml", "xml or json")
	fs.StringVar(&o.out, "out", "", "write each plan to a .sqlplan file in this directory")
	fs.BoolVar(&o.estimated, "estimated", false, "compile the query without running it")
	fs.BoolVar(&o.actual, "actual", false, "run the query and capture the actual plans")
	fs.IntVar(&o.timeout, "timeout", 0, "timeout in seconds (default: the configured timeout)")
	if err := fs.Parse(args); err != nil {
		return o, fmt.Errorf("%w: %v", errUsage, err)
	}
	switch {
	case fs.NArg() > 0:
		return o, fmt.Errorf("%w: unexpected argument %q (pass SQL with --query, --file or stdin)", errUsage, fs.Arg(0))
	case o.estimated == o.actual:
		return o, fmt.Errorf("%w: choose --estimated or --actual", errUsage)
	case o.format != "xml" && o.format != "json":
		return o, fmt.Errorf("%w: --format must be xml or json", errUsage)
	case o.file != "" && o.query != "":
		return o, fmt.Errorf("%w: use either --file or --query, not both", errUsage)
	case o.timeout != 0 && (o.timeout < config.MinTimeoutSeconds || o.timeout > config.MaxTimeoutSeconds):
		return o, fmt.Errorf("%w: --timeout must be between %d and %d seconds", errUsage, config.MinTimeoutSeconds, config.MaxTimeoutSeconds)
	}
	return o, nil
}

// planResult is the JSON output of "plan".
type planResult struct {
	Connection string            `json:"connection"`
	Database   string            `json:"database"`
	Mode       executor.PlanMode `json:"mode"`
	Outcome    executor.Outcome  `json:"outcome"`
	// Complete is false when the run stopped early or a plan was too large; Reason says why.
	Complete bool               `json:"complete"`
	Reason   string             `json:"reason,omitempty"`
	TooLarge int                `json:"tooLarge"`
	Plans    []planDocument     `json:"plans"`
	Messages []executor.Message `json:"messages"`
	Refusals []executor.Refusal `json:"refusals"`
	Error    string             `json:"error,omitempty"`
}

// planDocument is one plan. Batch is 1-based, as in the messages.
type planDocument struct {
	Batch     int    `json:"batch"`
	Execution int    `json:"execution"`
	Statement int    `json:"statement"`
	Bytes     int    `json:"bytes"`
	XML       string `json:"xml"`
	File      string `json:"file,omitempty"`
}

// planSink keeps plans and drops every result set: with --actual the query's own results are
// never printed.
type planSink struct {
	plans    []executor.Plan
	messages []executor.Message
}

func (s *planSink) ResultSet(int, []executor.Column) error { return nil }
func (s *planSink) Rows(int, [][]executor.Value) error     { return nil }
func (s *planSink) Message(m executor.Message) error {
	s.messages = append(s.messages, m)
	return nil
}
func (s *planSink) Plan(p executor.Plan) error {
	s.plans = append(s.plans, p)
	return nil
}

func cmdPlan(ctx context.Context, args []string, opts app.Options, stdout, stderr io.Writer) error {
	o, err := parsePlanFlags(args)
	if err != nil {
		return err
	}
	sql, err := readBatch(runOptions{file: o.file, query: o.query})
	if err != nil {
		return err
	}
	if o.out != "" {
		if fi, err := os.Stat(o.out); err != nil || !fi.IsDir() {
			return fmt.Errorf("%w: --out must be an existing directory", errUsage)
		}
	}

	core, err := app.New(opts)
	if err != nil {
		return err
	}
	defer core.Close()
	prof, err := pickConnection(core.Connections(), o.conn)
	if err != nil {
		return err
	}
	server, err := core.Pool().Connect(ctx, prof.ID)
	if err != nil {
		return fmt.Errorf("connection %q failed: %w", prof.Name, err)
	}
	database := o.database
	if database == "" {
		database = server.Database
	}
	limits := core.ExecutionLimits()
	if o.timeout > 0 {
		limits.Timeout = time.Duration(o.timeout) * time.Second
	}
	mode := executor.PlanEstimated
	if o.actual {
		mode = executor.PlanActual
	}

	sink := &planSink{}
	sum := core.Execute(ctx, prof.ID, executor.Request{SQL: sql, Database: database, Limits: limits, Plan: mode}, sink)
	res := planResult{Connection: prof.Name, Database: database, Mode: mode, Outcome: sum.Outcome, Plans: []planDocument{},
		Messages: sink.messages, Refusals: sum.Refusals, Error: sum.Error}
	if res.Messages == nil {
		res.Messages = []executor.Message{}
	}
	if sum.Plan != nil {
		res.Complete, res.Reason, res.TooLarge = sum.Plan.Complete, sum.Plan.Reason, sum.Plan.TooLarge
	}
	for _, p := range sink.plans {
		doc := planDocument{Batch: p.Batch + 1, Execution: p.Execution, Statement: p.Statement, Bytes: len(p.XML), XML: p.XML}
		if o.out != "" {
			doc.File = filepath.Join(o.out, fmt.Sprintf("plan-%d-%d-%d.sqlplan", doc.Batch, doc.Execution, doc.Statement))
			if err := os.WriteFile(doc.File, []byte(p.XML), 0o644); err != nil {
				return err
			}
		}
		res.Plans = append(res.Plans, doc)
	}

	switch {
	case o.format == "json":
		if err := writeJSON(stdout, res); err != nil {
			return err
		}
	case o.out != "":
		for _, d := range res.Plans {
			fmt.Fprintln(stdout, d.File)
		}
	default:
		for i, d := range res.Plans {
			if i > 0 {
				fmt.Fprintln(stdout)
			}
			fmt.Fprintln(stdout, d.XML)
		}
		if len(res.Plans) > 1 {
			fmt.Fprintf(stderr, "qryvanta-cli: %d plan documents; SSMS opens one per file, so use --out DIR to save each as a .sqlplan file.\n", len(res.Plans))
		}
	}
	if o.format != "json" {
		writeMessages(stderr, runResult{Messages: res.Messages})
		for _, r := range res.Refusals {
			fmt.Fprintf(stderr, "Refused, line %d, column %d: %s\n", r.Line, r.Column, r.Reason)
		}
		if res.Reason != "" {
			fmt.Fprintf(stderr, "qryvanta-cli: %s\n", res.Reason)
		}
		if len(res.Plans) == 0 && sum.Outcome == executor.OutcomeSucceeded && sum.ErrorCount == 0 {
			fmt.Fprintln(stderr, "qryvanta-cli: the server returned no plans (a batch with only SET or control-of-flow statements has none).")
		}
	}
	return exitFor(runResult{Outcome: sum.Outcome, Error: sum.Error, ErrorCount: sum.ErrorCount, TimeoutSecs: int(limits.Timeout / time.Second)})
}
