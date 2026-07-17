package config

import "testing"

func TestLinstorControllerURLSecurity(t *testing.T) {
	for _, tc := range []struct {
		name, url            string
		allowHTTP, wantError bool
	}{
		{"https", "https://linstor-controller.piraeus-datastore.svc:3371", false, false},
		{"explicit lab http", "http://linstor-controller.piraeus-datastore.svc:3370", true, false},
		{"implicit http rejected", "http://linstor-controller:3370", false, true},
		{"userinfo rejected", "https://reader:secret@linstor.example", false, true},
		{"query rejected", "https://linstor.example?target=other", false, true},
		{"file rejected", "file:///var/run/linstor", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HIGHLAND_ADMIN_USER", "admin")
			t.Setenv("HIGHLAND_ADMIN_PASSWORD", "secret")
			t.Setenv("HIGHLAND_LINSTOR_CONTROLLER_URL", tc.url)
			if tc.allowHTTP {
				t.Setenv("HIGHLAND_LINSTOR_ALLOW_HTTP", "true")
			} else {
				t.Setenv("HIGHLAND_LINSTOR_ALLOW_HTTP", "false")
			}
			_, err := LoadFromEnv()
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v wantError=%t", err, tc.wantError)
			}
		})
	}
}
