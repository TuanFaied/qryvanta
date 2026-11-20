//go:build parser

// Package golden pins the parser's output: every case is a .sql file with the expected parse
// result (batches, diagnostics and QueryModel) as .json next to it. The suite runs against the
// real sidecar and is the acceptance suite for any future parser behind parser.Parser.
//
// Run (after building the sidecar):
//
//	QRYVANTA_PARSER_PATH=bin/qryvanta-tsqlparser.exe go test -tags parser ./tests/parser/
//
// After an intended change to the parser or the model, rewrite the expected files and review
// the diff:
//
//	go test -tags parser ./tests/parser/ -update
package golden

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/TuanFaied/qryvanta/internal/parser"
	"github.com/TuanFaied/qryvanta/internal/parser/sidecar"
)

var update = flag.Bool("update", false, "rewrite the expected .json files from the current parser output")

type goldenCase struct {
	name     string
	sqlPath  string
	jsonPath string
}

// cases lists cases/*.sql (expected cases/*.json) and ../sample_queries/*.sql (expected
// samples/*.json).
func cases(t *testing.T) []goldenCase {
	t.Helper()
	var list []goldenCase
	add := func(sqlGlob, jsonDir, prefix string) {
		files, err := filepath.Glob(sqlGlob)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			base := strings.TrimSuffix(filepath.Base(f), ".sql")
			list = append(list, goldenCase{name: prefix + base, sqlPath: f, jsonPath: filepath.Join(jsonDir, base+".json")})
		}
	}
	add(filepath.Join("cases", "*.sql"), "cases", "")
	add(filepath.Join("..", "sample_queries", "*.sql"), "samples", "sample/")
	sort.Slice(list, func(i, j int) bool { return list[i].name < list[j].name })
	return list
}

func client(t *testing.T) *sidecar.Client {
	t.Helper()
	path := os.Getenv("QRYVANTA_PARSER_PATH")
	if path == "" {
		t.Skip("QRYVANTA_PARSER_PATH not set; build the sidecar with `wails3 task common:build:sidecar`")
	}
	c := sidecar.New(sidecar.Options{Path: path, Timeout: 20 * time.Second})
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func render(res *parser.Result) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func TestGolden(t *testing.T) {
	c := client(t)
	all := cases(t)
	if len(all) < 60 {
		t.Errorf("only %d golden cases; the suite promises at least 60", len(all))
	}
	for _, gc := range all {
		t.Run(gc.name, func(t *testing.T) {
			data, err := os.ReadFile(gc.sqlPath)
			if err != nil {
				t.Fatal(err)
			}
			sql := string(data)
			if !strings.HasPrefix(sql, "-- ") {
				t.Errorf("%s must start with a one-line comment saying what it checks", gc.sqlPath)
			}
			res, err := c.Parse(context.Background(), sql, parser.Options{Model: true})
			if err != nil {
				t.Fatal(err)
			}
			got, err := render(res)
			if err != nil {
				t.Fatal(err)
			}
			if *update {
				if err := os.WriteFile(gc.jsonPath, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(gc.jsonPath)
			if err != nil {
				t.Fatalf("no expected output (%v); run with -update to create %s, then review it", err, gc.jsonPath)
			}
			if bytes.Equal(normalize(want), normalize(got)) {
				return
			}
			var w, g any
			if err := json.Unmarshal(want, &w); err != nil {
				t.Fatalf("%s is not valid JSON: %v", gc.jsonPath, err)
			}
			_ = json.Unmarshal(got, &g)
			path, wv, gv := firstDifference("$", w, g)
			t.Errorf("%s differs from %s at %s\n  want: %s\n  got:  %s\nIf the change is intended, run with -update and review the diff.",
				gc.name, gc.jsonPath, path, short(wv), short(gv))
		})
	}
}

// TestNoOrphanedExpectations fails when an expected file has no case left.
func TestNoOrphanedExpectations(t *testing.T) {
	known := map[string]bool{}
	for _, gc := range cases(t) {
		known[filepath.Clean(gc.jsonPath)] = true
	}
	for _, dir := range []string{"cases", "samples"} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		for _, f := range files {
			if !known[filepath.Clean(f)] {
				t.Errorf("%s has no matching .sql case; delete it", f)
			}
		}
	}
}

// TestFirstDifference checks the diff helper itself.
func TestFirstDifference(t *testing.T) {
	var a, b any
	_ = json.Unmarshal([]byte(`{"batches":[{"statements":[{"kind":"select","query":{"where":{"kind":"or"}}}]}]}`), &a)
	_ = json.Unmarshal([]byte(`{"batches":[{"statements":[{"kind":"select","query":{"where":{"kind":"and"}}}]}]}`), &b)
	path, w, g := firstDifference("$", a, b)
	if path != "$.batches[0].statements[0].query.where.kind" || w != "or" || g != "and" {
		t.Fatalf("firstDifference = %s, %v, %v", path, w, g)
	}
	_ = json.Unmarshal([]byte(`{"a":[1,2]}`), &a)
	_ = json.Unmarshal([]byte(`{"a":[1]}`), &b)
	if path, _, _ := firstDifference("$", a, b); path != "$.a[1]" {
		t.Fatalf("missing element: %s", path)
	}
	_ = json.Unmarshal([]byte(`{"a":1}`), &a)
	_ = json.Unmarshal([]byte(`{"a":1,"b":2}`), &b)
	if path, w, _ := firstDifference("$", a, b); path != "$.b" || w != nil {
		t.Fatalf("added key: %s %v", path, w)
	}
}

func normalize(b []byte) []byte {
	return bytes.TrimSpace(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")))
}

// firstDifference returns the JSON path of the first difference between want and got, and the
// values there (nil for a missing value).
func firstDifference(path string, want, got any) (string, any, any) {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return path, want, got
		}
		keys := map[string]bool{}
		for k := range w {
			keys[k] = true
		}
		for k := range g {
			keys[k] = true
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			if p, wv, gv := firstDifference(path+"."+k, w[k], g[k]); p != "" {
				return p, wv, gv
			}
		}
		return "", nil, nil
	case []any:
		g, ok := got.([]any)
		if !ok {
			return path, want, got
		}
		for i := 0; i < max(len(w), len(g)); i++ {
			var wv, gv any
			if i < len(w) {
				wv = w[i]
			}
			if i < len(g) {
				gv = g[i]
			}
			if i >= len(w) || i >= len(g) {
				return fmt.Sprintf("%s[%d]", path, i), wv, gv
			}
			if p, a, b := firstDifference(fmt.Sprintf("%s[%d]", path, i), wv, gv); p != "" {
				return p, a, b
			}
		}
		return "", nil, nil
	default:
		if fmt.Sprint(want) == fmt.Sprint(got) && (want == nil) == (got == nil) {
			return "", nil, nil
		}
		return path, want, got
	}
}

func short(v any) string {
	if v == nil {
		return "(missing)"
	}
	b, _ := json.Marshal(v)
	s := string(b)
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
