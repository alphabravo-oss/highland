package storage

import (
	"testing"
)

// Characterization: inventory package exports stable public surface used by HTTP
// and context engines. Refactors must keep these symbols (ENG-E0 / Phase 5).
func TestInventoryPublicSurfaceStable(t *testing.T) {
	scope := NewScope("cluster", nil)
	if scope.Mode != "cluster" && scope.Mode != "" {
		// Mode field may normalize; just ensure constructor does not panic.
		t.Logf("scope mode=%q", scope.Mode)
	}
	scopeNS := NewScope("namespaces", []string{"tenant-a"})
	_ = scopeNS
	reg := NewRegistry()
	if reg == nil {
		t.Fatal("NewRegistry must return non-nil")
	}
}
