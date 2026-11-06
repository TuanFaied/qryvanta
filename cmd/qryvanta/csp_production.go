//go:build production

package main

import "github.com/TuanFaied/qryvanta/frontend"

// contentSecurityPolicy for production builds (`wails3 build` passes -tags production).
func contentSecurityPolicy() string { return frontend.ProductionCSP() }
