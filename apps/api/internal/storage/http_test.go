package storage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

type fixtureInventory struct{ snapshot bool }

type compositeIDProvider struct{ testProvider }

func (compositeIDProvider) ResourceKinds(context.Context) []string { return []string{"clusters"} }
func (compositeIDProvider) ListProviderResources(context.Context, string, PageRequest) (any, PageMeta, error) {
	return []any{map[string]any{"id": "rook-ceph/rook-ceph"}}, PageMeta{Total: 1}, nil
}
func (compositeIDProvider) GetProviderResource(_ context.Context, _, id string) (any, error) {
	if id == "rook-ceph/rook-ceph" {
		return map[string]any{"id": id}, nil
	}
	return nil, ErrNotFound
}

func (f fixtureInventory) Ready() bool             { return true }
func (f fixtureInventory) LastSync() time.Time     { return time.Unix(100, 0).UTC() }
func (f fixtureInventory) SnapshotAvailable() bool { return f.snapshot }
func (f fixtureInventory) DiscoveredDriverNames() ([]string, error) {
	return []string{"example.csi.io"}, nil
}
func (f fixtureInventory) Drivers(context.Context) ([]DriverSummary, error) {
	return []DriverSummary{{Name: "example.csi.io", ProviderID: GenericProviderID("example.csi.io")}}, nil
}
func (f fixtureInventory) StorageClasses() ([]StorageClassSummary, error) {
	return []StorageClassSummary{{Name: "fast", Provisioner: "example.csi.io", ProviderID: GenericProviderID("example.csi.io")}}, nil
}
func (f fixtureInventory) Claims(context.Context) ([]ClaimSummary, error) {
	return []ClaimSummary{{ID: "a/one", Namespace: "a", Name: "one", ProviderID: GenericProviderID("example.csi.io"), Driver: "example.csi.io", Phase: "Bound"}, {ID: "b/two", Namespace: "b", Name: "two", ProviderID: GenericProviderID("example.csi.io"), Driver: "example.csi.io", Phase: "Pending"}}, nil
}
func (f fixtureInventory) Volumes(context.Context) ([]PersistentVolumeSummary, error) {
	return []PersistentVolumeSummary{{Name: "pv-one", ProviderID: GenericProviderID("example.csi.io"), Driver: "example.csi.io", Phase: "Bound"}}, nil
}
func (f fixtureInventory) Snapshots() ([]SnapshotSummary, error) { return nil, nil }
func (f fixtureInventory) Attachments() ([]AttachmentSummary, error) {
	return []AttachmentSummary{{Name: "attach", ProviderID: GenericProviderID("example.csi.io"), Driver: "example.csi.io", Attached: true}}, nil
}
func (f fixtureInventory) Capacities() ([]CapacitySummary, error) {
	return []CapacitySummary{{ProviderID: GenericProviderID("example.csi.io"), Driver: "example.csi.io", Capacity: "8Pi"}}, nil
}
func (f fixtureInventory) Events() ([]StorageEvent, error) {
	return []StorageEvent{{Name: "event", Namespace: "a", Type: "Warning"}}, nil
}

func TestCommonReadEndpointsAndTypedErrors(t *testing.T) {
	api := NewHTTPAPI(fixtureInventory{}, NewRegistry())
	router := chi.NewRouter()
	api.Mount(router)
	for _, path := range []string{"/api/v1/storage/providers", "/api/v1/storage/drivers", "/api/v1/storage/classes", "/api/v1/storage/claims", "/api/v1/storage/claims/a/one", "/api/v1/storage/volumes", "/api/v1/storage/volumes/pv-one", "/api/v1/storage/snapshots", "/api/v1/storage/attachments", "/api/v1/storage/capacity", "/api/v1/storage/events"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Errorf("%s status=%d body=%s", path, recorder.Code, recorder.Body.String())
		}
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/storage/claims?limit=1&namespace=a", nil))
	if recorder.Code != http.StatusOK {
		t.Fatal(recorder.Body.String())
	}
	var claims Page[ClaimSummary]
	if err := json.Unmarshal(recorder.Body.Bytes(), &claims); err != nil {
		t.Fatal(err)
	}
	if len(claims.Data) != 1 || claims.Data[0].Namespace != "a" || claims.Page.Total != 1 {
		t.Fatalf("filtered page=%#v", claims)
	}
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/storage/claims?continue=not-valid", nil))
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"INVALID_PAGE"`) {
		t.Fatalf("invalid page response=%d %s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/storage/claims/a/missing", nil))
	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), `"code":"CLAIM_NOT_FOUND"`) {
		t.Fatalf("not found response=%d %s", recorder.Code, recorder.Body.String())
	}
}

func TestProviderResourceDetailRoundTripsCompositeID(t *testing.T) {
	registry := NewRegistry()
	provider := compositeIDProvider{testProvider{ProviderDescriptor{ID: "rook-ceph", Drivers: []string{"rook.example"}}}}
	if err := registry.Register(context.Background(), provider); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	NewHTTPAPI(fixtureInventory{}, registry).Mount(router)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/providers/rook-ceph/resources/clusters/rook-ceph%2Frook-ceph", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"id":"rook-ceph/rook-ceph"`) {
		t.Fatalf("composite resource ID response=%d %s", recorder.Code, recorder.Body.String())
	}
}

func TestMissingSnapshotAPIIsExplicitPartialCondition(t *testing.T) {
	api := NewHTTPAPI(fixtureInventory{snapshot: false}, NewRegistry())
	router := chi.NewRouter()
	api.Mount(router)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/storage/snapshots", nil))
	var page Page[SnapshotSummary]
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Conditions) != 1 || page.Conditions[0].Reason != "APIAbsent" {
		t.Fatalf("conditions=%#v", page.Conditions)
	}
}

func TestUnavailableInventoryReturnsCommonEnvelope(t *testing.T) {
	api := NewHTTPAPI(nil, nil)
	router := chi.NewRouter()
	api.Mount(router)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/storage/providers", nil))
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), `"code":"STORAGE_UNAVAILABLE"`) {
		t.Fatalf("response=%d %s", recorder.Code, recorder.Body.String())
	}
}
