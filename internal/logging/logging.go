// Package logging configures structured logging for Qryvanta.
//
// Every logger built here redacts secrets: attributes with sensitive names are masked and
// key=value secrets inside string values (connection strings, driver errors) are scrubbed.
package logging

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/TuanFaied/qryvanta/internal/config"
	"github.com/TuanFaied/qryvanta/internal/security"
)

// FileName is the active log file name.
const FileName = "qryvanta.log"

// Options configures New.
type Options struct {
	// Dir is the log directory. Empty disables file logging.
	Dir string
	// Level is the minimum level. It can be changed later through the returned LevelVar.
	Level slog.Level
	// MaxBytes rotates the log at startup when the existing file is larger. 0 uses 5 MiB.
	MaxBytes int64
	// Backups is the number of rotated files kept (qryvanta.log.1 ... .N). 0 uses 3.
	Backups int
	// Extra receives log records in addition to the file (for example os.Stderr in the CLI).
	Extra io.Writer
}

// Logger bundles the logger with its runtime-adjustable level and the file it writes to.
type Logger struct {
	*slog.Logger
	Level *slog.LevelVar
	Path  string // empty when file logging is disabled
	file  *os.File
}

// ForComponent returns a logger for a third-party component (for example the Wails runtime)
// that writes to the same destination with component=<name>, but only records at or above
// floor. When the application log level is Debug, the floor drops to Info, so the component's
// routine messages are available exactly when someone is debugging.
func (l *Logger) ForComponent(name string, floor slog.Level) *slog.Logger {
	h := &floorHandler{next: l.Logger.Handler(), app: l.Level, floor: floor}
	return slog.New(h).With("component", name)
}

// floorHandler drops records below the component floor; the wrapped handler still applies
// the application level.
type floorHandler struct {
	next  slog.Handler
	app   *slog.LevelVar
	floor slog.Level
}

func (h *floorHandler) min() slog.Level {
	if h.app != nil && h.app.Level() <= slog.LevelDebug && h.floor > slog.LevelInfo {
		return slog.LevelInfo
	}
	return h.floor
}

func (h *floorHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.min() && h.next.Enabled(ctx, level)
}

func (h *floorHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level < h.min() {
		return nil
	}
	return h.next.Handle(ctx, r)
}

func (h *floorHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &floorHandler{next: h.next.WithAttrs(attrs), app: h.app, floor: h.floor}
}

func (h *floorHandler) WithGroup(name string) slog.Handler {
	return &floorHandler{next: h.next.WithGroup(name), app: h.app, floor: h.floor}
}

// Close flushes and closes the log file.
func (l *Logger) Close() error {
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

// New creates a JSON logger writing to Options.Dir/qryvanta.log (and Options.Extra).
func New(opts Options) (*Logger, error) {
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = 5 << 20
	}
	if opts.Backups <= 0 {
		opts.Backups = 3
	}
	level := new(slog.LevelVar)
	level.Set(opts.Level)

	var writers []io.Writer
	l := &Logger{Level: level}

	if opts.Dir != "" {
		if err := os.MkdirAll(opts.Dir, 0o700); err != nil {
			return nil, fmt.Errorf("create log directory: %w", err)
		}
		path := filepath.Join(opts.Dir, FileName)
		if err := rotate(path, opts.MaxBytes, opts.Backups); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, fmt.Errorf("open log file: %w", err)
		}
		l.file = f
		l.Path = path
		writers = append(writers, f)
	}
	if opts.Extra != nil {
		writers = append(writers, opts.Extra)
	}
	var w io.Writer = io.Discard
	if len(writers) > 0 {
		w = io.MultiWriter(writers...)
	}

	base := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	l.Logger = slog.New(NewRedactingHandler(base))
	return l, nil
}

// ParseLevel converts a config log level to a slog level.
func ParseLevel(lv config.LogLevel) slog.Level {
	switch lv {
	case config.LogDebug:
		return slog.LevelDebug
	case config.LogWarn:
		return slog.LevelWarn
	case config.LogError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// DefaultDir returns the log directory under the OS user cache directory,
// for example %LocalAppData%\Qryvanta\logs on Windows.
func DefaultDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate user cache directory: %w", err)
	}
	return filepath.Join(dir, config.AppDirName, "logs"), nil
}

// rotate shifts path -> path.1 -> path.2 ... when path is larger than maxBytes.
func rotate(path string, maxBytes int64, backups int) error {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat log file: %w", err)
	}
	if info.Size() <= maxBytes {
		return nil
	}
	_ = os.Remove(fmt.Sprintf("%s.%d", path, backups))
	for i := backups - 1; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", path, i), fmt.Sprintf("%s.%d", path, i+1))
	}
	if err := os.Rename(path, path+".1"); err != nil {
		return fmt.Errorf("rotate log file: %w", err)
	}
	return nil
}

// RedactingHandler masks secrets before records reach the wrapped handler.
type RedactingHandler struct {
	next slog.Handler
}

// NewRedactingHandler wraps next.
func NewRedactingHandler(next slog.Handler) *RedactingHandler {
	return &RedactingHandler{next: next}
}

func (h *RedactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *RedactingHandler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, security.RedactString(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(redactAttr(a))
		return true
	})
	return h.next.Handle(ctx, out)
}

func (h *RedactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	red := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		red[i] = redactAttr(a)
	}
	return &RedactingHandler{next: h.next.WithAttrs(red)}
}

func (h *RedactingHandler) WithGroup(name string) slog.Handler {
	return &RedactingHandler{next: h.next.WithGroup(name)}
}

func redactAttr(a slog.Attr) slog.Attr {
	if security.IsSensitiveKey(a.Key) {
		return slog.String(a.Key, security.Mask)
	}
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, security.RedactString(v.String()))
	case slog.KindGroup:
		group := v.Group()
		red := make([]any, len(group))
		for i, ga := range group {
			red[i] = redactAttr(ga)
		}
		return slog.Group(a.Key, red...)
	case slog.KindAny:
		if err, ok := v.Any().(error); ok {
			return slog.String(a.Key, security.RedactString(err.Error()))
		}
		if s, ok := v.Any().(fmt.Stringer); ok {
			return slog.String(a.Key, security.RedactString(s.String()))
		}
		// Other values (structs, maps) are formatted and scrubbed, so a struct holding a
		// connection string cannot slip through unredacted.
		return slog.String(a.Key, security.RedactString(strings.TrimSpace(fmt.Sprintf("%+v", v.Any()))))
	default:
		return slog.Attr{Key: a.Key, Value: v}
	}
}
