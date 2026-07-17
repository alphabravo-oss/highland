package rookceph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/highland-io/highland/apps/api/internal/storage"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestAdapterDiscoversConfiguredClusterAndBoundsRookData(t *testing.T) {
	cluster := rookObject("CephCluster", "cephclusters", "rook-ceph", map[string]any{
		"state": "Created", "ceph": map[string]any{"health": "HEALTH_OK"},
	})
	pool := rookObject("CephBlockPool", "cephblockpools", "fast", map[string]any{"phase": "Ready"})
	pool.Object["spec"] = map[string]any{"replicated": map[string]any{"size": int64(3)}, "secretName": "must-not-leak"}
	adapter := testAdapter(t, cluster, pool)

	descriptor, err := adapter.Descriptor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.ID != "rook-ceph" || descriptor.SupportLevel != storage.SupportManaged || descriptor.Health.Status != storage.SeverityOK {
		t.Fatalf("unexpected descriptor: %#v", descriptor)
	}
	data, page, err := adapter.ListProviderResources(context.Background(), "pools", storage.PageRequest{Limit: 20})
	if err != nil || page.Total != 1 {
		t.Fatalf("data=%#v page=%#v err=%v", data, page, err)
	}
	item := data.([]any)[0].(map[string]any)
	if item["providerId"] != "rook-ceph" || item["providerVersion"] != "1.20.2" || item["rookApiVersion"] != "v1" {
		t.Fatalf("provider/version diagnostics missing: %#v", item)
	}
	spec := item["spec"].(map[string]any)
	if _, leaked := spec["secretName"]; leaked {
		t.Fatalf("secret reference leaked in provider response: %#v", item)
	}
}

func TestCephFSCorrelationRequiresExactCSIHandleAndMetadata(t *testing.T) {
	adapter := testAdapter(t, rookObject("CephCluster", "cephclusters", "rook-ceph", map[string]any{"state": "Created"}))
	claims := []storage.ClaimSummary{
		{Driver: "rook-ceph.cephfs.csi.ceph.com", VolumeHandle: "cephfs-handle", VolumeAttributes: map[string]string{"fsName": "shared", "subvolumeName": "csi-subvol"}},
		{Driver: "rook-ceph.cephfs.csi.ceph.com", VolumeHandle: "missing-metadata"},
	}
	if err := adapter.EnrichClaims(context.Background(), claims); err != nil {
		t.Fatal(err)
	}
	if claims[0].ProviderRef == nil || claims[0].ProviderRef.Kind != "cephfs-subvolume" || claims[0].ProviderRef.ID != "cephfs-handle" {
		t.Fatalf("authoritative CephFS metadata was not correlated: %#v", claims[0])
	}
	if claims[1].ProviderRef != nil {
		t.Fatalf("CephFS display/handle-only identity must remain unresolved: %#v", claims[1])
	}
}

func TestDescriptorReportsPrivateDashboardAvailabilityWithoutFetchingPublicURL(t *testing.T) {
	var publicRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"reader-token"}`))
		case "/api/health/minimal":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"HEALTH_OK"}`))
		case "/operator-dashboard":
			publicRequests++
			http.Error(w, "public URL must never be fetched", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dashboard, err := NewDashboardClient(DashboardConfig{URL: server.URL, Username: "reader", Password: "secret", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	adapter := testAdapter(t, rookObject("CephCluster", "cephclusters", "rook-ceph", map[string]any{"state": "Created", "ceph": map[string]any{"health": "HEALTH_OK"}}))
	adapter.dashboard = dashboard
	adapter.dashboardPublicURL = server.URL + "/operator-dashboard"

	descriptor, err := adapter.Descriptor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.Metadata["dashboardAvailability"] != "available" || descriptor.Metadata["dashboardPublicUrl"] != server.URL+"/operator-dashboard" {
		t.Fatalf("dashboard metadata = %#v", descriptor.Metadata)
	}
	if descriptor.Metadata["dashboardGatewayPath"] != "/ceph-dashboard/" {
		t.Fatalf("dashboard gateway path = %q", descriptor.Metadata["dashboardGatewayPath"])
	}
	if descriptor.Metadata["dashboardPublicUrlSecurity"] != "insecure-lab-http" {
		t.Fatalf("dashboard URL security = %q", descriptor.Metadata["dashboardPublicUrlSecurity"])
	}
	if publicRequests != 0 {
		t.Fatalf("public dashboard URL received %d server-side requests", publicRequests)
	}
	foundHealthyCondition := false
	for _, condition := range descriptor.Health.Conditions {
		if condition.Type == "DashboardAvailable" && condition.Status == "True" && condition.Reason == "PrivateReaderHealthy" {
			foundHealthyCondition = true
		}
	}
	if !foundHealthyCondition {
		t.Fatalf("private dashboard health condition missing: %#v", descriptor.Health.Conditions)
	}
}

func TestVerifyPoolEmptyFailsClosedAcrossEveryDependencySource(t *testing.T) {
	tests := []struct {
		name        string
		health      string
		images      string
		rookObjects []runtime.Object
		wantEmpty   bool
		wantReason  string
	}{
		{name: "proven empty", health: "HEALTH_OK", images: `[]`, wantEmpty: true, wantReason: "prove no discovered dependencies"},
		{name: "health warning", health: "HEALTH_WARN", images: `[]`, wantReason: "requires fresh HEALTH_OK"},
		{name: "RBD image", health: "HEALTH_OK", images: `[{"pool_name":"scratch","name":"image-a"}]`, wantReason: "RBD images"},
		{name: "filesystem reference", health: "HEALTH_OK", images: `[]`, rookObjects: []runtime.Object{&unstructured.Unstructured{Object: map[string]any{"apiVersion": "ceph.rook.io/v1", "kind": "CephFilesystem", "metadata": map[string]any{"name": "cephfs", "namespace": "rook-ceph"}, "spec": map[string]any{"dataPools": []any{map[string]any{"name": "scratch"}}}}}}, wantReason: "CephFilesystem"},
		{name: "mirroring reference", health: "HEALTH_OK", images: `[]`, rookObjects: []runtime.Object{&unstructured.Unstructured{Object: map[string]any{"apiVersion": "ceph.rook.io/v1", "kind": "CephRBDMirror", "metadata": map[string]any{"name": "mirror", "namespace": "rook-ceph"}, "spec": map[string]any{"pool": "scratch"}}}}, wantReason: "mirroring"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/auth":
					_ = json.NewEncoder(w).Encode(map[string]string{"token": testJWT()})
				case "/api/health/minimal":
					_, _ = fmt.Fprintf(w, `{"status":%q}`, tc.health)
				case "/api/block/image":
					_, _ = w.Write([]byte(tc.images))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			dashboard, err := NewDashboardClient(DashboardConfig{URL: server.URL, Username: "reader", Password: "secret", HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			adapter := testAdapter(t, tc.rookObjects...)
			adapter.dashboard = dashboard
			empty, reason, err := adapter.VerifyPoolEmpty(context.Background(), "rook-ceph", "scratch")
			if err != nil {
				t.Fatal(err)
			}
			if empty != tc.wantEmpty || !strings.Contains(reason, tc.wantReason) {
				t.Fatalf("empty=%v reason=%q, want empty=%v reason containing %q", empty, reason, tc.wantEmpty, tc.wantReason)
			}
		})
	}
}

func TestAdapterNeverSelectsAnUnconfiguredCluster(t *testing.T) {
	first := rookObject("CephCluster", "cephclusters", "first", map[string]any{"state": "Created"})
	second := rookObject("CephCluster", "cephclusters", "second", map[string]any{"state": "Created"})
	adapter := testAdapter(t, first, second)
	adapter.clusterName = "missing"
	health := adapter.Health(context.Background())
	if health.Status != storage.SeverityError || len(health.Conditions) == 0 || health.Conditions[0].Reason != "ClusterNotFoundOrAmbiguous" {
		t.Fatalf("expected explicit configured-cluster failure: %#v", health)
	}
}

func TestRookVersionRequiresCoreCRDs(t *testing.T) {
	discovery := &fakediscovery.FakeDiscovery{Fake: &ktesting.Fake{}}
	discovery.Resources = []*metav1.APIResourceList{{GroupVersion: "ceph.rook.io/v1", APIResources: []metav1.APIResource{{Name: "cephclusters"}}}}
	if _, err := detectRookVersion(discovery); err == nil {
		t.Fatal("expected incomplete Rook API to be rejected")
	}
}

func TestOptionalMirrorCRDDoesNotBlockCoreDiscovery(t *testing.T) {
	discovery := &fakediscovery.FakeDiscovery{Fake: &ktesting.Fake{}}
	discovery.Resources = []*metav1.APIResourceList{{GroupVersion: "ceph.rook.io/v1", APIResources: []metav1.APIResource{{Name: "cephclusters"}, {Name: "cephblockpools"}, {Name: "cephfilesystems"}}}}
	version, served, err := discoverRookResources(discovery)
	if err != nil || version != "v1" || !served["clusters"] || served["mirroring"] {
		t.Fatalf("version=%q served=%v err=%v", version, served, err)
	}
}

func TestSanitizedCompatibilityFixturesDecodeAndStayBounded(t *testing.T) {
	rookFixtures, err := filepath.Glob("testdata/rook-*/*.json")
	if err != nil || len(rookFixtures) < 12 {
		t.Fatalf("rook fixtures=%v err=%v", rookFixtures, err)
	}
	for _, path := range rookFixtures {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		assertSanitizedFixture(t, path, raw)
		var object unstructured.Unstructured
		if decodeErr := json.Unmarshal(raw, &object.Object); decodeErr != nil {
			t.Fatalf("decode %s: %v", path, decodeErr)
		}
		normalized := normalizeRook(&object)
		if normalized["name"] == "" || normalized["source"] != "rook-crd" {
			t.Fatalf("normalized %s = %#v", path, normalized)
		}
	}
	dashboardFixtures, err := filepath.Glob("testdata/ceph-*/*.json")
	if err != nil || len(dashboardFixtures) < 8 {
		t.Fatalf("dashboard fixtures=%v err=%v", dashboardFixtures, err)
	}
	for _, path := range dashboardFixtures {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		assertSanitizedFixture(t, path, raw)
		var value any
		if decodeErr := json.Unmarshal(raw, &value); decodeErr != nil {
			t.Fatalf("decode %s: %v", path, decodeErr)
		}
		items := normalizeDashboardList(value)
		if len(items) == 0 {
			t.Fatalf("fixture %s produced no bounded provider records", path)
		}
	}
}

func FuzzDashboardFixtureNormalizationIsBounded(f *testing.F) {
	f.Add([]byte(`[{"name":"pool-a","status":{"phase":"Ready"}}]`))
	f.Add([]byte(`{"items":[{"id":1,"hostname":"node-a"}]}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 1<<20 {
			t.Skip()
		}
		var value any
		if json.Unmarshal(raw, &value) != nil {
			return
		}
		items := normalizeDashboardList(value)
		if len(items) > 500 {
			t.Fatalf("normalization returned %d items", len(items))
		}
		encoded, err := json.Marshal(items)
		if err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(string(encoded))
		for _, marker := range []string{`"password"`, `"authorization"`, `"token"`, `"credential"`, `"secret"`} {
			if strings.Contains(lower, marker) {
				t.Fatalf("normalized response retained sensitive field %q", marker)
			}
		}
	})
}

func TestDashboardNormalizationBoundsRowsAndRedactsSensitiveKeys(t *testing.T) {
	rows := make([]any, 600)
	for index := range rows {
		rows[index] = map[string]any{"name": "pool", "password": "leak", "access_token": "leak", "accessKey": "leak", "nested": map[string]any{"credential": "leak", "secret_key": "leak", "safe": true}}
	}
	items := normalizeDashboardList(rows)
	if len(items) != 500 {
		t.Fatalf("items=%d, want 500", len(items))
	}
	encoded, _ := json.Marshal(items)
	lower := strings.ToLower(string(encoded))
	for _, marker := range []string{"password", "access_token", "accesskey", "secret_key", "credential", "leak"} {
		if strings.Contains(lower, marker) {
			t.Fatalf("sensitive marker %q retained in normalized provider response", marker)
		}
	}
}

func TestDashboardResourceNormalizationProvidesStableIdentityAndFlattensImages(t *testing.T) {
	grouped := []any{map[string]any{
		"pool_name": "replicapool",
		"value": []any{
			map[string]any{"id": "image-id", "unique_id": "replicapool/image-id", "name": "csi-image", "size": float64(1024)},
			map[string]any{"id": "second-id", "name": "second-image", "password": "must-not-leak"},
		},
	}}
	images := normalizeRBDImageList(grouped)
	if len(images) != 2 || images[0]["name"] != "csi-image" || images[0]["pool_name"] != "replicapool" {
		t.Fatalf("grouped images were not flattened: %#v", images)
	}
	if _, leaked := images[1]["password"]; leaked {
		t.Fatalf("grouped image retained a sensitive field: %#v", images[1])
	}
	addDashboardResourceIdentity("rbd-images", images[0])
	if images[0]["id"] != "image-id" {
		t.Fatalf("existing image identity was overwritten: %#v", images[0])
	}

	quorum := map[string]any{"health": map[string]any{"status": "HEALTH_OK"}}
	addDashboardResourceIdentity("quorum", quorum)
	if quorum["id"] != "cluster" || quorum["name"] != "Ceph quorum" || quorum["state"] != "HEALTH_OK" {
		t.Fatalf("quorum identity=%#v", quorum)
	}
	osd := map[string]any{"osd": float64(0), "id": float64(0)}
	addDashboardResourceIdentity("osds", osd)
	if osd["name"] != "osd.0" {
		t.Fatalf("OSD identity=%#v", osd)
	}
}

func TestPoolMergePreservesDesiredAndRuntimeSources(t *testing.T) {
	desired := []map[string]any{
		{"id": "rook-ceph/replicapool", "name": "replicapool", "source": "rook-crd", "spec": map[string]any{"replicated": map[string]any{"size": 3}}},
		{"id": "rook-ceph/pending", "name": "pending", "source": "rook-crd"},
	}
	runtimePools := []map[string]any{
		{"pool_name": "replicapool", "used": 1024, "observedAt": "now", "source": "ceph-dashboard"},
		{"pool_name": "legacy", "used": 2048, "observedAt": "now", "source": "ceph-dashboard"},
	}
	merged := mergePoolData(desired, runtimePools)
	if len(merged) != 3 {
		t.Fatalf("merged=%#v", merged)
	}
	byName := map[string]map[string]any{}
	for _, pool := range merged {
		byName[fmt.Sprint(pool["name"])] = pool
	}
	if byName["replicapool"]["source"] != "rook-crd+ceph-dashboard" || byName["replicapool"]["runtimeState"] != "observed" {
		t.Fatalf("joined pool=%#v", byName["replicapool"])
	}
	if byName["replicapool"]["spec"] == nil || byName["replicapool"]["runtime"] == nil {
		t.Fatal("pool merge overwrote one of the authoritative sources")
	}
	if byName["pending"]["runtimeState"] != "not-observed" || byName["legacy"]["runtimeState"] != "runtime-only" {
		t.Fatalf("unmatched states=%#v", byName)
	}
}

func TestPoolCapabilitiesRequireFreshDashboardVerification(t *testing.T) {
	adapter := testAdapter(t, rookObject("CephCluster", "cephclusters", "rook-ceph", map[string]any{"state": "Created", "ceph": map[string]any{"health": "HEALTH_OK"}}))
	adapter.version = "1.20.2"
	adapter.writesEnabled = true
	adapter.allowPoolDelete = true
	capabilities := adapter.Capabilities(context.Background())
	for _, capability := range capabilities {
		if capability == storage.CapabilityCephPoolCreate || capability == storage.CapabilityCephPoolDelete {
			t.Fatalf("pool capability %q advertised without a runtime verifier", capability)
		}
	}
}

func TestCephWriteCapabilitiesRequireSupportedOperatorVersion(t *testing.T) {
	cluster := rookObject("CephCluster", "cephclusters", "rook-ceph", map[string]any{"state": "Created", "ceph": map[string]any{"health": "HEALTH_OK"}})
	adapter := testAdapter(t, cluster)
	adapter.writesEnabled = true
	for _, version := range []string{"", "1.18.10", "2.0.0", "1.20.2-debug", "1.20.latest"} {
		adapter.version = version
		for _, capability := range adapter.Capabilities(context.Background()) {
			if capability == storage.CapabilityCephClassCreate {
				t.Fatalf("Ceph write capability advertised for unsupported version %q", version)
			}
		}
	}
	adapter.version = "1.19.6"
	if !containsCapability(adapter.Capabilities(context.Background()), storage.CapabilityCephClassCreate) {
		t.Fatal("supported current/previous Rook version did not advertise configured Class workflow")
	}
}

func TestCephCapabilitiesFollowRuntimeWritePolicy(t *testing.T) {
	cluster := rookObject("CephCluster", "cephclusters", "rook-ceph", map[string]any{"state": "Created", "ceph": map[string]any{"health": "HEALTH_OK"}})
	_ = unstructured.SetNestedField(cluster.Object, "quay.io/ceph/ceph:v19.2.3", "spec", "cephVersion", "image")
	adapter := testAdapter(t, cluster)
	adapter.version = "1.20.2"
	enabled := false
	adapter.writePolicy = func() (bool, bool, bool) { return enabled, false, false }
	if containsCapability(adapter.Capabilities(context.Background()), storage.CapabilityCephClassCreate) {
		t.Fatal("runtime policy disabled but Ceph write capability was advertised")
	}
	enabled = true
	if !containsCapability(adapter.Capabilities(context.Background()), storage.CapabilityCephClassCreate) {
		t.Fatal("runtime policy enabled but Ceph write capability remained unavailable")
	}
}

func TestCephWriteCapabilitiesRequireSupportedCephVersion(t *testing.T) {
	cluster := rookObject("CephCluster", "cephclusters", "rook-ceph", map[string]any{"state": "Created", "ceph": map[string]any{"health": "HEALTH_OK"}})
	adapter := testAdapter(t, cluster)
	adapter.writesEnabled = true
	for _, image := range []string{"quay.io/ceph/ceph:v20.2.0", "quay.io/ceph/ceph:v18.2.4", "quay.io/ceph/ceph:main", "quay.io/ceph/ceph@sha256:deadbeef"} {
		_ = unstructured.SetNestedField(cluster.Object, image, "spec", "cephVersion", "image")
		adapter = testAdapter(t, cluster)
		adapter.writesEnabled = true
		if containsCapability(adapter.Capabilities(context.Background()), storage.CapabilityCephClassCreate) {
			t.Fatalf("Ceph write capability advertised for unsupported image %q", image)
		}
	}
	for _, image := range []string{"quay.io/ceph/ceph:v19.2.3", "quay.io/ceph/ceph:v20.2.1", "quay.io/ceph/ceph:v20.2.7@sha256:deadbeef"} {
		_ = unstructured.SetNestedField(cluster.Object, image, "spec", "cephVersion", "image")
		adapter = testAdapter(t, cluster)
		adapter.writesEnabled = true
		if !containsCapability(adapter.Capabilities(context.Background()), storage.CapabilityCephClassCreate) {
			t.Fatalf("supported Ceph image %q did not advertise configured Class workflow", image)
		}
	}
}

func TestOperatorVersionComesFromFixedDeployment(t *testing.T) {
	deployment := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]any{"name": "rook-ceph-operator", "namespace": "rook-ceph"},
		"spec":     map[string]any{"template": map[string]any{"spec": map[string]any{"containers": []any{map[string]any{"name": "rook-ceph-operator", "image": "quay.io/rook/ceph:v1.20.2@sha256:0123456789abcdef"}}}}},
	}}
	adapter := testAdapter(t, rookObject("CephCluster", "cephclusters", "rook-ceph", map[string]any{"state": "Created"}), deployment)
	adapter.version = ""
	if version := adapter.OperatorVersion(context.Background()); version != "1.20.2" {
		t.Fatalf("operator version=%q", version)
	}
}

func containsCapability(capabilities []storage.Capability, expected storage.Capability) bool {
	for _, capability := range capabilities {
		if capability == expected {
			return true
		}
	}
	return false
}

func assertSanitizedFixture(t *testing.T, path string, raw []byte) {
	t.Helper()
	lower := strings.ToLower(string(raw))
	for _, marker := range []string{"bearer ", `"password"`, `"authorization"`, `"token"`, `"credential"`, `"secret"`} {
		if strings.Contains(lower, marker) {
			t.Fatalf("sensitive marker %q found in %s", marker, path)
		}
	}
}

func testAdapter(t *testing.T, objects ...runtime.Object) *Adapter {
	t.Helper()
	listKinds := map[schema.GroupVersionResource]string{}
	for _, resource := range rookResources {
		listKinds[schema.GroupVersionResource{Group: "ceph.rook.io", Version: "v1", Resource: resource}] = kindForResource(resource) + "List"
	}
	dynamic := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, objects...)
	discovery := &fakediscovery.FakeDiscovery{Fake: &ktesting.Fake{}}
	discovery.Resources = []*metav1.APIResourceList{{GroupVersion: "ceph.rook.io/v1", APIResources: []metav1.APIResource{{Name: "cephclusters"}, {Name: "cephblockpools"}, {Name: "cephfilesystems"}, {Name: "cephrbdmirrors"}}}}
	adapter, err := New(Config{Namespace: "rook-ceph", ClusterName: "rook-ceph", Version: "1.20.2", Dynamic: dynamic, Discovery: discovery})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func rookObject(kind, resource, name string, status map[string]any) *unstructured.Unstructured {
	object := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "ceph.rook.io/v1", "kind": kind,
		"metadata": map[string]any{"name": name, "namespace": "rook-ceph", "uid": resource + "-uid"},
		"status":   status,
	}}
	if kind == "CephCluster" {
		object.Object["spec"] = map[string]any{"cephVersion": map[string]any{"image": "quay.io/ceph/ceph:v20.2.1"}}
	}
	return object
}

func kindForResource(resource string) string {
	switch resource {
	case "cephclusters":
		return "CephCluster"
	case "cephblockpools":
		return "CephBlockPool"
	case "cephfilesystems":
		return "CephFilesystem"
	case "cephrbdmirrors":
		return "CephRBDMirror"
	default:
		return "Unknown"
	}
}
