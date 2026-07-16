package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/highland-io/highland/apps/api/internal/config"
)

func TestLoadFromConfigFileAndSecretEnv(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	content := `{
		"listenAddr": ":9090",
		"managerUrl": "http://longhorn-backend.longhorn-system:9500",
		"authMode": "local",
		"localAlways": true,
		"cookieSecure": false,
		"sessionTTL": "1h"
	}`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HIGHLAND_CONFIG_FILE", cfgPath)
	t.Setenv("HIGHLAND_ADMIN_USER", "admin")
	t.Setenv("HIGHLAND_ADMIN_PASSWORD", "from-k8s-secret")
	// Clear manager env so file wins
	t.Setenv("HIGHLAND_MANAGER_URL", "")
	t.Setenv("HIGHLAND_LISTEN_ADDR", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != ":9090" {
		t.Fatalf("listen %q", cfg.ListenAddr)
	}
	if cfg.ManagerURL != "http://longhorn-backend.longhorn-system:9500" {
		t.Fatalf("manager %q", cfg.ManagerURL)
	}
	if cfg.BootstrapPassword != "from-k8s-secret" {
		t.Fatalf("password should come from env secret ref")
	}
	if !cfg.LocalEnabled() {
		t.Fatal("local auth should be enabled")
	}
}

func TestNestedStorageProviderConfigAndEnvironmentPrecedence(t *testing.T) {
	for _, key := range []string{
		"HIGHLAND_STORAGE_ENABLED", "HIGHLAND_STORAGE_SCOPE", "HIGHLAND_STORAGE_NAMESPACES",
		"HIGHLAND_STORAGE_WRITES_ENABLED", "HIGHLAND_STORAGE_OPERATION_RECOVERY_ENABLED", "HIGHLAND_REQUIRED_PROVIDERS",
		"HIGHLAND_LONGHORN_ENABLED", "HIGHLAND_LONGHORN_REQUIRED", "HIGHLAND_LONGHORN_NAMESPACE",
		"HIGHLAND_ROOK_CEPH_ENABLED", "HIGHLAND_ROOK_CEPH_NAMESPACE", "HIGHLAND_ROOK_CEPH_CLUSTER_NAME",
		"HIGHLAND_ROOK_CEPH_DASHBOARD_URL", "HIGHLAND_ROOK_CEPH_DASHBOARD_PUBLIC_URL", "HIGHLAND_ROOK_CEPH_DASHBOARD_ALLOW_HTTP", "HIGHLAND_ROOK_CEPH_DASHBOARD_CA_FILE", "HIGHLAND_ROOK_CEPH_DASHBOARD_INSECURE_TLS",
		"HIGHLAND_ROOK_CEPH_PROMETHEUS_URL", "HIGHLAND_ROOK_CEPH_WRITES_ENABLED", "HIGHLAND_ROOK_CEPH_ALLOW_STORAGE_CLASS_DELETE", "HIGHLAND_ROOK_CEPH_ALLOW_POOL_DELETE",
	} {
		t.Setenv(key, "")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "storage.json")
	content := `{
      "managerUrl": "http://legacy-manager:9500",
      "storage": {
        "enabled": true,
        "scope": {"mode": "namespaces", "namespaces": ["team-a", "team-b"]},
        "writes": {"enabled": true, "recoveryEnabled": true},
        "requiredProviders": ["rook-ceph"]
      },
      "providers": {
        "longhorn": {"enabled": false, "required": false, "namespace": "storage-system"},
        "rookCeph": {
          "enabled": true, "namespace": "rook", "clusterName": "production",
		  "dashboard": {"url": "https://ceph.example", "publicUrl": "https://ceph-console.example", "allowHttp": false, "caFile": "/ca.crt", "insecureSkipVerify": false},
          "prometheus": {"url": "http://prometheus:9090"},
          "writes": {"enabled": true, "allowStorageClassDelete": true, "allowPoolDelete": false}
        }
      }
    }`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HIGHLAND_CONFIG_FILE", path)
	t.Setenv("HIGHLAND_ADMIN_USER", "admin")
	t.Setenv("HIGHLAND_ADMIN_PASSWORD", "secret")
	t.Setenv("HIGHLAND_ROOK_CEPH_DASHBOARD_USERNAME", "reader")
	t.Setenv("HIGHLAND_ROOK_CEPH_DASHBOARD_PASSWORD", "password")
	// Explicit environment policy wins over the mounted file.
	t.Setenv("HIGHLAND_STORAGE_WRITES_ENABLED", "false")

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StorageScopeMode != "namespaces" || len(cfg.StorageNamespaces) != 2 || cfg.StorageNamespaces[0] != "team-a" {
		t.Fatalf("scope was not loaded: %#v", cfg.StorageNamespaces)
	}
	if cfg.StorageWritesEnabled || !cfg.StorageOperationRecoveryEnabled {
		t.Fatalf("environment/file precedence failed: writes=%t recovery=%t", cfg.StorageWritesEnabled, cfg.StorageOperationRecoveryEnabled)
	}
	if cfg.LonghornEnabled || !cfg.RookCephEnabled || cfg.RookCephNamespace != "rook" || cfg.RookCephClusterName != "production" {
		t.Fatalf("provider config mismatch: %#v", cfg)
	}
	if cfg.RookCephDashboardURL != "https://ceph.example" || cfg.RookCephDashboardPublicURL != "https://ceph-console.example" || cfg.RookCephPrometheusURL != "http://prometheus:9090" || !cfg.RookCephWritesEnabled || cfg.RookCephAllowPoolDelete {
		t.Fatalf("nested Ceph config mismatch: %#v", cfg)
	}
	if !cfg.RookCephAllowStorageClassDelete {
		t.Fatal("nested StorageClass delete gate was not loaded")
	}
}
