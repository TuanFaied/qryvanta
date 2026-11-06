//go:build !production

package main

import "github.com/TuanFaied/qryvanta/internal/desktop"

// contentSecurityPolicy for development builds, which load the frontend from Vite's dev
// server with hot reload.
func contentSecurityPolicy() string { return desktop.DevelopmentCSP }
