package archtest

import (
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"testing"
)

const module = "github.com/TuanFaied/qryvanta"

// adapterPackages may import UI frameworks. Everything else under internal/ is core.
var adapterPackages = map[string]bool{
	module + "/internal/desktop": true,
}

var forbiddenForCore = []string{
	"github.com/wailsapp/",
	module + "/frontend",
	module + "/internal/desktop",
	module + "/cmd/",
}

type goPackage struct {
	ImportPath string
	Deps       []string
}

// TestCoreDoesNotDependOnAdapters fails when a core package (directly or transitively)
// imports Wails, the frontend, the desktop adapter or a command package.
func TestCoreDoesNotDependOnAdapters(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go tool not available")
	}
	cmd := exec.Command("go", "list", "-json=ImportPath,Deps", module+"/internal/...")
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("go list failed: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("go list failed: %v", err)
	}

	dec := json.NewDecoder(strings.NewReader(string(out)))
	checked := 0
	for {
		var p goPackage
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if adapterPackages[p.ImportPath] {
			continue
		}
		checked++
		for _, dep := range p.Deps {
			for _, bad := range forbiddenForCore {
				if strings.HasPrefix(dep, bad) || dep == strings.TrimSuffix(bad, "/") {
					t.Errorf("core package %s depends on %s", p.ImportPath, dep)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no core packages were checked; the package pattern is wrong")
	}
}
