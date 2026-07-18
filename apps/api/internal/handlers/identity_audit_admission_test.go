package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/highland-io/highland/apps/api/internal/audit"
	"github.com/highland-io/highland/apps/api/internal/auth"
	"github.com/highland-io/highland/apps/api/internal/benchmark"
	"github.com/highland-io/highland/apps/api/internal/handlers"
	"github.com/highland-io/highland/apps/api/internal/longhorn"
	"github.com/highland-io/highland/apps/api/internal/metrics"
)

// identityUsers is a thin view of auth.UserStore for list checks after blocked mutations.
func listUsernames(t *testing.T, users *auth.UserStore) map[string]bool {
	t.Helper()
	public, err := users.ListPublic(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, u := range public {
		out[u.Username] = true
	}
	return out
}

func routerWithDurableFailingAudit(t *testing.T) (http.Handler, *auth.UserStore, *audit.FailingSink) {
	t.Helper()
	deps := testDeps(t, "http://manager.example:9500")
	fail := audit.NewFailingSink(audit.ErrUnavailable)
	if !fail.Durable() {
		t.Fatal("failing sink must be durable")
	}
	deps.Audit = fail
	return handlers.NewRouter(deps), deps.Auth.Users(), fail
}

// TestIdentityUpdateFailClosedWhenDurableAuditUnavailable drives PUT
// /api/v1/users/{username} with a durable failing sink. UpdateAdmin must not
// apply (role/disabled unchanged).
func TestIdentityUpdateFailClosedWhenDurableAuditUnavailable(t *testing.T) {
	// Seed alice with a working (non-durable) memory audit first via a normal router.
	seedDeps := testDeps(t, "http://manager.example:9500")
	seed := handlers.NewRouter(seedDeps)
	admin := loginCookie(t, seed, "admin", "highland")
	create := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(
		`{"username":"alice","password":"several quiet copper forests","role":"operator"}`))
	create.Header.Set("Content-Type", "application/json")
	create.AddCookie(admin)
	crr := httptest.NewRecorder()
	seed.ServeHTTP(crr, create)
	if crr.Code != http.StatusCreated {
		t.Fatalf("seed create %d %s", crr.Code, crr.Body.String())
	}

	// Swap to durable failing audit on the same user store by rebuilding deps with injected users.
	// Simpler: build router with failing audit and recreate alice only if missing — use same Auth from seedDeps.
	fail := audit.NewFailingSink(audit.ErrUnavailable)
	proxy, err := longhorn.NewProxy("http://manager.example:9500")
	if err != nil {
		t.Fatal(err)
	}
	h := handlers.NewRouter(handlers.Deps{
		Cfg:         seedDeps.Cfg,
		Auth:        seedDeps.Auth,
		OIDCRuntime: seedDeps.OIDCRuntime,
		Proxy:       proxy,
		Audit:       fail,
		Metrics:     metrics.NewScraper("http://manager.example:9500", time.Hour, 10),
		Benchmarks:  benchmark.NewStore(nil),
	})
	admin2 := loginCookie(t, h, "admin", "highland")
	disabled := true
	body, _ := json.Marshal(map[string]any{"disabled": disabled, "role": "viewer"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/alice", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin2)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 admission failure, got %d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "required audit admission failed") {
		t.Fatalf("expected admission error message, body=%s", rr.Body.String())
	}

	// Alice must still login as operator (not disabled / not demoted).
	_ = loginCookie(t, h, "alice", "several quiet copper forests")
	// And role should remain operator: admin listing would show role; use Get via list public.
	users, err := seedDeps.Auth.Users().ListPublic(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if u.Username == "alice" {
			if u.Role != auth.RoleOperator {
				t.Fatalf("alice role mutated under audit outage: %v", u.Role)
			}
			if u.Disabled {
				t.Fatal("alice disabled under audit outage")
			}
			return
		}
	}
	t.Fatal("alice missing after blocked update")
}

// TestIdentityDeleteFailClosedWhenDurableAuditUnavailable drives DELETE
// /api/v1/users/{username}; user must still exist after 503.
func TestIdentityDeleteFailClosedWhenDurableAuditUnavailable(t *testing.T) {
	seedDeps := testDeps(t, "http://manager.example:9500")
	seed := handlers.NewRouter(seedDeps)
	admin := loginCookie(t, seed, "admin", "highland")
	create := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(
		`{"username":"bob","password":"several quiet copper forests","role":"viewer"}`))
	create.Header.Set("Content-Type", "application/json")
	create.AddCookie(admin)
	crr := httptest.NewRecorder()
	seed.ServeHTTP(crr, create)
	if crr.Code != http.StatusCreated {
		t.Fatalf("seed create %d %s", crr.Code, crr.Body.String())
	}

	fail := audit.NewFailingSink(audit.ErrUnavailable)
	proxy, err := longhorn.NewProxy("http://manager.example:9500")
	if err != nil {
		t.Fatal(err)
	}
	h := handlers.NewRouter(handlers.Deps{
		Cfg:         seedDeps.Cfg,
		Auth:        seedDeps.Auth,
		OIDCRuntime: seedDeps.OIDCRuntime,
		Proxy:       proxy,
		Audit:       fail,
		Metrics:     metrics.NewScraper("http://manager.example:9500", time.Hour, 10),
		Benchmarks:  benchmark.NewStore(nil),
	})
	admin2 := loginCookie(t, h, "admin", "highland")
	del := httptest.NewRequest(http.MethodDelete, "/api/v1/users/bob", nil)
	del.AddCookie(admin2)
	drr := httptest.NewRecorder()
	h.ServeHTTP(drr, del)
	if drr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d %s", drr.Code, drr.Body.String())
	}
	if !strings.Contains(drr.Body.String(), "required audit admission failed") {
		t.Fatalf("body=%s", drr.Body.String())
	}
	names := listUsernames(t, seedDeps.Auth.Users())
	if !names["bob"] {
		t.Fatal("bob must still exist when delete admission fails")
	}
}

// TestIdentityCreateFailClosedWhenDurableAuditUnavailable covers create admission.
func TestIdentityCreateFailClosedWhenDurableAuditUnavailable(t *testing.T) {
	h, users, _ := routerWithDurableFailingAudit(t)
	admin := loginCookie(t, h, "admin", "highland")
	create := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(
		`{"username":"carol","password":"several quiet copper forests","role":"viewer"}`))
	create.Header.Set("Content-Type", "application/json")
	create.AddCookie(admin)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, create)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d %s", rr.Code, rr.Body.String())
	}
	if listUsernames(t, users)["carol"] {
		t.Fatal("carol must not be created when admission fails")
	}
}
