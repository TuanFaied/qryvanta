package desktop

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/frontend"
)

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("<html></html>"))
	})
	h := SecurityHeaders("default-src 'self'")(next)

	for _, path := range []string{"/", "/assets/index.js", "/missing"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if got := rec.Header().Get("Content-Security-Policy"); got != "default-src 'self'" {
			t.Errorf("%s: CSP = %q", path, got)
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s: missing hardening headers: %v", path, rec.Header())
		}
	}
}

// directive returns the value of one CSP directive.
func directive(policy, name string) string {
	for _, d := range strings.Split(policy, ";") {
		d = strings.TrimSpace(d)
		if d == name || strings.HasPrefix(d, name+" ") {
			return strings.TrimSpace(strings.TrimPrefix(d, name))
		}
	}
	return ""
}

func TestProductionPolicyIsStrict(t *testing.T) {
	p := frontend.ProductionCSP()
	if p == "" {
		t.Fatal("frontend/csp-production.txt is empty")
	}
	checks := map[string]string{
		"default-src":     "'self'",
		"script-src":      "'self'",
		"connect-src":     "'self'",
		"object-src":      "'none'",
		"base-uri":        "'none'",
		"frame-ancestors": "'none'",
	}
	for name, want := range checks {
		if got := directive(p, name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if strings.Contains(p, "unsafe-eval") || strings.Contains(p, "http://") || strings.Contains(p, "https://") {
		t.Errorf("production policy allows eval or external origins: %s", p)
	}
}

func TestDevelopmentPolicyOnlyRelaxesWhatViteNeeds(t *testing.T) {
	if strings.Contains(DevelopmentCSP, "unsafe-eval") {
		t.Error("development policy must not allow eval")
	}
	if !strings.Contains(directive(DevelopmentCSP, "script-src"), "'unsafe-inline'") {
		t.Error("React Refresh needs inline scripts in development")
	}
	for _, src := range strings.Fields(directive(DevelopmentCSP, "connect-src")) {
		if src != "'self'" && !strings.Contains(src, "127.0.0.1") && !strings.Contains(src, "localhost") {
			t.Errorf("development connect-src allows a non-local origin: %s", src)
		}
	}
	if directive(DevelopmentCSP, "object-src") != "'none'" {
		t.Error("development policy must still block plugins")
	}
}
