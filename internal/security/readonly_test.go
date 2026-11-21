package security

import (
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/parser"
)

func model(statements ...parser.Statement) *parser.Result {
	return &parser.Result{ModelVersion: parser.ModelVersion, Batches: []parser.Batch{{Statements: statements}}}
}

func span(offset int) parser.Span {
	return parser.Span{Offset: offset, Length: 3, Line: 1, Column: offset + 1}
}

// Read-only mode refuses what it cannot verify instead of guessing.
func TestReadOnlyRefusesWhatItCannotCheck(t *testing.T) {
	for name, res := range map[string]*parser.Result{
		"no result":            nil,
		"no model":             {Batches: []parser.Batch{{Text: "UPDATE t SET a = 1"}}},
		"syntax error":         {ModelVersion: 1, Diagnostics: []parser.Diagnostic{{Line: 2, Column: 3, Message: "Incorrect syntax near 'FROM'."}}},
		"unsupported":          model(parser.Statement{Kind: parser.KindUnsupported, Type: "SomethingNewStatement", Span: span(0)}),
		"unknown kind":         model(parser.Statement{Kind: "teleport", Span: span(0)}),
		"unknown effect":       model(parser.Statement{Kind: parser.StmtSelect, Effects: []parser.Effect{{Kind: "launch", Span: span(4)}}}),
		"write without target": model(parser.Statement{Kind: parser.StmtUpdate, Effects: []parser.Effect{{Kind: parser.EffectWrite, Span: span(4)}}}),
		"SET without options":  model(parser.Statement{Kind: parser.StmtSetOption, Type: "PredicateSetStatement", Span: span(0)}),
		"DDL without objects":  model(parser.Statement{Kind: parser.StmtDDL, Type: "CreateTableStatement", Span: span(0)}),
	} {
		if got := ReadOnlyRefusals(res); len(got) == 0 {
			t.Errorf("%s: allowed", name)
		}
	}
	got := ReadOnlyRefusals(&parser.Result{ModelVersion: 1, Diagnostics: []parser.Diagnostic{{Line: 2, Column: 3, Message: "Incorrect syntax near 'FROM'."}}})
	if got[0].Line != 2 || got[0].Column != 3 || !strings.Contains(got[0].Reason, "Incorrect syntax near 'FROM'.") {
		t.Errorf("syntax refusal = %+v", got[0])
	}
}

// An effect is reported by every statement containing it; it is refused once, under the
// innermost statement.
func TestReadOnlyReportsEachProblemOnce(t *testing.T) {
	drop := parser.Effect{Kind: parser.EffectWrite, Object: &parser.ObjectName{Schema: "dbo", Name: "X"}, Span: span(20)}
	res := model(parser.Statement{Kind: parser.StmtIf, Span: span(0), Effects: []parser.Effect{drop},
		Body: []parser.Statement{{Kind: parser.StmtDDL, Type: "DropTableStatement", Span: span(10), Effects: []parser.Effect{drop}}}})
	got := ReadOnlyRefusals(res)
	if len(got) != 1 || got[0].Statement != "DROP TABLE" || !strings.Contains(got[0].Reason, "dbo.X") {
		t.Errorf("refusals = %+v", got)
	}
}

func TestReadOnlyAllowsEmptyBatches(t *testing.T) {
	// A batch of only comments has no statements and no syntax errors.
	if got := ReadOnlyRefusals(&parser.Result{ModelVersion: 1, Batches: []parser.Batch{{Text: "-- note"}}}); len(got) != 0 {
		t.Errorf("refusals = %+v", got)
	}
}

func TestFriendlyType(t *testing.T) {
	for in, want := range map[string]string{
		"BulkInsertStatement": "BULK INSERT", "DbccStatement": "DBCC", "CreateTableStatement": "CREATE TABLE", "": "This statement",
	} {
		if got := friendlyType(in); got != want {
			t.Errorf("friendlyType(%q) = %q, want %q", in, got, want)
		}
	}
}
