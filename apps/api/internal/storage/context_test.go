package storage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

type contextFixtureInventory struct {
	now     time.Time
	partial bool
}

func (f contextFixtureInventory) Ready() bool             { return true }
func (f contextFixtureInventory) LastSync() time.Time     { return f.now }
func (f contextFixtureInventory) SnapshotAvailable() bool { return true }
func (f contextFixtureInventory) DiscoveredDriverNames() ([]string, error) {
	return []string{"rook-ceph.rbd.csi.ceph.com"}, nil
}
func (f contextFixtureInventory) Drivers(context.Context) ([]DriverSummary, error) {
	return []DriverSummary{{Name: "rook-ceph.rbd.csi.ceph.com", ProviderID: "rook-ceph", SupportLevel: SupportManaged, NodeCount: 3, StorageClassCount: 1, PersistentVolCount: 1}}, nil
}
func (f contextFixtureInventory) StorageClasses() ([]StorageClassSummary, error) {
	return []StorageClassSummary{{Name: "ceph-rbd", UID: "sc-uid", ProviderID: "rook-ceph", Provisioner: "rook-ceph.rbd.csi.ceph.com", ReclaimPolicy: "Delete", ClaimCount: 1, VolumeCount: 1}}, nil
}
func (f contextFixtureInventory) Claims(context.Context) ([]ClaimSummary, error) {
	return []ClaimSummary{{
		ID: "cluster/tenant-a/pvc/data", Namespace: "tenant-a", Name: "data", UID: "pvc-uid",
		ProviderID: "rook-ceph", Driver: "rook-ceph.rbd.csi.ceph.com", StorageClass: "ceph-rbd", PVName: "pv-data",
		Phase: "Bound", RequestedCapacity: "8Gi", Provisioned: "8Gi", AccessModes: []string{"ReadWriteOnce"},
		ReclaimPolicy: "Delete", VolumeHandle: "image-uuid", ProviderRef: &ProviderReference{Kind: "ceph-rbd-image", ID: "image-uuid"},
		Workloads: []WorkloadReference{{Namespace: "tenant-a", Kind: "StatefulSet", Name: "database", PodName: "database-0", PodPhase: "Running", NodeName: "node-a"}},
	}}, nil
}
func (f contextFixtureInventory) Volumes(context.Context) ([]PersistentVolumeSummary, error) {
	return []PersistentVolumeSummary{{
		Name: "pv-data", UID: "pv-uid", ProviderID: "rook-ceph", Driver: "rook-ceph.rbd.csi.ceph.com",
		VolumeHandle: "image-uuid", StorageClass: "ceph-rbd", Phase: "Bound", Capacity: "8Gi",
		AccessModes: []string{"ReadWriteOnce"}, ReclaimPolicy: "Delete", ClaimNamespace: "tenant-a", ClaimName: "data",
		ProviderRef: &ProviderReference{Kind: "ceph-rbd-image", ID: "image-uuid"},
	}}, nil
}
func (f contextFixtureInventory) Snapshots() ([]SnapshotSummary, error) {
	if f.partial {
		return nil, context.DeadlineExceeded
	}
	return []SnapshotSummary{{ID: "cluster/tenant-a/snapshot/nightly", Namespace: "tenant-a", Name: "nightly", UID: "snapshot-uid", ProviderID: "rook-ceph", Driver: "rook-ceph.rbd.csi.ceph.com", SourcePVC: "data", RestoreSize: "8Gi"}}, nil
}
func (f contextFixtureInventory) Attachments() ([]AttachmentSummary, error) {
	return []AttachmentSummary{{Name: "attachment-a", UID: "attachment-uid", ProviderID: "rook-ceph", Driver: "rook-ceph.rbd.csi.ceph.com", PVName: "pv-data", NodeName: "node-a", Attached: true}}, nil
}
func (f contextFixtureInventory) Capacities() ([]CapacitySummary, error) { return nil, nil }
func (f contextFixtureInventory) Events() ([]StorageEvent, error)        { return nil, nil }

type contextFixtureProvider struct {
	now         time.Time
	missingPool bool
	runtimeOnly bool
}

func (p *contextFixtureProvider) Descriptor(context.Context) (ProviderDescriptor, error) {
	return ProviderDescriptor{ID: "rook-ceph", Kind: "rook-ceph", DisplayName: "Rook/Ceph", SupportLevel: SupportManaged, Drivers: []string{"rook-ceph.rbd.csi.ceph.com"}, Health: ProviderHealth{Status: SeverityOK, ObservedAt: p.now}}, nil
}
func (p *contextFixtureProvider) Health(context.Context) ProviderHealth {
	return ProviderHealth{Status: SeverityOK, ObservedAt: p.now}
}
func (p *contextFixtureProvider) Capabilities(context.Context) []Capability {
	return []Capability{CapabilityClaimsRead, CapabilityProviderHealth}
}
func (p *contextFixtureProvider) ResourceKinds(context.Context) []string {
	return []string{"clusters", "pools", "filesystems", "mirroring", "osds", "rbd-images"}
}
func (p *contextFixtureProvider) ListProviderResources(_ context.Context, kind string, page PageRequest) (any, PageMeta, error) {
	items := []any{}
	switch kind {
	case "clusters":
		items = append(items, map[string]any{"id": "rook-ceph/rook-ceph", "name": "rook-ceph", "namespace": "rook-ceph", "kind": "CephCluster", "kubernetesUid": "cluster-uid", "observedAt": p.now, "status": map[string]any{"state": "Created"}})
	case "pools":
		state := "observed"
		if p.missingPool {
			state = "not-observed"
		}
		items = append(items, map[string]any{"id": "rook-ceph/pool-a", "name": "pool-a", "namespace": "rook-ceph", "kind": "CephBlockPool", "kubernetesUid": "pool-uid", "observedAt": p.now, "runtimeObservedAt": p.now, "runtimeState": state, "spec": map[string]any{"replicated": map[string]any{"size": 3}}, "status": map[string]any{"phase": "Ready"}, "runtime": map[string]any{"pool_name": "pool-a"}})
		if p.runtimeOnly {
			items = append(items, map[string]any{"id": "runtime/orphan", "name": "orphan", "observedAt": p.now, "runtimeObservedAt": p.now, "runtimeState": "runtime-only", "runtime": map[string]any{"pool_name": "orphan"}})
		}
	case "filesystems":
		items = append(items, map[string]any{"id": "rook-ceph/shared", "name": "shared", "namespace": "rook-ceph", "observedAt": p.now, "status": map[string]any{"phase": "Ready"}})
	case "mirroring":
		items = append(items, map[string]any{"id": "rook-ceph/mirror", "name": "mirror", "namespace": "rook-ceph", "observedAt": p.now, "status": map[string]any{"phase": "Ready"}})
	case "osds":
		items = append(items, map[string]any{"id": "0", "name": "osd.0", "observedAt": p.now, "up": 1, "in": 1})
	case "rbd-images":
		items = append(items, map[string]any{"id": "image-uuid", "name": "csi-vol-display-name", "pool_name": "pool-a", "observedAt": p.now})
	}
	return items, PageMeta{Limit: page.Limit, Total: len(items)}, nil
}
func (p *contextFixtureProvider) GetProviderResource(context.Context, string, string) (any, error) {
	return nil, ErrNotFound
}

type nonCephContextFixtureProvider struct {
	*contextFixtureProvider
}

func (p *nonCephContextFixtureProvider) Descriptor(context.Context) (ProviderDescriptor, error) {
	return ProviderDescriptor{ID: "longhorn", Kind: "longhorn", DisplayName: "Longhorn", SupportLevel: SupportManaged, Drivers: []string{"driver.longhorn.io"}, Health: ProviderHealth{Status: SeverityOK, ObservedAt: p.now}}, nil
}

func (p *nonCephContextFixtureProvider) ResourceKinds(context.Context) []string {
	return []string{"volumes", "nodes", "backups"}
}

func newContextFixtureAPI(t *testing.T, partial, missingPool, runtimeOnly bool) *HTTPAPI {
	t.Helper()
	now := time.Now().UTC()
	registry := NewRegistry()
	if err := registry.Register(context.Background(), &contextFixtureProvider{now: now, missingPool: missingPool, runtimeOnly: runtimeOnly}); err != nil {
		t.Fatal(err)
	}
	return NewHTTPAPI(contextFixtureInventory{now: now, partial: partial}, registry)
}

func mountedStorageAPI(api *HTTPAPI) http.Handler {
	router := chi.NewRouter()
	api.Mount(router)
	return router
}

func TestRelationshipGraphUsesExactEvidenceAndBoundedQueries(t *testing.T) {
	api := newContextFixtureAPI(t, false, false, false)
	pvcID := CanonicalGraphID("pvc", "rook-ceph", "tenant-a", "data")
	result, err := api.context.relationships(context.Background(), graphQuery{provider: "rook-ceph", namespace: "tenant-a", kind: "pvc", targetID: pvcID, depth: 4, page: PageRequest{Limit: 200}})
	if err != nil {
		t.Fatal(err)
	}
	if result.APIVersion != GraphAPIVersion || result.Incomplete || len(result.Nodes) < 8 {
		t.Fatalf("unexpected graph: %#v", result)
	}
	var backendEdge, poolEdge bool
	for _, edge := range result.Edges {
		if edge.Type == "backed-by" && edge.Evidence[0].Ref == "image-uuid" {
			backendEdge = edge.Confidence == ConfidenceAuthoritative
		}
		if edge.Type == "belongs-to-pool" && edge.Evidence[0].Ref == "pool-a" {
			poolEdge = edge.Confidence == ConfidenceDerived
		}
	}
	if !backendEdge || !poolEdge {
		t.Fatalf("missing authoritative backend or derived pool edge: %#v", result.Edges)
	}

	router := mountedStorageAPI(api)
	validPaths := []string{
		"/api/v1/storage/relationships?provider=rook-ceph&kind=pvc&namespace=tenant-a&depth=3&limit=50",
		"/api/v1/storage/resources/pvc/" + pvcID + "/relationships?provider=rook-ceph&depth=2&limit=50",
		"/api/v1/providers/rook-ceph/relationships?kind=pvc&namespace=tenant-a&depth=2&limit=50",
	}
	for _, path := range validPaths {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Errorf("%s status=%d body=%s", path, recorder.Code, recorder.Body.String())
		}
	}
	for _, path := range []string{
		"/api/v1/storage/relationships?kind=pvc&namespace=tenant-a",
		"/api/v1/storage/relationships?provider=rook-ceph&kind=pvc",
		"/api/v1/storage/relationships?provider=rook-ceph&kind=pvc&namespace=tenant-a&depth=7",
		"/api/v1/storage/relationships?provider=rook-ceph&kind=pvc&namespace=tenant-a&limit=201",
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s status=%d body=%s", path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestImpactTraversesPoolToKubernetesConsumersAndFailsPartialClosed(t *testing.T) {
	api := newContextFixtureAPI(t, false, false, false)
	poolID := CanonicalGraphID("ceph-block-pool", "rook-ceph", "rook-ceph", "rook-ceph/pool-a")
	result, err := api.context.impact(context.Background(), "rook-ceph", "ceph-block-pool", poolID, 5)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, item := range result.Confirmed {
		kinds[item.Node.Kind] = true
	}
	for _, kind := range []string{"rbd-image", "pv", "pvc", "pod", "workload"} {
		if !kinds[kind] {
			t.Fatalf("confirmed impact omitted %s: %#v", kind, result.Confirmed)
		}
	}
	if result.Summary.RequestedCapacity != "8Gi" || result.Summary.ProvisionedCapacity != "8Gi" || result.Summary.AttachedCount != 1 || result.Summary.WorkloadCount != 1 {
		t.Fatalf("impact summary=%#v", result.Summary)
	}
	recorder := httptest.NewRecorder()
	mountedStorageAPI(api).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/storage/impact?provider=rook-ceph&kind=ceph-block-pool&id="+url.QueryEscape(poolID), nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("impact endpoint status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	partial := newContextFixtureAPI(t, true, false, false)
	partialResult, err := partial.context.impact(context.Background(), "rook-ceph", "ceph-block-pool", poolID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !partialResult.Incomplete || !hasCondition(partialResult.Conditions, "ImpactComplete") {
		t.Fatalf("partial impact did not fail closed: %#v", partialResult)
	}
}

func TestOSDImpactRemainsPotentialWithoutPlacementEvidence(t *testing.T) {
	api := newContextFixtureAPI(t, false, false, false)
	osdID := CanonicalGraphID("osd", "rook-ceph", "", "0")
	result, err := api.context.impact(context.Background(), "rook-ceph", "osd", osdID, 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Confirmed) != 0 {
		t.Fatalf("OSD impact must not be confirmed without PG/object placement evidence: %#v", result.Confirmed)
	}
	foundWorkload := false
	for _, item := range result.Potential {
		if item.Node.Kind == "workload" {
			foundWorkload = true
		}
	}
	if !foundWorkload {
		t.Fatalf("expected explicitly potential OSD-to-workload impact: %#v", result.Potential)
	}
}

func TestDriftKeepsDesiredAndRuntimeSeparateAndTracksGrace(t *testing.T) {
	api := newContextFixtureAPI(t, false, true, true)
	report, err := api.context.driftReport(context.Background(), "rook-ceph")
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Total != 2 || report.Summary.Suppressed != 2 {
		t.Fatalf("drift summary=%#v records=%#v", report.Summary, report.Data)
	}
	categories := map[DriftCategory]bool{}
	for _, record := range report.Data {
		categories[record.Category] = true
		if record.FirstObserved.IsZero() || record.LastObserved.IsZero() || !record.Suppressed {
			t.Fatalf("drift observation/grace missing: %#v", record)
		}
		if record.Category == DriftMissingRuntime && (record.Desired == nil || record.Runtime == nil) {
			t.Fatalf("missing desired/runtime separation: %#v", record)
		}
		if record.Category == DriftUnexpectedRuntime && record.Desired != nil {
			t.Fatalf("runtime-only record must not fabricate desired state: %#v", record)
		}
	}
	if !categories[DriftMissingRuntime] || !categories[DriftUnexpectedRuntime] {
		t.Fatalf("categories=%#v", categories)
	}

	router := mountedStorageAPI(api)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/providers/rook-ceph/drift?limit=1", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body DriftReport
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 1 || body.Page.Total != 2 || body.Page.Continue == "" {
		t.Fatalf("bounded drift page=%#v data=%#v", body.Page, body.Data)
	}
}

func TestDriftIgnoresCephInternalAndRookManagedFilesystemPools(t *testing.T) {
	now := time.Now().UTC()
	expected := expectedCephFilesystemPools([]map[string]any{{
		"name": "shared",
		"spec": map[string]any{"dataPools": []any{
			map[string]any{"name": "data0"},
			map[string]any{},
		}},
	}})
	for _, name := range []string{".mgr", "shared-metadata", "shared-data0", "shared-data1"} {
		records := classifyDriftItem("rook-ceph", "pools", map[string]any{
			"id": "runtime/" + name, "name": name, "runtimeState": "runtime-only",
			"observedAt": now, "runtimeObservedAt": now,
		}, expected, now)
		if len(records) != 0 {
			t.Errorf("normal Ceph/Rook pool %q reported as drift: %#v", name, records)
		}
	}
	records := classifyDriftItem("rook-ceph", "pools", map[string]any{
		"id": "runtime/orphan", "name": "orphan", "runtimeState": "runtime-only",
		"observedAt": now, "runtimeObservedAt": now,
	}, expected, now)
	if len(records) != 1 || records[0].Category != DriftUnexpectedRuntime {
		t.Fatalf("unowned runtime pool must remain visible: %#v", records)
	}
}

func TestEmptyDriftReportSerializesDataAsArray(t *testing.T) {
	api := newContextFixtureAPI(t, false, false, false)
	report, err := api.context.driftReport(context.Background(), "rook-ceph")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"data":null`) {
		t.Fatalf("empty drift data must be a JSON array: %s", encoded)
	}
}

func TestDriftSkipsCephOnlyKindsForOtherProviders(t *testing.T) {
	now := time.Now().UTC()
	registry := NewRegistry()
	if err := registry.Register(context.Background(), &nonCephContextFixtureProvider{contextFixtureProvider: &contextFixtureProvider{now: now}}); err != nil {
		t.Fatal(err)
	}
	api := NewHTTPAPI(contextFixtureInventory{now: now}, registry)
	report, err := api.context.driftReport(context.Background(), "longhorn")
	if err != nil {
		t.Fatal(err)
	}
	if report.Incomplete {
		t.Fatalf("unsupported Ceph kinds must not make Longhorn drift partial: %#v", report.Conditions)
	}
	if report.Summary.Total != 0 || len(report.Data) != 0 {
		t.Fatalf("unexpected Longhorn drift: summary=%#v data=%#v", report.Summary, report.Data)
	}
	for _, condition := range report.Conditions {
		if condition.Reason == "ResourceKindUnavailable" {
			t.Fatalf("Ceph-only resource probe leaked into Longhorn drift: %#v", condition)
		}
	}
}

func TestCanonicalGraphIDDoesNotCollideOnDelimiters(t *testing.T) {
	a := CanonicalGraphID("pvc", "rook-ceph", "a/b", "c")
	b := CanonicalGraphID("pvc", "rook-ceph", "a", "b/c")
	if a == b || strings.Contains(a, "a/b") {
		t.Fatalf("canonical IDs must be URL-safe and collision resistant: %q %q", a, b)
	}
}
