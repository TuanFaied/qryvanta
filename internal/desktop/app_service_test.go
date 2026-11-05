package desktop

import (
	"path/filepath"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/config"
)

func newService(t *testing.T) *AppService {
	t.Helper()
	dir := t.TempDir()
	core, err := app.New(app.Options{ConfigPath: filepath.Join(dir, "config.json"), DisableFileLog: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = core.Close() })
	return NewAppService(core)
}

func TestUpdateConfigReturnsFieldErrorsAsData(t *testing.T) {
	s := newService(t)
	cfg := s.GetConfig()
	cfg.Execution.TimeoutSeconds = 0

	res, err := s.UpdateConfig(cfg)
	if err != nil {
		t.Fatalf("UpdateConfig returned error %v, want field errors as data", err)
	}
	if res.OK {
		t.Fatal("OK = true for invalid config")
	}
	if _, ok := res.FieldErrors["execution.timeoutSeconds"]; !ok {
		t.Errorf("FieldErrors = %v", res.FieldErrors)
	}
	if s.GetConfig().Execution.TimeoutSeconds != config.Default().Execution.TimeoutSeconds {
		t.Error("invalid config was applied")
	}
}

func TestUpdateConfigSuccess(t *testing.T) {
	s := newService(t)
	cfg := s.GetConfig()
	cfg.Appearance.Theme = config.ThemeLight
	res, err := s.UpdateConfig(cfg)
	if err != nil || !res.OK {
		t.Fatalf("UpdateConfig = %+v, %v", res, err)
	}
	if res.Config.Appearance.Theme != config.ThemeLight || res.FieldErrors == nil {
		t.Errorf("unexpected result %+v", res)
	}
}

func TestInfoNeverReturnsNilWarnings(t *testing.T) {
	if s := newService(t); s.Info().StartupWarnings == nil {
		t.Error("StartupWarnings is nil; the frontend expects an array")
	}
}
