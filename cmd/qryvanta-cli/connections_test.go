package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/connection"
)

func TestConnectionsListEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	code, out, errOut := runCLI(t, "--config", path, "connections", "list")
	if code != 0 || strings.TrimSpace(out) != "No saved connections." {
		t.Fatalf("code=%d out=%q stderr=%q", code, out, errOut)
	}
}

func TestConnectionsListShowsProfilesWithoutPasswords(t *testing.T) {
	opts := app.Options{
		ConfigPath:     filepath.Join(t.TempDir(), "config.json"),
		DisableFileLog: true,
		Credentials:    connection.NewMemoryStore(),
	}
	core, err := app.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	const pw = "do-not-print-me"
	if _, err := core.Connections().Create(connection.Profile{
		Name: "Local", Server: "localhost", Port: 14333, Auth: connection.AuthSQL, Username: "reader",
	}, pw); err != nil {
		t.Fatal(err)
	}
	if _, err := core.Connections().Create(connection.Profile{
		Name: "Corp", Server: "db1.corp", Database: "Sales", Auth: connection.AuthWindows,
	}, ""); err != nil {
		t.Fatal(err)
	}
	_ = core.Close()

	var table bytes.Buffer
	if err := cmdConnectionsList(nil, opts, &table); err != nil {
		t.Fatal(err)
	}
	out := table.String()
	for _, want := range []string{"Corp", "db1.corp", "Sales", "Windows", "Local", "localhost,14333", "SQL: reader", "stored"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "Corp") > strings.Index(out, "Local") {
		t.Error("profiles not sorted by name")
	}

	var js bytes.Buffer
	if err := cmdConnectionsList([]string{"--json"}, opts, &js); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(js.String()+out, pw) {
		t.Fatal("password printed")
	}
	var views []map[string]any
	if err := json.Unmarshal(js.Bytes(), &views); err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 || views[1]["hasPassword"] != true || views[0]["hasPassword"] != false {
		t.Errorf("unexpected JSON: %s", js.String())
	}
}

func TestConnectionsUsageErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	for _, args := range [][]string{{"connections"}, {"connections", "drop"}, {"connections", "list", "--bogus"}} {
		if code, _, _ := runCLI(t, append([]string{"--config", path}, args...)...); code != 2 {
			t.Errorf("%v: code=%d, want 2", args, code)
		}
	}
}
