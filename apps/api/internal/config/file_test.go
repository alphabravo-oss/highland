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
