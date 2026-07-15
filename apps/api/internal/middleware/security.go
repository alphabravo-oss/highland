package middleware

import "net/http"

// SecurityHeaders sets hardening response headers on every BFF response.
//
// The BFF returns only JSON/binary and never serves an HTML document, so a
// maximally strict `default-src 'none'` CSP is safe here (the SPA's own,
// looser CSP is applied by nginx where the document is served). HSTS is emitted
// only when hsts is true (wired to CookieSecure, the existing TLS signal) so we
// don't pin HTTPS on a plain-HTTP dev deployment.
func SecurityHeaders(hsts bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=(), interest-cohort=()")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Cross-Origin-Resource-Policy", "same-origin")
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}
