package insights

import (
	"strings"
	"testing"
	"time"
)

func TestRemediationPrefersAvailableHighlandWorkflowAndNeverExecutes(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	builder := RemediationBuilder{Definitions: []RemediationDefinition{
		{
			ID: "expand-in-highland", ConditionCode: "pvc-near-capacity",
			Title: "Review PVC expansion", Explanation: "Highland can plan a guarded PVC expansion.",
			Surface: SurfaceHighland, HighlandActionID: "volume.expand",
			Prerequisites: []string{"StorageClass expansion is enabled"}, Risks: []string{"Filesystem growth may require workload support"},
			Escalation: EscalationOperator,
		},
		{
			ID: "review-runbook", ConditionCode: "pvc-near-capacity",
			Title: "Review capacity runbook", Explanation: "Use the reviewed capacity response procedure.",
			Surface: SurfaceRunbook, RunbookURL: "https://docs.example.test/storage/capacity",
			Prerequisites: []string{"Confirm workload ownership"}, Escalation: EscalationOperator,
		},
	}}
	result, err := builder.Build([]ObservedCondition{{
		Code: "pvc-near-capacity", ProviderID: "rook-ceph",
		Evidence: []RemediationEvidence{{
			Source: "prometheus", Strength: EvidenceAuthoritative, ObservedAt: now,
			Summary: "usable headroom is below policy",
		}},
	}}, RemediationContext{
		Profile:               VersionProfile{ProviderKind: "rook-ceph", ProviderVersion: "20.2.1"},
		AvailableCapabilities: []string{"volume.expand"}, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Recommendations) != 2 || result.Recommendations[0].Surface != SurfaceHighland {
		t.Fatalf("safe Highland workflow was not preferred: %#v", result.Recommendations)
	}
	recommendation := result.Recommendations[0]
	if !recommendation.ReadOnly || !recommendation.Fresh || recommendation.HighlandActionID != "volume.expand" {
		t.Fatalf("guidance lost safety/evidence metadata: %#v", recommendation)
	}
}

func TestRemediationDisablesUnavailableHighlandAction(t *testing.T) {
	now := time.Now().UTC()
	builder := RemediationBuilder{Definitions: []RemediationDefinition{{
		ID: "expand", ConditionCode: "pvc-near-capacity", Title: "Review expansion",
		Explanation: "A guarded workflow may be available.", Surface: SurfaceHighland,
		HighlandActionID: "volume.expand", Escalation: EscalationOperator,
	}}}
	result, err := builder.Build([]ObservedCondition{{
		Code: "pvc-near-capacity", ProviderID: "generic",
		Evidence: []RemediationEvidence{{Source: "kubernetes", ObservedAt: now, Summary: "claim pressure"}},
	}}, RemediationContext{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	recommendation := result.Recommendations[0]
	if recommendation.Surface != SurfaceObserveOnly || recommendation.HighlandActionID != "" {
		t.Fatalf("disabled action remained actionable: %#v", recommendation)
	}
	if len(recommendation.Risks) != 1 {
		t.Fatalf("disabled capability was not explained: %#v", recommendation)
	}
}

func TestRemediationRequiresCompatibilityReviewForNativeDestination(t *testing.T) {
	now := time.Now().UTC()
	builder := RemediationBuilder{Definitions: []RemediationDefinition{{
		ID: "inspect-osds", ConditionCode: "osd-degraded", Title: "Inspect OSD health",
		Explanation: "Highland cannot perform daemon recovery; inspect the native administration surface.",
		Surface:     SurfaceCephDashboard, DashboardDestination: "osds",
		RunbookURL:       "https://docs.example.test/ceph/osd-health",
		Prerequisites:    []string{"Authenticate separately to the Ceph Dashboard"},
		Risks:            []string{"Daemon administration can affect data availability"},
		Escalation:       EscalationStorage,
		ReviewedProfiles: []VersionProfile{{ProviderKind: "rook-ceph", ProviderVersion: "20.2.1", DashboardVersion: "20.2.1"}},
	}}}
	result, err := builder.Build([]ObservedCondition{{
		Code: "osd-degraded", ProviderID: "rook-ceph",
		Evidence: []RemediationEvidence{{Source: "ceph-health", ObservedAt: now.Add(-time.Hour), Summary: "OSD is down"}},
	}}, RemediationContext{
		Profile: VersionProfile{ProviderKind: "rook-ceph", ProviderVersion: "21.0.0", DashboardVersion: "21.0.0"},
		Now:     now, MaximumEvidenceAge: 15 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	recommendation := result.Recommendations[0]
	if recommendation.CompatibilityReviewed || recommendation.DashboardDestination != "" {
		t.Fatalf("unreviewed deep link remained enabled: %#v", recommendation)
	}
	if recommendation.Fresh {
		t.Fatal("stale evidence was marked fresh")
	}
	if !strings.Contains(strings.Join(recommendation.Risks, " "), "not been reviewed") {
		t.Fatalf("compatibility limitation was not disclosed: %#v", recommendation.Risks)
	}
}

func TestRemediationRejectsUnsafeOrDestructiveGuidance(t *testing.T) {
	tests := []RemediationDefinition{
		{
			ID: "unsafe-url", ConditionCode: "x", Title: "Runbook", Explanation: "Review",
			Surface: SurfaceRunbook, RunbookURL: "http://user:secret@example.test/runbook",
		},
		{
			ID: "destructive", ConditionCode: "x", Title: "Force delete it", Explanation: "Repair",
			Surface: SurfaceObserveOnly,
		},
		{
			ID: "raw-command", ConditionCode: "x", Title: "Repair", Explanation: "Run ceph osd down.",
			Surface: SurfaceCephCLI,
		},
	}
	for _, definition := range tests {
		t.Run(definition.ID, func(t *testing.T) {
			_, err := (RemediationBuilder{Definitions: []RemediationDefinition{definition}}).Build(nil, RemediationContext{})
			if err == nil {
				t.Fatalf("unsafe definition was accepted: %#v", definition)
			}
		})
	}
}

func TestRemediationBoundsAndReportsMissingGuidance(t *testing.T) {
	builder := RemediationBuilder{Definitions: []RemediationDefinition{
		{ID: "one", ConditionCode: "x", Title: "One", Explanation: "Observe", Surface: SurfaceObserveOnly},
		{ID: "two", ConditionCode: "x", Title: "Two", Explanation: "Escalate", Surface: SurfaceObserveOnly},
	}, MaximumResults: 1}
	evidence := []RemediationEvidence{{Source: "test", Summary: "condition observed", ObservedAt: time.Now()}}
	if _, err := builder.Build([]ObservedCondition{{Code: "x", ProviderID: "rook-ceph", Evidence: evidence}}, RemediationContext{}); err == nil {
		t.Fatal("expected result cardinality bound")
	}
	result, err := (RemediationBuilder{}).Build([]ObservedCondition{{Code: "unknown", ProviderID: "rook-ceph", Evidence: evidence}}, RemediationContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Conditions) != 1 || result.Conditions[0].Code != "guidance-unavailable" {
		t.Fatalf("missing guidance was silently omitted: %#v", result)
	}
}

func TestRemediationWithholdsGuidanceWithoutEvidence(t *testing.T) {
	builder := RemediationBuilder{Definitions: []RemediationDefinition{{
		ID: "observe", ConditionCode: "x", Title: "Observe", Explanation: "Review the evidence.",
		Surface: SurfaceObserveOnly,
	}}}
	result, err := builder.Build([]ObservedCondition{{Code: "x", ProviderID: "rook-ceph"}}, RemediationContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Recommendations) != 0 || len(result.Conditions) != 1 ||
		result.Conditions[0].Code != "insufficient-evidence" {
		t.Fatalf("guidance without evidence was emitted: %#v", result)
	}
}
