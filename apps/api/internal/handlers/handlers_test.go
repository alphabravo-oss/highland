package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/highland-io/highland/apps/api/internal/audit"
	"github.com/highland-io/highland/apps/api/internal/auth"
	"github.com/highland-io/highland/apps/api/internal/benchmark"
	"github.com/highland-io/highland/apps/api/internal/config"
	"github.com/highland-io/highland/apps/api/internal/handlers"
	"github.com/highland-io/highland/apps/api/internal/longhorn"
	"github.com/highland-io/highland/apps/api/internal/metrics"
)

func testDeps(t *testing.T, managerURL string) handlers.Deps {
	t.Helper()
	_ = os.Setenv("HIGHLAND_DEV_ROLES", "1")
	cfg := &config.Config{
		ListenAddr:        ":0",
		ManagerURL:        managerURL,
		BootstrapUsername: "admin",
		BootstrapPassword: "highland",
		SessionTTL:        time.Hour,
		CookieName:        "highland_session",
		AuthMode:          config.AuthModeLocalOIDC,
		LocalAlways:       true,
		OIDCMock:          true, // tests that need mock; production default is false
		Version:           "0.1.0-test",
		AllowedOrigins:    []string{"http://localhost:5173"},
	}
	users := auth.NewUserStoreFromEnv(cfg.BootstrapUsername, cfg.BootstrapPassword)
	store := auth.NewStore(cfg.SessionTTL)
	authenticator := auth.NewAuthenticator(users, store)
	proxy, err := longhorn.NewProxy(managerURL)
	if err != nil {
		t.Fatalf("proxy: %v", err)
	}
	oidcRuntime := auth.NewOIDCRuntime(authenticator, "")
	return handlers.Deps{
		Cfg:         cfg,
		Auth:        authenticator,
		OIDCRuntime: oidcRuntime,
		Proxy:       proxy,
		Audit:       audit.NewStore(100, ""),
		Metrics:     metrics.NewScraper(managerURL, time.Hour, 10), // no auto poll in tests unless started
		Benchmarks:  benchmark.NewStore(nil),
	}
}

func newTestServer(t *testing.T, managerURL string) http.Handler {
	return handlers.NewRouter(testDeps(t, managerURL))
}

func loginCookie(t *testing.T, h http.Handler, user, pass string) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"username":"`+user+`","password":"`+pass+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("login %s: %d %s", user, rr.Code, rr.Body.String())
	}
	for _, c := range rr.Result().Cookies() {
		if c.Name == "highland_session" {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func TestHealthz(t *testing.T) {
	h := newTestServer(t, "http://manager.example:9500")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
}

func TestReadyz(t *testing.T) {
	// Reachable manager -> ready (200). Use a stub server standing in for the manager.
	mgr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer mgr.Close()

	h := newTestServer(t, mgr.URL)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("reachable manager: status = %d, body = %s", rr.Code, rr.Body.String())
	}
}

func TestReadyzManagerUnreachable(t *testing.T) {
	// Unreachable manager -> not ready (503). Real reachability probe must fail closed.
	h := newTestServer(t, "http://127.0.0.1:1/") // nothing listens on port 1
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("unreachable manager: status = %d, want 503", rr.Code)
	}
}

func TestAuthMeUnauthorized(t *testing.T) {
	h := newTestServer(t, "http://manager.example:9500")
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rr.Code)
	}
}

func TestProtectedProxyUnauthorized(t *testing.T) {
	h := newTestServer(t, "http://manager.example:9500")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/lh/volumes", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rr.Code)
	}
}

func TestLoginAndMe(t *testing.T) {
	h := newTestServer(t, "http://manager.example:9500")
	c := loginCookie(t, h, "admin", "highland")
	meReq := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	meReq.AddCookie(c)
	meRR := httptest.NewRecorder()
	h.ServeHTTP(meRR, meReq)
	if meRR.Code != http.StatusOK {
		t.Fatalf("me %d", meRR.Code)
	}
	var body map[string]any
	_ = json.NewDecoder(meRR.Body).Decode(&body)
	user := body["user"].(map[string]any)
	if user["role"] != "admin" {
		t.Fatalf("role %v", user["role"])
	}
}

func TestProvidersAdvertisesLocalWithoutOIDC(t *testing.T) {
	h := newTestServer(t, "http://manager.example:9500")
	req := httptest.NewRequest(http.MethodGet, "/auth/providers", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("providers %d", rr.Code)
	}
	var body map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&body)
	if body["local"] != true {
		t.Fatalf("local should be true: %v", body)
	}
}

func TestLocalLoginWorksWithOIDCMockDisabled(t *testing.T) {
	deps := testDeps(t, "http://manager.example:9500")
	deps.Cfg.OIDCMock = false
	deps.Cfg.OIDCIssuer = ""
	deps.Cfg.AuthMode = config.AuthModeLocal
	deps.Cfg.LocalAlways = true
	h := handlers.NewRouter(deps)
	// Local admin must work with zero OIDC config
	c := loginCookie(t, h, "admin", "highland")
	if c == nil || c.Value == "" {
		t.Fatal("expected local session without OIDC")
	}
	meReq := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	meReq.AddCookie(c)
	meRR := httptest.NewRecorder()
	h.ServeHTTP(meRR, meReq)
	if meRR.Code != 200 {
		t.Fatalf("me %d", meRR.Code)
	}
}

func TestViewerCannotMutate(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"type":"collection","data":[]}`))
	}))
	defer upstream.Close()
	h := newTestServer(t, upstream.URL)
	c := loginCookie(t, h, "viewer", "viewer")

	// GET allowed
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/lh/volumes", nil)
	getReq.AddCookie(c)
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("viewer GET %d %s", getRR.Code, getRR.Body.String())
	}

	// POST denied
	postReq := httptest.NewRequest(http.MethodPost, "/api/v1/lh/volumes", strings.NewReader(`{"name":"x"}`))
	postReq.Header.Set("Content-Type", "application/json")
	postReq.AddCookie(c)
	postRR := httptest.NewRecorder()
	h.ServeHTTP(postRR, postReq)
	if postRR.Code != http.StatusForbidden {
		t.Fatalf("viewer POST want 403 got %d %s", postRR.Code, postRR.Body.String())
	}
}

func TestOperatorCannotChangeSettings(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()
	h := newTestServer(t, upstream.URL)
	c := loginCookie(t, h, "operator", "operator")
	req := httptest.NewRequest(http.MethodPut, "/api/v1/lh/settings/default-replica-count", strings.NewReader(`{"value":"5"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(c)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("operator settings PUT want 403 got %d", rr.Code)
	}
}

func TestBenchmarksAndAudit(t *testing.T) {
	h := newTestServer(t, "http://manager.example:9500")
	c := loginCookie(t, h, "admin", "highland")

	create := httptest.NewRequest(http.MethodPost, "/api/v1/benchmarks", strings.NewReader(`{"profile":"quick","type":"Disk","nodeName":"node-1"}`))
	create.Header.Set("Content-Type", "application/json")
	create.AddCookie(c)
	crr := httptest.NewRecorder()
	h.ServeHTTP(crr, create)
	if crr.Code != http.StatusCreated {
		t.Fatalf("create bench %d %s", crr.Code, crr.Body.String())
	}

	list := httptest.NewRequest(http.MethodGet, "/api/v1/benchmarks", nil)
	list.AddCookie(c)
	lrr := httptest.NewRecorder()
	h.ServeHTTP(lrr, list)
	if lrr.Code != 200 {
		t.Fatalf("list %d", lrr.Code)
	}

	// Wait for simulation
	time.Sleep(1200 * time.Millisecond)
	var created map[string]any
	_ = json.NewDecoder(crr.Body).Decode(&created)
	name, _ := created["name"].(string)
	get := httptest.NewRequest(http.MethodGet, "/api/v1/benchmarks/"+name, nil)
	get.AddCookie(c)
	grr := httptest.NewRecorder()
	h.ServeHTTP(grr, get)
	var got map[string]any
	_ = json.NewDecoder(grr.Body).Decode(&got)
	if got["phase"] != "Succeeded" {
		t.Fatalf("phase %v", got["phase"])
	}

	auditReq := httptest.NewRequest(http.MethodGet, "/api/v1/audit", nil)
	auditReq.AddCookie(c)
	arr := httptest.NewRecorder()
	h.ServeHTTP(arr, auditReq)
	if arr.Code != 200 {
		t.Fatalf("audit %d", arr.Code)
	}
	var auditBody map[string]any
	_ = json.NewDecoder(arr.Body).Decode(&auditBody)
	data, _ := auditBody["data"].([]any)
	if len(data) == 0 {
		t.Fatal("expected audit events from benchmark create")
	}
}

func TestViewerCannotReadAudit(t *testing.T) {
	h := newTestServer(t, "http://manager.example:9500")
	c := loginCookie(t, h, "viewer", "viewer")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit", nil)
	req.AddCookie(c)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("want 403 got %d", rr.Code)
	}
}

func TestAdminUserCRUD(t *testing.T) {
	h := newTestServer(t, "http://manager.example:9500")
	c := loginCookie(t, h, "admin", "highland")
	create := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(`{"username":"alice","password":"alice-pass","role":"operator"}`))
	create.Header.Set("Content-Type", "application/json")
	create.AddCookie(c)
	crr := httptest.NewRecorder()
	h.ServeHTTP(crr, create)
	if crr.Code != http.StatusCreated {
		t.Fatalf("create %d %s", crr.Code, crr.Body.String())
	}
	// new user can login
	c2 := loginCookie(t, h, "alice", "alice-pass")
	if c2.Value == "" {
		t.Fatal("alice login")
	}
	// delete
	del := httptest.NewRequest(http.MethodDelete, "/api/v1/users/alice", nil)
	del.AddCookie(c)
	drr := httptest.NewRecorder()
	h.ServeHTTP(drr, del)
	if drr.Code != 200 {
		t.Fatalf("delete %d", drr.Code)
	}
}

func TestOIDCMockLogin(t *testing.T) {
	h := newTestServer(t, "http://manager.example:9500")
	req := httptest.NewRequest(http.MethodPost, "/auth/oidc/mock", strings.NewReader(`{"email":"alice@corp","role":"operator"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("oidc mock %d %s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&body)
	user := body["user"].(map[string]any)
	if user["role"] != "operator" {
		t.Fatalf("role %v", user["role"])
	}
}

func TestOIDCConfigAdminGetPut(t *testing.T) {
	deps := testDeps(t, "http://manager.example:9500")
	h := handlers.NewRouter(deps)
	c := loginCookie(t, h, "admin", "highland")

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc-config", nil)
	getReq.AddCookie(c)
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("GET oidc-config %d %s", getRR.Code, getRR.Body.String())
	}
	var before map[string]any
	_ = json.NewDecoder(getRR.Body).Decode(&before)
	if before["secretSet"] != false {
		t.Fatalf("expected secretSet false initially: %v", before)
	}
	// secret must never appear in GET body
	if _, ok := before["clientSecret"]; ok {
		t.Fatal("clientSecret must not be returned")
	}

	// PUT stores settings even if discovery fails (no real IdP in unit tests).
	putBody := `{"enabled":true,"issuerURL":"https://idp.example/realms/highland","clientID":"highland-ui","clientSecret":"s3cret","redirectURL":"http://localhost:8080/auth/oidc/callback","roleClaim":"highland_role"}`
	putReq := httptest.NewRequest(http.MethodPut, "/api/v1/auth/oidc-config", strings.NewReader(putBody))
	putReq.Header.Set("Content-Type", "application/json")
	putReq.AddCookie(c)
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, putReq)
	if putRR.Code != http.StatusOK {
		t.Fatalf("PUT oidc-config %d %s", putRR.Code, putRR.Body.String())
	}
	var after map[string]any
	_ = json.NewDecoder(putRR.Body).Decode(&after)
	if after["enabled"] != true {
		t.Fatalf("enabled: %v", after)
	}
	if after["issuerURL"] != "https://idp.example/realms/highland" {
		t.Fatalf("issuerURL: %v", after)
	}
	if after["clientID"] != "highland-ui" {
		t.Fatalf("clientID: %v", after)
	}
	if after["secretSet"] != true {
		t.Fatalf("secretSet should be true after PUT: %v", after)
	}
	if _, ok := after["clientSecret"]; ok {
		t.Fatal("clientSecret must not be returned from PUT")
	}

	// Empty secret on subsequent PUT leaves secret set.
	put2 := httptest.NewRequest(http.MethodPut, "/api/v1/auth/oidc-config", strings.NewReader(
		`{"enabled":true,"issuerURL":"https://idp.example/realms/highland","clientID":"highland-ui","clientSecret":"","redirectURL":"http://localhost:8080/auth/oidc/callback","roleClaim":"groups"}`,
	))
	put2.Header.Set("Content-Type", "application/json")
	put2.AddCookie(c)
	put2RR := httptest.NewRecorder()
	h.ServeHTTP(put2RR, put2)
	if put2RR.Code != http.StatusOK {
		t.Fatalf("PUT2 %d %s", put2RR.Code, put2RR.Body.String())
	}
	var after2 map[string]any
	_ = json.NewDecoder(put2RR.Body).Decode(&after2)
	if after2["secretSet"] != true {
		t.Fatalf("empty secret must keep existing: %v", after2)
	}
	if after2["roleClaim"] != "groups" {
		t.Fatalf("roleClaim: %v", after2)
	}

	// /auth/providers should reflect runtime enabled state.
	provReq := httptest.NewRequest(http.MethodGet, "/auth/providers", nil)
	provRR := httptest.NewRecorder()
	h.ServeHTTP(provRR, provReq)
	if provRR.Code != 200 {
		t.Fatalf("providers %d", provRR.Code)
	}
	var prov map[string]any
	_ = json.NewDecoder(provRR.Body).Decode(&prov)
	if prov["oidc"] != true {
		t.Fatalf("providers.oidc should be true when runtime enabled: %v", prov)
	}
}

func TestOIDCConfigViewerForbidden(t *testing.T) {
	h := newTestServer(t, "http://manager.example:9500")
	c := loginCookie(t, h, "viewer", "viewer")

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc-config", nil)
	getReq.AddCookie(c)
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusForbidden {
		t.Fatalf("viewer GET want 403 got %d %s", getRR.Code, getRR.Body.String())
	}

	putReq := httptest.NewRequest(http.MethodPut, "/api/v1/auth/oidc-config", strings.NewReader(`{"enabled":false}`))
	putReq.Header.Set("Content-Type", "application/json")
	putReq.AddCookie(c)
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, putReq)
	if putRR.Code != http.StatusForbidden {
		t.Fatalf("viewer PUT want 403 got %d %s", putRR.Code, putRR.Body.String())
	}
}

func TestLonghornProxyForwardsAndRewrites(t *testing.T) {
	var receivedPath string
	var receivedCookie string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedCookie = r.Header.Get("Cookie")
		managerBase := "http://" + r.Host
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "collection",
			"data": []any{map[string]any{"id": "vol-1", "name": "pvc-db"}},
			"links": map[string]any{
				"self": managerBase + "/v1/volumes",
			},
		})
	}))
	defer upstream.Close()

	h := newTestServer(t, upstream.URL)
	c := loginCookie(t, h, "admin", "highland")
	proxyReq := httptest.NewRequest(http.MethodGet, "/api/v1/lh/volumes", nil)
	proxyReq.AddCookie(c)
	proxyRR := httptest.NewRecorder()
	h.ServeHTTP(proxyRR, proxyReq)
	if proxyRR.Code != 200 {
		t.Fatalf("proxy %d %s", proxyRR.Code, proxyRR.Body.String())
	}
	if receivedPath != "/v1/volumes" {
		t.Fatalf("path %q", receivedPath)
	}
	if receivedCookie != "" {
		t.Fatal("cookie leaked to manager")
	}
	var body map[string]any
	_ = json.NewDecoder(proxyRR.Body).Decode(&body)
	self := body["links"].(map[string]any)["self"].(string)
	if !strings.HasPrefix(self, "/api/v1/lh") {
		t.Fatalf("self %q", self)
	}
}

func TestRewritePath(t *testing.T) {
	p, err := longhorn.NewProxy("http://longhorn-backend:9500")
	if err != nil {
		t.Fatal(err)
	}
	if p.RewritePath("/api/v1/lh/volumes") != "/v1/volumes" {
		t.Fatal(p.RewritePath("/api/v1/lh/volumes"))
	}
}

func TestRewriteLinks(t *testing.T) {
	in := []byte(`{"links":{"self":"http://longhorn-backend:9500/v1/volumes"},"name":"keep"}`)
	out := longhorn.RewriteLinks(in, "http://longhorn-backend:9500", "/api/v1/lh", "/v1")
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	if m["links"].(map[string]any)["self"] != "/api/v1/lh/volumes" {
		t.Fatal(string(out))
	}
}

func TestPreflightAndHealth(t *testing.T) {
	h := newTestServer(t, "http://manager.example:9500")
	c := loginCookie(t, h, "admin", "highland")
	for _, path := range []string{"/api/v1/preflight", "/api/v1/health", "/api/v1/compatibility", "/api/v1/capacity"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(c)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Fatalf("%s => %d", path, rr.Code)
		}
	}
}
