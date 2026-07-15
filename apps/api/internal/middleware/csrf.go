package middleware

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"github.com/highland-io/highland/apps/api/internal/observability"
	"net/http"
	"strings"
	"time"
)

// CSRF enforces a stateless, signed double-submit token on state-changing
// requests. The token is `<nonce>.<base64url(HMAC-SHA256(secret, nonce))>`,
// mirroring the session token scheme so it needs no server-side storage.
//
// Safe methods (GET/HEAD/OPTIONS) mint a non-HttpOnly cookie (readable by the
// SPA) when one is missing or its signature does not verify. Unsafe methods must
// present an `X-CSRF-Token` header that both equals the cookie and carries a
// valid signature. The signature stops a same-site subdomain from injecting an
// arbitrary cookie value; the double-submit equality is the primary CSRF
// defense (a cross-origin attacker cannot read the victim's cookie to echo it).
// Tokens are NOT bound to a specific session — any validly-signed token is
// accepted; per-session binding is a possible future hardening.
//
// `secret` MUST be the same key used to sign session tokens; pass it explicitly
// (the config string is empty on the ephemeral-secret path).
func CSRF(secret []byte, cookieName string, secure bool, ttl time.Duration, m *observability.Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookieVal := ""
			if c, err := r.Cookie(cookieName); err == nil {
				cookieVal = c.Value
			}
			if !validCSRFToken(secret, cookieVal) {
				cookieVal = newCSRFToken(secret)
				http.SetCookie(w, &http.Cookie{
					Name:     cookieName,
					Value:    cookieVal,
					Path:     "/",
					HttpOnly: false, // the SPA must read it to echo the header
					Secure:   secure,
					SameSite: http.SameSiteLaxMode,
					MaxAge:   int(ttl.Seconds()),
				})
			}

			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r) // issuance only
				return
			}

			header := r.Header.Get("X-CSRF-Token")
			if header == "" ||
				subtle.ConstantTimeCompare([]byte(header), []byte(cookieVal)) != 1 ||
				!validCSRFToken(secret, header) {
				m.IncCSRFRejection()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"csrf token invalid"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func newCSRFToken(secret []byte) string {
	nonce := make([]byte, 32)
	_, _ = rand.Read(nonce)
	n := hex.EncodeToString(nonce)
	return n + "." + csrfSign(secret, n)
}

func validCSRFToken(secret []byte, tok string) bool {
	if tok == "" {
		return false
	}
	i := strings.LastIndexByte(tok, '.')
	if i <= 0 || i == len(tok)-1 {
		return false
	}
	return hmac.Equal([]byte(csrfSign(secret, tok[:i])), []byte(tok[i+1:]))
}

func csrfSign(secret []byte, msg string) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(msg))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
