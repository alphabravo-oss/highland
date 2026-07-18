package operations

import (
	"context"
	"errors"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"

	"github.com/highland-io/highland/apps/api/internal/storage"
)

// TestActionRegistrationExhaustive fails when any registered Action lacks
// planner coverage, audit metadata, feature-flag mapping, or provider-kind
// consistency (ENG-E2.10).
func TestActionRegistrationExhaustive(t *testing.T) {
	actions := Actions()
	if len(actions) == 0 {
		t.Fatal("Actions() returned no entries")
	}
	if len(actions) != len(actionRegistry) {
		t.Fatalf("Actions() len %d != actionRegistry len %d", len(actions), len(actionRegistry))
	}
	for key, action := range actionRegistry {
		if key != action.ID {
			t.Errorf("actionRegistry key %q != Action.ID %q", key, action.ID)
		}
	}

	seenIDs := map[string]struct{}{}
	seenAudit := map[string]string{}

	for _, action := range actions {
		id := action.ID
		if id == "" {
			t.Errorf("action registry entry has empty ID: %+v", action)
			continue
		}
		if got, ok := ActionByID(id); !ok || got.ID != id {
			t.Errorf("%s: ActionByID registry key/ID mismatch", id)
		}
		if _, dup := seenIDs[id]; dup {
			t.Errorf("duplicate action ID %q", id)
		}
		seenIDs[id] = struct{}{}

		// Stable identity + audit metadata
		if action.Capability == "" {
			t.Errorf("%s: Capability is empty", id)
		}
		if action.AuditAction == "" {
			t.Errorf("%s: AuditAction is empty", id)
		} else if other, ok := seenAudit[action.AuditAction]; ok && other != id {
			t.Errorf("%s: AuditAction %q collides with %s", id, action.AuditAction, other)
		} else {
			seenAudit[action.AuditAction] = id
		}

		// Feature flag / policy mapping
		if action.FeatureFlag == "" {
			t.Errorf("%s: FeatureFlag is empty", id)
		} else if _, ok := knownFeatureFlags[action.FeatureFlag]; !ok {
			t.Errorf("%s: FeatureFlag %q is not handled by actionEnabledByPolicy", id, action.FeatureFlag)
		}

		// Provider kind ↔ feature flag consistency
		switch action.ProviderKind {
		case "":
			if action.FeatureFlag != "storage.writes.enabled" {
				t.Errorf("%s: portable action should use storage.writes.enabled, got %q", id, action.FeatureFlag)
			}
		case "longhorn":
			if action.FeatureFlag != "storage.writes.enabled" {
				t.Errorf("%s: longhorn action should use storage.writes.enabled, got %q", id, action.FeatureFlag)
			}
			if !strings.HasPrefix(id, "longhorn-") {
				t.Errorf("%s: longhorn ProviderKind expects longhorn-* action ID", id)
			}
		case "rook-ceph":
			if !strings.HasPrefix(action.FeatureFlag, "providers.rookCeph.writes.") {
				t.Errorf("%s: rook-ceph action FeatureFlag %q is inconsistent", id, action.FeatureFlag)
			}
		default:
			t.Errorf("%s: unknown ProviderKind %q", id, action.ProviderKind)
		}

		// Risk / confirmation / role contract
		switch action.Risk {
		case RiskLow, RiskMedium, RiskHigh, RiskCritical:
		default:
			t.Errorf("%s: invalid Risk %q", id, action.Risk)
		}
		switch action.Confirmation {
		case ConfirmSummary, ConfirmTypedName:
		default:
			t.Errorf("%s: invalid Confirmation %q", id, action.Confirmation)
		}
		if action.MinimumRole != "operator" && action.MinimumRole != "admin" {
			t.Errorf("%s: invalid MinimumRole %q", id, action.MinimumRole)
		}
		if len(action.PreflightChecks) == 0 {
			t.Errorf("%s: PreflightChecks is empty", id)
		}

		// Planner support (checklist and/or registered ActionPlanner)
		if !HasPlannerSupport(id) {
			t.Errorf("%s: missing planner support — add to plannerDispatchIDs and Plan() switch (or RegisterActionPlanner)", id)
		}

		// Parameter allowlist entry must exist (empty map is valid for no-params actions)
		if _, ok := actionParameterAllowlist[id]; !ok {
			t.Errorf("%s: missing actionParameterAllowlist entry", id)
		}

		// Target kind mapping is non-empty for every known action
		if kind := targetKindForAction(id); kind == "" {
			t.Errorf("%s: targetKindForAction returned empty", id)
		}
	}

	// Checklist must not list IDs that are not registered actions
	for id := range plannerDispatchIDs {
		if _, ok := seenIDs[id]; !ok {
			t.Errorf("plannerDispatchIDs lists %q which is not in Actions()", id)
		}
	}
	// Every registered ActionPlanner must target a known action
	for id := range actionPlanners {
		if _, ok := seenIDs[id]; !ok {
			t.Errorf("actionPlanners has entry %q which is not in Actions()", id)
		}
	}
	// Parameter allowlist must not drift ahead of Actions()
	for id := range actionParameterAllowlist {
		if _, ok := seenIDs[id]; !ok {
			t.Errorf("actionParameterAllowlist has entry %q which is not in Actions()", id)
		}
	}

	if len(seenIDs) != len(plannerDispatchIDs) {
		t.Errorf("Actions() count %d != plannerDispatchIDs count %d", len(seenIDs), len(plannerDispatchIDs))
	}
}

// TestPlanDoesNotReportUnsupportedForRegisteredActions ensures Plan resolves
// every registered action ID (never ACTION_NOT_SUPPORTED). Preflight failures
// for missing cluster state are expected and ignored.
func TestPlanDoesNotReportUnsupportedForRegisteredActions(t *testing.T) {
	core := kubernetesfake.NewSimpleClientset()
	dyn := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	planner, err := NewPlanner(PlannerConfig{
		Core: core, Dynamic: dyn, Scope: storage.NewScope("cluster", nil),
		Secret:   []byte("0123456789abcdef0123456789abcdef"),
		Longhorn: &longhornClientStub{gets: map[string]map[string]any{}, lists: map[string][]map[string]any{}},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, action := range Actions() {
		action := action
		t.Run(action.ID, func(t *testing.T) {
			req := Request{
				ActionID:   action.ID,
				ProviderID: action.ProviderKind,
				Target: ResourceTarget{
					Kind:      targetKindForAction(action.ID),
					Name:      "exhaustive-probe",
					Namespace: "default",
				},
				Parameters: map[string]any{},
			}
			_, planErr := planner.Plan(context.Background(), "exhaustive-tester", req)
			if planErr == nil {
				// Some paths may succeed with empty fixtures; that is fine.
				return
			}
			var pe *PlanError
			if errors.As(planErr, &pe) {
				switch pe.Code {
				case "ACTION_NOT_SUPPORTED", "ACTION_PLANNER_MISSING":
					t.Fatalf("Plan returned %s for registered action %s: %s", pe.Code, action.ID, pe.Message)
				}
				return
			}
			// Non-PlanError failures (client quirks) still mean the action was dispatched.
		})
	}
}

func TestHasPlannerSupportReflectsRegistry(t *testing.T) {
	if HasPlannerSupport("definitely-not-an-action") {
		t.Fatal("unknown action should lack planner support")
	}
	if !HasPlannerSupport("create-pvc") {
		t.Fatal("create-pvc must be in plannerDispatchIDs")
	}
}
