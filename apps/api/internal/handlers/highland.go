package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/highland-io/highland/apps/api/internal/audit"
	"github.com/highland-io/highland/apps/api/internal/auth"
	"github.com/highland-io/highland/apps/api/internal/benchmark"
	"github.com/highland-io/highland/apps/api/internal/metrics"
	"github.com/highland-io/highland/apps/api/internal/middleware"
	"k8s.io/client-go/kubernetes"
)

// HighlandAPI serves native Highland endpoints (not Longhorn proxy).
type HighlandAPI struct {
	Audit             *audit.Store
	Metrics           *metrics.Scraper
	Benchmarks        *benchmark.Store
	Users             *auth.UserStore
	Version           string
	ManagerURL        string
	K8s               kubernetes.Interface
	LonghornNamespace string
	SessionBackend    string
	BenchmarkMode     string
}

// ListAudit GET /api/v1/audit
func (h *HighlandAPI) ListAudit(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": h.Audit.List(limit),
	})
}

// ListUsers GET /api/v1/users
func (h *HighlandAPI) ListUsers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"data": h.Users.ListPublic(),
	})
}

// CreateUser POST /api/v1/users
func (h *HighlandAPI) CreateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if err := h.Users.Create(body.Username, body.Password, auth.ParseRole(body.Role)); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if h.Audit != nil {
		user, _ := middleware.UserFromContext(r.Context())
		h.Audit.Append(audit.Event{
			Username: user.Username, Role: string(user.Role),
			Action: "user_create", Target: body.Username, Method: r.Method, Path: r.URL.Path, Result: "ok", SourceIP: r.RemoteAddr,
		})
	}
	writeJSON(w, http.StatusCreated, map[string]any{"username": body.Username, "role": auth.ParseRole(body.Role)})
}

// UpdateUser PUT /api/v1/users/{username}
func (h *HighlandAPI) UpdateUser(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	var body struct {
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if err := h.Users.Update(username, body.Password, auth.ParseRole(body.Role)); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// DeleteUser DELETE /api/v1/users/{username}
func (h *HighlandAPI) DeleteUser(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	if err := h.Users.Delete(username); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// Compatibility GET /api/v1/compatibility
func (h *HighlandAPI) Compatibility(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"highlandVersion": h.Version,
		"managerUrl":      h.ManagerURL,
		"longhornSupport": []string{"1.12.x", "1.11.x"},
		"mode":            "bolt-on",
		"notes":           "Point HIGHLAND_MANAGER_URL at longhorn-backend when on k3s/kind",
	})
}

// HealthNarrative GET /api/v1/health
func (h *HighlandAPI) HealthNarrative(w http.ResponseWriter, r *http.Request) {
	// Offline-friendly narrative; live cluster would enrich from LH collections.
	items := []map[string]any{
		{"severity": "info", "code": "proxy", "message": "Longhorn manager access is via authenticated Highland BFF only"},
		{"severity": "ok", "code": "auth", "message": "Local username/password login is primary; OIDC is optional and not required for admin access"},
	}
	if h.Metrics != nil {
		if err := h.Metrics.LastError(); err != "" {
			items = append(items, map[string]any{
				"severity": "warning", "code": "metrics", "message": "metrics scrape: " + err,
			})
		} else {
			items = append(items, map[string]any{
				"severity": "ok", "code": "metrics", "message": "metrics scraper active",
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "generatedAt": time.Now().UTC()})
}

// Preflight GET /api/v1/preflight — static checks Highland can do without cluster admin.
func (h *HighlandAPI) Preflight(w http.ResponseWriter, r *http.Request) {
	checks := []map[string]any{
		{"id": "bff-health", "name": "Highland API process", "status": "pass", "detail": "API is serving"},
		{"id": "manager-url", "name": "Manager URL configured", "status": "pass", "detail": h.ManagerURL},
		{"id": "session-store", "name": "Session store", "status": "pass", "detail": "in-memory (use Redis for HA on multi-replica)"},
		{"id": "cluster-iscsi", "name": "Node iSCSI (cluster)", "status": "skip", "detail": "Validate on k3s nodes: iscsiadm / open-iscsi"},
		{"id": "cluster-multipath", "name": "Multipath blacklist", "status": "skip", "detail": "Validate on k3s per Longhorn docs"},
		{"id": "networkpolicy", "name": "NetworkPolicy to manager", "status": "skip", "detail": "Apply chart networkPolicy when installing on cluster"},
	}
	writeJSON(w, http.StatusOK, map[string]any{"checks": checks})
}

// VolumeMetrics GET /api/v1/volumes/{name}/metrics
func (h *HighlandAPI) VolumeMetrics(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if h.Metrics == nil {
		writeJSON(w, http.StatusOK, map[string]any{"series": []any{}, "note": "metrics disabled"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"volume":      name,
		"series":      h.Metrics.Snapshot(name),
		"scrapeError": h.Metrics.LastError(),
	})
}

// AllMetrics GET /api/v1/metrics
func (h *HighlandAPI) AllMetrics(w http.ResponseWriter, r *http.Request) {
	if h.Metrics == nil {
		writeJSON(w, http.StatusOK, map[string]any{"series": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"series":      h.Metrics.Snapshot(""),
		"scrapeError": h.Metrics.LastError(),
	})
}

// ListBenchmarks GET /api/v1/benchmarks
func (h *HighlandAPI) ListBenchmarks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": h.Benchmarks.List()})
}

// CreateBenchmark POST /api/v1/benchmarks
func (h *HighlandAPI) CreateBenchmark(w http.ResponseWriter, r *http.Request) {
	user, _ := middleware.UserFromContext(r.Context())
	var body benchmark.Benchmark
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	b, err := h.Benchmarks.Create(body)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if h.Audit != nil {
		h.Audit.Append(audit.Event{
			Username: user.Username,
			Role:     string(user.Role),
			Action:   "benchmark_create",
			Target:   b.Name,
			Method:   r.Method,
			Path:     r.URL.Path,
			Result:   "ok",
			SourceIP: r.RemoteAddr,
		})
	}
	writeJSON(w, http.StatusCreated, b)
}

// GetBenchmark GET /api/v1/benchmarks/{name}
func (h *HighlandAPI) GetBenchmark(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	b, ok := h.Benchmarks.Get(name)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// DeleteBenchmark DELETE /api/v1/benchmarks/{name}
func (h *HighlandAPI) DeleteBenchmark(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !h.Benchmarks.Delete(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// GetOIDCConfig GET /api/v1/auth/oidc-config — admin-only public-safe SSO settings.
func (a *API) GetOIDCConfig(w http.ResponseWriter, r *http.Request) {
	if a.OIDCRuntime == nil {
		writeJSON(w, http.StatusOK, auth.PublicOIDCConfig{
			RoleClaim: a.Cfg.OIDCRoleClaim,
		})
		return
	}
	writeJSON(w, http.StatusOK, a.OIDCRuntime.Public())
}

// PutOIDCConfig PUT /api/v1/auth/oidc-config — admin updates runtime SSO; re-inits provider if possible.
func (a *API) PutOIDCConfig(w http.ResponseWriter, r *http.Request) {
	if a.OIDCRuntime == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "oidc runtime not available"})
		return
	}
	var body auth.UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	pub, err := a.OIDCRuntime.Update(r.Context(), body)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, pub)
}

// OIDCStart GET /auth/oidc/start — redirects to IdP (real OIDC).
func (a *API) OIDCStart(w http.ResponseWriter, r *http.Request) {
	prov := a.oidcProvider()
	if prov == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]any{
			"error":   "oidc_not_configured",
			"message": "Configure issuer/client via Admin → SSO or Helm Secret+values. Local admin login still works without OIDC. HIGHLAND_OIDC_MOCK=1 for offline demo.",
		})
		return
	}
	url, state, err := prov.AuthCodeURL()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	auth.SetOIDCStateCookie(w, "highland_oidc_state", state, a.Cfg.CookieSecure)
	http.Redirect(w, r, url, http.StatusFound)
}

// OIDCCallback GET /auth/oidc/callback
func (a *API) OIDCCallback(w http.ResponseWriter, r *http.Request) {
	prov := a.oidcProvider()
	if prov == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "oidc_not_configured"})
		return
	}
	stateCookie, err := r.Cookie("highland_oidc_state")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing state cookie"})
		return
	}
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if state == "" || code == "" || state != stateCookie.Value {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid state or code"})
		return
	}
	auth.ClearOIDCStateCookie(w, "highland_oidc_state")
	sid, user, err := prov.HandleCallback(r.Context(), state, code)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     a.Cfg.CookieName,
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.Cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(a.Cfg.SessionTTL.Seconds()),
	})
	// Prefer browser redirect to app root after IdP login
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "provider": "oidc"})
}

// OIDCMockLogin POST /auth/oidc/mock — offline demo of OIDC role mapping.
func (a *API) OIDCMockLogin(w http.ResponseWriter, r *http.Request) {
	if !a.Cfg.OIDCMock {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "oidc mock disabled"})
		return
	}
	var body struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if body.Email == "" {
		body.Email = "oidc-user@example.com"
	}
	role := auth.ParseRole(body.Role)
	if body.Role == "" {
		role = auth.RoleOperator
	}
	user := auth.User{Username: body.Email, Role: role}
	id, err := a.Store.Create(user)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "session failed"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     a.Cfg.CookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.Cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(a.Cfg.SessionTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "provider": "oidc-mock"})
}

// DashboardAggregate GET /api/v1/dashboard — thin wrapper note; UI also uses LH proxy.
func (h *HighlandAPI) DashboardAggregate(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"source": "highland",
		"hint":   "UI merges this with GET /api/v1/lh/dashboard and collections",
		"metricsTopN": func() any {
			if h.Metrics == nil {
				return []any{}
			}
			return h.Metrics.Snapshot("")
		}(),
	})
}

// CapacityPlanning GET /api/v1/capacity — synthetic planning from metrics labels when present.
func (h *HighlandAPI) CapacityPlanning(w http.ResponseWriter, r *http.Request) {
	series := []metrics.Series{}
	if h.Metrics != nil {
		series = h.Metrics.Snapshot("")
	}
	var used, total float64
	for _, s := range series {
		if strings.Contains(s.Name, "capacity") || strings.Contains(s.Name, "storage") {
			if len(s.Points) > 0 {
				// last point
				v := s.Points[len(s.Points)-1].V
				if strings.Contains(s.Name, "used") || strings.Contains(s.Name, "actual") {
					used += v
				}
				if strings.Contains(s.Name, "maximum") || strings.Contains(s.Name, "capacity") {
					total += v
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"usedBytes":   used,
		"totalBytes":  total,
		"note":        "Offline: values come from scraped /metrics when available; on k3s scrape longhorn manager",
		"seriesCount": len(series),
	})
}
