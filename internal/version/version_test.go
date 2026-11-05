package version

import (
	"runtime"
	"testing"
)

func TestGet(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })
	Version = "1.2.3"

	info := Get()
	if info.Version != "1.2.3" {
		t.Errorf("Version = %q, want value set by the linker", info.Version)
	}
	if info.GoVersion != runtime.Version() || info.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		t.Errorf("unexpected runtime info %+v", info)
	}
}
