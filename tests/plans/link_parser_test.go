//go:build parser

package golden

// Links every captured plan to its sample query, parsed by the real parser. Run:
//
//	QRYVANTA_PARSER_PATH=bin/qryvanta-tsqlparser.exe go test -tags parser ./tests/plans/

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TuanFaied/qryvanta/internal/parser"
	"github.com/TuanFaied/qryvanta/internal/parser/sidecar"
	"github.com/TuanFaied/qryvanta/internal/plan"
)

func TestPlansLinkToTheirScript(t *testing.T) {
	path := os.Getenv("QRYVANTA_PARSER_PATH")
	if path == "" {
		t.Skip("QRYVANTA_PARSER_PATH not set; build the sidecar with `wails3 task common:build:sidecar`")
	}
	c := sidecar.New(sidecar.Options{Path: path, Timeout: 20 * time.Second})
	t.Cleanup(func() { _ = c.Close() })

	files, _ := filepath.Glob(filepath.Join("fixtures", "*.sqlplan"))
	byText := 0
	for _, f := range files {
		base := filepath.Base(f)
		sample := base[:strings.Index(base, ".buggy.")+len(".buggy")]
		if !strings.Contains(base, ".buggy.") {
			sample = base[:strings.Index(base, ".correct.")+len(".correct")]
		}
		sql, err := os.ReadFile(filepath.Join("..", "sample_queries", sample+".sql"))
		if err != nil {
			t.Fatal(err)
		}
		res, err := c.Parse(context.Background(), string(sql), parser.Options{Model: true})
		if err != nil || len(res.Batches) != 1 {
			t.Fatalf("%s: %v", sample, err)
		}
		in, _ := os.Open(f)
		p, err := plan.Parse(in, 0)
		in.Close()
		if err != nil {
			t.Fatal(err)
		}
		plan.Link(p, 0, string(sql), res.Batches[0])
		for _, st := range p.Statements {
			if st.Script == nil || st.Script.Batch != 0 || st.Script.Statement != 0 {
				t.Fatalf("%s: statement %d is not linked to the query: %+v", base, st.ID, st.Script)
			}
			if st.Script.By == "text" {
				byText++
			}
		}
	}
	// Plans match by text, apart from auto-parameterized ones (sample 05's literals become @1),
	// which match by position.
	if byText < len(files)-2 {
		t.Fatalf("only %d of %d plans matched by text", byText, len(files))
	}
}
