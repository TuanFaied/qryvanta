// Package golden pins the showplan parser's output: every fixtures/*.sqlplan (estimated and
// actual plans of each sample query, captured from the fixture database) has its expected operator
// graph as expected/*.json.
//
// After an intended change to the parser or the model, rewrite the expected files and review the
// diff:
//
//	go test ./tests/plans/ -update
//
// To recapture the fixtures from the fixture database (timings in actual plans then change):
//
//	QRYVANTA_CAPTURE_PLANS=1 go test -tags integration -run TestCapturePlanFixtures ./tests/integration/
package golden

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/plan"
)

var update = flag.Bool("update", false, "rewrite the expected .json files from the current parser output")

func render(p *plan.Plan) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(p); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func TestGolden(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("fixtures", "*.sqlplan"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 40 {
		t.Fatalf("plan fixtures = %d, want the estimated and actual plans of all 20 sample queries", len(files))
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".sqlplan")
		t.Run(name, func(t *testing.T) {
			in, err := os.Open(f)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			p, err := plan.Parse(in, 0)
			if err != nil {
				t.Fatal(err)
			}
			got, err := render(p)
			if err != nil {
				t.Fatal(err)
			}
			want := filepath.Join("expected", name+".json")
			if *update {
				if err := os.MkdirAll("expected", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(want, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			exp, err := os.ReadFile(want)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			if !bytes.Equal(bytes.ReplaceAll(exp, []byte("\r\n"), []byte("\n")), got) {
				t.Fatalf("%s differs from the parser output; run with -update and review the diff", want)
			}
		})
	}
}

// TestFixtureInvariants checks properties every captured plan must have, independent of the
// expected files: estimated plans have no actual values; actual plans have them (SQL Server
// reports none for some operators, Compute Scalar typically, and those stay absent, not zero);
// and every statement with a plan has a root.
func TestFixtureInvariants(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("fixtures", "*.sqlplan"))
	for _, f := range files {
		in, err := os.Open(f)
		if err != nil {
			t.Fatal(err)
		}
		p, err := plan.Parse(in, 0)
		in.Close()
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		actual := strings.Contains(f, ".actual.")
		if p.Actual != actual {
			t.Errorf("%s: Actual = %v", f, p.Actual)
		}
		measured := 0
		for _, st := range p.Statements {
			if st.Type == "SELECT" && st.Root == nil {
				t.Errorf("%s: statement %d has no operators", f, st.ID)
			}
			st.Walk(func(o *plan.Operator) {
				if o.Actual != nil {
					measured++
					if o.Actual.Rows == nil || o.Actual.Executions == nil || o.Actual.Threads == 0 {
						t.Errorf("%s: node %d (%s) has incomplete actual values", f, o.NodeID, o.PhysicalOp)
					}
				}
				if !actual && o.Actual != nil {
					t.Errorf("%s: node %d of an estimated plan has actual values", f, o.NodeID)
				}
				if o.Estimated.Rows == nil || o.Estimated.SubtreeCost == nil {
					t.Errorf("%s: node %d has no estimates", f, o.NodeID)
				}
			})
		}
		if actual && measured == 0 {
			t.Errorf("%s: an actual plan without any measured operator", f)
		}
	}
}
