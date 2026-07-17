package longhorn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/highland-io/highland/apps/api/internal/storage"
)

func TestAdapterDescriptorAndAuthoritativeEnrichment(t *testing.T) {
	manager := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1" {
			_ = json.NewEncoder(w).Encode(map[string]any{})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": "lh-handle", "name": "unrelated-pvc-name", "robustness": "healthy", "actualSize": "10995116277760"}}})
	}))
	defer manager.Close()
	adapter, err := New(Config{ManagerURL: manager.URL, Namespace: "longhorn-system", Version: "v1.12.0", HTTPClient: manager.Client()})
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := adapter.Descriptor(context.Background())
	if err != nil || descriptor.SupportLevel != storage.SupportManaged || descriptor.Health.Status != storage.SeverityOK {
		t.Fatalf("descriptor = %#v, err = %v", descriptor, err)
	}
	volumes := []storage.PersistentVolumeSummary{{Driver: DriverName, VolumeHandle: "lh-handle"}, {Driver: DriverName, VolumeHandle: "missing"}}
	if err := adapter.EnrichVolumes(context.Background(), volumes); err != nil {
		t.Fatal(err)
	}
	if volumes[0].ProviderRef == nil || volumes[0].ProviderRef.ID != "lh-handle" || volumes[0].BackendAllocated != "10995116277760" {
		t.Fatalf("not enriched: %#v", volumes[0])
	}
	if volumes[1].ProviderRef != nil || len(volumes[1].Conditions) == 0 {
		t.Fatalf("missing handle should be explicit: %#v", volumes[1])
	}
}

func TestUntestedVersionWithholdsVersionSensitiveCapabilities(t *testing.T) {
	manager := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer manager.Close()
	adapter, err := New(Config{ManagerURL: manager.URL, Version: "v2.0.0", HTTPClient: manager.Client()})
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := adapter.Descriptor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range descriptor.Capabilities {
		if capability == storage.CapabilityVolumeDelete {
			t.Fatal("untested Longhorn version exposed version-sensitive common writes")
		}
	}
	if descriptor.Health.Status != storage.SeverityWarning {
		t.Fatalf("expected untested version warning: %#v", descriptor.Health)
	}
}

func TestInvalidManagerURL(t *testing.T) {
	if _, err := New(Config{ManagerURL: "file:///tmp/socket"}); err == nil {
		t.Fatal("expected malformed URL rejection")
	}
}
