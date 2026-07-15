package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func resolvedIP(trusted []string, remoteAddr string, headers map[string]string) string {
	var got string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.RemoteAddr })
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	ClientIP(trusted)(next).ServeHTTP(httptest.NewRecorder(), req)
	return got
}

func TestClientIPIgnoresHeadersWhenNoTrust(t *testing.T) {
	// Default (no trusted proxies): forwarding headers must be ignored.
	got := resolvedIP(nil, "203.0.113.9:5000", map[string]string{
		"X-Forwarded-For": "1.1.1.1",
		"True-Client-IP":  "2.2.2.2",
		"X-Real-IP":       "3.3.3.3",
	})
	if got != "203.0.113.9:5000" {
		t.Fatalf("untrusted peer headers must be ignored, got %q", got)
	}
}

func TestClientIPIgnoresHeadersFromUntrustedPeer(t *testing.T) {
	got := resolvedIP([]string{"10.0.0.0/8"}, "203.0.113.9:5000", map[string]string{
		"X-Forwarded-For": "1.1.1.1",
	})
	if got != "203.0.113.9:5000" {
		t.Fatalf("peer outside trusted CIDR must not be honored, got %q", got)
	}
}

func TestClientIPHonorsXFFFromTrustedProxy(t *testing.T) {
	// Peer is the trusted proxy; the right-most untrusted XFF entry is the client.
	got := resolvedIP([]string{"10.0.0.0/8"}, "10.0.0.5:5000", map[string]string{
		"X-Forwarded-For": "9.9.9.9, 10.0.0.5",
	})
	if got != "9.9.9.9:0" {
		t.Fatalf("expected client 9.9.9.9 from trusted XFF, got %q", got)
	}
}

func TestClientIPFallsBackToRealIP(t *testing.T) {
	got := resolvedIP([]string{"10.0.0.0/8"}, "10.0.0.5:5000", map[string]string{
		"X-Real-IP": "8.8.8.8",
	})
	if got != "8.8.8.8:0" {
		t.Fatalf("expected X-Real-IP fallback, got %q", got)
	}
}

func TestClientIPBareTrustedIP(t *testing.T) {
	got := resolvedIP([]string{"10.0.0.5"}, "10.0.0.5:5000", map[string]string{
		"X-Forwarded-For": "7.7.7.7",
	})
	if got != "7.7.7.7:0" {
		t.Fatalf("bare trusted IP should be honored as /32, got %q", got)
	}
}
