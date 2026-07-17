package linstor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/highland-io/highland/apps/api/internal/storage"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func testAdapter(t *testing.T) *Adapter {
	t.Helper()
	cluster := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "piraeus.io/v1", "kind": "LinstorCluster", "metadata": map[string]any{"name": "linstorcluster"}, "status": map[string]any{"conditions": []any{map[string]any{"type": "Available", "status": "True"}}}}}
	cluster.SetGroupVersionKind(schema.GroupVersionKind{Group: "piraeus.io", Version: "v1", Kind: "LinstorCluster"})
	listKinds := map[schema.GroupVersionResource]string{}
	for _, gvr := range crdByKind {
		listKinds[gvr] = "LinstorClusterList"
	}
	listKinds[schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}] = "DeploymentList"
	listKinds[schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}] = "StatefulSetList"
	listKinds[schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}] = "DaemonSetList"
	dynamic := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, cluster)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/controller/version":
			_, _ = w.Write([]byte(`{"version":"1.33.3"}`))
		case "/v1/resource-definitions":
			_, _ = w.Write([]byte(`[{"name":"csi-volume-a","props":{"Aux/csi.storage.k8s.io/pvc/name":"data"}}]`))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(ClientConfig{URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := New(Config{Namespace: "piraeus", Dynamic: dynamic, Client: client})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func TestDescriptorAndAuthoritativeVolumeCorrelation(t *testing.T) {
	a := testAdapter(t)
	ctx := context.Background()
	descriptor, err := a.Descriptor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.Kind != "linstor" || descriptor.Version != "1.33.3" || descriptor.Metadata["managementMode"] != "external" {
		t.Fatalf("descriptor=%#v", descriptor)
	}
	volumes := []storage.PersistentVolumeSummary{{Driver: Driver, VolumeHandle: "csi-volume-a/2"}, {Driver: Driver, VolumeHandle: "missing"}, {Driver: "other.csi", VolumeHandle: "csi-volume-a"}}
	if err := a.EnrichVolumes(ctx, volumes); err != nil {
		t.Fatal(err)
	}
	if volumes[0].ProviderRef == nil || volumes[0].ProviderRef.ID != "csi-volume-a" || volumes[0].Backend["csiVolumeNumber"] != 2 {
		t.Fatalf("correlated=%#v", volumes[0])
	}
	if volumes[1].ProviderRef != nil || len(volumes[1].Conditions) != 1 {
		t.Fatalf("missing=%#v", volumes[1])
	}
	if volumes[2].ProviderRef != nil {
		t.Fatal("another driver's volume was correlated")
	}
}

func TestKubernetesHealthSurvivesWithoutController(t *testing.T) {
	a := testAdapter(t)
	a.client = nil
	health := a.Health(context.Background())
	if health.Status == storage.SeverityError {
		t.Fatalf("health=%#v", health)
	}
	seen := false
	for _, condition := range health.Conditions {
		if condition.Type == "ControllerAPI" && condition.Reason == "NotConfigured" {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("conditions=%#v", health.Conditions)
	}
	items, _, err := a.ListProviderResources(context.Background(), "clusters", storage.PageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items.([]any)) != 1 {
		t.Fatalf("items=%#v", items)
	}
}

func TestNormalizeAPIUsesStableUniqueIdentifiers(t *testing.T) {
	pool := normalizeAPI("linstor", "storage-pools", 0, map[string]any{"node_name": "node-a", "storage_pool_name": "pool-a", "uuid": "pool-uuid"})
	if pool["id"] != "pool-uuid" || pool["name"] != "pool-a" {
		t.Fatalf("pool=%#v", pool)
	}
	fallback := normalizeAPI("linstor", "storage-pools", 0, map[string]any{"node_name": "node-a", "storage_pool_name": "pool-a"})
	if fallback["id"] != "node-a~pool-a" {
		t.Fatalf("pool fallback=%#v", fallback)
	}
	report := normalizeAPI("linstor", "error-reports", 0, map[string]any{"node_name": "node-a", "filename": "ErrorReport-1.log"})
	if report["id"] != "ErrorReport-1.log" || report["name"] != "ErrorReport-1.log" {
		t.Fatalf("report=%#v", report)
	}
	replica := normalizeAPI("linstor", "resources", 0, map[string]any{"name": "resource-a", "node_name": "node-a", "uuid": "replica-uuid"})
	if replica["id"] != "replica-uuid" || replica["name"] != "resource-a" {
		t.Fatalf("replica=%#v", replica)
	}
}

func TestHealthSurfacesOfflineNodesAndStaleReplicas(t *testing.T) {
	a := testAdapter(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/controller/version":
			_, _ = w.Write([]byte(`{"version":"1.33.3"}`))
		case "/v1/nodes":
			_, _ = w.Write([]byte(`[{"name":"node-a","connection_status":"OFFLINE"}]`))
		case "/v1/view/resources":
			_, _ = w.Write([]byte(`[{"name":"resource-a","volumes":[{"state":{"disk_state":"Inconsistent"}}]}]`))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(ClientConfig{URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	a.client = client
	health := a.Health(context.Background())
	if health.Status != storage.SeverityError {
		t.Fatalf("health=%#v", health)
	}
	reasons := map[string]bool{}
	for _, condition := range health.Conditions {
		reasons[condition.Reason] = true
	}
	if !reasons["OfflineNodes"] || !reasons["UnhealthyReplicaVolumes"] {
		t.Fatalf("conditions=%#v", health.Conditions)
	}
}

var _ = metav1.NamespaceAll
