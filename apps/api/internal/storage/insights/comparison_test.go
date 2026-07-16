package insights

import (
	"strings"
	"testing"
	"time"
)

func TestComparisonAssessesFactsWithoutOpaqueScore(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	evidence := ComparisonEvidence{Source: "compatibility-matrix", Strength: EvidenceAuthoritative, ObservedAt: now}
	candidates := []PlacementCandidate{
		{
			ProviderID: "rook-ceph", ProviderName: "Rook / Ceph", StorageClass: "rook-ceph-block",
			SupportLevel: ComparisonManaged,
			Profile:      TestedProfile{ProviderKind: "rook-ceph", ProviderVersion: "20.2.1", Driver: "rook-ceph.rbd.csi.ceph.com", DriverVersion: "3.15"},
			Health:       &HealthFact{Status: "healthy", Evidence: evidence},
			Capabilities: []CapabilityFact{
				{ID: "snapshot.create", State: FactSupported, Verified: true, Evidence: evidence},
				{ID: "volume.expand", State: FactSupported, Verified: true, Evidence: evidence},
				{ID: "volume.encryption", State: FactUnsupported, Evidence: evidence},
			},
			AccessModes: []string{"ReadWriteOnce"}, TopologyKeys: []string{"topology.kubernetes.io/zone"},
			Headroom: &HeadroomFact{Percent: 42, Evidence: evidence},
		},
		{
			ProviderID: "generic", ProviderName: "Unknown CSI", StorageClass: "generic",
			SupportLevel: ComparisonDetected,
			Profile:      TestedProfile{ProviderKind: "csi", Driver: "csi.example.test"},
			Capabilities: []CapabilityFact{{ID: "snapshot.create", State: FactUnknown, Evidence: ComparisonEvidence{Source: "discovery"}}},
		},
	}
	comparison, err := (ComparisonBuilder{}).Build(candidates, PlacementPolicy{
		RequiredAccessMode: "ReadWriteOnce", RequiredTopology: []string{"topology.kubernetes.io/zone"},
		RequireSnapshot: true, RequireExpansion: true, RequireHealthy: true,
		MinimumHeadroom: 20, MinimumSupport: ComparisonVerified,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(comparison.Assessments) != 2 {
		t.Fatalf("unexpected assessment count: %#v", comparison)
	}
	if comparison.Assessments[0].Candidate.ProviderID != "rook-ceph" ||
		comparison.Assessments[0].Eligibility != "eligible" {
		t.Fatalf("eligible candidate was not surfaced first: %#v", comparison.Assessments)
	}
	if comparison.Assessments[1].Eligibility != "ineligible" {
		t.Fatalf("detected candidate should fail the minimum support requirement: %#v", comparison.Assessments[1])
	}
	if len(comparison.Assessments[0].Criteria) != 8 {
		t.Fatalf("expected contributing facts instead of a score: %#v", comparison.Assessments[0])
	}
	if comparison.Assessments[0].Candidate.Profile.ProviderVersion != "20.2.1" {
		t.Fatal("tested version profile was lost")
	}
}

func TestComparisonTreatsStaleOrMissingEvidenceAsUnknown(t *testing.T) {
	now := time.Now().UTC()
	candidate := PlacementCandidate{
		ProviderID: "rook-ceph", StorageClass: "rook-ceph-block",
		SupportLevel: ComparisonManaged,
		Profile: TestedProfile{
			ProviderKind: "rook-ceph", ProviderVersion: "20.2.1",
			Driver: "rook-ceph.rbd.csi.ceph.com", DriverVersion: "3.15",
		},
		Capabilities: []CapabilityFact{{
			ID: "snapshot.create", State: FactSupported,
			Evidence: ComparisonEvidence{Source: "snapshot-api", ObservedAt: now.Add(-time.Hour), Stale: true},
		}},
		Health: &HealthFact{Status: "healthy", Evidence: ComparisonEvidence{Source: "dashboard", Stale: true}},
	}
	comparison, err := (ComparisonBuilder{}).Build([]PlacementCandidate{candidate}, PlacementPolicy{
		RequireSnapshot: true, RequireHealthy: true, MinimumHeadroom: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	assessment := comparison.Assessments[0]
	if assessment.Eligibility != "unknown" {
		t.Fatalf("stale/missing facts must not produce an eligible recommendation: %#v", assessment)
	}
	for _, criterion := range assessment.Criteria {
		if criterion.Criterion == "tested-profile" {
			continue
		}
		if criterion.State != FactUnknown {
			t.Fatalf("criterion %s falsely claimed support: %#v", criterion.Criterion, criterion)
		}
	}
}

func TestComparisonKeepsSemanticallyDifferentMetricsSeparate(t *testing.T) {
	now := time.Now().UTC()
	candidates := []PlacementCandidate{
		{
			ProviderID: "rook-ceph", StorageClass: "rbd", SupportLevel: ComparisonManaged,
			Profile:    TestedProfile{ProviderKind: "rook-ceph", ProviderVersion: "20.2.1", Driver: "rbd.csi.ceph.com", DriverVersion: "3.15"},
			Benchmarks: []BenchmarkFact{{Semantic: "random-read-iops", Unit: "iops", Method: "fio", Profile: "4k-qd32", Value: 100, Evidence: ComparisonEvidence{ObservedAt: now}}},
		},
		{
			ProviderID: "longhorn", StorageClass: "longhorn", SupportLevel: ComparisonManaged,
			Profile:    TestedProfile{ProviderKind: "longhorn", ProviderVersion: "1.9", Driver: "driver.longhorn.io", DriverVersion: "1.9"},
			Benchmarks: []BenchmarkFact{{Semantic: "random-read-iops", Unit: "ops/s", Method: "provider-counter", Profile: "unspecified", Value: 200, Evidence: ComparisonEvidence{ObservedAt: now}}},
		},
	}
	comparison, err := (ComparisonBuilder{}).Build(candidates, PlacementPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if len(comparison.Conditions) != 1 || comparison.Conditions[0].Code != "non-comparable-benchmarks" {
		t.Fatalf("semantic mismatch was not disclosed: %#v", comparison.Conditions)
	}
	for _, assessment := range comparison.Assessments {
		if assessment.Eligibility != "unknown" {
			t.Fatalf("empty policy should not rank candidates: %#v", assessment)
		}
	}
}

func TestComparisonRequiresVerifiedCapabilityAndExactTestedProfile(t *testing.T) {
	now := time.Now().UTC()
	candidate := PlacementCandidate{
		ProviderID: "rook-ceph", StorageClass: "rbd", SupportLevel: ComparisonManaged,
		Profile: TestedProfile{ProviderKind: "rook-ceph", Driver: "rbd.csi.ceph.com"},
		Capabilities: []CapabilityFact{{
			ID: "snapshot.create", State: FactSupported, Verified: false,
			Evidence: ComparisonEvidence{Source: "discovery", ObservedAt: now},
		}},
	}
	comparison, err := (ComparisonBuilder{}).Build([]PlacementCandidate{candidate}, PlacementPolicy{RequireSnapshot: true})
	if err != nil {
		t.Fatal(err)
	}
	assessment := comparison.Assessments[0]
	if assessment.Eligibility != "unknown" {
		t.Fatalf("unverified capability or incomplete profile became a recommendation: %#v", assessment)
	}
	for _, criterion := range assessment.Criteria {
		if criterion.State == FactSupported {
			t.Fatalf("incomplete evidence was marked supported: %#v", criterion)
		}
	}
}

func TestComparisonBoundsAndValidatesPolicy(t *testing.T) {
	candidates := []PlacementCandidate{
		{ProviderID: "a", StorageClass: "a"},
		{ProviderID: "b", StorageClass: "b"},
	}
	if _, err := (ComparisonBuilder{MaximumCandidates: 1}).Build(candidates, PlacementPolicy{}); err == nil ||
		!strings.Contains(err.Error(), "exceeding maximum") {
		t.Fatalf("expected candidate bound, got %v", err)
	}
	if _, err := (ComparisonBuilder{}).Build(candidates[:1], PlacementPolicy{MinimumHeadroom: 101}); err == nil {
		t.Fatal("expected invalid headroom policy to fail")
	}
	if _, err := (ComparisonBuilder{}).Build([]PlacementCandidate{{StorageClass: "missing-provider"}}, PlacementPolicy{}); err == nil {
		t.Fatal("expected missing candidate identity to fail")
	}
}
