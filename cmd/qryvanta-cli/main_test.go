package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/config"
)

func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestVersion(t *testing.T) {
	code, out, _ := runCLI(t, "version")
	if code != 0 || !strings.HasPrefix(out, "qryvanta ") {
		t.Fatalf("version: code=%d out=%q", code, out)
	}
	code, out, _ = runCLI(t, "version", "--json")
	var v map[string]string
	if code != 0 || json.Unmarshal([]byte(out), &v) != nil || v["version"] == "" {
		t.Fatalf("version --json: code=%d out=%q", code, out)
	}
}

func TestConfigShowAndPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	code, out, errOut := runCLI(t, "--config", path, "config", "show")
	if code != 0 {
		t.Fatalf("config show: code=%d stderr=%q", code, errOut)
	}
	var cfg config.Config
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg != config.Default() {
		t.Errorf("config show = %+v, want defaults", cfg)
	}

	code, out, _ = runCLI(t, "--config", path, "config", "path")
	if code != 0 || strings.TrimSpace(out) != path {
		t.Fatalf("config path: code=%d out=%q", code, out)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"bogus"}, {"config"}, {"config", "nope"}, {"--nope"}} {
		if code, _, errOut := runCLI(t, args...); code != 2 || !strings.Contains(errOut, "Usage:") {
			t.Errorf("%v: code=%d stderr=%q, want usage error", args, code, errOut)
		}
	}
}

func TestHelp(t *testing.T) {
	for _, args := range [][]string{{}, {"help"}, {"-h"}} {
		if code, out, _ := runCLI(t, args...); code != 0 || !strings.Contains(out, "Commands:") {
			t.Errorf("%v: code=%d out=%q", args, code, out)
		}
	}
}
