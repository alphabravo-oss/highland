package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// FileConfig is the shape of a mounted config file (ConfigMap) — Kubernetes-native settings.
// Secrets (passwords, OIDC client secret) stay in env from Secret keyRefs, not in this file.
type FileConfig struct {
	ListenAddr      string `json:"listenAddr" yaml:"listenAddr"`
	ManagerURL      string `json:"managerUrl" yaml:"managerUrl"`
	AuthMode        string `json:"authMode" yaml:"authMode"`
	LocalAlways     *bool  `json:"localAlways" yaml:"localAlways"`
	CookieName      string `json:"cookieName" yaml:"cookieName"`
	CookieSecure    *bool  `json:"cookieSecure" yaml:"cookieSecure"`
	SessionTTL      string `json:"sessionTTL" yaml:"sessionTTL"`
	MetricsInterval string `json:"metricsInterval" yaml:"metricsInterval"`
	OIDCIssuer      string `json:"oidcIssuer" yaml:"oidcIssuer"`
	OIDCClientID    string `json:"oidcClientId" yaml:"oidcClientId"`
	OIDCRedirectURL string `json:"oidcRedirectUrl" yaml:"oidcRedirectUrl"`
	OIDCMock        *bool  `json:"oidcMock" yaml:"oidcMock"`
	Version         string `json:"version" yaml:"version"`
	AllowedOrigins  string `json:"allowedOrigins" yaml:"allowedOrigins"` // comma-separated
}

// applyFile overlays non-empty file fields onto cfg (env still wins if set after LoadFromEnv).
// Call order: LoadFromEnv (reads env) then optionally load file first into env — simpler:
// LoadFromEnv already reads env. For file-first: load file into process env only for unset keys.
func applyFile(cfg *Config, f *FileConfig) error {
	if f == nil {
		return nil
	}
	if f.ListenAddr != "" {
		cfg.ListenAddr = f.ListenAddr
	}
	if f.ManagerURL != "" {
		cfg.ManagerURL = strings.TrimRight(f.ManagerURL, "/")
	}
	if f.AuthMode != "" {
		cfg.AuthMode = AuthMode(strings.ToLower(f.AuthMode))
	}
	if f.LocalAlways != nil {
		cfg.LocalAlways = *f.LocalAlways
	}
	if f.CookieName != "" {
		cfg.CookieName = f.CookieName
	}
	if f.CookieSecure != nil {
		cfg.CookieSecure = *f.CookieSecure
	}
	if f.SessionTTL != "" {
		d, err := time.ParseDuration(f.SessionTTL)
		if err != nil {
			return fmt.Errorf("sessionTTL: %w", err)
		}
		cfg.SessionTTL = d
	}
	if f.MetricsInterval != "" {
		d, err := time.ParseDuration(f.MetricsInterval)
		if err != nil {
			return fmt.Errorf("metricsInterval: %w", err)
		}
		cfg.MetricsInterval = d
	}
	if f.OIDCIssuer != "" {
		cfg.OIDCIssuer = f.OIDCIssuer
	}
	if f.OIDCClientID != "" {
		cfg.OIDCClientID = f.OIDCClientID
	}
	if f.OIDCRedirectURL != "" {
		cfg.OIDCRedirectURL = f.OIDCRedirectURL
	}
	if f.OIDCMock != nil {
		cfg.OIDCMock = *f.OIDCMock
	}
	if f.Version != "" {
		cfg.Version = f.Version
	}
	if f.AllowedOrigins != "" {
		cfg.AllowedOrigins = nil
		for _, o := range strings.Split(f.AllowedOrigins, ",") {
			o = strings.TrimSpace(o)
			if o != "" {
				cfg.AllowedOrigins = append(cfg.AllowedOrigins, o)
			}
		}
	}
	return nil
}

// Load merges file config (if HIGHLAND_CONFIG_FILE set) then environment (env overrides file).
// Kubernetes: mount ConfigMap at /etc/highland/config.json; inject Secret via env keyRefs.
func Load() (*Config, error) {
	var fileCfg *FileConfig
	if path := os.Getenv("HIGHLAND_CONFIG_FILE"); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read config file %s: %w", path, err)
		}
		fileCfg = &FileConfig{}
		if err := json.Unmarshal(raw, fileCfg); err != nil {
			if err2 := unmarshalLooseYAML(raw, fileCfg); err2 != nil {
				return nil, fmt.Errorf("parse config file: json=%v yaml=%v", err, err2)
			}
		}
	}
	return loadWithFileFirst(fileCfg)
}

func loadWithFileFirst(fileCfg *FileConfig) (*Config, error) {
	if fileCfg != nil {
		_ = seedEnvFromFile(fileCfg)
	}
	cfg, err := LoadFromEnv()
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// seedEnvFromFile sets env vars only if not already set (env wins over file).
func seedEnvFromFile(f *FileConfig) error {
	set := func(k, v string) {
		if v == "" {
			return
		}
		if os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
	set("HIGHLAND_LISTEN_ADDR", f.ListenAddr)
	set("HIGHLAND_MANAGER_URL", f.ManagerURL)
	set("HIGHLAND_AUTH_MODE", f.AuthMode)
	if f.LocalAlways != nil && os.Getenv("HIGHLAND_LOCAL_ALWAYS") == "" {
		_ = os.Setenv("HIGHLAND_LOCAL_ALWAYS", fmt.Sprintf("%v", *f.LocalAlways))
	}
	set("HIGHLAND_COOKIE_NAME", f.CookieName)
	if f.CookieSecure != nil && os.Getenv("HIGHLAND_COOKIE_SECURE") == "" {
		_ = os.Setenv("HIGHLAND_COOKIE_SECURE", fmt.Sprintf("%v", *f.CookieSecure))
	}
	set("HIGHLAND_SESSION_TTL", f.SessionTTL)
	set("HIGHLAND_METRICS_INTERVAL", f.MetricsInterval)
	set("HIGHLAND_OIDC_ISSUER", f.OIDCIssuer)
	set("HIGHLAND_OIDC_CLIENT_ID", f.OIDCClientID)
	set("HIGHLAND_OIDC_REDIRECT_URL", f.OIDCRedirectURL)
	if f.OIDCMock != nil && os.Getenv("HIGHLAND_OIDC_MOCK") == "" {
		_ = os.Setenv("HIGHLAND_OIDC_MOCK", fmt.Sprintf("%v", *f.OIDCMock))
	}
	set("HIGHLAND_VERSION", f.Version)
	set("HIGHLAND_ALLOWED_ORIGINS", f.AllowedOrigins)
	return nil
}

// unmarshalLooseYAML supports a tiny subset for config maps without a YAML dependency.
func unmarshalLooseYAML(raw []byte, f *FileConfig) error {
	// Also try converting simple yaml to json via line parse
	m := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// key: value
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.TrimSpace(parts[0])
		v := strings.TrimSpace(parts[1])
		v = strings.Trim(v, `"'`)
		m[k] = v
	}
	if len(m) == 0 {
		return fmt.Errorf("no keys parsed")
	}
	b, _ := json.Marshal(m)
	// map uses camelCase keys same as JSON tags — remap common yaml keys
	remap := map[string]string{
		"listenAddr": m["listenAddr"], "listen_addr": m["listen_addr"],
		"managerUrl": m["managerUrl"], "manager_url": m["managerUrl"],
		"authMode": m["authMode"], "auth_mode": m["authMode"],
	}
	_ = remap
	// Build FileConfig from known keys
	if v, ok := m["listenAddr"]; ok {
		f.ListenAddr = v
	}
	if v, ok := m["managerUrl"]; ok {
		f.ManagerURL = v
	}
	if v, ok := m["manager_url"]; ok {
		f.ManagerURL = v
	}
	if v, ok := m["authMode"]; ok {
		f.AuthMode = v
	}
	if v, ok := m["auth_mode"]; ok {
		f.AuthMode = v
	}
	if v, ok := m["cookieName"]; ok {
		f.CookieName = v
	}
	if v, ok := m["sessionTTL"]; ok {
		f.SessionTTL = v
	}
	if v, ok := m["oidcIssuer"]; ok {
		f.OIDCIssuer = v
	}
	if v, ok := m["version"]; ok {
		f.Version = v
	}
	if v, ok := m["localAlways"]; ok {
		b := v == "true" || v == "1"
		f.LocalAlways = &b
	}
	if v, ok := m["cookieSecure"]; ok {
		b := v == "true" || v == "1"
		f.CookieSecure = &b
	}
	_ = b
	return nil
}
