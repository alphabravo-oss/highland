package insights

import (
	"strings"
	"testing"
	"time"
)

type resolverFunc func(ResourceIdentity) Attribution

func (f resolverFunc) ResolveProvider(resource ResourceIdentity) Attribution {
	return f(resource)
}

func TestNormalizeKubernetesEventsRequiresAuthoritativeProviderCorrelation(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	events := []KubernetesEvent{
		{
			UID: "event-resolved", Namespace: "apps", Type: "Warning",
			Reason: "FailedAttachVolume", Message: "attach failed",
			Regarding: ResourceIdentity{Kind: "PersistentVolumeClaim", Namespace: "apps", Name: "data", UID: "pvc-1"},
			Count:     3, FirstObservedAt: now.Add(-time.Minute), LastObservedAt: now, CollectionTime: now,
		},
		{
			UID: "event-derived", Namespace: "apps", Type: "Warning",
			Reason: "ProvisioningFailed", Message: "looks like storage but is not exact",
			Regarding:      ResourceIdentity{Kind: "PersistentVolumeClaim", Namespace: "apps", Name: "maybe", UID: "pvc-2"},
			CollectionTime: now,
		},
		{
			UID: "event-unrelated", Namespace: "apps", Type: "Warning",
			Reason: "FailedMount", Message: "storage-shaped event from another provider",
			Regarding:      ResourceIdentity{Kind: "Pod", Namespace: "apps", Name: "database", UID: "pod-other"},
			CollectionTime: now,
		},
	}
	resolver := resolverFunc(func(resource ResourceIdentity) Attribution {
		switch resource.UID {
		case "pvc-1":
			return Attribution{ProviderID: "rook-ceph", Evidence: EvidenceAuthoritative, Reason: "PVC -> PV -> CSI driver"}
		case "pvc-2":
			return Attribution{ProviderID: "rook-ceph", Evidence: EvidenceDerived, Reason: "name heuristic"}
		default:
			return Attribution{ProviderID: "longhorn", Evidence: EvidenceAuthoritative, Reason: "exact pod graph"}
		}
	})

	observations := NormalizeKubernetesEvents(events, resolver)
	timeline, err := (TimelineBuilder{}).Build(observations, TimelineFilter{ProviderID: "rook-ceph"})
	if err != nil {
		t.Fatal(err)
	}
	if timeline.Total != 1 {
		t.Fatalf("expected only the authoritative Rook event, got %#v", timeline.Entries)
	}
	entry := timeline.Entries[0]
	if entry.ID != "event-resolved" || entry.Count != 3 {
		t.Fatalf("unexpected normalized entry: %#v", entry)
	}
	if entry.Attribution.Evidence != EvidenceAuthoritative {
		t.Fatalf("provider event attribution was not preserved: %#v", entry.Attribution)
	}
	if observations[1].ProviderID != "" {
		t.Fatalf("derived Kubernetes attribution must be discarded, got %#v", observations[1].Attribution)
	}
}

func TestTimelineDeduplicatesKubernetesRelistsWithoutInflatingCount(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	resource := ResourceIdentity{Kind: "PersistentVolumeClaim", Namespace: "apps", Name: "data", UID: "pvc-1"}
	resolver := resolverFunc(func(ResourceIdentity) Attribution {
		return Attribution{ProviderID: "rook-ceph", Evidence: EvidenceAuthoritative}
	})
	observations := NormalizeKubernetesEvents([]KubernetesEvent{
		{
			UID: "event-1", Namespace: "apps", Type: "Warning", Reason: "ProvisioningFailed",
			Message: "first observation", Regarding: resource, Count: 2,
			FirstObservedAt: now.Add(-5 * time.Minute), LastObservedAt: now.Add(-2 * time.Minute),
			CollectionTime: now.Add(-time.Minute), GraphLink: "/graph/pvc-1",
		},
		{
			UID: "event-1", Namespace: "apps", Type: "Warning", Reason: "ProvisioningFailed",
			Message: "latest observation", Regarding: resource, Count: 5,
			FirstObservedAt: now.Add(-5 * time.Minute), LastObservedAt: now,
			CollectionTime: now, GraphLink: "/graph/pvc-1",
		},
	}, resolver)

	timeline, err := (TimelineBuilder{}).Build(observations, TimelineFilter{ProviderID: "rook-ceph"})
	if err != nil {
		t.Fatal(err)
	}
	if timeline.Total != 1 {
		t.Fatalf("expected one deduplicated event, got %d", timeline.Total)
	}
	entry := timeline.Entries[0]
	if entry.Count != 5 {
		t.Fatalf("cumulative Kubernetes count was inflated: got %d, want 5", entry.Count)
	}
	if !entry.FirstOccurredAt.Equal(now.Add(-5*time.Minute)) || !entry.LastOccurredAt.Equal(now) {
		t.Fatalf("occurrence range was not preserved: %#v", entry)
	}
	if entry.Message != "latest observation" || len(entry.Links) != 1 {
		t.Fatalf("latest details or link deduplication incorrect: %#v", entry)
	}
}

func TestTimelineFiltersSortsBoundsAndAnnotatesOrdering(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	observations := []TimelineObservation{
		{
			ID: "old", ProviderID: "rook-ceph", Namespace: "apps",
			Severity: TimelineInfo, Source: SourceOperation, Action: "create",
			LastOccurredAt: now.Add(-time.Hour), ObservedAt: now.Add(-time.Hour),
			Attribution: Attribution{ProviderID: "rook-ceph", Evidence: EvidenceAuthoritative},
		},
		{
			ID: "new", ProviderID: "rook-ceph", Namespace: "apps",
			Severity: TimelineError, Source: SourceCephHealth, Action: "health-change",
			LastOccurredAt: now.Add(-time.Minute), ObservedAt: now,
			Attribution: Attribution{ProviderID: "rook-ceph", Evidence: EvidenceAuthoritative},
		},
		{
			ID: "unknown-time", ProviderID: "rook-ceph", Namespace: "other",
			Severity: TimelineWarning, Source: SourceProvider,
			ObservedAt: now, Attribution: Attribution{ProviderID: "rook-ceph", Evidence: EvidenceAuthoritative},
		},
	}
	builder := TimelineBuilder{MaximumEntries: 2, ClockSkewLimit: 30 * time.Second}
	timeline, err := builder.Build(observations, TimelineFilter{
		ProviderID: "rook-ceph", Namespaces: []string{"apps"},
		Severities: []TimelineSeverity{TimelineInfo, TimelineError}, Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if timeline.Total != 2 || !timeline.Truncated || len(timeline.Entries) != 1 || timeline.Entries[0].ID != "new" {
		t.Fatalf("unexpected filtered timeline: %#v", timeline)
	}
	if timeline.Entries[0].Ordering != OrderingSkewed || timeline.Entries[0].ClockSkew != time.Minute {
		t.Fatalf("clock skew was not annotated: %#v", timeline.Entries[0])
	}
	if _, err := builder.Build(observations, TimelineFilter{Limit: 3}); err == nil || !strings.Contains(err.Error(), "exceeds maximum") {
		t.Fatalf("expected bounded limit error, got %v", err)
	}
	if _, err := builder.Build(observations, TimelineFilter{Since: now, Until: now.Add(-time.Minute)}); err == nil {
		t.Fatal("expected invalid time range to fail")
	}
}

func TestTimelineProviderFilterRejectsUnexplainedAttribution(t *testing.T) {
	observations := []TimelineObservation{
		{ID: "unexplained", ProviderID: "rook-ceph", Source: SourceProvider, Attribution: Attribution{Evidence: EvidenceUnknown}},
		{ID: "potential", ProviderID: "rook-ceph", Source: SourceCephHealth, Attribution: Attribution{ProviderID: "rook-ceph", Evidence: EvidencePotential}},
		{ID: "native", ProviderID: "rook-ceph", Source: SourceCephHealth, Attribution: Attribution{ProviderID: "rook-ceph", Evidence: EvidenceDerived}},
	}
	timeline, err := (TimelineBuilder{}).Build(observations, TimelineFilter{ProviderID: "rook-ceph"})
	if err != nil {
		t.Fatal(err)
	}
	if timeline.Total != 1 || timeline.Entries[0].ID != "native" {
		t.Fatalf("provider filter admitted untrusted entries: %#v", timeline.Entries)
	}
}

func TestNormalizeProviderRecordsCoversSourcesAndRetention(t *testing.T) {
	now := time.Now().UTC()
	sources := []struct {
		source    TimelineSource
		retention RetentionClass
	}{
		{SourceRookCondition, RetentionTransient},
		{SourceCephHealth, RetentionTransient},
		{SourceProvider, RetentionTransient},
		{SourceOperation, RetentionDurable},
		{SourceAudit, RetentionAudit},
		{SourceConfiguration, RetentionAudit},
	}
	records := make([]ProviderTimelineRecord, 0, len(sources))
	for _, source := range sources {
		records = append(records, ProviderTimelineRecord{
			ID: string(source.source), ProviderID: "rook-ceph", Source: source.source,
			Severity: TimelineInfo, LastOccurredAt: now, ObservedAt: now,
			AttributionReason: "direct provider-scoped source",
		})
	}
	observations, err := NormalizeProviderRecords(records)
	if err != nil {
		t.Fatal(err)
	}
	timeline, err := (TimelineBuilder{}).Build(observations, TimelineFilter{ProviderID: "rook-ceph"})
	if err != nil {
		t.Fatal(err)
	}
	if timeline.Total != len(sources) {
		t.Fatalf("got %d records, want %d", timeline.Total, len(sources))
	}
	retention := map[TimelineSource]RetentionClass{}
	for _, entry := range timeline.Entries {
		retention[entry.Source] = entry.Retention
		if entry.Attribution.Evidence != EvidenceAuthoritative {
			t.Fatalf("direct source attribution is not authoritative: %#v", entry)
		}
	}
	for _, source := range sources {
		if retention[source.source] != source.retention {
			t.Fatalf("%s retention = %s, want %s", source.source, retention[source.source], source.retention)
		}
	}
	if _, err := NormalizeProviderRecords([]ProviderTimelineRecord{{
		ID: "bypass", ProviderID: "rook-ceph", Source: SourceKubernetesEvent,
	}}); err == nil {
		t.Fatal("Kubernetes Event bypassed authoritative object correlation")
	}
	if _, err := NormalizeProviderRecords([]ProviderTimelineRecord{{
		ID: "unscoped", Source: SourceCephHealth,
	}}); err == nil {
		t.Fatal("provider-native record without a provider was accepted")
	}
}

func TestTimelineBoundsInputCardinality(t *testing.T) {
	observations := []TimelineObservation{
		{ID: "one", Source: SourceAudit},
		{ID: "two", Source: SourceAudit},
	}
	if _, err := (TimelineBuilder{MaximumObservations: 1}).Build(observations, TimelineFilter{}); err == nil ||
		!strings.Contains(err.Error(), "observations") {
		t.Fatalf("expected observation cardinality error, got %v", err)
	}
}
