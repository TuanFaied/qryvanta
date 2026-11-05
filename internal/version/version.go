// Package version reports build information for Qryvanta binaries.
//
// Version and Commit are set at build time with -ldflags "-X ...". See Taskfile.yml.
package version

import "runtime"

// These variables are overwritten by the linker in release builds.
var (
	Version = "0.1.0-dev"
	Commit  = "unknown"
)

// Info describes the running build.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	GoVersion string `json:"goVersion"`
	Platform  string `json:"platform"`
}

// Get returns information about the current build.
func Get() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
}
