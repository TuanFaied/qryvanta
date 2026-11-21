// Package parsertest provides a fake T-SQL parser for tests of packages that need one (read-only
// mode refuses to run anything without a parser) but do not test parsing itself.
package parsertest

import (
	"context"
	"unicode/utf16"

	"github.com/TuanFaied/qryvanta/internal/parser"
)

// Fake returns the whole script as one batch. With Options.Model, the batch holds the
// statements from Statements, or one SELECT covering the script when Statements is nil, so
// read-only mode allows it. Err, when set, is returned instead.
type Fake struct {
	Err        error
	Statements func(sql string) []parser.Statement
}

func (f Fake) Parse(_ context.Context, sql string, opts parser.Options) (*parser.Result, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	n := len(utf16.Encode([]rune(sql)))
	b := parser.Batch{Text: sql, Length: n, ByteLength: len(sql), Line: 1, Column: 1, Repeat: 1}
	res := &parser.Result{CompatLevel: 160}
	if opts.Model {
		res.ModelVersion = parser.ModelVersion
		if f.Statements != nil {
			b.Statements = f.Statements(sql)
		} else {
			b.Statements = []parser.Statement{{Kind: parser.StmtSelect, Type: "SelectStatement",
				Span: parser.Span{Length: n, Line: 1, Column: 1}}}
		}
	}
	res.Batches = []parser.Batch{b}
	return res, nil
}

func (Fake) Check(context.Context) parser.Status { return parser.Status{Available: true} }
func (Fake) Close() error                        { return nil }
