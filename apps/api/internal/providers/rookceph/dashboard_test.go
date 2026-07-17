package rookceph

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testJWT() string {
	payload, _ := json.Marshal(map[string]int64{"exp": time.Now().Add(time.Hour).Unix()})
	return "x." + base64.RawURLEncoding.EncodeToString(payload) + ".x"
}

func TestDashboardAuthCacheAnd401Refresh(t *testing.T) {
	var logins atomic.Int32
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/auth" {
			logins.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{"token": testJWT()})
			return
		}
		if r.Header.Get("Accept") != dashboardMediaType {
			t.Errorf("unexpected Accept %q", r.Header.Get("Accept"))
		}
		if reads.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"status":"HEALTH_OK"}`))
	}))
	defer server.Close()
	client, err := NewDashboardClient(DashboardConfig{URL: server.URL, Username: "reader", Password: "secret", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Get(context.Background(), "/api/health/minimal")
	if err != nil || result.Stale || !strings.Contains(string(result.Data), "HEALTH_OK") {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if logins.Load() != 2 {
		t.Fatalf("logins=%d, want 2 after 401", logins.Load())
	}
}

func TestDashboardUsesEndpointSpecificMediaTypes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/auth" {
			_ = json.NewEncoder(w).Encode(map[string]string{"token": testJWT()})
			return
		}
		want := dashboardMediaType
		if r.URL.Path == "/api/block/image" {
			want = dashboardMediaTypeV2
		}
		if got := r.Header.Get("Accept"); got != want {
			t.Errorf("%s Accept=%q, want %q", r.URL.Path, got, want)
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	client, err := NewDashboardClient(DashboardConfig{URL: server.URL, Username: "reader", Password: "secret", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"/api/health/minimal", "/api/osd", "/api/pool", "/api/block/image"} {
		if _, err := client.Get(context.Background(), endpoint); err != nil {
			t.Fatalf("Get(%q): %v", endpoint, err)
		}
	}
}

func TestDashboardPreservesConfiguredURLPrefix(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/ceph-dashboard/api/auth" {
			_ = json.NewEncoder(w).Encode(map[string]string{"token": testJWT()})
			return
		}
		if r.URL.Path != "/ceph-dashboard/api/health/minimal" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"status":"HEALTH_OK"}`))
	}))
	defer server.Close()

	client, err := NewDashboardClient(DashboardConfig{
		URL:        server.URL + "/ceph-dashboard/",
		Username:   "reader",
		Password:   "secret",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(context.Background(), "/api/health/minimal"); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(paths, ","), "/ceph-dashboard/api/auth,/ceph-dashboard/api/health/minimal"; got != want {
		t.Fatalf("paths=%q, want %q", got, want)
	}
}

func TestDashboardLogoutClearsSessionAndUsesFixedEndpoint(t *testing.T) {
	var logins atomic.Int32
	var logouts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/auth":
			logins.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{"token": testJWT()})
		case "/api/auth/logout":
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				t.Error("logout request omitted the current bearer token")
			}
			logouts.Add(1)
			_, _ = w.Write([]byte(`{}`))
		default:
			_, _ = w.Write([]byte(`{"status":"HEALTH_OK"}`))
		}
	}))
	defer server.Close()
	client, err := NewDashboardClient(DashboardConfig{URL: server.URL, Username: "reader", Password: "secret", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(context.Background(), "/api/health/minimal"); err != nil {
		t.Fatal(err)
	}
	if err := client.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(context.Background(), "/api/health/minimal"); err != nil {
		t.Fatal(err)
	}
	if logins.Load() != 2 || logouts.Load() != 1 {
		t.Fatalf("logins=%d logouts=%d, want 2/1", logins.Load(), logouts.Load())
	}
}

func TestDashboardRejectsSSRFAndOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/auth" {
			_ = json.NewEncoder(w).Encode(map[string]string{"token": testJWT()})
			return
		}
		_, _ = fmt.Fprint(w, `"`+strings.Repeat("x", maxDashboardBody)+`"`)
	}))
	defer server.Close()
	if _, err := NewDashboardClient(DashboardConfig{URL: "https://reader:password@ceph.example", Username: "reader", Password: "secret"}); err == nil {
		t.Fatal("expected URL userinfo to be rejected")
	}
	client, _ := NewDashboardClient(DashboardConfig{URL: server.URL, Username: "reader", Password: "secret", HTTPClient: server.Client()})
	if _, err := client.Get(context.Background(), "http://metadata.internal/"); err == nil {
		t.Fatal("expected arbitrary endpoint rejection")
	}
	if _, err := client.Get(context.Background(), "/api/admin/anything"); err == nil {
		t.Fatal("expected non-allowlisted Ceph endpoint rejection")
	}
	if _, err := client.Get(context.Background(), "/api/osd"); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("expected size limit error, got %v", err)
	}
}

func TestDashboardDoesNotFollowRedirectsToAnotherOrigin(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/auth" {
			_ = json.NewEncoder(w).Encode(map[string]string{"token": testJWT()})
			return
		}
		http.Redirect(w, r, target.URL+"/metadata", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client, err := NewDashboardClient(DashboardConfig{URL: server.URL, Username: "reader", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(context.Background(), "/api/health/minimal"); err == nil || !strings.Contains(err.Error(), "307") {
		t.Fatalf("redirect error=%v", err)
	}
	if redirected.Load() != 0 {
		t.Fatal("Ceph client followed a redirect to another origin")
	}
}

func TestDashboardTLSVerificationAndCustomCAClient(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/auth" {
			_ = json.NewEncoder(w).Encode(map[string]string{"token": testJWT()})
			return
		}
		_, _ = w.Write([]byte(`{"status":"HEALTH_OK"}`))
	}))
	defer server.Close()

	verified, err := NewDashboardClient(DashboardConfig{URL: server.URL, Username: "reader", Password: "secret", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verified.Get(context.Background(), "/api/health/minimal"); err != nil {
		t.Fatalf("custom CA-equivalent client rejected: %v", err)
	}

	untrusted, err := NewDashboardClient(DashboardConfig{URL: server.URL, Username: "reader", Password: "secret", HTTPClient: &http.Client{Timeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := untrusted.Get(context.Background(), "/api/health/minimal"); err == nil {
		t.Fatal("default trust store unexpectedly accepted the test server certificate")
	}
}

func TestDashboardTimeoutMalformedResponsesAndErrorRedaction(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		delay       time.Duration
		want        string
	}{
		{name: "malformed JSON", contentType: "application/json", body: `{broken`, want: "malformed JSON"},
		{name: "unsupported media", contentType: "text/html", body: `{}`, want: "unsupported content type"},
		{name: "timeout", contentType: "application/json", body: `{}`, delay: 100 * time.Millisecond, want: "failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/auth" {
					_ = json.NewEncoder(w).Encode(map[string]string{"token": testJWT()})
					return
				}
				time.Sleep(tc.delay)
				w.Header().Set("Content-Type", tc.contentType)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			httpClient := server.Client()
			if tc.delay > 0 {
				httpClient.Timeout = 20 * time.Millisecond
			}
			client, err := NewDashboardClient(DashboardConfig{URL: server.URL, Username: "reader-user", Password: "super-secret-password", HTTPClient: httpClient})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Get(context.Background(), "/api/health/minimal")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want substring %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "reader-user") || strings.Contains(err.Error(), "super-secret-password") {
				t.Fatalf("credentials leaked in error: %v", err)
			}
		})
	}
}

func TestDashboardDoesNotServeExpiredStaleCache(t *testing.T) {
	client := &DashboardClient{cache: map[string]dashboardCacheEntry{"/api/osd": {value: []byte(`[]`), observedAt: time.Now().Add(-maxDashboardStale - time.Minute)}}, circuits: map[string]dashboardCircuitState{"/api/osd": {openUntil: time.Now().Add(time.Minute)}}}
	if _, err := client.Get(context.Background(), "/api/osd"); err == nil || !strings.Contains(err.Error(), "circuit is open") {
		t.Fatalf("expired cache result error=%v", err)
	}
}

func TestDashboardCircuitIsIsolatedByEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/auth" {
			_ = json.NewEncoder(w).Encode(map[string]string{"token": testJWT()})
			return
		}
		if r.URL.Path == "/api/block/image" {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	client, err := NewDashboardClient(DashboardConfig{URL: server.URL, Username: "reader", Password: "secret", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := client.Get(context.Background(), "/api/block/image"); err == nil {
			t.Fatal("expected image endpoint failure")
		}
	}
	if _, err := client.Get(context.Background(), "/api/block/image"); err == nil || !strings.Contains(err.Error(), "circuit is open") {
		t.Fatalf("image circuit error=%v", err)
	}
	if _, err := client.Get(context.Background(), "/api/osd"); err != nil {
		t.Fatalf("unrelated OSD endpoint was blocked: %v", err)
	}
}
