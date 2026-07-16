package config

import (
	"strings"
	"testing"
)

func TestCephDashboardPublicURLValidation(t *testing.T) {
	tests := []struct {
		name      string
		publicURL string
		allowHTTP bool
		wantError string
	}{
		{name: "https root", publicURL: "https://ceph.example.test/dashboard"},
		{name: "lab http explicitly allowed", publicURL: "http://ceph.lab.test", allowHTTP: true},
		{name: "http denied by default", publicURL: "http://ceph.example.test", wantError: "requires HTTPS"},
		{name: "userinfo rejected", publicURL: "https://admin:secret@ceph.example.test", wantError: "without userinfo"},
		{name: "query rejected", publicURL: "https://ceph.example.test?token=secret", wantError: "query or fragment"},
		{name: "fragment rejected", publicURL: "https://ceph.example.test/#/osd", wantError: "query or fragment"},
		{name: "protocol relative rejected", publicURL: "//ceph.example.test", wantError: "absolute URL"},
		{name: "javascript rejected", publicURL: "javascript:alert(1)", wantError: "absolute URL"},
		{name: "data rejected", publicURL: "data:text/html,hello", wantError: "absolute URL"},
		{name: "malformed port rejected", publicURL: "https://ceph.example.test:not-a-port", wantError: "absolute URL"},
		{name: "empty DNS label rejected", publicURL: "https://ceph..example.test", wantError: "absolute URL"},
		{name: "invalid DNS label rejected", publicURL: "https://-ceph.example.test", wantError: "absolute URL"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HIGHLAND_ROOK_CEPH_DASHBOARD_PUBLIC_URL", tc.publicURL)
			if tc.allowHTTP {
				t.Setenv("HIGHLAND_ROOK_CEPH_DASHBOARD_ALLOW_HTTP", "true")
			} else {
				t.Setenv("HIGHLAND_ROOK_CEPH_DASHBOARD_ALLOW_HTTP", "false")
			}
			cfg, err := LoadFromEnv()
			if tc.wantError == "" {
				if err != nil {
					t.Fatalf("LoadFromEnv() error = %v", err)
				}
				if cfg.RookCephDashboardPublicURL != strings.TrimRight(tc.publicURL, "/") {
					t.Fatalf("public URL = %q", cfg.RookCephDashboardPublicURL)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("LoadFromEnv() error = %v, want substring %q", err, tc.wantError)
			}
		})
	}
}
