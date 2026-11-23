//go:build parser

package instrumentation

// Every template must pass the read-only statement allowlist (#44) as generated, tricky names
// included. Run (after building the sidecar):
//
//	QRYVANTA_PARSER_PATH=bin/qryvanta-tsqlparser.exe go test -tags parser ./internal/instrumentation/

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/TuanFaied/qryvanta/internal/diagnostics"
	"github.com/TuanFaied/qryvanta/internal/parser"
	"github.com/TuanFaied/qryvanta/internal/parser/sidecar"
	"github.com/TuanFaied/qryvanta/internal/security"
)

func TestEveryTemplatePassesTheAllowlist(t *testing.T) {
	path := os.Getenv("QRYVANTA_PARSER_PATH")
	if path == "" {
		t.Skip("QRYVANTA_PARSER_PATH not set; build the sidecar with `wails3 task common:build:sidecar`")
	}
	p := sidecar.New(sidecar.Options{Path: path, Timeout: 20 * time.Second})
	t.Cleanup(func() { _ = p.Close() })

	for _, table := range []diagnostics.TableRef{orders, tricky} {
		other := tricky
		measurements := []diagnostics.Measurement{
			{Kind: diagnostics.MeasureFanOut, Table: table, Columns: []string{"OrderID", "Line]No"}},
			{Kind: diagnostics.MeasureUnmatched, Table: table, Columns: []string{"CustomerID", "x y"}, Other: &other, OtherColumns: []string{"ID", "z"}},
			{Kind: diagnostics.MeasureNulls, Table: table, Columns: []string{"CustomerID"}},
			{Kind: diagnostics.MeasureRangeEnd, Table: table, Columns: []string{"OrderDate"}, Values: []string{"2026-01-31", "2026-02-01"}},
		}
		for _, m := range measurements {
			probe, err := Build(m)
			if err != nil {
				t.Fatal(err)
			}
			res, err := p.Parse(context.Background(), probe.SQL, parser.Options{Model: true})
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Diagnostics) > 0 {
				t.Errorf("%s probe on %s does not parse: %v\n%s", m.Kind, table.Name, res.Diagnostics, probe.SQL)
				continue
			}
			if refusals := security.ReadOnlyRefusals(res); len(refusals) > 0 {
				t.Errorf("%s probe on %s is refused: %+v\n%s", m.Kind, table.Name, refusals, probe.SQL)
			}
		}
	}
}
