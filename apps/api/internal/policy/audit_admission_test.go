package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/highland-io/highland/apps/api/internal/audit"
	"github.com/highland-io/highland/apps/api/internal/auth"
)

// TestPolicyApplyFailClosedWhenDurableAuditUnavailable drives the real
// PUT /api/v1/admin/storage-policy path with a durable failing audit sink.
// store.Update must never run (ADR-0004 DEC-3).
func TestPolicyApplyFailClosedWhenDurableAuditUnavailable(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	store := &policyStoreStub{enabled: true, snapshot: Snapshot{
		Requested: StoragePolicy{}, Effective: StoragePolicy{},
		Ceiling: Ceiling{LonghornWrites: true}, Source: "runtime-policy",
		Generation: 1, ObservedGeneration: 1, ResourceVersion: "rv-1", ObservedAt: now,
	}}
	failAudit := audit.NewFailingSink(audit.ErrUnavailable)
	if !failAudit.Durable() {
		t.Fatal("failing sink must be durable")
	}
	api, err := NewAPI(APIConfig{
		Store: store, Audit: failAudit, Secret: bytes.Repeat([]byte("x"), 32),
		ClusterIdentity: "lab-cluster", Now: func() time.Time { return now },
		ImpactResolver: func(_, _ StoragePolicy) Impact {
			return Impact{ActionIDs: []string{"longhorn-volume-attach"}, Roles: []string{"operator"}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	router, sessions := policyTestRouter(t, api)
	adminCookie := policySession(t, sessions, auth.User{Username: "admin", Role: auth.RoleAdmin})

	request := ChangeRequest{
		Policy: StoragePolicy{AcceptNewOperations: true, LonghornWrites: true}, ResourceVersion: "rv-1",
	}
	planned := policyRequest(t, router, adminCookie, http.MethodPost, "/api/v1/admin/storage-policy/plans", request)
	if planned.Code != http.StatusOK {
		t.Fatalf("plan status=%d body=%s", planned.Code, planned.Body.String())
	}
	var plan Plan
	if err := json.Unmarshal(planned.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	request.Confirmation = Confirmation{
		Challenge: plan.Challenge, ClusterIdentity: "lab-cluster",
		EnablePhrase: "ENABLE STORAGE CHANGES", ImpactAcknowledged: true,
	}
	applied := policyRequest(t, router, adminCookie, http.MethodPut, "/api/v1/admin/storage-policy", request)
	if applied.Code != http.StatusServiceUnavailable || !bytes.Contains(applied.Body.Bytes(), []byte("AUDIT_REQUIRED_UNAVAILABLE")) {
		t.Fatalf("expected AUDIT_REQUIRED_UNAVAILABLE, status=%d body=%s", applied.Code, applied.Body.String())
	}
	// Snapshot resource version must be unchanged (Update never applied).
	if store.Snapshot().ResourceVersion != "rv-1" {
		t.Fatalf("policy store was mutated under audit outage: rv=%s", store.Snapshot().ResourceVersion)
	}
}

func TestPolicyHistoryUsesSharedSinkList(t *testing.T) {
	shared := audit.NewSharedMemorySink()
	ctx := context.Background()
	if err := shared.Append(ctx, audit.Event{Action: "policy_change_applied", Result: "ok", ID: "p1", Username: "admin"}); err != nil {
		t.Fatal(err)
	}
	if err := shared.Append(ctx, audit.Event{Action: "login", Result: "ok", ID: "l1"}); err != nil {
		t.Fatal(err)
	}
	store := &policyStoreStub{enabled: true, snapshot: Snapshot{Source: "runtime-policy", ResourceVersion: "rv-1"}}
	api, err := NewAPI(APIConfig{
		Store: store, Audit: shared, Secret: bytes.Repeat([]byte("x"), 32), ClusterIdentity: "lab",
	})
	if err != nil {
		t.Fatal(err)
	}
	router, sessions := policyTestRouter(t, api)
	adminCookie := policySession(t, sessions, auth.User{Username: "admin", Role: auth.RoleAdmin})
	history := policyRequest(t, router, adminCookie, http.MethodGet, "/api/v1/admin/storage-policy/history", nil)
	if history.Code != http.StatusOK || !bytes.Contains(history.Body.Bytes(), []byte("policy_change_applied")) {
		t.Fatalf("history status=%d body=%s", history.Code, history.Body.String())
	}
	if bytes.Contains(history.Body.Bytes(), []byte(`"action":"login"`)) {
		t.Fatal("history must filter to policy_change_ events only")
	}
}
