package config_test

import (
	"testing"

	"github.com/highland-io/highland/apps/api/internal/config"
)

// TestBootstrapStorageEnabledFlagsParse is a characterization test for the
// storage/provider enablement matrix used at process bootstrap. Defaults and
// env overrides must remain stable for fail-closed write gates.
func TestBootstrapStorageEnabledFlagsParse(t *testing.T) {
	// Clear related env so defaults are deterministic.
	keys := []string{
		"HIGHLAND_CONFIG_FILE",
		"HIGHLAND_STORAGE_ENABLED",
		"HIGHLAND_STORAGE_WRITES_ENABLED",
		"HIGHLAND_LONGHORN_ENABLED",
		"HIGHLAND_LONGHORN_REQUIRED",
		"HIGHLAND_OPENEBS_ENABLED",
		"HIGHLAND_LINSTOR_ENABLED",
		"HIGHLAND_ROOK_CEPH_ENABLED",
		"HIGHLAND_ROOK_CEPH_WRITES_ENABLED",
		"HIGHLAND_ROOK_CEPH_ALLOW_STORAGE_CLASS_DELETE",
		"HIGHLAND_ROOK_CEPH_ALLOW_POOL_DELETE",
		"HIGHLAND_ADMIN_USER",
		"HIGHLAND_ADMIN_PASSWORD",
	}
	for _, key := range keys {
		t.Setenv(key, "")
	}
	t.Setenv("HIGHLAND_ADMIN_USER", "admin")
	t.Setenv("HIGHLAND_ADMIN_PASSWORD", "test-password")

	t.Run("defaults", func(t *testing.T) {
		cfg, err := config.LoadFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.StorageEnabled {
			t.Fatal("StorageEnabled default should be true")
		}
		if cfg.StorageWritesEnabled {
			t.Fatal("StorageWritesEnabled default should be false (fail-closed writes)")
		}
		if !cfg.LonghornEnabled {
			t.Fatal("LonghornEnabled default should be true")
		}
		if cfg.OpenEBSEnabled || cfg.LinstorEnabled || cfg.RookCephEnabled {
			t.Fatalf("optional providers should default off: openebs=%v linstor=%v rook=%v",
				cfg.OpenEBSEnabled, cfg.LinstorEnabled, cfg.RookCephEnabled)
		}
		if cfg.RookCephWritesEnabled || cfg.RookCephAllowPoolDelete || cfg.RookCephAllowStorageClassDelete {
			t.Fatal("Rook/Ceph write gates should default off")
		}
	})

	t.Run("storage writes require storage enabled", func(t *testing.T) {
		t.Setenv("HIGHLAND_STORAGE_ENABLED", "false")
		t.Setenv("HIGHLAND_STORAGE_WRITES_ENABLED", "true")
		if _, err := config.LoadFromEnv(); err == nil {
			t.Fatal("expected error when writes enabled without storage")
		}
	})

	t.Run("explicit enable matrix", func(t *testing.T) {
		t.Setenv("HIGHLAND_STORAGE_ENABLED", "true")
		t.Setenv("HIGHLAND_STORAGE_WRITES_ENABLED", "true")
		t.Setenv("HIGHLAND_LONGHORN_ENABLED", "false")
		t.Setenv("HIGHLAND_OPENEBS_ENABLED", "true")
		t.Setenv("HIGHLAND_LINSTOR_ENABLED", "true")
		t.Setenv("HIGHLAND_ROOK_CEPH_ENABLED", "true")
		t.Setenv("HIGHLAND_ROOK_CEPH_WRITES_ENABLED", "true")
		t.Setenv("HIGHLAND_ROOK_CEPH_ALLOW_POOL_DELETE", "true")
		t.Setenv("HIGHLAND_ROOK_CEPH_ALLOW_STORAGE_CLASS_DELETE", "true")

		cfg, err := config.LoadFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.StorageEnabled || !cfg.StorageWritesEnabled {
			t.Fatalf("storage flags: enabled=%v writes=%v", cfg.StorageEnabled, cfg.StorageWritesEnabled)
		}
		if cfg.LonghornEnabled {
			t.Fatal("LonghornEnabled should honor false")
		}
		if !cfg.OpenEBSEnabled || !cfg.LinstorEnabled || !cfg.RookCephEnabled {
			t.Fatalf("optional providers not enabled: %#v", cfg)
		}
		if !cfg.RookCephWritesEnabled || !cfg.RookCephAllowPoolDelete || !cfg.RookCephAllowStorageClassDelete {
			t.Fatalf("ceph write gates: writes=%v poolDelete=%v scDelete=%v",
				cfg.RookCephWritesEnabled, cfg.RookCephAllowPoolDelete, cfg.RookCephAllowStorageClassDelete)
		}
	})

	t.Run("ceph writes require ceph enabled", func(t *testing.T) {
		t.Setenv("HIGHLAND_ROOK_CEPH_ENABLED", "false")
		t.Setenv("HIGHLAND_ROOK_CEPH_WRITES_ENABLED", "true")
		t.Setenv("HIGHLAND_ROOK_CEPH_ALLOW_POOL_DELETE", "false")
		t.Setenv("HIGHLAND_ROOK_CEPH_ALLOW_STORAGE_CLASS_DELETE", "false")
		if _, err := config.LoadFromEnv(); err == nil {
			t.Fatal("expected error when ceph writes enabled without rook-ceph")
		}
	})
}
