package storage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type insightFixtureInventory struct {
	contextFixtureInventory
}

func (f insightFixtureInventory) Events() ([]StorageEvent, error) {
	return []StorageEvent{{
		Namespace: "tenant-a", Name: "pvc-event-uid", Type: "Warning",
		Reason: "ProvisioningDelayed", Message: "waiting for storage",
		RegardingKind: "PersistentVolumeClaim", RegardingName: "data", RegardingUID: "pvc-uid",
		Count: 2, FirstObservedAt: f.now.Add(-time.Minute), LastObservedAt: f.now,
	}, {
		Namespace: "tenant-a", Name: "unrelated-event-uid", Type: "Warning",
		Reason: "Unrelated", Message: "not storage attributed",
		RegardingKind: "Pod", RegardingName: "other", RegardingUID: "other-uid",
		Count: 1, LastObservedAt: f.now,
	}}, nil
}

func newInsightFixtureAPI(t *testing.T) *HTTPAPI {
	t.Helper()
	base := newContextFixtureAPI(t, false, false, false)
	inventory := insightFixtureInventory{contextFixtureInventory{now: base.inventory.LastSync()}}
	base.inventory = inventory
	base.context = NewContextEngine(inventory, base.registry)
	return base
}

func TestTimelineProviderFilterRequiresAuthoritativeAttribution(t *testing.T) {
	api := newInsightFixtureAPI(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/storage/timeline?provider=rook-ceph&limit=20", nil)
	mountedStorageAPI(api).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Entries []struct {
			ID          string `json:"id"`
			ProviderID  string `json:"providerId"`
			Count       int64  `json:"count"`
			Attribution struct {
				Evidence string `json:"evidence"`
			} `json:"attribution"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Entries) != 1 {
		t.Fatalf("provider-filtered entries=%#v", body.Entries)
	}
	if body.Entries[0].ID != "pvc-event-uid" || body.Entries[0].ProviderID != "rook-ceph" ||
		body.Entries[0].Attribution.Evidence != "authoritative" || body.Entries[0].Count != 2 {
		t.Fatalf("unexpected timeline entry=%#v", body.Entries[0])
	}
}

func TestTimelineAndGraphIncludeDurableOperationAndAuditSources(t *testing.T) {
	api := newInsightFixtureAPI(t)
	now := api.inventory.LastSync()
	api.SetContextSources(func(context.Context, int) ([]ContextOperationRecord, error) {
		return []ContextOperationRecord{{
			ID: "storage-op-1", ProviderID: "rook-ceph", ActionID: "delete-pvc",
			Phase: "Succeeded", TargetKind: "PersistentVolumeClaim", Namespace: "tenant-a",
			TargetName: "data", TargetUID: "pvc-uid", RequestedAt: now.Add(-time.Minute), ObservedAt: now,
		}}, nil
	}, func(int) []ContextAuditRecord {
		return []ContextAuditRecord{{
			ID: "audit-1", ProviderID: "rook-ceph", Action: "storage_operation_succeeded",
			Result: "ok", OperationID: "storage-op-1", TargetKind: "PersistentVolumeClaim",
			Namespace: "tenant-a", TargetName: "data", TargetUID: "pvc-uid", ObservedAt: now,
		}}
	})

	recorder := httptest.NewRecorder()
	mountedStorageAPI(api).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/storage/timeline?provider=rook-ceph&limit=20", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("timeline status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var timeline struct {
		Entries []struct {
			Source string `json:"source"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &timeline); err != nil {
		t.Fatal(err)
	}
	sources := map[string]bool{}
	for _, entry := range timeline.Entries {
		sources[entry.Source] = true
	}
	if !sources["storage-operation"] || !sources["audit"] {
		t.Fatalf("durable context sources missing from timeline: %#v", sources)
	}

	graph, err := api.context.relationships(context.Background(), graphQuery{
		provider: "rook-ceph", namespace: "tenant-a", kind: "pvc",
		targetID: CanonicalGraphID("pvc", "rook-ceph", "tenant-a", "data"),
		depth:    2, page: PageRequest{Limit: 200},
	})
	if err != nil {
		t.Fatal(err)
	}
	foundOperation := false
	for _, node := range graph.Nodes {
		foundOperation = foundOperation || node.Kind == "storage-operation"
	}
	if !foundOperation {
		t.Fatalf("StorageOperation node was not linked into graph: %#v", graph.Nodes)
	}
}

func TestCapacityOwnershipKeepsMeasuresSeparate(t *testing.T) {
	api := newInsightFixtureAPI(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/storage/capacity/ownership?provider=rook-ceph&namespace=tenant-a", nil)
	mountedStorageAPI(api).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Groups []struct {
			Measure string `json:"measure"`
			Bytes   uint64 `json:"bytes"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	measures := map[string]uint64{}
	for _, group := range body.Groups {
		measures[group.Measure] += group.Bytes
	}
	const eightGiB = uint64(8 * 1024 * 1024 * 1024)
	if measures["pvc-requested"] != eightGiB || measures["pv-provisioned"] != eightGiB {
		t.Fatalf("capacity measures were omitted or collapsed: %#v", measures)
	}
}

func TestCapacityForecastFailsHonestWithoutReviewedPrometheusHistory(t *testing.T) {
	api := newInsightFixtureAPI(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/providers/rook-ceph/capacity/forecast?measure=pvc-requested&horizon=720h", nil)
	mountedStorageAPI(api).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Status      string `json:"status"`
		SampleCount int    `json:"sampleCount"`
		Conditions  []struct {
			Code string `json:"code"`
		} `json:"conditions"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "unavailable" || body.SampleCount != 0 || len(body.Conditions) == 0 ||
		body.Conditions[0].Code != "prometheus-history-unavailable" {
		t.Fatalf("forecast must disclose unavailable Prometheus history: %#v", body)
	}
}

func TestInsightQueriesAreBounded(t *testing.T) {
	api := newInsightFixtureAPI(t)
	for _, path := range []string{
		"/api/v1/storage/timeline?limit=501",
		"/api/v1/storage/timeline?source=made-up",
		"/api/v1/storage/capacity/ownership?measure=combined-total",
		"/api/v1/providers/rook-ceph/capacity/forecast?measure=pvc-requested&horizon=3000h",
	} {
		recorder := httptest.NewRecorder()
		mountedStorageAPI(api).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s status=%d body=%s", path, recorder.Code, recorder.Body.String())
		}
	}
}

var _ InventoryReader = insightFixtureInventory{}
