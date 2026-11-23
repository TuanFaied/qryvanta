//go:build integration

package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TuanFaied/qryvanta/internal/executor"
)

// TestCapturePlanFixtures writes the estimated and actual plans of every sample query to
// tests/plans/fixtures, the input of the plan parser's golden tests. It runs only when asked:
//
//	QRYVANTA_CAPTURE_PLANS=1 go test -tags integration -run TestCapturePlanFixtures ./tests/integration/
//
// Actual plans hold timings, so recapturing changes them; the golden files are then updated with
// go test ./tests/plans/ -update.
func TestCapturePlanFixtures(t *testing.T) {
	if os.Getenv("QRYVANTA_CAPTURE_PLANS") == "" {
		t.Skip("set QRYVANTA_CAPTURE_PLANS=1 to recapture the plan fixtures")
	}
	db := onePool(t)
	dir := filepath.Join("..", "plans", "fixtures")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(sampleDir, "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("samples: %v", err)
	}
	for _, f := range files {
		base := strings.TrimSuffix(filepath.Base(f), ".sql")
		for _, mode := range []executor.PlanMode{executor.PlanEstimated, executor.PlanActual} {
			sum, c := planRun(t, db, mode, executor.Limits{Timeout: 60 * time.Second},
				executor.Batch{SQL: readSample(t, filepath.Base(f)), Line: 1})
			if sum.Outcome != executor.OutcomeSucceeded || len(c.Plans) == 0 {
				t.Fatalf("%s %s: outcome %s, plans %d, %s", base, mode, sum.Outcome, len(c.Plans), sum.Error)
			}
			for i, p := range c.Plans {
				name := fmt.Sprintf("%s.%s.sqlplan", base, mode)
				if len(c.Plans) > 1 {
					name = fmt.Sprintf("%s.%s.%d.sqlplan", base, mode, i+1)
				}
				if err := os.WriteFile(filepath.Join(dir, name), []byte(p.XML), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}
