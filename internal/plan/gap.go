package plan

import "math"

// The estimate-against-actual gap that the plan view highlights and the plan rules report: one
// threshold, shared, so the view never flags what the rules ignore or the other way round.
const (
	// GapFactor is how many times larger one side must be than the other.
	GapFactor = 10.0
	// GapMinRows is the smallest absolute difference that counts: 1 row estimated against 9 is a
	// factor of 9 but changes no plan choice.
	GapMinRows = 100.0
)

// Gap compares an operator's estimated rows for all its executions with its actual rows.
type Gap struct {
	// EstimatedRows is the estimate for all executions (rows per execution × executions).
	EstimatedRows float64 `json:"estimatedRows"`
	ActualRows    float64 `json:"actualRows"`
	// Factor is the larger of the two divided by the smaller (each at least 1).
	Factor float64 `json:"factor"`
	// Big is true when the gap reaches GapFactor and GapMinRows.
	Big bool `json:"big"`
	// Under is true when the optimizer under-estimated (actual > estimated).
	Under bool `json:"under"`
}

// EstimateGap returns the gap of an operator, or false when it lacks an estimate or a measured
// row count (estimated plans, and operators SQL Server reports no counters for).
func EstimateGap(o *Operator) (Gap, bool) {
	if o == nil || o.Actual == nil || o.Actual.Rows == nil || o.Estimated.Rows == nil {
		return Gap{}, false
	}
	execs := 1.0
	if o.Actual.Executions != nil && *o.Actual.Executions > 0 {
		// Per-execution estimates times the executions that actually happened, as SSMS's
		// "Estimated Number of Rows for All Executions" does.
		execs = *o.Actual.Executions
	} else if o.Estimated.Executions != nil {
		execs = *o.Estimated.Executions
	}
	g := Gap{EstimatedRows: *o.Estimated.Rows * execs, ActualRows: *o.Actual.Rows}
	hi, lo := math.Max(g.EstimatedRows, g.ActualRows), math.Min(g.EstimatedRows, g.ActualRows)
	g.Factor = math.Max(hi, 1) / math.Max(lo, 1)
	g.Big = g.Factor >= GapFactor && hi-lo >= GapMinRows
	g.Under = g.ActualRows > g.EstimatedRows
	return g, true
}
