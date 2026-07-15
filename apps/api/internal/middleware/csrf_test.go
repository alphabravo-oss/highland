package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var csrfSecret = []byte("test-secret-key-please-ignore")

func csrfHandler() http.Handler {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	return CSRF(csrfSecret, "highland_csrf", false, time.Hour)(next)
}

func TestCSRFGetIssuesCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	csrfHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("GET should pass through, got %d", rec.Code)
	}
	var tok string
	for _, c := range rec.Result().Cookies() {
		if c.Name == "highland_csrf" {
			tok = c.Value
		}
	}
	if tok == "" {
		t.Fatal("GET must mint a highland_csrf cookie")
	}
	if !validCSRFToken(csrfSecret, tok) {
		t.Fatal("minted token must verify")
	}
}

func TestCSRFPostRequiresValidToken(t *testing.T) {
	tok := newCSRFToken(csrfSecret)

	// No header → 403.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.AddCookie(&http.Cookie{Name: "highland_csrf", Value: tok})
	csrfHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST without header should 403, got %d", rec.Code)
	}

	// Matching header + cookie with valid signature → pass.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/x", nil)
	req.AddCookie(&http.Cookie{Name: "highland_csrf", Value: tok})
	req.Header.Set("X-CSRF-Token", tok)
	csrfHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("valid double-submit should pass, got %d", rec.Code)
	}

	// Forged token (wrong secret) equal in header and cookie → 403 (signature fails).
	forged := newCSRFToken([]byte("attacker-secret"))
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/x", nil)
	req.AddCookie(&http.Cookie{Name: "highland_csrf", Value: forged})
	req.Header.Set("X-CSRF-Token", forged)
	csrfHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("forged-signature token should 403, got %d", rec.Code)
	}

	// Header not matching cookie → 403.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/x", nil)
	req.AddCookie(&http.Cookie{Name: "highland_csrf", Value: tok})
	req.Header.Set("X-CSRF-Token", newCSRFToken(csrfSecret)) // valid sig but different value
	csrfHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("mismatched header/cookie should 403, got %d", rec.Code)
	}
}
