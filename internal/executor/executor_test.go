package executor

import "testing"

func TestBudgetRows(t *testing.T) {
	b := NewBudget(Limits{MaxRows: 2})
	if b.Add(10) != "" || b.Add(10) != "" {
		t.Fatal("first two rows must fit")
	}
	if got := b.Add(10); got != "rows" {
		t.Fatalf("third row: %q, want rows", got)
	}
	if b.Rows != 2 || b.Bytes != 20 {
		t.Errorf("rejected row was counted: %+v", b)
	}
}

func TestBudgetBytes(t *testing.T) {
	b := NewBudget(Limits{MaxBytes: 25})
	if b.Add(20) != "" {
		t.Fatal("first row must fit")
	}
	if got := b.Add(10); got != "bytes" {
		t.Fatalf("second row: %q, want bytes", got)
	}
	if b.Add(5) != "" {
		t.Error("a smaller row that fits is still accepted")
	}
}

func TestBudgetUnlimited(t *testing.T) {
	b := NewBudget(Limits{})
	for i := 0; i < 10000; i++ {
		if b.Add(1000) != "" {
			t.Fatal("zero limits must not truncate")
		}
	}
}

func TestSizeOf(t *testing.T) {
	if SizeOf(nil) != 1 || SizeOf("abcd") != 6 || SizeOf(int64(5)) != 8 || SizeOf(true) != 1 {
		t.Error("unexpected sizes")
	}
}

func TestCollector(t *testing.T) {
	var c Collector
	_ = c.ResultSet(0, []Column{{Name: "a"}})
	_ = c.Rows(0, [][]Value{{int64(1)}, {int64(2)}})
	_ = c.ResultSet(1, []Column{{Name: "b"}})
	_ = c.Rows(1, [][]Value{{"x"}})
	_ = c.Message(Message{Kind: "info", Text: "hi"})
	if len(c.Data) != 2 || len(c.Data[0]) != 2 || c.Data[1][0][0] != "x" || len(c.Messages) != 1 {
		t.Fatalf("unexpected collector state: %+v", c)
	}
}
