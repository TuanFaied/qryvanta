package app

import (
	"context"

	"github.com/TuanFaied/qryvanta/internal/analyzer"
	"github.com/TuanFaied/qryvanta/internal/parser"
)

// Analysis is the result of analyzing a script.
type Analysis struct {
	analyzer.Report
	// Statements is the number of top-level statements that were analyzed.
	Statements int `json:"statements"`
	// SyntaxErrors is the number of syntax errors; the batches that have them were not analyzed.
	SyntaxErrors int `json:"syntaxErrors"`
}

// Analyze parses sql with the QueryModel and runs the built-in diagnostic rules over it. It needs
// no connection and executes nothing. A parser that cannot run is an error (wrapping
// parser.ErrUnavailable); rule failures are part of the result and are logged.
func (c *Core) Analyze(ctx context.Context, sql string) (Analysis, error) {
	res, err := c.parser.Parse(ctx, sql, parser.Options{Model: true})
	if err != nil {
		return Analysis{}, err
	}
	rep, err := analyzer.NewEngine(analyzer.Default()).Analyze(ctx, &analyzer.Input{SQL: sql, Result: res})
	if err != nil {
		return Analysis{}, err
	}
	a := Analysis{Report: rep, SyntaxErrors: len(res.Diagnostics)}
	for _, b := range res.Batches {
		a.Statements += len(b.Statements)
	}
	for _, f := range rep.Failures {
		c.log.Warn("a diagnostic rule failed", "rule", f.RuleID, "error", f.Message)
	}
	c.log.Debug("query analyzed", "statements", a.Statements, "findings", len(rep.Findings),
		"rule_failures", len(rep.Failures), "syntax_errors", a.SyntaxErrors)
	return a, nil
}
