package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/highland-io/highland/apps/api/internal/audit"
	"github.com/highland-io/highland/apps/api/internal/auth"
	"github.com/highland-io/highland/apps/api/internal/benchmark"
	"github.com/highland-io/highland/apps/api/internal/config"
	"github.com/highland-io/highland/apps/api/internal/longhorn"
	"github.com/highland-io/highland/apps/api/internal/metrics"
	mw "github.com/highland-io/highland/apps/api/internal/middleware"
	"github.com/highland-io/highland/apps/api/internal/ratelimit"
	"github.com/highland-io/highland/apps/api/internal/watch"
	"k8s.io/client-go/kubernetes"
)

// Deps bundles runtime dependencies for the router.
type Deps struct {
	Cfg         *config.Config
	Auth        *auth.Authenticator
	OIDC        *auth.OIDCProvider // optional static provider (tests / legacy)
	OIDCRuntime *auth.OIDCRuntime  // runtime-configurable enterprise SSO
	Proxy       *longhorn.Proxy
	Stream      *longhorn.StreamProxy
	Audit       *audit.Store
	Metrics     *metrics.Scraper
	Benchmarks  *benchmark.Store
	// Optional cluster/runtime facts for the status page.
	K8s               kubernetes.Interface
	LonghornNamespace string
	SessionBackend    string
	BenchmarkMode     string
	// SessionSecret is the raw HMAC key used to sign session tokens; passed so
	// CSRF tokens share the exact same key (Cfg.SessionSecret is empty on the
	// ephemeral-secret path).
	SessionSecret []byte
	// WatchHub streams Longhorn change events over SSE; nil when no cluster.
	WatchHub *watch.Hub
}

// NewRouter builds the Highland API HTTP router.
func NewRouter(d Deps) http.Handler {
	oidcProv := d.OIDC
	if oidcProv == nil && d.OIDCRuntime != nil {
		oidcProv = d.OIDCRuntime.Provider()
	}
	limiter := ratelimit.New(ratelimit.Options{
		Enabled:         d.Cfg.LoginRateLimitEnabled,
		MaxFailuresUser: d.Cfg.LoginMaxFailuresUser,
		MaxFailuresIP:   d.Cfg.LoginMaxFailuresIP,
		LockoutBase:     d.Cfg.LoginLockoutBase,
		LockoutMax:      d.Cfg.LoginLockoutMax,
		FailureWindow:   d.Cfg.LoginFailureWindow,
		MaxEntries:      d.Cfg.LoginMaxEntries,
	})
	api := &API{
		Cfg:         d.Cfg,
		Auth:        d.Auth,
		Store:       d.Auth.Store(),
		Users:       d.Auth.Users(),
		OIDC:        oidcProv,
		OIDCRuntime: d.OIDCRuntime,
		Limiter:     limiter,
		Started:     time.Now(),
	}
	hapi := &HighlandAPI{
		Audit:             d.Audit,
		Metrics:           d.Metrics,
		Benchmarks:        d.Benchmarks,
		Users:             d.Auth.Users(),
		Version:           d.Cfg.Version,
		ManagerURL:        d.Cfg.ManagerURL,
		K8s:               d.K8s,
		LonghornNamespace: d.LonghornNamespace,
		SessionBackend:    d.SessionBackend,
		BenchmarkMode:     d.BenchmarkMode,
	}

	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	// Trusted-proxy-aware client IP (replaces chi's spoofable RealIP) so the
	// login limiter and audit source IP cannot be forged via forwarding headers.
	r.Use(mw.ClientIP(d.Cfg.TrustedProxies))
	r.Use(chimw.Recoverer)
	r.Use(mw.SecurityHeaders(d.Cfg.CookieSecure))
	r.Use(mw.CORS(d.Cfg.AllowedOrigins))

	r.Get("/healthz", api.Healthz)
	r.Get("/readyz", api.Readyz)

	r.Route("/auth", func(r chi.Router) {
		r.Get("/providers", api.Providers)
		r.Post("/login", api.Login)
		r.Post("/logout", api.Logout)
		r.Get("/oidc/start", api.OIDCStart)
		r.Get("/oidc/callback", api.OIDCCallback)
		r.Post("/oidc/mock", api.OIDCMockLogin)
		r.Group(func(r chi.Router) {
			r.Use(mw.SessionAuth(api.Store, d.Cfg.CookieName))
			r.Get("/me", api.Me)
		})
	})

	r.Group(func(r chi.Router) {
		r.Use(mw.SessionAuth(api.Store, d.Cfg.CookieName))
		if d.Cfg.CSRFEnabled {
			r.Use(mw.CSRF(d.SessionSecret, d.Cfg.CSRFCookieName, d.Cfg.CookieSecure, d.Cfg.SessionTTL))
		}
		r.Use(mw.RequireRole(d.Audit))

		// Streaming path for large backing-image upload/download (no full buffer)
		if d.Stream != nil {
			r.HandleFunc("/api/v1/lh/backingimages/*", func(w http.ResponseWriter, req *http.Request) {
				if req.Method == http.MethodPost || req.Method == http.MethodPut ||
					strings.Contains(req.URL.Path, "upload") || strings.Contains(req.URL.Path, "download") {
					d.Stream.ServeHTTP(w, req)
					return
				}
				d.Proxy.ServeHTTP(w, req)
			})
		}

		// Real Longhorn (unlike the bundled mock) has no /v1/dashboard endpoint.
		// The UI treats this as an optional overlay and computes the dashboard from
		// collections, so serve an empty 200 here instead of proxying a 404 upstream.
		r.Get("/api/v1/lh/dashboard", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		})

		r.Handle("/api/v1/lh/*", d.Proxy)
		r.Handle("/api/v1/lh", d.Proxy)

		r.Get("/api/v1/audit", hapi.ListAudit)
		r.Get("/api/v1/users", hapi.ListUsers)
		r.Post("/api/v1/users", hapi.CreateUser)
		r.Put("/api/v1/users/{username}", hapi.UpdateUser)
		r.Delete("/api/v1/users/{username}", hapi.DeleteUser)
		r.Get("/api/v1/auth/oidc-config", api.GetOIDCConfig)
		r.Put("/api/v1/auth/oidc-config", api.PutOIDCConfig)
		r.Get("/api/v1/compatibility", hapi.Compatibility)
		r.Get("/api/v1/status", hapi.Status)
		r.Post("/api/v1/backup-credential", hapi.CreateBackupCredential)
		r.Get("/api/v1/health", hapi.HealthNarrative)
		r.Get("/api/v1/preflight", hapi.Preflight)
		r.Get("/api/v1/dashboard", hapi.DashboardAggregate)
		r.Get("/api/v1/capacity", hapi.CapacityPlanning)
		r.Get("/api/v1/metrics", hapi.AllMetrics)
		r.Get("/api/v1/volumes/{name}/metrics", hapi.VolumeMetrics)
		r.Get("/api/v1/benchmarks", hapi.ListBenchmarks)
		r.Post("/api/v1/benchmarks", hapi.CreateBenchmark)
		r.Get("/api/v1/benchmarks/{name}", hapi.GetBenchmark)
		r.Delete("/api/v1/benchmarks/{name}", hapi.DeleteBenchmark)

		// Real-time change stream (SSE). GET is safe under CSRF; auth via cookie.
		if d.WatchHub != nil {
			r.Get("/api/v1/events/stream", d.WatchHub.ServeSSE)
		}
	})

	return r
}
