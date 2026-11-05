package logging

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newBufferLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(NewRedactingHandler(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
}

func TestRedactingHandlerMasksSecrets(t *testing.T) {
	var buf bytes.Buffer
	log := newBufferLogger(&buf)

	type profile struct {
		Server string
		DSN    string
	}

	log.With("password", "with-attr-secret").Info("connect failed: password=msg-secret;",
		"Password", "attr-secret",
		"connection_string", "server=x;pwd=cs-secret",
		"detail", "login failed; Password=inline-secret;Database=app",
		"err", errors.New("dial sqlserver://sa:url-secret@db1"),
		slog.Group("conn", "token", "group-secret", "server", "db1"),
		"profile", profile{Server: "db1", DSN: "server=db1;password=struct-secret"},
		"rows", 42,
	)

	out := buf.String()
	for _, secret := range []string{"with-attr-secret", "msg-secret", "attr-secret", "cs-secret",
		"inline-secret", "url-secret", "group-secret", "struct-secret"} {
		if strings.Contains(out, secret) {
			t.Errorf("log output contains %q:\n%s", secret, out)
		}
	}
	for _, keep := range []string{`"rows":42`, `"server":"db1"`, "Database=app"} {
		if !strings.Contains(out, keep) {
			t.Errorf("log output lost %q:\n%s", keep, out)
		}
	}
}

func TestNewWritesFileAndHonoursLevel(t *testing.T) {
	dir := t.TempDir()
	l, err := New(Options{Dir: dir, Level: slog.LevelWarn})
	if err != nil {
		t.Fatal(err)
	}
	l.Info("hidden")
	l.Warn("shown")
	l.Level.Set(slog.LevelDebug)
	l.Debug("now visible")
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, "hidden") || !strings.Contains(s, "shown") || !strings.Contains(s, "now visible") {
		t.Errorf("unexpected log content:\n%s", s)
	}
}

func TestRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	big := bytes.Repeat([]byte("x"), 200)
	for i := 0; i < 4; i++ {
		if err := os.WriteFile(path, big, 0o600); err != nil {
			t.Fatal(err)
		}
		l, err := New(Options{Dir: dir, MaxBytes: 100, Backups: 2})
		if err != nil {
			t.Fatal(err)
		}
		_ = l.Close()
	}
	for _, name := range []string{FileName, FileName + ".1", FileName + ".2"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, FileName+".3")); err == nil {
		t.Error("more backups kept than configured")
	}
}
