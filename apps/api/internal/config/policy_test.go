package config

import (
	"strings"
	"testing"
)

func TestAdminPolicyConfigurationDependencies(t *testing.T) {
	t.Setenv("HIGHLAND_CONFIG_FILE", "")
	t.Setenv("HIGHLAND_ADMIN_POLICY_CONTROL_ENABLED", "false")
	t.Setenv("HIGHLAND_ADMIN_POLICY_INSTALL_WRITER_RBAC", "true")
	if _, err := LoadFromEnv(); err == nil || !strings.Contains(err.Error(), "requires HIGHLAND_ADMIN_POLICY_CONTROL_ENABLED") {
		t.Fatalf("writer permission install dependency error=%v", err)
	}

	t.Setenv("HIGHLAND_ADMIN_POLICY_CONTROL_ENABLED", "true")
	t.Setenv("HIGHLAND_ADMIN_POLICY_INSTALL_WRITER_RBAC", "false")
	t.Setenv("HIGHLAND_POLICY_CEILING_LONGHORN_WRITES", "true")
	if _, err := LoadFromEnv(); err == nil || !strings.Contains(err.Error(), "policy write ceilings require") {
		t.Fatalf("ceiling install dependency error=%v", err)
	}

	t.Setenv("HIGHLAND_ADMIN_POLICY_INSTALL_WRITER_RBAC", "true")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AdminPolicyControlEnabled || !cfg.AdminPolicyInstallWriterRBAC || !cfg.PolicyCeilingLonghornWrites {
		t.Fatalf("valid policy config not loaded: %+v", cfg)
	}
}

func TestAdminPolicyCephChildCeilingsRequireParent(t *testing.T) {
	t.Setenv("HIGHLAND_CONFIG_FILE", "")
	t.Setenv("HIGHLAND_ADMIN_POLICY_CONTROL_ENABLED", "true")
	t.Setenv("HIGHLAND_ADMIN_POLICY_INSTALL_WRITER_RBAC", "true")
	t.Setenv("HIGHLAND_POLICY_CEILING_CEPH_POOL_DELETE", "true")
	t.Setenv("HIGHLAND_POLICY_CEILING_ROOK_CEPH_WRITES", "false")
	if _, err := LoadFromEnv(); err == nil || !strings.Contains(err.Error(), "requires the Rook/Ceph write ceiling") {
		t.Fatalf("Ceph child ceiling dependency error=%v", err)
	}
}
