package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// records parses the JSON lines written to buf.
func records(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("bad log line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func messages(recs []map[string]any) []string {
	var out []string
	for _, r := range recs {
		out = append(out, r["msg"].(string))
	}
	return out
}

func TestComponentLoggerKeepsOnlyWarningsAndErrors(t *testing.T) {
	var buf bytes.Buffer
	l, err := New(Options{Level: slog.LevelInfo, Extra: &buf})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	wails := l.ForComponent("wails", slog.LevelWarn)

	wails.Info("[AssetFileServerFS] Handling request", "url", "/assets/index.js")
	wails.Debug("debug chatter")
	wails.Warn("webview2 runtime is outdated")
	wails.Error("window creation failed")
	l.Info("qryvanta starting") // the application's own info logs are unaffected

	recs := records(t, &buf)
	got := strings.Join(messages(recs), " | ")
	want := "webview2 runtime is outdated | window creation failed | qryvanta starting"
	if got != want {
		t.Fatalf("messages = %q, want %q", got, want)
	}
	if recs[0]["component"] != "wails" || recs[2]["component"] != nil {
		t.Errorf("component attribute wrong: %v / %v", recs[0], recs[2])
	}
}

func TestComponentLoggerShowsInfoWhileDebugging(t *testing.T) {
	var buf bytes.Buffer
	l, err := New(Options{Level: slog.LevelInfo, Extra: &buf})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	wails := l.ForComponent("wails", slog.LevelWarn)

	wails.Info("hidden at info level")
	l.Level.Set(slog.LevelDebug) // the user switches Qryvanta's log level to Debug in Settings
	wails.Info("visible while debugging")
	wails.Debug("still hidden: the floor only drops to Info")
	l.Level.Set(slog.LevelWarn)
	wails.Info("hidden again")
	wails.Warn("warnings always pass")

	got := strings.Join(messages(records(t, &buf)), " | ")
	if got != "visible while debugging | warnings always pass" {
		t.Fatalf("messages = %q", got)
	}
}

func TestComponentLoggerRedactsAndKeepsAttrs(t *testing.T) {
	var buf bytes.Buffer
	l, err := New(Options{Level: slog.LevelInfo, Extra: &buf})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.ForComponent("wails", slog.LevelWarn).With("window", 1).Warn("bad dsn password=hunter2;")

	recs := records(t, &buf)
	if len(recs) != 1 || recs[0]["window"] != float64(1) || strings.Contains(buf.String(), "hunter2") {
		t.Fatalf("unexpected record: %s", buf.String())
	}
}
