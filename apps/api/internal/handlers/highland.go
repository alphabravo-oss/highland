package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/highland-io/highland/apps/api/internal/audit"
	"github.com/highland-io/highland/apps/api/internal/auth"
	"github.com/highland-io/highland/apps/api/internal/benchmark"
	"github.com/highland-io/highland/apps/api/internal/metrics"
	"github.com/highland-io/highland/apps/api/internal/middleware"
	"github.com/highland-io/highland/apps/api/internal/policy"
	"github.com/highland-io/highland/apps/api/internal/storage"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// HighlandAPI serves native Highland endpoints (not Longhorn proxy).
type HighlandAPI struct {
	Audit                           *audit.Store
	Metrics                         *metrics.Scraper
	Benchmarks                      *benchmark.Store
	Users                           *auth.UserStore
	Version                         string
	ManagerURL                      string
	LonghornEnabled                 bool
	K8s                             kubernetes.Interface
	LonghornNamespace               string
	RookCephNamespace               string
	RookCephCredentialRevealEnabled bool
	RookCephDashboardAdminUsername  string
	RookCephDashboardAdminSecret    string
	SessionBackend                  string
	BenchmarkMode                   string
	Storage                         *storage.HTTPAPI
	Policy                          interface{ Snapshot() policy.Snapshot }
}

// RevealCephDashboardCredential POST /api/v1/admin/providers/rook-ceph/dashboard-credential/reveal
// returns Rook's generated dashboard administrator credential only after an
// explicit admin action. The fixed, namespace-scoped Secret read is audited and
// the response is never cacheable.
func (h *HighlandAPI) RevealCephDashboardCredential(w http.ResponseWriter, r *http.Request) {
	user, _ := middleware.UserFromContext(r.Context())
	result := "error"
	message := ""
	defer func() {
		if h.Audit != nil {
			h.Audit.Append(audit.Event{
				Username: user.Username, Role: string(user.Role),
				Action: "ceph_dashboard_credential_reveal", Target: "rook-ceph/dashboard-admin",
				Method: r.Method, Path: r.URL.Path, Result: result, SourceIP: r.RemoteAddr,
				ProviderID: "rook-ceph", ProviderKind: "rook-ceph", Message: message,
			})
		}
	}()
	if user.Role != auth.RoleAdmin {
		message = "admin required"
		writeJSON(w, http.StatusForbidden, map[string]string{"error": message})
		return
	}
	if !h.RookCephCredentialRevealEnabled {
		message = "credential reveal is disabled"
		writeJSON(w, http.StatusNotFound, map[string]string{"error": message})
		return
	}
	if h.K8s == nil {
		message = "cluster access unavailable"
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": message})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	secret, err := h.K8s.CoreV1().Secrets(h.RookCephNamespace).Get(ctx, h.RookCephDashboardAdminSecret, metav1.GetOptions{})
	if err != nil {
		message = "dashboard credential unavailable"
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": message})
		return
	}
	password := string(secret.Data["password"])
	if password == "" || len(password) > 4096 {
		message = "dashboard credential is invalid"
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": message})
		return
	}
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	result = "ok"
	message = "administrator revealed the Ceph Dashboard credential"
	writeJSON(w, http.StatusOK, map[string]string{
		"username": h.RookCephDashboardAdminUsername,
		"password": password,
	})
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
	users, err := h.Users.ListPublic(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "identity service unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": users,
	})
}

// CreateUser POST /api/v1/users
func (h *HighlandAPI) CreateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if err := h.Users.Create(r.Context(), auth.CreateUserRequest{
		Username: body.Username, Email: body.Email, Password: body.Password, Role: auth.ParseRole(body.Role),
	}); err != nil {
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
	writeJSON(w, http.StatusCreated, map[string]any{"username": body.Username, "email": body.Email, "role": auth.ParseRole(body.Role)})
}

// UpdateUser PUT /api/v1/users/{username}
func (h *HighlandAPI) UpdateUser(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	principal, _ := middleware.UserFromContext(r.Context())
	if principal.Username == username {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "manage your own email, password, and MFA from My account; another administrator must change your role or account status"})
		return
	}
	var body struct {
		Email    *string `json:"email"`
		Password string  `json:"password"`
		Role     *string `json:"role"`
		Disabled *bool   `json:"disabled"`
		ResetMFA bool    `json:"resetMfa"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	var role *auth.Role
	if body.Role != nil {
		parsed := auth.ParseRole(*body.Role)
		role = &parsed
	}
	if err := h.Users.UpdateAdmin(r.Context(), username, auth.AdminUserUpdate{
		Email: body.Email, Password: body.Password, Role: role, Disabled: body.Disabled, ResetMFA: body.ResetMFA,
	}); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// DeleteUser DELETE /api/v1/users/{username}
func (h *HighlandAPI) DeleteUser(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	principal, _ := middleware.UserFromContext(r.Context())
	if principal.Username == username {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "you cannot delete your own signed-in account"})
		return
	}
	if err := h.Users.Delete(r.Context(), username); err != nil {
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
		"enabled":         h.LonghornEnabled,
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
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be between 1 and 200"})
			return
		}
		limit = parsed
	}
	offset, err := storage.DecodePageOffset(r.URL.Query().Get("continue"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	all := h.Benchmarks.List()
	if offset > len(all) {
		offset = len(all)
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	data := append([]benchmark.Benchmark(nil), all[offset:end]...)
	if r.URL.Query().Get("fields") == "summary" {
		for index := range data {
			data[index].FioCmd = ""
		}
	}
	page := storage.PageMeta{Limit: limit, Total: len(all)}
	if end < len(all) {
		page.Continue = storage.EncodePageOffset(end)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": data,
		"page": page,
		"meta": map[string]any{
			"observedAt": time.Now().UTC(), "stale": false, "partial": false,
			"benchmarkMode": orUnknown(h.BenchmarkMode), "requestId": chimw.GetReqID(r.Context()),
		},
	})
}

// CreateBenchmark POST /api/v1/benchmarks
func (h *HighlandAPI) CreateBenchmark(w http.ResponseWriter, r *http.Request) {
	user, _ := middleware.UserFromContext(r.Context())
	if h.BenchmarkMode != "kubernetes-job" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "real benchmark execution is disabled; enable benchmark.kubernetesJobEnabled to run fio Jobs",
			"mode":  orUnknown(h.BenchmarkMode),
		})
		return
	}
	var body benchmark.Benchmark
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if body.RetainFailedPVC {
		if user.Role != auth.RoleAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required to retain a failed benchmark PVC"})
			return
		}
		if body.RetainConfirmation != "RETAIN FAILED PVC" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "explicit retain confirmation is required"})
			return
		}
	}
	b, err := h.Benchmarks.Create(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
	user := auth.User{Username: body.Email, Email: body.Email, Role: role, AuthSource: "oidc"}
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
