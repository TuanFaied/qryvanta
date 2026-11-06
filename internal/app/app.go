// Package app is the composition root shared by the desktop app and the CLI.
//
// It owns configuration, logging and connection profiles, and (in later phases) constructs
// the remaining core implementations: SQL Server dialect, parser sidecar, local store.
// Front ends depend on *Core and never build those pieces themselves.
package app

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/TuanFaied/qryvanta/internal/config"
	"github.com/TuanFaied/qryvanta/internal/connection"
	"github.com/TuanFaied/qryvanta/internal/dialect/mssql"
	"github.com/TuanFaied/qryvanta/internal/logging"
	"github.com/TuanFaied/qryvanta/internal/schema"
	"github.com/TuanFaied/qryvanta/internal/version"
)

// Options configures New. Empty paths use the OS defaults.
type Options struct {
	ConfigPath string
	LogDir     string
	// DisableFileLog skips the log file (used by short-lived CLI commands and tests).
	DisableFileLog bool
	// LogExtra additionally receives log output (for example os.Stderr).
	LogExtra io.Writer
	// RecoverInvalidConfig moves an unreadable or invalid config file aside and starts with
	// defaults, recording a startup warning. The desktop app sets it so a bad file cannot
	// prevent the app from opening; the CLI leaves it off and reports the error.
	RecoverInvalidConfig bool
	// ConnectionsPath overrides the connections file (default: connections.json next to the
	// config file).
	ConnectionsPath string
	// Credentials overrides the credential store (default: the OS credential manager).
	// Tests pass connection.NewMemoryStore() so they never touch the real keyring.
	Credentials connection.CredentialStore
	// Driver overrides the database driver (default: SQL Server).
	Driver connection.Driver
	// Schema overrides the metadata reader (default: SQL Server catalog views).
	Schema schema.Reader
	// Access overrides the write-access checker (default: SQL Server permissions).
	Access connection.AccessChecker
}

// Core holds the shared application state.
type Core struct {
	configPath  string
	log         *logging.Logger
	warnings    []string
	connections *connection.Manager
	schema      schema.Reader
	pool        *connection.Pool

	mu  sync.RWMutex
	cfg config.Config
}

// New loads configuration and starts logging.
//
// If the config file is invalid, New returns an error rather than silently replacing the
// user's file with defaults.
func New(opts Options) (*Core, error) {
	path := opts.ConfigPath
	if path == "" {
		p, err := config.DefaultPath()
		if err != nil {
			return nil, err
		}
		path = p
	}
	var warnings []string
	cfg, err := config.Load(path)
	if err != nil {
		if !opts.RecoverInvalidConfig {
			return nil, err
		}
		backup := fmt.Sprintf("%s.invalid-%s", path, time.Now().Format("20060102-150405"))
		if renameErr := os.Rename(path, backup); renameErr != nil {
			return nil, errors.Join(err, fmt.Errorf("move invalid config aside: %w", renameErr))
		}
		cfg = config.Default()
		warnings = append(warnings, fmt.Sprintf(
			"Your configuration file could not be loaded (%v). It was moved to %s and defaults are in use.", err, backup))
	}

	logDir := ""
	if !opts.DisableFileLog {
		logDir = opts.LogDir
		if logDir == "" {
			d, err := logging.DefaultDir()
			if err != nil {
				return nil, err
			}
			logDir = d
		}
	}
	log, err := logging.New(logging.Options{
		Dir:   logDir,
		Level: logging.ParseLevel(cfg.Logging.Level),
		Extra: opts.LogExtra,
	})
	if err != nil {
		return nil, err
	}

	v := version.Get()
	log.Info("qryvanta starting", "version", v.Version, "commit", v.Commit, "platform", v.Platform)
	for _, w := range warnings {
		log.Warn(w)
	}

	connPath := opts.ConnectionsPath
	if connPath == "" {
		connPath = filepath.Join(filepath.Dir(path), connection.FileName)
	}
	creds := opts.Credentials
	if creds == nil {
		creds = connection.NewKeyringStore()
	}
	conns := connection.NewManager(connection.NewStore(connPath), creds, log.Logger)

	driver := opts.Driver
	if driver == nil {
		driver = mssql.New()
	}
	reader := opts.Schema
	if reader == nil {
		reader = mssql.Metadata{}
	}
	c := &Core{configPath: path, log: log, cfg: cfg, warnings: warnings, connections: conns, schema: reader}
	c.pool = connection.NewPool(conns, driver, c.openOptions, log.Logger)
	access := opts.Access
	if access == nil {
		access = mssql.Access{}
	}
	c.pool.SetAccessChecker(access)
	return c, nil
}

// Connections returns the connection profile manager.
func (c *Core) Connections() *connection.Manager { return c.connections }

// Pool returns the open-connection pool.
func (c *Core) Pool() *connection.Pool { return c.pool }

// Schema returns the metadata reader for the configured dialect.
func (c *Core) Schema() schema.Reader { return c.schema }

// openOptions reads the current settings each time a connection is opened.
func (c *Core) openOptions() connection.OpenOptions {
	cfg := c.Config()
	return connection.OpenOptions{
		ConnectTimeout: time.Duration(cfg.Execution.ConnectTimeoutSec) * time.Second,
		AppName:        "Qryvanta " + version.Get().Version,
	}
}

// StartupWarnings returns problems found during startup that the user should see.
func (c *Core) StartupWarnings() []string { return slices.Clone(c.warnings) }

// Logger returns the application logger.
func (c *Core) Logger() *slog.Logger { return c.log.Logger }

// ComponentLogger returns a logger for a third-party component that only records warnings and
// errors (and info while Qryvanta's own level is Debug). The desktop app gives it to Wails so
// routine runtime chatter does not bury Qryvanta's own entries.
func (c *Core) ComponentLogger(name string) *slog.Logger {
	return c.log.ForComponent(name, slog.LevelWarn)
}

// ConfigPath returns the path of the active config file.
func (c *Core) ConfigPath() string { return c.configPath }

// LogPath returns the active log file path, or "" when file logging is disabled.
func (c *Core) LogPath() string { return c.log.Path }

// Config returns a copy of the current configuration.
func (c *Core) Config() config.Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cfg
}

// UpdateConfig validates, saves and applies cfg. On error nothing changes.
func (c *Core) UpdateConfig(cfg config.Config) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := config.Save(c.configPath, cfg); err != nil {
		return err
	}
	cfg.SchemaVersion = config.SchemaVersion
	old := c.cfg
	c.cfg = cfg
	c.log.Level.Set(logging.ParseLevel(cfg.Logging.Level))
	if old.Logging.Level != cfg.Logging.Level {
		c.log.Info("log level changed", "from", old.Logging.Level, "to", cfg.Logging.Level)
	}
	c.log.Info("configuration saved")
	return nil
}

// Close releases resources. It is safe to call more than once.
func (c *Core) Close() error {
	c.log.Info("qryvanta stopping")
	var errs []error
	if err := c.pool.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close connections: %w", err))
	}
	if err := c.log.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close log: %w", err))
	}
	return errors.Join(errs...)
}
