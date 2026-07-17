package insights

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestCapacityOwnershipNeverCombinesUnlikeMeasures(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	dimensions := CapacityDimensions{
		ProviderID: "rook-ceph", Driver: "rook-ceph.rbd.csi.ceph.com",
		StorageClass: "rook-ceph-block", Namespace: "apps",
		WorkloadKind: "StatefulSet", Workload: "db", ReclaimPolicy: "Delete", Pool: "replicapool",
	}
	observations := []CapacityObservation{
		{ID: "pvc-a", Measure: CapacityPVCRequested, Bytes: 10, Dimensions: dimensions, Evidence: authoritative(now)},
		{ID: "pvc-b", Measure: CapacityPVCRequested, Bytes: 20, Dimensions: dimensions, Evidence: authoritative(now)},
		{ID: "pv-a", Measure: CapacityPVProvisioned, Bytes: 40, Dimensions: dimensions, Evidence: authoritative(now)},
		{ID: "image-a", Measure: CapacityBackendLogical, Bytes: 50, Dimensions: dimensions, Evidence: authoritative(now)},
		{ID: "used-a", Measure: CapacityBackendAllocated, Bytes: 7, Dimensions: dimensions, Evidence: authoritative(now)},
		{ID: "usable", Measure: CapacityPoolUsable, Bytes: 100, Dimensions: dimensions, Evidence: authoritative(now)},
		{ID: "pool-raw", Measure: CapacityPoolRaw, Bytes: 300, Dimensions: dimensions, Evidence: authoritative(now)},
		{ID: "cluster-raw", Measure: CapacityClusterRaw, Bytes: 500, Dimensions: dimensions, Evidence: authoritative(now)},
	}
	ownership, err := (CapacityBuilder{}).Build(observations, CapacityOwnershipQuery{ProviderID: "rook-ceph"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ownership.Groups) != 7 {
		t.Fatalf("expected seven separate measurement groups, got %#v", ownership.Groups)
	}
	for _, group := range ownership.Groups {
		if len(group.Evidence) != 1 || group.Evidence[0] != EvidenceAuthoritative {
			t.Fatalf("group lost its ownership evidence: %#v", group)
		}
	}
	requested, err := ownership.Total(CapacityPVCRequested)
	if err != nil {
		t.Fatal(err)
	}
	if requested != 30 {
		t.Fatalf("requested total = %d, want 30", requested)
	}
	raw, _ := ownership.Total(CapacityClusterRaw)
	if raw != 500 {
		t.Fatalf("cluster raw total = %d, want 500", raw)
	}
	if _, err := ownership.Total("everything"); err == nil {
		t.Fatal("expected a total across an unnamed/invalid measure to be rejected")
	}
}

func TestCapacityOwnershipFiltersProviderNamespaceEvidenceAndCardinality(t *testing.T) {
	now := time.Now().UTC()
	base := CapacityDimensions{ProviderID: "rook-ceph", Namespace: "team-a"}
	observations := []CapacityObservation{
		{ID: "authoritative", Measure: CapacityPVCRequested, Bytes: 10, Dimensions: base, Evidence: authoritative(now)},
		{ID: "potential", Measure: CapacityPVCRequested, Bytes: 20, Dimensions: CapacityDimensions{ProviderID: "rook-ceph", Namespace: "team-b"}, Evidence: CapacityEvidence{Strength: EvidencePotential, ObservedAt: now}},
		{ID: "longhorn", Measure: CapacityPVCRequested, Bytes: 30, Dimensions: CapacityDimensions{ProviderID: "longhorn", Namespace: "team-a"}, Evidence: CapacityEvidence{Strength: EvidencePotential, ObservedAt: now}},
	}
	builder := CapacityBuilder{MaximumGroups: 2}
	ownership, err := builder.Build(observations, CapacityOwnershipQuery{
		ProviderID: "rook-ceph", Namespaces: []string{"team-a"}, AuthoritativeOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ownership.Groups) != 1 || ownership.Groups[0].Bytes != 10 || len(ownership.Conditions) != 0 {
		t.Fatalf("unexpected authoritative scoped ownership: %#v", ownership)
	}

	ownership, err = builder.Build(observations, CapacityOwnershipQuery{ProviderID: "rook-ceph"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ownership.Groups) != 2 || len(ownership.Conditions) != 1 ||
		ownership.Conditions[0].Code != "non-authoritative-attribution" {
		t.Fatalf("non-authoritative ownership was not disclosed: %#v", ownership)
	}
	if _, err := builder.Build(observations, CapacityOwnershipQuery{MaximumGroups: 3}); err == nil {
		t.Fatal("expected requested cardinality above the package bound to fail")
	}
}

func TestCapacityOwnershipRejectsInvalidInputAndOverflow(t *testing.T) {
	if _, err := (CapacityBuilder{}).Build([]CapacityObservation{{
		ID: "missing-provider", Measure: CapacityPVCRequested,
	}}, CapacityOwnershipQuery{}); err == nil || !strings.Contains(err.Error(), "no provider") {
		t.Fatalf("expected missing provider error, got %v", err)
	}
	dimensions := CapacityDimensions{ProviderID: "rook-ceph"}
	if _, err := (CapacityBuilder{}).Build([]CapacityObservation{
		{ID: "a", Measure: CapacityClusterRaw, Bytes: math.MaxUint64, Dimensions: dimensions},
		{ID: "b", Measure: CapacityClusterRaw, Bytes: 1, Dimensions: dimensions},
	}, CapacityOwnershipQuery{}); err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("expected overflow error, got %v", err)
	}
	if _, err := (CapacityBuilder{}).Build([]CapacityObservation{{
		ID: "bad-measure", Measure: "capacity", Dimensions: dimensions,
	}}, CapacityOwnershipQuery{}); err == nil {
		t.Fatal("expected invalid measure to fail")
	}
}

func TestEvaluateHeadroom(t *testing.T) {
	policy := HeadroomPolicy{WarningPercent: 75, CriticalPercent: 90, MinimumFreeBytes: 15}
	tests := []struct {
		name     string
		used     uint64
		capacity uint64
		want     PressureState
		wantErr  bool
	}{
		{name: "unknown", used: 0, capacity: 0, want: PressureUnknown},
		{name: "ok", used: 50, capacity: 100, want: PressureOK},
		{name: "warning", used: 80, capacity: 100, want: PressureWarning},
		{name: "minimum-free-critical", used: 86, capacity: 100, want: PressureCritical},
		{name: "over-capacity", used: 101, capacity: 100, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			headroom, err := EvaluateHeadroom(test.used, test.capacity, policy)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if headroom.State != test.want {
				t.Fatalf("state = %s, want %s (%#v)", headroom.State, test.want, headroom)
			}
		})
	}
	if _, err := EvaluateHeadroom(1, 100, HeadroomPolicy{WarningPercent: 95, CriticalPercent: 90}); err == nil {
		t.Fatal("expected inverted thresholds to fail")
	}
}

func TestForecastRequiresFreshSufficientHistory(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	policy := ForecastPolicy{
		MinimumSamples: 4, MinimumWindow: 3 * time.Hour,
		MaximumWindow: 24 * time.Hour, MaximumAge: 30 * time.Minute, MaximumSamples: 10,
	}
	forecast, err := Forecast("rook-ceph", CapacityBackendAllocated,
		[]MetricSample{{Timestamp: now, Bytes: 10}}, 24*time.Hour, now, policy)
	if err != nil {
		t.Fatal(err)
	}
	if forecast.Status != ForecastUnavailable || forecast.Conditions[0].Code != "insufficient-samples" {
		t.Fatalf("single observation fabricated a forecast: %#v", forecast)
	}

	stale := []MetricSample{
		{Timestamp: now.Add(-5 * time.Hour), Bytes: 10},
		{Timestamp: now.Add(-4 * time.Hour), Bytes: 20},
		{Timestamp: now.Add(-3 * time.Hour), Bytes: 30},
		{Timestamp: now.Add(-2 * time.Hour), Bytes: 40},
	}
	forecast, err = Forecast("rook-ceph", CapacityBackendAllocated, stale, time.Hour, now, policy)
	if err != nil {
		t.Fatal(err)
	}
	if forecast.Status != ForecastUnavailable || forecast.Conditions[0].Code != "stale-history" {
		t.Fatalf("stale history was accepted: %#v", forecast)
	}
}

func TestForecastLinearTrendCarriesEvidenceAndConfidence(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	samples := make([]MetricSample, 0, 8)
	for i := range 8 {
		samples = append(samples, MetricSample{
			Timestamp: now.Add(time.Duration(i-7) * time.Hour),
			Bytes:     uint64(100 + i*10),
		})
	}
	policy := ForecastPolicy{
		MinimumSamples: 4, MinimumWindow: 3 * time.Hour,
		MaximumWindow: 24 * time.Hour, MaximumAge: time.Minute, MaximumSamples: 20,
	}
	forecast, err := Forecast("rook-ceph", CapacityBackendAllocated, samples, 2*time.Hour, now, policy)
	if err != nil {
		t.Fatal(err)
	}
	if forecast.Status != ForecastAvailable {
		t.Fatalf("forecast unavailable: %#v", forecast)
	}
	if forecast.ProjectedBytes != 190 || forecast.SampleCount != 8 || forecast.Window != 7*time.Hour {
		t.Fatalf("unexpected projection evidence: %#v", forecast)
	}
	if forecast.RSquared < .999 || forecast.Confidence != ConfidenceHigh {
		t.Fatalf("unexpected confidence: %#v", forecast)
	}
	if len(forecast.Conditions) != 1 || forecast.Conditions[0].Code != "trend-not-guarantee" {
		t.Fatalf("forecast caveat missing: %#v", forecast.Conditions)
	}
}

func TestForecastBoundsAndDeduplicatesMetricSamples(t *testing.T) {
	now := time.Now().UTC()
	policy := ForecastPolicy{MinimumSamples: 2, MinimumWindow: time.Minute, MaximumWindow: time.Hour, MaximumAge: time.Hour, MaximumSamples: 2}
	if _, err := Forecast("rook-ceph", CapacityBackendAllocated, []MetricSample{
		{Timestamp: now.Add(-2 * time.Minute), Bytes: 1},
		{Timestamp: now.Add(-time.Minute), Bytes: 2},
		{Timestamp: now, Bytes: 3},
	}, time.Hour, now, policy); err == nil || !strings.Contains(err.Error(), "exceeds maximum") {
		t.Fatalf("expected sample query bound, got %v", err)
	}
	forecast, err := Forecast("rook-ceph", CapacityBackendAllocated, []MetricSample{
		{Timestamp: now.Add(-time.Minute), Bytes: 1},
		{Timestamp: now, Bytes: 2},
		{Timestamp: now, Bytes: 3},
	}, time.Hour, now, ForecastPolicy{
		MinimumSamples: 2, MinimumWindow: time.Minute, MaximumWindow: time.Hour,
		MaximumAge: time.Hour, MaximumSamples: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if forecast.SampleCount != 2 || forecast.CurrentBytes != 3 {
		t.Fatalf("duplicate timestamp handling is incorrect: %#v", forecast)
	}
}

func TestForecastRejectsFutureAndGappedHistory(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	base := ForecastPolicy{
		MinimumSamples: 3, MinimumWindow: 2 * time.Hour, MaximumWindow: 24 * time.Hour,
		MaximumAge: time.Hour, MaximumSamples: 10, MaximumGap: 90 * time.Minute,
		FutureTolerance: time.Minute,
	}
	future := []MetricSample{
		{Timestamp: now.Add(-2 * time.Hour), Bytes: 1},
		{Timestamp: now.Add(-time.Hour), Bytes: 2},
		{Timestamp: now.Add(2 * time.Minute), Bytes: 3},
	}
	forecast, err := Forecast("rook-ceph", CapacityBackendAllocated, future, time.Hour, now, base)
	if err != nil {
		t.Fatal(err)
	}
	if forecast.Status != ForecastUnavailable || forecast.Conditions[0].Code != "future-history" {
		t.Fatalf("future sample was accepted: %#v", forecast)
	}

	gapped := []MetricSample{
		{Timestamp: now.Add(-4 * time.Hour), Bytes: 1},
		{Timestamp: now.Add(-3 * time.Hour), Bytes: 2},
		{Timestamp: now, Bytes: 3},
	}
	forecast, err = Forecast("rook-ceph", CapacityBackendAllocated, gapped, time.Hour, now, base)
	if err != nil {
		t.Fatal(err)
	}
	if forecast.Status != ForecastUnavailable || forecast.Conditions[0].Code != "missing-history" {
		t.Fatalf("gapped history was accepted: %#v", forecast)
	}
}

func TestCapacityMeasurementDefinitionsAreCompleteAndDistinct(t *testing.T) {
	definitions := CapacityMeasurementDefinitions()
	if len(definitions) != 7 {
		t.Fatalf("got %d measurement definitions, want 7", len(definitions))
	}
	seen := map[CapacityMeasure]bool{}
	for _, definition := range definitions {
		if !definition.Measure.Valid() || definition.Description == "" || definition.Scope == "" {
			t.Fatalf("invalid measurement definition: %#v", definition)
		}
		if seen[definition.Measure] {
			t.Fatalf("duplicate measurement definition: %s", definition.Measure)
		}
		seen[definition.Measure] = true
	}
}

func authoritative(observedAt time.Time) CapacityEvidence {
	return CapacityEvidence{Strength: EvidenceAuthoritative, Source: "kubernetes", ObservedAt: observedAt}
}
