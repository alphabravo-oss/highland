package storage

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOpenAPIContainsEveryStorageRoute(t *testing.T) {
	contract := readRepoFile(t, "docs/openapi/storage-v1.yaml")
	routes := []string{
		"/api/v1/storage/providers", "/api/v1/storage/providers/{providerId}",
		"/api/v1/storage/drivers", "/api/v1/storage/classes", "/api/v1/storage/claims",
		"/api/v1/storage/claims/{namespace}/{name}", "/api/v1/storage/claims/{namespace}/{name}/size",
		"/api/v1/storage/volumes", "/api/v1/storage/volumes/{name}", "/api/v1/storage/snapshots",
		"/api/v1/storage/snapshots/{namespace}/{name}", "/api/v1/storage/attachments",
		"/api/v1/storage/capacity", "/api/v1/storage/events", "/api/v1/storage/actions",
		"/api/v1/storage/timeline", "/api/v1/storage/capacity/ownership",
		"/api/v1/storage/comparison", "/api/v1/storage/remediations",
		"/api/v1/storage/plans", "/api/v1/storage/operations", "/api/v1/storage/operations/{operationId}",
		"/api/v1/storage/restores", "/api/v1/storage/clones",
		"/api/v1/storage/relationships", "/api/v1/storage/resources/{kind}/{id}/relationships",
		"/api/v1/storage/impact", "/api/v1/providers/{providerId}/relationships",
		"/api/v1/providers/{providerId}/drift",
		"/api/v1/providers/{providerId}/capacity/forecast",
		"/api/v1/providers/{providerId}/summary", "/api/v1/providers/{providerId}/health",
		"/api/v1/providers/{providerId}/resources/{kind}", "/api/v1/providers/{providerId}/resources/{kind}/{id}",
		"/api/v1/providers/{providerId}/ceph/block-pools",
		"/api/v1/providers/{providerId}/ceph/block-pools/{namespace}/{name}",
		"/api/v1/providers/{providerId}/ceph/storage-classes",
		"/api/v1/providers/{providerId}/ceph/storage-classes/{name}",
	}
	for _, route := range routes {
		if !strings.Contains(contract, "  "+route+":") {
			t.Errorf("OpenAPI is missing route %s", route)
		}
	}
	for _, required := range []string{"ErrorEnvelope:", "OperationPlan:", "StorageOperation:", "requestedCapacity:", "provisionedCapacity:", "continuation-token"} {
		if !strings.Contains(contract, required) {
			t.Errorf("OpenAPI is missing contract marker %q", required)
		}
	}
}

func TestCompatibilityContractPinsRequiredVersions(t *testing.T) {
	compatibility := readRepoFile(t, "docs/compatibility.yaml")
	for _, required := range []string{"clientLibrary: \"k8s.io/client-go v0.36.2\"", "embeddedChart: \"1.12.0\"", "rookApi: ceph.rook.io/v1", "release-gate"} {
		if !strings.Contains(compatibility, required) {
			t.Errorf("compatibility matrix is missing %q", required)
		}
	}
}

func readRepoFile(t *testing.T, relative string) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate contract test source")
	}
	repo := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../.."))
	contents, err := os.ReadFile(filepath.Join(repo, relative))
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}
