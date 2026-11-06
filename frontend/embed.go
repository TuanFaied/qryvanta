// Package frontend embeds the built web assets (frontend/dist) into the desktop binary.
//
// Run `pnpm -C frontend build` (or `wails3 build`) before building cmd/qryvanta.
package frontend

import (
	"embed"
	_ "embed"
	"strings"
)

// Assets holds the production build of the React frontend.
//
//go:embed all:dist
var Assets embed.FS

//go:embed csp-production.txt
var productionCSP string

// ProductionCSP is the Content-Security-Policy for production builds. The same file is
// written into index.html as a <meta> tag by the Vite production build (vite.config.ts), so
// the header sent by the Go asset server and the page's own policy never drift apart.
func ProductionCSP() string { return strings.TrimSpace(productionCSP) }
