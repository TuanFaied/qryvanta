package plan

import "testing"

func op(estRows, estExecs float64, act *Actuals) *Operator {
	return &Operator{Estimated: Estimates{Rows: f(estRows), Executions: f(estExecs)}, Actual: act}
}

func TestEstimateGap(t *testing.T) {
	cases := []struct {
		name           string
		o              *Operator
		ok, big, under bool
		factor         float64
	}{
		{"estimated plan", op(10, 1, nil), false, false, false, 0},
		{"no measured rows", op(10, 1, &Actuals{}), false, false, false, 0},
		{"exact", op(500, 1, &Actuals{Rows: f(500), Executions: f(1)}), true, false, false, 1},
		{"under-estimate", op(10, 1, &Actuals{Rows: f(5000), Executions: f(1)}), true, true, true, 500},
		{"over-estimate", op(2000, 1, &Actuals{Rows: f(20), Executions: f(1)}), true, true, false, 100},
		// Large factor, small difference: not worth a highlight.
		{"tiny numbers", op(1, 1, &Actuals{Rows: f(30), Executions: f(1)}), true, false, true, 30},
		// Per-execution estimates count for every execution: 3 rows × 500 executions = 1500.
		{"nested loops inner side", op(3, 500, &Actuals{Rows: f(1500), Executions: f(500)}), true, false, false, 1},
		{"never executed", op(5, 1, &Actuals{Rows: f(0), Executions: f(0)}), true, false, false, 5},
	}
	for _, c := range cases {
		g, ok := EstimateGap(c.o)
		if ok != c.ok || g.Big != c.big || g.Under != c.under || (ok && g.Factor != c.factor) {
			t.Errorf("%s: gap %+v ok %v; want ok %v big %v under %v factor %v", c.name, g, ok, c.ok, c.big, c.under, c.factor)
		}
	}
}
