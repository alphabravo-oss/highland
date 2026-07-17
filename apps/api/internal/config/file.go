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
	ListenAddr      string                 `json:"listenAddr" yaml:"listenAddr"`
	ManagerURL      string                 `json:"managerUrl" yaml:"managerUrl"`
	AuthMode        string                 `json:"authMode" yaml:"authMode"`
	LocalAlways     *bool                  `json:"localAlways" yaml:"localAlways"`
	CookieName      string                 `json:"cookieName" yaml:"cookieName"`
	CookieSecure    *bool                  `json:"cookieSecure" yaml:"cookieSecure"`
	SessionTTL      string                 `json:"sessionTTL" yaml:"sessionTTL"`
	MetricsInterval string                 `json:"metricsInterval" yaml:"metricsInterval"`
	OIDCIssuer      string                 `json:"oidcIssuer" yaml:"oidcIssuer"`
	OIDCClientID    string                 `json:"oidcClientId" yaml:"oidcClientId"`
	OIDCRedirectURL string                 `json:"oidcRedirectUrl" yaml:"oidcRedirectUrl"`
	OIDCMock        *bool                  `json:"oidcMock" yaml:"oidcMock"`
	Version         string                 `json:"version" yaml:"version"`
	AllowedOrigins  string                 `json:"allowedOrigins" yaml:"allowedOrigins"` // comma-separated
	Storage         *StorageFileConfig     `json:"storage,omitempty" yaml:"storage,omitempty"`
	Providers       *ProvidersFileConfig   `json:"providers,omitempty" yaml:"providers,omitempty"`
	AdminPolicy     *AdminPolicyFileConfig `json:"adminPolicyControl,omitempty" yaml:"adminPolicyControl,omitempty"`
	ClusterIdentity string                 `json:"clusterIdentity,omitempty" yaml:"clusterIdentity,omitempty"`
}

type AdminPolicyFileConfig struct {
	Enabled                         *bool                     `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	InstallStorageWriterPermissions *bool                     `json:"installStorageWriterPermissions,omitempty" yaml:"installStorageWriterPermissions,omitempty"`
	Ceiling                         *AdminPolicyCeilingConfig `json:"ceiling,omitempty" yaml:"ceiling,omitempty"`
}

type AdminPolicyCeilingConfig struct {
	PortableKubernetesWrites    *bool `json:"portableKubernetesWrites,omitempty" yaml:"portableKubernetesWrites,omitempty"`
	LonghornWrites              *bool `json:"longhornWrites,omitempty" yaml:"longhornWrites,omitempty"`
	RookCephWrites              *bool `json:"rookCephWrites,omitempty" yaml:"rookCephWrites,omitempty"`
	AllowCephStorageClassDelete *bool `json:"allowCephStorageClassDelete,omitempty" yaml:"allowCephStorageClassDelete,omitempty"`
	AllowCephPoolDelete         *bool `json:"allowCephPoolDelete,omitempty" yaml:"allowCephPoolDelete,omitempty"`
}

type StorageFileConfig struct {
	Enabled           *bool                    `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Scope             *StorageScopeFileConfig  `json:"scope,omitempty" yaml:"scope,omitempty"`
	ScopeMode         string                   `json:"scopeMode,omitempty" yaml:"scopeMode,omitempty"`
	Namespaces        []string                 `json:"namespaces,omitempty" yaml:"namespaces,omitempty"`
	Writes            *StorageWritesFileConfig `json:"writes,omitempty" yaml:"writes,omitempty"`
	WritesEnabled     *bool                    `json:"writesEnabled,omitempty" yaml:"writesEnabled,omitempty"`
	RequiredProviders []string                 `json:"requiredProviders,omitempty" yaml:"requiredProviders,omitempty"`
}

type StorageScopeFileConfig struct {
	Mode       string   `json:"mode,omitempty" yaml:"mode,omitempty"`
	Namespaces []string `json:"namespaces,omitempty" yaml:"namespaces,omitempty"`
}

type StorageWritesFileConfig struct {
	Enabled         *bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	RecoveryEnabled *bool `json:"recoveryEnabled,omitempty" yaml:"recoveryEnabled,omitempty"`
}

type ProvidersFileConfig struct {
	Longhorn *LonghornFileConfig `json:"longhorn,omitempty" yaml:"longhorn,omitempty"`
	OpenEBS  *OpenEBSFileConfig  `json:"openebs,omitempty" yaml:"openebs,omitempty"`
	Linstor  *LinstorFileConfig  `json:"linstor,omitempty" yaml:"linstor,omitempty"`
	RookCeph *RookCephFileConfig `json:"rookCeph,omitempty" yaml:"rookCeph,omitempty"`
}

type LonghornFileConfig struct {
	Enabled    *bool  `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Required   *bool  `json:"required,omitempty" yaml:"required,omitempty"`
	ManagerURL string `json:"managerUrl,omitempty" yaml:"managerUrl,omitempty"`
	Namespace  string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
}

type OpenEBSFileConfig struct {
	Enabled   *bool  `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
}

type LinstorFileConfig struct {
	Enabled       *bool  `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Namespace     string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	ControllerURL string `json:"controllerUrl,omitempty" yaml:"controllerUrl,omitempty"`
	CAFile        string `json:"caFile,omitempty" yaml:"caFile,omitempty"`
	InsecureTLS   *bool  `json:"insecureSkipVerify,omitempty" yaml:"insecureSkipVerify,omitempty"`
	AllowHTTP     *bool  `json:"allowHttp,omitempty" yaml:"allowHttp,omitempty"`
	Timeout       string `json:"timeout,omitempty" yaml:"timeout,omitempty"`
}

type RookCephFileConfig struct {
	Enabled              *bool                         `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Namespace            string                        `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	ClusterName          string                        `json:"clusterName,omitempty" yaml:"clusterName,omitempty"`
	DashboardURL         string                        `json:"dashboardUrl,omitempty" yaml:"dashboardUrl,omitempty"`
	DashboardCAFile      string                        `json:"dashboardCaFile,omitempty" yaml:"dashboardCaFile,omitempty"`
	DashboardInsecureTLS *bool                         `json:"dashboardInsecureTls,omitempty" yaml:"dashboardInsecureTls,omitempty"`
	PrometheusURL        string                        `json:"prometheusUrl,omitempty" yaml:"prometheusUrl,omitempty"`
	WritesEnabled        *bool                         `json:"writesEnabled,omitempty" yaml:"writesEnabled,omitempty"`
	AllowPoolDelete      *bool                         `json:"allowPoolDelete,omitempty" yaml:"allowPoolDelete,omitempty"`
	Dashboard            *RookCephDashboardFileConfig  `json:"dashboard,omitempty" yaml:"dashboard,omitempty"`
	Prometheus           *RookCephPrometheusFileConfig `json:"prometheus,omitempty" yaml:"prometheus,omitempty"`
	Writes               *RookCephWritesFileConfig     `json:"writes,omitempty" yaml:"writes,omitempty"`
}

type RookCephDashboardFileConfig struct {
	URL                string `json:"url,omitempty" yaml:"url,omitempty"`
	PublicURL          string `json:"publicUrl,omitempty" yaml:"publicUrl,omitempty"`
	AllowHTTP          *bool  `json:"allowHttp,omitempty" yaml:"allowHttp,omitempty"`
	CAFile             string `json:"caFile,omitempty" yaml:"caFile,omitempty"`
	InsecureSkipVerify *bool  `json:"insecureSkipVerify,omitempty" yaml:"insecureSkipVerify,omitempty"`
	CredentialReveal   *bool  `json:"credentialReveal,omitempty" yaml:"credentialReveal,omitempty"`
	AdminUsername      string `json:"adminUsername,omitempty" yaml:"adminUsername,omitempty"`
	AdminSecret        string `json:"adminSecret,omitempty" yaml:"adminSecret,omitempty"`
}

type RookCephPrometheusFileConfig struct {
	URL string `json:"url,omitempty" yaml:"url,omitempty"`
}

type RookCephWritesFileConfig struct {
	Enabled                 *bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	AllowStorageClassDelete *bool `json:"allowStorageClassDelete,omitempty" yaml:"allowStorageClassDelete,omitempty"`
	AllowPoolDelete         *bool `json:"allowPoolDelete,omitempty" yaml:"allowPoolDelete,omitempty"`
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
	set("HIGHLAND_CLUSTER_IDENTITY", f.ClusterIdentity)
	if f.AdminPolicy != nil {
		if f.AdminPolicy.Enabled != nil && os.Getenv("HIGHLAND_ADMIN_POLICY_CONTROL_ENABLED") == "" {
			_ = os.Setenv("HIGHLAND_ADMIN_POLICY_CONTROL_ENABLED", fmt.Sprintf("%v", *f.AdminPolicy.Enabled))
		}
		if f.AdminPolicy.InstallStorageWriterPermissions != nil && os.Getenv("HIGHLAND_ADMIN_POLICY_INSTALL_WRITER_RBAC") == "" {
			_ = os.Setenv("HIGHLAND_ADMIN_POLICY_INSTALL_WRITER_RBAC", fmt.Sprintf("%v", *f.AdminPolicy.InstallStorageWriterPermissions))
		}
		if ceiling := f.AdminPolicy.Ceiling; ceiling != nil {
			setBoolEnv("HIGHLAND_POLICY_CEILING_PORTABLE_WRITES", ceiling.PortableKubernetesWrites)
			setBoolEnv("HIGHLAND_POLICY_CEILING_LONGHORN_WRITES", ceiling.LonghornWrites)
			setBoolEnv("HIGHLAND_POLICY_CEILING_ROOK_CEPH_WRITES", ceiling.RookCephWrites)
			setBoolEnv("HIGHLAND_POLICY_CEILING_CEPH_STORAGECLASS_DELETE", ceiling.AllowCephStorageClassDelete)
			setBoolEnv("HIGHLAND_POLICY_CEILING_CEPH_POOL_DELETE", ceiling.AllowCephPoolDelete)
		}
	}
	if f.Storage != nil {
		if f.Storage.Enabled != nil && os.Getenv("HIGHLAND_STORAGE_ENABLED") == "" {
			_ = os.Setenv("HIGHLAND_STORAGE_ENABLED", fmt.Sprintf("%v", *f.Storage.Enabled))
		}
		scope, namespaces := f.Storage.ScopeMode, f.Storage.Namespaces
		if f.Storage.Scope != nil {
			scope, namespaces = f.Storage.Scope.Mode, f.Storage.Scope.Namespaces
		}
		set("HIGHLAND_STORAGE_SCOPE", scope)
		set("HIGHLAND_STORAGE_NAMESPACES", strings.Join(namespaces, ","))
		if f.Storage.Writes != nil && f.Storage.Writes.Enabled != nil && os.Getenv("HIGHLAND_STORAGE_WRITES_ENABLED") == "" {
			_ = os.Setenv("HIGHLAND_STORAGE_WRITES_ENABLED", fmt.Sprintf("%v", *f.Storage.Writes.Enabled))
		}
		if f.Storage.Writes != nil && f.Storage.Writes.RecoveryEnabled != nil && os.Getenv("HIGHLAND_STORAGE_OPERATION_RECOVERY_ENABLED") == "" {
			_ = os.Setenv("HIGHLAND_STORAGE_OPERATION_RECOVERY_ENABLED", fmt.Sprintf("%v", *f.Storage.Writes.RecoveryEnabled))
		}
		if f.Storage.WritesEnabled != nil && os.Getenv("HIGHLAND_STORAGE_WRITES_ENABLED") == "" {
			_ = os.Setenv("HIGHLAND_STORAGE_WRITES_ENABLED", fmt.Sprintf("%v", *f.Storage.WritesEnabled))
		}
		set("HIGHLAND_REQUIRED_PROVIDERS", strings.Join(f.Storage.RequiredProviders, ","))
	}
	if f.Providers != nil && f.Providers.Longhorn != nil {
		p := f.Providers.Longhorn
		if p.Enabled != nil && os.Getenv("HIGHLAND_LONGHORN_ENABLED") == "" {
			_ = os.Setenv("HIGHLAND_LONGHORN_ENABLED", fmt.Sprintf("%v", *p.Enabled))
		}
		if p.Required != nil && os.Getenv("HIGHLAND_LONGHORN_REQUIRED") == "" {
			_ = os.Setenv("HIGHLAND_LONGHORN_REQUIRED", fmt.Sprintf("%v", *p.Required))
		}
		set("HIGHLAND_MANAGER_URL", p.ManagerURL)
		set("HIGHLAND_LONGHORN_NAMESPACE", p.Namespace)
	}
	if f.Providers != nil && f.Providers.OpenEBS != nil {
		p := f.Providers.OpenEBS
		if p.Enabled != nil && os.Getenv("HIGHLAND_OPENEBS_ENABLED") == "" {
			_ = os.Setenv("HIGHLAND_OPENEBS_ENABLED", fmt.Sprintf("%v", *p.Enabled))
		}
		set("HIGHLAND_OPENEBS_NAMESPACE", p.Namespace)
	}
	if f.Providers != nil && f.Providers.Linstor != nil {
		p := f.Providers.Linstor
		setBoolEnv("HIGHLAND_LINSTOR_ENABLED", p.Enabled)
		set("HIGHLAND_LINSTOR_NAMESPACE", p.Namespace)
		set("HIGHLAND_LINSTOR_CONTROLLER_URL", p.ControllerURL)
		set("HIGHLAND_LINSTOR_CA_FILE", p.CAFile)
		setBoolEnv("HIGHLAND_LINSTOR_INSECURE_TLS", p.InsecureTLS)
		setBoolEnv("HIGHLAND_LINSTOR_ALLOW_HTTP", p.AllowHTTP)
		set("HIGHLAND_LINSTOR_TIMEOUT", p.Timeout)
	}
	if f.Providers != nil && f.Providers.RookCeph != nil {
		p := f.Providers.RookCeph
		if p.Enabled != nil && os.Getenv("HIGHLAND_ROOK_CEPH_ENABLED") == "" {
			_ = os.Setenv("HIGHLAND_ROOK_CEPH_ENABLED", fmt.Sprintf("%v", *p.Enabled))
		}
		set("HIGHLAND_ROOK_CEPH_NAMESPACE", p.Namespace)
		set("HIGHLAND_ROOK_CEPH_CLUSTER_NAME", p.ClusterName)
		set("HIGHLAND_ROOK_CEPH_DASHBOARD_URL", p.DashboardURL)
		set("HIGHLAND_ROOK_CEPH_DASHBOARD_CA_FILE", p.DashboardCAFile)
		if p.DashboardInsecureTLS != nil && os.Getenv("HIGHLAND_ROOK_CEPH_DASHBOARD_INSECURE_TLS") == "" {
			_ = os.Setenv("HIGHLAND_ROOK_CEPH_DASHBOARD_INSECURE_TLS", fmt.Sprintf("%v", *p.DashboardInsecureTLS))
		}
		set("HIGHLAND_ROOK_CEPH_PROMETHEUS_URL", p.PrometheusURL)
		if p.Dashboard != nil {
			set("HIGHLAND_ROOK_CEPH_DASHBOARD_URL", p.Dashboard.URL)
			set("HIGHLAND_ROOK_CEPH_DASHBOARD_PUBLIC_URL", p.Dashboard.PublicURL)
			if p.Dashboard.AllowHTTP != nil && os.Getenv("HIGHLAND_ROOK_CEPH_DASHBOARD_ALLOW_HTTP") == "" {
				_ = os.Setenv("HIGHLAND_ROOK_CEPH_DASHBOARD_ALLOW_HTTP", fmt.Sprintf("%v", *p.Dashboard.AllowHTTP))
			}
			set("HIGHLAND_ROOK_CEPH_DASHBOARD_CA_FILE", p.Dashboard.CAFile)
			if p.Dashboard.InsecureSkipVerify != nil && os.Getenv("HIGHLAND_ROOK_CEPH_DASHBOARD_INSECURE_TLS") == "" {
				_ = os.Setenv("HIGHLAND_ROOK_CEPH_DASHBOARD_INSECURE_TLS", fmt.Sprintf("%v", *p.Dashboard.InsecureSkipVerify))
			}
			if p.Dashboard.CredentialReveal != nil && os.Getenv("HIGHLAND_ROOK_CEPH_CREDENTIAL_REVEAL_ENABLED") == "" {
				_ = os.Setenv("HIGHLAND_ROOK_CEPH_CREDENTIAL_REVEAL_ENABLED", fmt.Sprintf("%v", *p.Dashboard.CredentialReveal))
			}
			set("HIGHLAND_ROOK_CEPH_DASHBOARD_ADMIN_USERNAME", p.Dashboard.AdminUsername)
			set("HIGHLAND_ROOK_CEPH_DASHBOARD_ADMIN_SECRET", p.Dashboard.AdminSecret)
		}
		if p.Prometheus != nil {
			set("HIGHLAND_ROOK_CEPH_PROMETHEUS_URL", p.Prometheus.URL)
		}
		if p.Writes != nil {
			if p.Writes.Enabled != nil && os.Getenv("HIGHLAND_ROOK_CEPH_WRITES_ENABLED") == "" {
				_ = os.Setenv("HIGHLAND_ROOK_CEPH_WRITES_ENABLED", fmt.Sprintf("%v", *p.Writes.Enabled))
			}
			if p.Writes.AllowStorageClassDelete != nil && os.Getenv("HIGHLAND_ROOK_CEPH_ALLOW_STORAGE_CLASS_DELETE") == "" {
				_ = os.Setenv("HIGHLAND_ROOK_CEPH_ALLOW_STORAGE_CLASS_DELETE", fmt.Sprintf("%v", *p.Writes.AllowStorageClassDelete))
			}
			if p.Writes.AllowPoolDelete != nil && os.Getenv("HIGHLAND_ROOK_CEPH_ALLOW_POOL_DELETE") == "" {
				_ = os.Setenv("HIGHLAND_ROOK_CEPH_ALLOW_POOL_DELETE", fmt.Sprintf("%v", *p.Writes.AllowPoolDelete))
			}
		}
		if p.WritesEnabled != nil && os.Getenv("HIGHLAND_ROOK_CEPH_WRITES_ENABLED") == "" {
			_ = os.Setenv("HIGHLAND_ROOK_CEPH_WRITES_ENABLED", fmt.Sprintf("%v", *p.WritesEnabled))
		}
		if p.AllowPoolDelete != nil && os.Getenv("HIGHLAND_ROOK_CEPH_ALLOW_POOL_DELETE") == "" {
			_ = os.Setenv("HIGHLAND_ROOK_CEPH_ALLOW_POOL_DELETE", fmt.Sprintf("%v", *p.AllowPoolDelete))
		}
	}
	return nil
}

func setBoolEnv(key string, value *bool) {
	if value != nil && os.Getenv(key) == "" {
		_ = os.Setenv(key, fmt.Sprintf("%v", *value))
	}
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
