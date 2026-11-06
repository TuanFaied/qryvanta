package desktop

import (
	"net/http"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// DevelopmentCSP is used by development builds (`wails3 dev`, DEV=true). Vite's dev server
// injects an inline script (React Refresh) and opens a WebSocket for hot reload, so this
// policy allows inline scripts and local WebSockets. Production builds never use it.
const DevelopmentCSP = "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self' data:; " +
	"connect-src 'self' ws://127.0.0.1:* ws://localhost:* http://127.0.0.1:* http://localhost:*; " +
	"object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// SecurityHeaders returns asset-server middleware that adds a Content-Security-Policy and
// related headers to every response served to the webview.
//
// The webview only loads Qryvanta's own bundled assets and talks to the Go backend through
// same-origin requests, so the production policy can be strict: no inline or external
// scripts, no eval, no external connections, no plugins, no framing.
func SecurityHeaders(policy string) application.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", policy)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "no-referrer")
			next.ServeHTTP(w, r)
		})
	}
}
