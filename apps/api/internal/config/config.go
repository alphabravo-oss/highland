package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// AuthMode controls which login methods are offered.
//   local       — username/password only (default; no IdP required)
//   oidc        — OIDC only (still allows bootstrap recovery if LocalAlways)
//   local+oidc  — both local form and OIDC
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
	// AuditFile optional append-only audit log path.
	AuditFile string
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
		SessionSecret:     os.Getenv("HIGHLAND_SESSION_SECRET"),
		AuditFile:         os.Getenv("HIGHLAND_AUDIT_FILE"),
		MetricsInterval:   envDuration("HIGHLAND_METRICS_INTERVAL", 10*time.Second),
		AuthMode:          mode,
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
		Version:       envOr("HIGHLAND_VERSION", "0.1.0"),
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

	if cfg.BootstrapUsername == "" || cfg.BootstrapPassword == "" {
		return nil, fmt.Errorf("HIGHLAND_ADMIN_USER and HIGHLAND_ADMIN_PASSWORD are required")
	}
	if cfg.ManagerURL == "" {
		return nil, fmt.Errorf("HIGHLAND_MANAGER_URL is required")
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
