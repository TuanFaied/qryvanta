package plan

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// FuzzParse feeds arbitrary bytes to the parser: it must never panic, must stay within its size
// limit, and anything it accepts must be a well-formed model. Seeds are the captured fixtures and
// the unit-test plans. Run longer with:
//
//	go test ./internal/plan/ -run '^$' -fuzz FuzzParse -fuzztime 60s
func FuzzParse(f *testing.F) {
	files, _ := filepath.Glob(filepath.Join("..", "..", "tests", "plans", "fixtures", "*.sqlplan"))
	for _, name := range files {
		if data, err := os.ReadFile(name); err == nil {
			f.Add(data)
		}
	}
	for _, s := range []string{doc("1.564", parallelPlan), doc("1.539", nestedLoops), doc("1.5", ""), "<ShowPlanXML/>", ""} {
		f.Add([]byte(s))
	}
	const limit = 1 << 20
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := Parse(bytes.NewReader(data), limit)
		if err != nil {
			if p != nil {
				t.Fatal("an error with a plan")
			}
			return
		}
		if len(data) > limit+1 {
			t.Fatalf("accepted %d bytes over the %d limit", len(data), limit)
		}
		if p.Statements == nil || !supportedVersion(p.Version) {
			t.Fatalf("accepted plan = %+v", p)
		}
		for i := range p.Statements {
			p.Statements[i].Walk(func(o *Operator) {
				if o.Children == nil || o.Objects == nil || o.Predicates == nil || o.Output == nil || o.Warnings == nil {
					t.Fatalf("node %d has nil lists", o.NodeID)
				}
			})
		}
		if _, err := json.Marshal(p); err != nil {
			t.Fatalf("model does not serialize: %v", err)
		}
	})
}
