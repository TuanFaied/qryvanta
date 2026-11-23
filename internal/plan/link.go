package plan

import (
	"strings"

	"github.com/TuanFaied/qryvanta/internal/parser"
)

// Link connects the plan's statements to the top-level statements of one batch of the parsed
// script (sql is the script, batchIndex the batch's index), where that is unambiguous:
//
//   - by text: the plan statement's text, with whitespace collapsed and a trailing semicolon
//     dropped, equals exactly one statement of the batch that is not linked yet;
//   - by position: otherwise, when the plan and the batch have the same number of statements, the
//     statement at the same position. This covers plans whose text SQL Server rewrote
//     (auto-parameterized literals become @1, @2).
//
// Statements that match neither way stay unlinked (Script is nil).
func Link(p *Plan, batchIndex int, sql string, b parser.Batch) {
	script := make([]string, len(b.Statements))
	for i, st := range b.Statements {
		script[i] = normalize(parser.SpanText(sql, st.Span))
	}
	used := make([]bool, len(script))
	for pi := range p.Statements {
		text := normalize(p.Statements[pi].Text)
		match, n := -1, 0
		for i, s := range script {
			if !used[i] && s == text {
				match, n = i, n+1
			}
		}
		if n == 1 {
			used[match] = true
			p.Statements[pi].Script = &ScriptLink{Batch: batchIndex, Statement: match, By: "text"}
		}
	}
	if len(p.Statements) != len(script) {
		return
	}
	for pi := range p.Statements {
		if p.Statements[pi].Script == nil && !used[pi] {
			used[pi] = true
			p.Statements[pi].Script = &ScriptLink{Batch: batchIndex, Statement: pi, By: "position"}
		}
	}
}

// normalize collapses whitespace, drops a trailing semicolon and leading comments: estimated
// plans include the comments before a statement in its text, actual plans do not.
func normalize(s string) string {
	for {
		s = strings.TrimLeft(s, " \t\r\n")
		switch {
		case strings.HasPrefix(s, "--"):
			if i := strings.IndexByte(s, '\n'); i >= 0 {
				s = s[i+1:]
				continue
			}
			s = ""
		case strings.HasPrefix(s, "/*"):
			if i := strings.Index(s, "*/"); i >= 0 {
				s = s[i+2:]
				continue
			}
			s = ""
		}
		break
	}
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(strings.TrimSuffix(s, ";"))
}
