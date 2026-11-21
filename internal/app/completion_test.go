package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/TuanFaied/qryvanta/internal/parser"
)

// placeholderParser parses only text that contains want; everything else has a syntax error.
type placeholderParser struct {
	want  string
	err   error
	texts []string
}

func (p *placeholderParser) Parse(_ context.Context, sql string, _ parser.Options) (*parser.Result, error) {
	p.texts = append(p.texts, sql)
	if p.err != nil {
		return nil, p.err
	}
	n := len(utf16.Encode([]rune(sql)))
	b := parser.Batch{Text: sql, Length: n, Line: 1, Column: 1, Repeat: 1}
	res := &parser.Result{ModelVersion: parser.ModelVersion}
	if strings.Contains(sql, p.want) {
		b.Statements = []parser.Statement{{Kind: parser.StmtSelect, Span: parser.Span{Length: n},
			Query: &parser.Query{Kind: parser.QuerySelect, Span: parser.Span{Length: n},
				From: &parser.Source{Kind: parser.SourceTable, Span: parser.Span{Offset: n - 1, Length: 1},
					Name: &parser.ObjectName{Schema: "dbo", Name: "Orders"}, Alias: "o"}}}}
	} else {
		res.Diagnostics = []parser.Diagnostic{{Offset: 0, Line: 1, Column: 1, Message: "Incorrect syntax."}}
	}
	res.Batches = []parser.Batch{b}
	return res, nil
}
func (*placeholderParser) Check(context.Context) parser.Status { return parser.Status{} }
func (*placeholderParser) Close() error                        { return nil }

func TestCompletionScopeTriesPlaceholders(t *testing.T) {
	// "o." parses only once "qryvanta_cursor IS NULL" follows it (as in a WHERE).
	p := &placeholderParser{want: "o.qryvanta_cursor IS NULL"}
	c, _ := coreWith(t, p, &batchRunner{})
	sql := "SELECT * FROM dbo.Orders o WHERE o."
	scope, err := c.CompletionScope(context.Background(), sql, len(sql))
	if err != nil || !scope.Found || len(scope.Sources) != 1 || scope.Sources[0].Name != "o" {
		t.Fatalf("scope = %+v, %v", scope, err)
	}
	if len(p.texts) != 3 || p.texts[1] != sql+"qryvanta_cursor " {
		t.Errorf("attempts = %q", p.texts)
	}
}

func TestCompletionScopeGivesUpWithoutAParse(t *testing.T) {
	p := &placeholderParser{want: "never"}
	c, _ := coreWith(t, p, &batchRunner{})
	if scope, err := c.CompletionScope(context.Background(), "SELEC", 5); err != nil || scope.Found {
		t.Errorf("scope = %+v, %v", scope, err)
	}
	if len(p.texts) != 5 {
		t.Errorf("attempts = %d, want 5", len(p.texts))
	}
	// After a word, the placeholder is separated by a space.
	if p.texts[1] != "SELEC qryvanta_cursor " {
		t.Errorf("attempt = %q", p.texts[1])
	}

	unavailable := &placeholderParser{err: fmt.Errorf("%w: missing", parser.ErrUnavailable)}
	c2, _ := coreWith(t, unavailable, &batchRunner{})
	if _, err := c2.CompletionScope(context.Background(), "SELECT 1", 8); !errors.Is(err, parser.ErrUnavailable) {
		t.Errorf("err = %v", err)
	}
}

func TestScopeAtOutsideAStatement(t *testing.T) {
	if s := parser.ScopeAt(nil, 0, ""); s.Found {
		t.Error("nil result found")
	}
	res := &parser.Result{ModelVersion: 1, Batches: []parser.Batch{{Statements: []parser.Statement{{Kind: parser.StmtSelect, Span: parser.Span{Offset: 10, Length: 5}}}}}}
	if s := parser.ScopeAt(res, 2, ""); s.Found {
		t.Error("offset before the statement found")
	}
}
