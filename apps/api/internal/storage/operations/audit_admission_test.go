package operations

import (
	"context"
	"errors"
	"testing"

	"github.com/highland-io/highland/apps/api/internal/audit"
)

// TestRequireAppendBlocksPrivilegedMutation proves the fail-closed admission
// helper used before StorageOperation creation (ADR-0004).
func TestRequireAppendBlocksPrivilegedMutation(t *testing.T) {
	fail := audit.NewFailingSink(audit.ErrUnavailable)
	err := audit.RequireAppend(context.Background(), fail, audit.Event{
		Action: "create_pvc_admit", Result: "ok", Username: "admin", Role: "admin",
	})
	if err == nil {
		t.Fatal("required audit admission must fail when sink is unavailable")
	}
	if !errors.Is(err, audit.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable wrap, got %v", err)
	}
}

func TestSharedAuditVisibleAcrossReplicaHandles(t *testing.T) {
	shared := audit.NewSharedMemorySink()
	// Two API replicas share one durable sink handle (Postgres in production).
	replicaA := shared
	replicaB := shared
	ctx := context.Background()
	if err := replicaA.Append(ctx, audit.Event{Action: "from-a", Result: "ok", ID: "1"}); err != nil {
		t.Fatal(err)
	}
	if err := replicaB.Append(ctx, audit.Event{Action: "from-b", Result: "ok", ID: "2"}); err != nil {
		t.Fatal(err)
	}
	page, err := replicaA.List(ctx, audit.Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 {
		t.Fatalf("expected cross-replica visibility of 2 events, got %d", len(page.Events))
	}
	// Durable flag must be true so admission paths engage.
	if !shared.Durable() {
		t.Fatal("shared durable sink must report Durable()")
	}
}

func TestMemorySinkDoesNotForceAdmission(t *testing.T) {
	// Non-durable memory store: submit path skips RequireAppend (see http.go).
	mem := audit.NewStore(10, "")
	if mem.Durable() {
		t.Fatal("memory ring must not be durable")
	}
}
