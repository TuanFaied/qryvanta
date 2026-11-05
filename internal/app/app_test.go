package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/config"
	"github.com/TuanFaied/qryvanta/internal/connection"
)

func newTestCore(t *testing.T) (*Core, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	c, err := New(Options{
		ConfigPath:  cfgPath,
		LogDir:      filepath.Join(dir, "logs"),
		Credentials: connection.NewMemoryStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, cfgPath
}

func TestConnectionsAreStoredNextToConfig(t *testing.T) {
	c, cfgPath := newTestCore(t)
	_, err := c.Connections().Create(connection.Profile{Name: "dev", Server: "localhost", Auth: connection.AuthWindows}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cfgPath), connection.FileName)); err != nil {
		t.Errorf("connections file not next to config: %v", err)
	}
}

func TestNewUsesDefaultsWithoutConfigFile(t *testing.T) {
	c, _ := newTestCore(t)
	if c.Config() != config.Default() {
		t.Errorf("Config() = %+v, want defaults", c.Config())
	}
	if c.LogPath() == "" {
		t.Error("expected a log file")
	}
}

func TestUpdateConfigPersistsAndApplies(t *testing.T) {
	c, path := newTestCore(t)
	cfg := c.Config()
	cfg.Appearance.Theme = config.ThemeLight
	cfg.Logging.Level = config.LogDebug
	if err := c.UpdateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if c.Config().Appearance.Theme != config.ThemeLight {
		t.Error("in-memory config not updated")
	}
	onDisk, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if onDisk.Appearance.Theme != config.ThemeLight {
		t.Error("config not persisted")
	}
}

func TestUpdateConfigRejectsInvalid(t *testing.T) {
	c, _ := newTestCore(t)
	bad := c.Config()
	bad.Execution.MaxRows = 0
	err := c.UpdateConfig(bad)
	var ve *config.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("UpdateConfig(invalid) = %v, want ValidationError", err)
	}
	if c.Config().Execution.MaxRows != config.Default().Execution.MaxRows {
		t.Error("invalid config was applied")
	}
}

func TestNewFailsOnCorruptConfigInsteadOfOverwriting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Options{ConfigPath: path, DisableFileLog: true}); err == nil {
		t.Fatal("New succeeded with corrupt config")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "{broken") {
		t.Error("corrupt config file was modified")
	}
}

func TestRecoverInvalidConfigMovesFileAside(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"execution":{"maxRows":0}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := New(Options{ConfigPath: path, DisableFileLog: true, RecoverInvalidConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if c.Config() != config.Default() {
		t.Error("expected defaults after recovery")
	}
	if w := c.StartupWarnings(); len(w) != 1 || !strings.Contains(w[0], "invalid-") {
		t.Errorf("StartupWarnings() = %v", w)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "config.json.invalid-*"))
	if len(matches) != 1 {
		t.Fatalf("expected one backup file, found %v", matches)
	}
	backup, _ := os.ReadFile(matches[0])
	if !strings.Contains(string(backup), `"maxRows":0`) {
		t.Error("backup does not contain the original content")
	}
}
