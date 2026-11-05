// Package frontend embeds the built web assets (frontend/dist) into the desktop binary.
//
// Run `pnpm -C frontend build` (or `wails3 build`) before building cmd/qryvanta.
package frontend

import "embed"

// Assets holds the production build of the React frontend.
//
//go:embed all:dist
var Assets embed.FS
