package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// AuthMode controls which login methods are offered.
//
//	local       — username/password only (default; no IdP required)
//	oidc        — OIDC only (still allows bootstrap recovery if LocalAlways)
//	local+oidc  — both local form and OIDC
type AuthMode string

const (
	AuthModeLocal     AuthMode = "local"
	AuthModeOIDC      AuthMode = "oidc"
	AuthModeLocalOIDC AuthMode = "local+oidc"
)

// Config holds runtime settings for the Highland API (BFF).
type Config struct {
	ListenAddr        string
	ManagerURL        string
	BootstrapUsername string
	BootstrapPassword string
	SessionTTL        time.Duration
	SessionSecret     string
	CookieName        string
	CookieSecure      bool
	AllowedOrigins    []string
	// AuditFile optional append-only audit log path (single-replica JSONL).
	AuditFile string
	// AuditPostgresDSN selects the multi-replica durable Postgres audit sink
	// (ADR-0004). When set, it takes precedence over AuditFile for primary writes.
	AuditPostgresDSN string
	// RequireAuditDurable fails startup when the audit sink is not durable
	// (HIGHLAND_AUDIT_REQUIRED). Production HA profiles with writes/policy
	// mutations should enable this (ADR-0004).
	RequireAuditDurable bool
	// MetricsInterval for manager /metrics scrape.
	MetricsInterval time.Duration
	// AuthMode is local | oidc | local+oidc. Default local (admin login without IdP).
	AuthMode AuthMode
	// LocalAlways keeps POST /auth/login available even in oidc-only mode (break-glass).
	LocalAlways bool
	// OIDC (optional — never required for local admin login)
	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string
	OIDCMock         bool
	OIDCRoleClaim    string
	// Redis for HA sessions (optional)
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	// Version reported by compatibility API.
	Version string

	// Provider-neutral Kubernetes storage inventory and operation policy.
	StorageEnabled                  bool
	StorageScopeMode                string
	StorageNamespaces               []string
	StorageWritesEnabled            bool
	StorageOperationRecoveryEnabled bool
	RequiredProviders               []string
	AdminPolicyControlEnabled       bool
	AdminPolicyInstallWriterRBAC    bool
	PolicyCeilingPortableWrites     bool
	PolicyCeilingLonghornWrites     bool
	PolicyCeilingRookCephWrites     bool
	PolicyCeilingCephSCDelete       bool
	PolicyCeilingCephPoolDelete     bool
	ClusterIdentity                 string

	// Longhorn managed-provider compatibility. ManagerURL remains the legacy
	// connection setting and is synthesized into this provider when enabled.
	LonghornEnabled   bool
	LonghornRequired  bool
	LonghornNamespace string

	// Optional read-only OpenEBS managed provider. Individual engines are
	// discovered from their documented drivers, workloads, and CRDs.
	OpenEBSEnabled   bool
	OpenEBSNamespace string

	// Optional read-only Piraeus/LINSTOR managed provider. Highland observes
	// the independently managed CSI deployment and never owns its lifecycle.
	LinstorEnabled       bool
	LinstorNamespace     string
	LinstorControllerURL string
	LinstorAuthToken     string
	LinstorCAFile        string
	LinstorInsecureTLS   bool
	LinstorAllowHTTP     bool
	LinstorTimeout       time.Duration

	// Optional read-only Rook/Ceph managed provider. Write gates are evaluated
	// separately by the operation policy.
	RookCephEnabled                 bool
	RookCephNamespace               string
	RookCephClusterName             string
	RookCephDashboardURL            string
	RookCephDashboardPublicURL      string
	RookCephDashboardAllowHTTP      bool
	RookCephDashboardUsername       string
	RookCephDashboardPassword       string
	RookCephDashboardCAFile         string
	RookCephDashboardInsecureTLS    bool
	RookCephCredentialRevealEnabled bool
	RookCephDashboardAdminUsername  string
	RookCephDashboardAdminSecret    string
	RookCephPrometheusURL           string
	RookCephWritesEnabled           bool
	RookCephAllowStorageClassDelete bool
	RookCephAllowPoolDelete         bool

	// KubernetesBenchmarkEnabled permits fio Job/PVC and ConfigMap persistence.
	// Synthetic/offline benchmark records remain available when it is false.
	KubernetesBenchmarkEnabled bool

	// TrustedProxies are CIDRs of reverse proxies whose forwarding headers we
	// trust when deriving the client IP. Empty = trust none (use socket peer).
	TrustedProxies []string

	// CSRF double-submit token protection for state-changing requests.
	CSRFEnabled    bool
	CSRFCookieName string

	// Local-login brute-force protection (in-memory, dual-keyed by user + IP).
	LoginRateLimitEnabled bool
	LoginMaxFailuresUser  int
	LoginMaxFailuresIP    int
	LoginLockoutBase      time.Duration
	LoginLockoutMax       time.Duration
	LoginFailureWindow    time.Duration
	LoginMaxEntries       int
}

// LocalEnabled is true when username/password login is accepted.
func (c *Config) LocalEnabled() bool {
	if c.LocalAlways {
		return true
	}
	return c.AuthMode == AuthModeLocal || c.AuthMode == AuthModeLocalOIDC || c.AuthMode == ""
}

// OIDCEnabled is true when enterprise IdP (or mock) login is offered.
func (c *Config) OIDCEnabled() bool {
	if c.OIDCMock {
		return true
	}
	return (c.AuthMode == AuthModeOIDC || c.AuthMode == AuthModeLocalOIDC) && c.OIDCIssuer != ""
}

// LoadFromEnv builds Config from environment variables with safe local-dev defaults.
func LoadFromEnv() (*Config, error) {
	mode := AuthMode(strings.ToLower(envOr("HIGHLAND_AUTH_MODE", "local")))
	switch mode {
	case AuthModeLocal, AuthModeOIDC, AuthModeLocalOIDC:
	default:
		mode = AuthModeLocal
	}
	cfg := &Config{
		ListenAddr:        envOr("HIGHLAND_LISTEN_ADDR", ":8080"),
		ManagerURL:        strings.TrimRight(envOr("HIGHLAND_MANAGER_URL", "http://127.0.0.1:9500"), "/"),
		BootstrapUsername: envOr("HIGHLAND_ADMIN_USER", "admin"),
		BootstrapPassword: envOr("HIGHLAND_ADMIN_PASSWORD", "highland"),
		CookieName:        envOr("HIGHLAND_COOKIE_NAME", "highland_session"),
		CookieSecure:      envBool("HIGHLAND_COOKIE_SECURE", false),
		SessionTTL:        envDuration("HIGHLAND_SESSION_TTL", 24*time.Hour),
		SessionSecret:         os.Getenv("HIGHLAND_SESSION_SECRET"),
		AuditFile:             os.Getenv("HIGHLAND_AUDIT_FILE"),
		AuditPostgresDSN:      strings.TrimSpace(os.Getenv("HIGHLAND_AUDIT_POSTGRES_DSN")),
		RequireAuditDurable:   envBool("HIGHLAND_AUDIT_REQUIRED", false),
		MetricsInterval:       envDuration("HIGHLAND_METRICS_INTERVAL", 10*time.Second),
		AuthMode:              mode,
		// Break-glass local admin always on unless explicitly disabled
		LocalAlways:      envBool("HIGHLAND_LOCAL_ALWAYS", true),
		OIDCIssuer:       os.Getenv("HIGHLAND_OIDC_ISSUER"),
		OIDCClientID:     os.Getenv("HIGHLAND_OIDC_CLIENT_ID"),
		OIDCClientSecret: os.Getenv("HIGHLAND_OIDC_CLIENT_SECRET"),
		OIDCRedirectURL:  os.Getenv("HIGHLAND_OIDC_REDIRECT_URL"),
		// OIDC mock off by default — local admin is the primary path
		OIDCMock:      envBool("HIGHLAND_OIDC_MOCK", false),
		OIDCRoleClaim: envOr("HIGHLAND_OIDC_ROLE_CLAIM", "highland_role"),
		RedisAddr:     os.Getenv("HIGHLAND_REDIS_ADDR"),
		RedisPassword: os.Getenv("HIGHLAND_REDIS_PASSWORD"),
		RedisDB:       envInt("HIGHLAND_REDIS_DB", 0),
		Version:       envOr("HIGHLAND_VERSION", "0.4.0"),

		StorageEnabled:                  envBool("HIGHLAND_STORAGE_ENABLED", true),
		StorageScopeMode:                strings.ToLower(envOr("HIGHLAND_STORAGE_SCOPE", "cluster")),
		StorageNamespaces:               envCSV("HIGHLAND_STORAGE_NAMESPACES"),
		StorageWritesEnabled:            envBool("HIGHLAND_STORAGE_WRITES_ENABLED", false),
		StorageOperationRecoveryEnabled: envBool("HIGHLAND_STORAGE_OPERATION_RECOVERY_ENABLED", false),
		RequiredProviders:               envCSV("HIGHLAND_REQUIRED_PROVIDERS"),
		AdminPolicyControlEnabled:       envBool("HIGHLAND_ADMIN_POLICY_CONTROL_ENABLED", false),
		AdminPolicyInstallWriterRBAC:    envBool("HIGHLAND_ADMIN_POLICY_INSTALL_WRITER_RBAC", false),
		PolicyCeilingPortableWrites:     envBool("HIGHLAND_POLICY_CEILING_PORTABLE_WRITES", false),
		PolicyCeilingLonghornWrites:     envBool("HIGHLAND_POLICY_CEILING_LONGHORN_WRITES", false),
		PolicyCeilingRookCephWrites:     envBool("HIGHLAND_POLICY_CEILING_ROOK_CEPH_WRITES", false),
		PolicyCeilingCephSCDelete:       envBool("HIGHLAND_POLICY_CEILING_CEPH_STORAGECLASS_DELETE", false),
		PolicyCeilingCephPoolDelete:     envBool("HIGHLAND_POLICY_CEILING_CEPH_POOL_DELETE", false),
		ClusterIdentity:                 envOr("HIGHLAND_CLUSTER_IDENTITY", "local"),
		LonghornEnabled:                 envBool("HIGHLAND_LONGHORN_ENABLED", true),
		LonghornRequired:                envBool("HIGHLAND_LONGHORN_REQUIRED", true),
		LonghornNamespace:               envOr("HIGHLAND_LONGHORN_NAMESPACE", "longhorn-system"),
		OpenEBSEnabled:                  envBool("HIGHLAND_OPENEBS_ENABLED", false),
		OpenEBSNamespace:                envOr("HIGHLAND_OPENEBS_NAMESPACE", "openebs"),
		LinstorEnabled:                  envBool("HIGHLAND_LINSTOR_ENABLED", false),
		LinstorNamespace:                envOr("HIGHLAND_LINSTOR_NAMESPACE", "piraeus-datastore"),
		LinstorControllerURL:            strings.TrimRight(os.Getenv("HIGHLAND_LINSTOR_CONTROLLER_URL"), "/"),
		LinstorAuthToken:                os.Getenv("HIGHLAND_LINSTOR_AUTH_TOKEN"),
		LinstorCAFile:                   os.Getenv("HIGHLAND_LINSTOR_CA_FILE"),
		LinstorInsecureTLS:              envBool("HIGHLAND_LINSTOR_INSECURE_TLS", false),
		LinstorAllowHTTP:                envBool("HIGHLAND_LINSTOR_ALLOW_HTTP", false),
		LinstorTimeout:                  envDuration("HIGHLAND_LINSTOR_TIMEOUT", 5*time.Second),
		RookCephEnabled:                 envBool("HIGHLAND_ROOK_CEPH_ENABLED", false),
		RookCephNamespace:               envOr("HIGHLAND_ROOK_CEPH_NAMESPACE", "rook-ceph"),
		RookCephClusterName:             envOr("HIGHLAND_ROOK_CEPH_CLUSTER_NAME", "rook-ceph"),
		RookCephDashboardURL:            strings.TrimRight(os.Getenv("HIGHLAND_ROOK_CEPH_DASHBOARD_URL"), "/"),
		RookCephDashboardPublicURL:      strings.TrimRight(os.Getenv("HIGHLAND_ROOK_CEPH_DASHBOARD_PUBLIC_URL"), "/"),
		RookCephDashboardAllowHTTP:      envBool("HIGHLAND_ROOK_CEPH_DASHBOARD_ALLOW_HTTP", false),
		RookCephDashboardUsername:       os.Getenv("HIGHLAND_ROOK_CEPH_DASHBOARD_USERNAME"),
		RookCephDashboardPassword:       os.Getenv("HIGHLAND_ROOK_CEPH_DASHBOARD_PASSWORD"),
		RookCephDashboardCAFile:         os.Getenv("HIGHLAND_ROOK_CEPH_DASHBOARD_CA_FILE"),
		RookCephDashboardInsecureTLS:    envBool("HIGHLAND_ROOK_CEPH_DASHBOARD_INSECURE_TLS", false),
		RookCephCredentialRevealEnabled: envBool("HIGHLAND_ROOK_CEPH_CREDENTIAL_REVEAL_ENABLED", false),
		RookCephDashboardAdminUsername:  envOr("HIGHLAND_ROOK_CEPH_DASHBOARD_ADMIN_USERNAME", "admin"),
		RookCephDashboardAdminSecret:    envOr("HIGHLAND_ROOK_CEPH_DASHBOARD_ADMIN_SECRET", "rook-ceph-dashboard-password"),
		RookCephPrometheusURL:           strings.TrimRight(os.Getenv("HIGHLAND_ROOK_CEPH_PROMETHEUS_URL"), "/"),
		RookCephWritesEnabled:           envBool("HIGHLAND_ROOK_CEPH_WRITES_ENABLED", false),
		RookCephAllowStorageClassDelete: envBool("HIGHLAND_ROOK_CEPH_ALLOW_STORAGE_CLASS_DELETE", false),
		RookCephAllowPoolDelete:         envBool("HIGHLAND_ROOK_CEPH_ALLOW_POOL_DELETE", false),
		KubernetesBenchmarkEnabled:      envBool("HIGHLAND_KUBERNETES_BENCHMARK_ENABLED", true),

		CSRFEnabled:    envBool("HIGHLAND_CSRF_ENABLED", true),
		CSRFCookieName: envOr("HIGHLAND_CSRF_COOKIE_NAME", "highland_csrf"),

		LoginRateLimitEnabled: envBool("HIGHLAND_LOGIN_RATELIMIT_ENABLED", true),
		LoginMaxFailuresUser:  envInt("HIGHLAND_LOGIN_MAX_FAILURES_USER", 5),
		LoginMaxFailuresIP:    envInt("HIGHLAND_LOGIN_MAX_FAILURES_IP", 15),
		LoginLockoutBase:      envDuration("HIGHLAND_LOGIN_LOCKOUT_BASE", time.Minute),
		LoginLockoutMax:       envDuration("HIGHLAND_LOGIN_LOCKOUT_MAX", 15*time.Minute),
		LoginFailureWindow:    envDuration("HIGHLAND_LOGIN_FAILURE_WINDOW", 15*time.Minute),
		LoginMaxEntries:       envInt("HIGHLAND_LOGIN_MAX_ENTRIES", 100000),
	}

	if origins := os.Getenv("HIGHLAND_ALLOWED_ORIGINS"); origins != "" {
		for _, o := range strings.Split(origins, ",") {
			o = strings.TrimSpace(o)
			if o != "" {
				cfg.AllowedOrigins = append(cfg.AllowedOrigins, o)
			}
		}
	} else {
		cfg.AllowedOrigins = []string{"http://localhost:5173", "http://127.0.0.1:5173"}
	}

	// Trusted reverse-proxy CIDRs whose X-Forwarded-For / X-Real-IP we honor when
	// resolving the client IP. Empty (default) means trust NONE — the raw socket
	// peer is used, so forwarding headers cannot spoof the login limiter or audit
	// source IP. Set to your ingress/proxy CIDR(s) in production.
	for _, p := range strings.Split(os.Getenv("HIGHLAND_TRUSTED_PROXIES"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			cfg.TrustedProxies = append(cfg.TrustedProxies, p)
		}
	}

	if cfg.BootstrapUsername == "" || cfg.BootstrapPassword == "" {
		return nil, fmt.Errorf("HIGHLAND_ADMIN_USER and HIGHLAND_ADMIN_PASSWORD are required")
	}
	if cfg.LonghornEnabled && cfg.ManagerURL == "" {
		return nil, fmt.Errorf("HIGHLAND_MANAGER_URL is required")
	}
	if cfg.StorageScopeMode != "cluster" && cfg.StorageScopeMode != "namespaces" {
		return nil, fmt.Errorf("HIGHLAND_STORAGE_SCOPE must be cluster or namespaces")
	}
	if cfg.StorageScopeMode == "namespaces" && len(cfg.StorageNamespaces) == 0 {
		return nil, fmt.Errorf("HIGHLAND_STORAGE_NAMESPACES is required when scope is namespaces")
	}
	if cfg.RookCephEnabled && cfg.RookCephDashboardURL != "" && (cfg.RookCephDashboardUsername == "" || cfg.RookCephDashboardPassword == "") {
		return nil, fmt.Errorf("Rook/Ceph Dashboard username and password are required when dashboard URL is configured")
	}
	if cfg.LinstorControllerURL != "" {
		controllerURL, err := url.Parse(cfg.LinstorControllerURL)
		if err != nil || !controllerURL.IsAbs() || controllerURL.Opaque != "" || controllerURL.Host == "" || controllerURL.User != nil {
			return nil, fmt.Errorf("HIGHLAND_LINSTOR_CONTROLLER_URL must be an absolute URL without userinfo")
		}
		if controllerURL.RawQuery != "" || controllerURL.ForceQuery || controllerURL.Fragment != "" {
			return nil, fmt.Errorf("HIGHLAND_LINSTOR_CONTROLLER_URL must not contain a query or fragment")
		}
		switch strings.ToLower(controllerURL.Scheme) {
		case "https":
		case "http":
			if !cfg.LinstorAllowHTTP {
				return nil, fmt.Errorf("HIGHLAND_LINSTOR_CONTROLLER_URL requires HTTPS unless HIGHLAND_LINSTOR_ALLOW_HTTP is explicitly enabled")
			}
		default:
			return nil, fmt.Errorf("HIGHLAND_LINSTOR_CONTROLLER_URL must use HTTPS")
		}
	}
	if cfg.LinstorTimeout <= 0 || cfg.LinstorTimeout > 30*time.Second {
		return nil, fmt.Errorf("HIGHLAND_LINSTOR_TIMEOUT must be greater than zero and no more than 30s")
	}
	if cfg.RookCephDashboardPublicURL != "" {
		publicURL, err := url.Parse(cfg.RookCephDashboardPublicURL)
		if err != nil || !publicURL.IsAbs() || publicURL.Opaque != "" || publicURL.Host == "" || !validPublicHostname(publicURL.Hostname()) || publicURL.User != nil {
			return nil, fmt.Errorf("HIGHLAND_ROOK_CEPH_DASHBOARD_PUBLIC_URL must be an absolute URL without userinfo")
		}
		if publicURL.RawQuery != "" || publicURL.ForceQuery || publicURL.Fragment != "" {
			return nil, fmt.Errorf("HIGHLAND_ROOK_CEPH_DASHBOARD_PUBLIC_URL must not contain a query or fragment")
		}
		switch strings.ToLower(publicURL.Scheme) {
		case "https":
		case "http":
			if !cfg.RookCephDashboardAllowHTTP {
				return nil, fmt.Errorf("HIGHLAND_ROOK_CEPH_DASHBOARD_PUBLIC_URL requires HTTPS unless HIGHLAND_ROOK_CEPH_DASHBOARD_ALLOW_HTTP is explicitly enabled for a disposable lab")
			}
		default:
			return nil, fmt.Errorf("HIGHLAND_ROOK_CEPH_DASHBOARD_PUBLIC_URL must use HTTPS")
		}
	}
	if cfg.StorageWritesEnabled && !cfg.StorageEnabled {
		return nil, fmt.Errorf("HIGHLAND_STORAGE_WRITES_ENABLED requires HIGHLAND_STORAGE_ENABLED")
	}
	if cfg.AdminPolicyControlEnabled && !cfg.StorageEnabled {
		return nil, fmt.Errorf("HIGHLAND_ADMIN_POLICY_CONTROL_ENABLED requires HIGHLAND_STORAGE_ENABLED")
	}
	if cfg.AdminPolicyInstallWriterRBAC && !cfg.AdminPolicyControlEnabled {
		return nil, fmt.Errorf("HIGHLAND_ADMIN_POLICY_INSTALL_WRITER_RBAC requires HIGHLAND_ADMIN_POLICY_CONTROL_ENABLED")
	}
	if (cfg.PolicyCeilingPortableWrites || cfg.PolicyCeilingLonghornWrites || cfg.PolicyCeilingRookCephWrites || cfg.PolicyCeilingCephSCDelete || cfg.PolicyCeilingCephPoolDelete) && !cfg.AdminPolicyInstallWriterRBAC {
		return nil, fmt.Errorf("policy write ceilings require HIGHLAND_ADMIN_POLICY_INSTALL_WRITER_RBAC")
	}
	if cfg.PolicyCeilingRookCephWrites && !cfg.RookCephEnabled {
		return nil, fmt.Errorf("HIGHLAND_POLICY_CEILING_ROOK_CEPH_WRITES requires HIGHLAND_ROOK_CEPH_ENABLED")
	}
	if cfg.PolicyCeilingCephSCDelete && !cfg.PolicyCeilingRookCephWrites {
		return nil, fmt.Errorf("HIGHLAND_POLICY_CEILING_CEPH_STORAGECLASS_DELETE requires the Rook/Ceph write ceiling")
	}
	if cfg.PolicyCeilingCephPoolDelete && !cfg.PolicyCeilingRookCephWrites {
		return nil, fmt.Errorf("HIGHLAND_POLICY_CEILING_CEPH_POOL_DELETE requires the Rook/Ceph write ceiling")
	}
	if strings.TrimSpace(cfg.ClusterIdentity) == "" || len(cfg.ClusterIdentity) > 128 {
		return nil, fmt.Errorf("HIGHLAND_CLUSTER_IDENTITY must be a nonempty value no longer than 128 characters")
	}
	if cfg.RookCephWritesEnabled && !cfg.RookCephEnabled {
		return nil, fmt.Errorf("HIGHLAND_ROOK_CEPH_WRITES_ENABLED requires HIGHLAND_ROOK_CEPH_ENABLED")
	}
	if cfg.RookCephAllowPoolDelete && !cfg.RookCephWritesEnabled {
		return nil, fmt.Errorf("HIGHLAND_ROOK_CEPH_ALLOW_POOL_DELETE requires HIGHLAND_ROOK_CEPH_WRITES_ENABLED")
	}
	if cfg.RookCephAllowStorageClassDelete && !cfg.RookCephWritesEnabled {
		return nil, fmt.Errorf("HIGHLAND_ROOK_CEPH_ALLOW_STORAGE_CLASS_DELETE requires HIGHLAND_ROOK_CEPH_WRITES_ENABLED")
	}
	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envCSV(key string) []string {
	var out []string
	for _, value := range strings.Split(os.Getenv(key), ",") {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func validPublicHostname(hostname string) bool {
	if hostname == "" {
		return false
	}
	if net.ParseIP(hostname) != nil {
		return true
	}
	if len(hostname) > 253 {
		return false
	}
	for _, label := range strings.Split(hostname, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' {
				return false
			}
		}
	}
	return true
}
