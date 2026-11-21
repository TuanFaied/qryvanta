package analyzer

import (
	"math/big"
	"testing"
)

func r(op string, v int64) rangeSet {
	s, ok := comparison(op, big.NewRat(v, 1))
	if !ok {
		panic(op)
	}
	return s
}

func TestRangeSetDisjoint(t *testing.T) {
	cases := []struct {
		a, b rangeSet
		want bool
	}{
		{r("=", 1), r("=", 2), true},
		{r("=", 1), r("=", 1), false},
		{r(">", 5), r("<", 3), true},
		{r(">", 5), r("<=", 5), true},
		{r(">=", 5), r("<=", 5), false},
		{r(">", 5), r(">", 9), false},
		{r("=", 3), r("<>", 3), true},
		{r("<>", 3), r("<>", 4), false},
		{r("<>", 3), r(">", 1), false},
		{r("!<", 5), r("<", 5), true},
		{r("!>", 5), r(">", 5), true},
	}
	for i, c := range cases {
		if got := c.a.disjoint(c.b); got != c.want {
			t.Errorf("case %d: disjoint = %v, want %v", i, got, c.want)
		}
		if got := c.b.disjoint(c.a); got != c.want {
			t.Errorf("case %d reversed: disjoint = %v, want %v", i, got, c.want)
		}
	}
}

func TestRangeSetWithin(t *testing.T) {
	cases := []struct {
		a, b rangeSet
		want bool
	}{
		{r(">", 5), r(">", 3), true},
		{r(">", 3), r(">", 5), false},
		{r(">", 3), r(">=", 3), true},
		{r(">=", 3), r(">", 3), false},
		{r("=", 4), r(">", 3), true},
		{r("=", 1), r("<>", 2), true},
		{r("=", 2), r("<>", 2), false},
		{r("<>", 2), r("<>", 2), true},
		{r("<>", 2), r(">", 0), false},
		{r("<", 3), r("<", 10), true},
		{r("<", 3), r(">", 0), false},
	}
	for i, c := range cases {
		if got := c.a.within(c.b); got != c.want {
			t.Errorf("case %d: within = %v, want %v", i, got, c.want)
		}
	}
}

func TestNextDay(t *testing.T) {
	cases := map[string]string{
		"2026-01-31":              "2026-02-01",
		"20251231":                "20260101",
		"2028/02/28":              "2028/02/29",
		"2026-01-31 23:59:59":     "2026-02-01",
		"2026-01-31T23:59:59.997": "2026-02-01",
	}
	for in, want := range cases {
		if got, ok := nextDay(in); !ok || got != want {
			t.Errorf("nextDay(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"2026-01-31 12:00", "2026-0131", "abc", "2026-02-30", "31/01/2026"} {
		if got, ok := nextDay(in); ok {
			t.Errorf("nextDay(%q) = %q, want no result", in, got)
		}
	}
}
