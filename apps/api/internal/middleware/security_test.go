package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })

	for _, hsts := range []bool{false, true} {
		rec := httptest.NewRecorder()
		SecurityHeaders(hsts)(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		h := rec.Header()
		if h.Get("X-Content-Type-Options") != "nosniff" {
			t.Error("missing X-Content-Type-Options")
		}
		if h.Get("X-Frame-Options") != "DENY" {
			t.Error("missing X-Frame-Options")
		}
		if h.Get("Referrer-Policy") != "no-referrer" {
			t.Error("missing Referrer-Policy")
		}
		if h.Get("Permissions-Policy") == "" {
			t.Error("missing Permissions-Policy")
		}
		if h.Get("Content-Security-Policy") != "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'" {
			t.Errorf("unexpected CSP: %q", h.Get("Content-Security-Policy"))
		}
		got := h.Get("Strict-Transport-Security")
		if hsts && got == "" {
			t.Error("expected HSTS when secure")
		}
		if !hsts && got != "" {
			t.Errorf("did not expect HSTS on plain HTTP, got %q", got)
		}
	}
}
