package parser

import (
	"path/filepath"
	"testing"
)

func TestByteOffsets(t *testing.T) {
	s := "aé🐘b" // a: 1 byte/1 unit, é: 2 bytes/1 unit, 🐘: 4 bytes/2 units, b: 1 byte/1 unit
	got := ByteOffsets(s, 0, 1, 2, 4, 5, 99, 2)
	want := []int{0, 1, 3, 7, 8, 8, 3}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ByteOffsets = %v, want %v", got, want)
		}
	}
	if len(ByteOffsets(s)) != 0 {
		t.Fatal("no offsets should give an empty slice")
	}
}

func TestDefaultPathHonoursOverride(t *testing.T) {
	t.Setenv("QRYVANTA_PARSER_PATH", filepath.Join("x", "parser"))
	if got := DefaultPath(); got != filepath.Join("x", "parser") {
		t.Fatalf("DefaultPath = %q", got)
	}
	t.Setenv("QRYVANTA_PARSER_PATH", "")
	if filepath.Base(DefaultPath()) != ExecutableName() {
		t.Fatalf("DefaultPath = %q, want a file named %s", DefaultPath(), ExecutableName())
	}
}
