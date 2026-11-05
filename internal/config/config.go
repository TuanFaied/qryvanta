// Package config loads, validates and saves Qryvanta's user configuration.
//
// The configuration file never contains secrets. Connection credentials live in the
// operating system's credential store and are referenced by ID only.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/TuanFaied/qryvanta/internal/fsutil"
)

// SchemaVersion is the current configuration file format version.
const SchemaVersion = 1

// AppDirName is the directory name used under the OS config and cache directories.
const AppDirName = "Qryvanta"

// Theme is the UI color theme.
type Theme string

const (
	ThemeDark   Theme = "dark"
	ThemeLight  Theme = "light"
	ThemeSystem Theme = "system"
)

// LogLevel is the minimum level written to the log file.
type LogLevel string

const (
	LogDebug LogLevel = "debug"
	LogInfo  LogLevel = "info"
	LogWarn  LogLevel = "warn"
	LogError LogLevel = "error"
)

// Config is the full user configuration.
type Config struct {
	SchemaVersion int        `json:"schemaVersion"`
	Appearance    Appearance `json:"appearance"`
	Execution     Execution  `json:"execution"`
	History       History    `json:"history"`
	Logging       Logging    `json:"logging"`
}

// Appearance holds UI preferences.
type Appearance struct {
	Theme          Theme `json:"theme"`
	EditorFontSize int   `json:"editorFontSize"`
}

// Execution holds default limits applied to every query execution.
type Execution struct {
	// ReadOnly rejects statements that are not provably read-only. See docs/architecture (D5).
	ReadOnly          bool `json:"readOnly"`
	TimeoutSeconds    int  `json:"timeoutSeconds"`
	MaxRows           int  `json:"maxRows"`
	MaxResultMB       int  `json:"maxResultMB"`
	ProbeTimeoutSecs  int  `json:"probeTimeoutSeconds"`
	MaxProbesPerRun   int  `json:"maxProbesPerRun"`
	ConnectTimeoutSec int  `json:"connectTimeoutSeconds"`
}

// History controls local session history retention.
type History struct {
	Enabled       bool `json:"enabled"`
	RetentionDays int  `json:"retentionDays"`
	// StoreResultSamples stores a small sample of result rows with each session. Off by default
	// because results may contain sensitive data.
	StoreResultSamples bool `json:"storeResultSamples"`
}

// Logging controls the application log file.
type Logging struct {
	Level LogLevel `json:"level"`
	// LogSQLText includes SQL text in debug-level log records. Off by default.
	LogSQLText bool `json:"logSqlText"`
}

// Limits on user-editable values. They protect the app and the database from settings
// that are clearly mistakes (for example, a 0-second timeout or 100 million rows in memory).
const (
	MinTimeoutSeconds = 1
	MaxTimeoutSeconds = 3600
	MinMaxRows        = 1
	MaxMaxRows        = 1_000_000
	MinMaxResultMB    = 1
	MaxMaxResultMB    = 2048
	MinProbeTimeout   = 1
	MaxProbeTimeout   = 600
	MaxProbesPerRun   = 50
	MinConnectTimeout = 1
	MaxConnectTimeout = 300
	MinRetentionDays  = 1
	MaxRetentionDays  = 3650
	MinEditorFontSize = 8
	MaxEditorFontSize = 32
)

// Default returns the configuration used when no file exists.
func Default() Config {
	return Config{
		SchemaVersion: SchemaVersion,
		Appearance: Appearance{
			Theme:          ThemeDark,
			EditorFontSize: 14,
		},
		Execution: Execution{
			ReadOnly:          true,
			TimeoutSeconds:    30,
			MaxRows:           10_000,
			MaxResultMB:       64,
			ProbeTimeoutSecs:  15,
			MaxProbesPerRun:   10,
			ConnectTimeoutSec: 15,
		},
		History: History{
			Enabled:            true,
			RetentionDays:      30,
			StoreResultSamples: false,
		},
		Logging: Logging{
			Level:      LogInfo,
			LogSQLText: false,
		},
	}
}

// ValidationError lists every invalid field, so the UI can show them all at once.
type ValidationError struct {
	Fields map[string]string // JSON path -> message
}

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Fields))
	for _, k := range sortedKeys(e.Fields) {
		parts = append(parts, k+": "+e.Fields[k])
	}
	return "invalid configuration: " + strings.Join(parts, "; ")
}

// Validate checks every field and returns a *ValidationError if any are invalid.
func (c Config) Validate() error {
	errs := map[string]string{}
	checkRange := func(path string, v, min, max int) {
		if v < min || v > max {
			errs[path] = fmt.Sprintf("must be between %d and %d", min, max)
		}
	}

	switch c.Appearance.Theme {
	case ThemeDark, ThemeLight, ThemeSystem:
	default:
		errs["appearance.theme"] = `must be "dark", "light" or "system"`
	}
	checkRange("appearance.editorFontSize", c.Appearance.EditorFontSize, MinEditorFontSize, MaxEditorFontSize)

	checkRange("execution.timeoutSeconds", c.Execution.TimeoutSeconds, MinTimeoutSeconds, MaxTimeoutSeconds)
	checkRange("execution.maxRows", c.Execution.MaxRows, MinMaxRows, MaxMaxRows)
	checkRange("execution.maxResultMB", c.Execution.MaxResultMB, MinMaxResultMB, MaxMaxResultMB)
	checkRange("execution.probeTimeoutSeconds", c.Execution.ProbeTimeoutSecs, MinProbeTimeout, MaxProbeTimeout)
	checkRange("execution.maxProbesPerRun", c.Execution.MaxProbesPerRun, 0, MaxProbesPerRun)
	checkRange("execution.connectTimeoutSeconds", c.Execution.ConnectTimeoutSec, MinConnectTimeout, MaxConnectTimeout)

	checkRange("history.retentionDays", c.History.RetentionDays, MinRetentionDays, MaxRetentionDays)

	switch c.Logging.Level {
	case LogDebug, LogInfo, LogWarn, LogError:
	default:
		errs["logging.level"] = `must be "debug", "info", "warn" or "error"`
	}

	if len(errs) > 0 {
		return &ValidationError{Fields: errs}
	}
	return nil
}

// DefaultPath returns the config file path in the OS user config directory,
// for example %AppData%\Qryvanta\config.json on Windows.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	return filepath.Join(dir, AppDirName, "config.json"), nil
}

// Load reads the config file at path. A missing file returns Default() and no error.
// Fields missing from the file keep their default values, and unknown fields are ignored so
// that an older build can open a newer file. The result is validated.
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	if cfg.SchemaVersion > SchemaVersion {
		return Config{}, fmt.Errorf("config %s was written by a newer Qryvanta (schema %d, supported %d)",
			path, cfg.SchemaVersion, SchemaVersion)
	}
	cfg.SchemaVersion = SchemaVersion
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Save validates cfg and writes it to path atomically: it writes a temporary file in the
// same directory and renames it over the target, so a crash never leaves a half-written file.
func Save(path string, cfg Config) error {
	cfg.SchemaVersion = SchemaVersion
	if err := cfg.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')
	if err := fsutil.WriteFileAtomic(path, data, 0o600); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	return slices.Sorted(maps.Keys(m))
}
