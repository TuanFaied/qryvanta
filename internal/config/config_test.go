package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultIsValid(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Default().Validate() = %v", err)
	}
	if !cfg.Execution.ReadOnly {
		t.Error("default must be read-only")
	}
	if cfg.History.StoreResultSamples {
		t.Error("default must not store result samples")
	}
	if cfg.Logging.LogSQLText {
		t.Error("default must not log SQL text")
	}
	if cfg.Appearance.Theme != ThemeDark {
		t.Errorf("default theme = %q, want dark", cfg.Appearance.Theme)
	}
}

func TestValidateReportsEveryInvalidField(t *testing.T) {
	cfg := Default()
	cfg.Appearance.Theme = "neon"
	cfg.Execution.TimeoutSeconds = 0
	cfg.Execution.MaxRows = MaxMaxRows + 1
	cfg.Logging.Level = "verbose"
	cfg.Formatting.KeywordCase = "shout"
	cfg.Formatting.IndentSize = 0

	err := cfg.Validate()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("Validate() = %v, want *ValidationError", err)
	}
	want := []string{"appearance.theme", "execution.timeoutSeconds", "execution.maxRows", "logging.level",
		"formatting.keywordCase", "formatting.indentSize"}
	for _, field := range want {
		if _, ok := ve.Fields[field]; !ok {
			t.Errorf("missing validation error for %s; got %v", field, ve.Fields)
		}
	}
	if len(ve.Fields) != len(want) {
		t.Errorf("got %d errors, want %d: %v", len(ve.Fields), len(want), ve.Fields)
	}
	// Error text is deterministic (sorted by field).
	if !strings.HasPrefix(ve.Error(), "invalid configuration: appearance.theme") {
		t.Errorf("unexpected error text: %s", ve.Error())
	}
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg != Default() {
		t.Errorf("Load(missing) = %+v, want defaults", cfg)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	want := Default()
	want.Appearance.Theme = ThemeLight
	want.Execution.MaxRows = 500
	want.History.RetentionDays = 7

	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("round trip mismatch:\n got  %+v\n want %+v", got, want)
	}

	// No temp files are left behind.
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("expected only config.json in dir, found %d entries", len(entries))
	}
}

func TestSaveRejectsInvalidConfigAndKeepsOldFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, Default()); err != nil {
		t.Fatal(err)
	}
	bad := Default()
	bad.Execution.TimeoutSeconds = -5
	if err := Save(path, bad); err == nil {
		t.Fatal("Save(invalid) succeeded")
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Execution.TimeoutSeconds != Default().Execution.TimeoutSeconds {
		t.Error("invalid save modified the existing file")
	}
}

func TestLoadPartialFileKeepsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{"schemaVersion":1,"appearance":{"theme":"light"},"futureField":{"x":1}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Appearance.Theme != ThemeLight {
		t.Errorf("theme = %q, want light", cfg.Appearance.Theme)
	}
	if cfg.Execution != Default().Execution {
		t.Errorf("execution = %+v, want defaults", cfg.Execution)
	}
}

func TestLoadRejectsNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"schemaVersion":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("Load(newer schema) error = %v, want 'newer' error", err)
	}
}

func TestLoadRejectsMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load(malformed) succeeded")
	}
}
