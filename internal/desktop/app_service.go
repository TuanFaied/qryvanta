// Package desktop adapts the Qryvanta core to the Wails desktop runtime.
//
// It is the only package that imports Wails. Bound services expose DTOs and never
// hand credentials or internal types to the frontend.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/config"
	"github.com/TuanFaied/qryvanta/internal/parser"
	"github.com/TuanFaied/qryvanta/internal/version"
)

// AppInfo describes the running application for the header, status bar and about dialog.
type AppInfo struct {
	Version         version.Info `json:"version"`
	ConfigPath      string       `json:"configPath"`
	LogPath         string       `json:"logPath"`
	StartupWarnings []string     `json:"startupWarnings"`
}

// SaveConfigResult reports the outcome of UpdateConfig. Field errors are returned as data,
// not as a rejected promise, so the settings form can show them next to each field.
type SaveConfigResult struct {
	OK          bool              `json:"ok"`
	Config      config.Config     `json:"config"`
	FieldErrors map[string]string `json:"fieldErrors"`
}

// AppService exposes application-level operations to the frontend.
type AppService struct {
	core *app.Core
}

// NewAppService creates the service.
func NewAppService(core *app.Core) *AppService {
	return &AppService{core: core}
}

// Info returns build and environment information.
func (s *AppService) Info() AppInfo {
	warnings := s.core.StartupWarnings()
	if warnings == nil {
		warnings = []string{}
	}
	return AppInfo{
		Version:         version.Get(),
		ConfigPath:      s.core.ConfigPath(),
		LogPath:         s.core.LogPath(),
		StartupWarnings: warnings,
	}
}

// GetConfig returns the current configuration.
func (s *AppService) GetConfig() config.Config {
	return s.core.Config()
}

// DefaultConfig returns the built-in defaults, for a "Reset to defaults" action.
func (s *AppService) DefaultConfig() config.Config {
	return config.Default()
}

// UpdateConfig validates and saves cfg.
func (s *AppService) UpdateConfig(cfg config.Config) (SaveConfigResult, error) {
	err := s.core.UpdateConfig(cfg)
	var ve *config.ValidationError
	switch {
	case err == nil:
		return SaveConfigResult{OK: true, Config: s.core.Config(), FieldErrors: map[string]string{}}, nil
	case errors.As(err, &ve):
		return SaveConfigResult{OK: false, Config: cfg, FieldErrors: ve.Fields}, nil
	default:
		s.core.Logger().Error("save configuration failed", "err", err)
		return SaveConfigResult{}, fmt.Errorf("could not save settings: %w", err)
	}
}

// ParserStatus starts the T-SQL parser if needed and reports whether it works, for the status
// bar. Queries still run when it is unavailable.
func (s *AppService) ParserStatus(ctx context.Context) parser.Status {
	return s.core.Parser().Check(ctx)
}

// OpenLogFolder opens the folder that contains the log file in the OS file manager.
func (s *AppService) OpenLogFolder() error {
	p := s.core.LogPath()
	if p == "" {
		return errors.New("file logging is disabled")
	}
	return openFolder(filepath.Dir(p))
}

func openFolder(dir string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer.exe", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	// explorer.exe returns exit code 1 even on success, so only start errors are reported.
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open folder: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
