package policy

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestValidateAndIntersectFailClosed(t *testing.T) {
	if err := Validate(StoragePolicy{LonghornWrites: true}); err == nil {
		t.Fatal("provider write accepted without master gate")
	}
	if err := Validate(StoragePolicy{AcceptNewOperations: true, AllowCephPoolDelete: true}); err == nil {
		t.Fatal("pool delete accepted without Ceph writes")
	}
	requested := StoragePolicy{
		AcceptNewOperations: true, PortableKubernetesWrites: true,
		LonghornWrites: true, RookCephWrites: true, AllowCephPoolDelete: true,
	}
	effective, conditions := Intersect(requested, Ceiling{PortableKubernetesWrites: true}, time.Now())
	if !effective.AcceptNewOperations || !effective.PortableKubernetesWrites || effective.LonghornWrites || effective.RookCephWrites || effective.AllowCephPoolDelete {
		t.Fatalf("unexpected effective policy: %+v", effective)
	}
	if len(conditions) != 2 || conditions[1].Status != "False" {
		t.Fatalf("ceiling condition=%+v", conditions)
	}
}

func TestStaticManagerPreservesLegacyPolicy(t *testing.T) {
	requested := StoragePolicy{AcceptNewOperations: true, LonghornWrites: true}
	manager, err := NewManager(Config{
		Namespace: "test", StaticRequested: requested,
		Ceiling: Ceiling{LonghornWrites: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := manager.Snapshot()
	if snapshot.Source != "static-helm" || !snapshot.Effective.LonghornWrites {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}

func TestRuntimeManagerUpdateUsesOptimisticConcurrency(t *testing.T) {
	scheme := runtime.NewScheme()
	object := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": APIVersion, "kind": Kind,
		"metadata": map[string]any{"name": Name, "namespace": "test", "resourceVersion": "1"},
		"spec": map[string]any{"storage": map[string]any{
			"acceptNewOperations": false, "portableKubernetesWrites": false,
			"longhornWrites": false, "rookCephWrites": false,
			"allowCephStorageClassDelete": false, "allowCephPoolDelete": false,
		}},
	}}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme, map[schema.GroupVersionResource]string{GVR: "HighlandPolicyList"}, object,
	)
	manager, err := NewManager(Config{
		Dynamic: client, Namespace: "test", Enabled: true,
		Ceiling: Ceiling{LonghornWrites: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Update(context.Background(), StoragePolicy{
		AcceptNewOperations: true, LonghornWrites: true,
	}, "stale", "admin", "request-1"); !errors.Is(err, ErrStale) {
		t.Fatalf("stale update error=%v", err)
	}
	current, _ := client.Resource(GVR).Namespace("test").Get(context.Background(), Name, metav1.GetOptions{})
	updated, err := manager.Update(context.Background(), StoragePolicy{
		AcceptNewOperations: true, LonghornWrites: true,
	}, current.GetResourceVersion(), "admin", "request-2")
	if err != nil {
		t.Fatal(err)
	}
	value, _, _ := unstructured.NestedBool(updated.Object, "spec", "storage", "longhornWrites")
	if !value || updated.GetAnnotations()["highland.io/last-changed-by"] != "admin" {
		t.Fatalf("updated=%v", updated.Object)
	}
}

func TestRuntimeManagerRejectsCeilingEscalation(t *testing.T) {
	object := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": APIVersion, "kind": Kind,
		"metadata": map[string]any{"name": Name, "namespace": "test", "resourceVersion": "1"},
		"spec": map[string]any{"storage": map[string]any{
			"acceptNewOperations": false, "portableKubernetesWrites": false,
			"longhornWrites": false, "rookCephWrites": false,
			"allowCephStorageClassDelete": false, "allowCephPoolDelete": false,
		}},
	}}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(), map[schema.GroupVersionResource]string{GVR: "HighlandPolicyList"}, object,
	)
	manager, err := NewManager(Config{
		Dynamic:   client,
		Namespace: "test", Enabled: true, Ceiling: Ceiling{},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.Update(context.Background(), StoragePolicy{
		AcceptNewOperations: true, LonghornWrites: true,
	}, "1", "admin", "request")
	var ceilingErr *CeilingError
	if !errors.As(err, &ceilingErr) {
		t.Fatalf("unexpected error=%v", err)
	}
}

func TestRuntimeManagerPublishesCoherentSnapshotsToConcurrentReaders(t *testing.T) {
	manager, err := NewManager(Config{
		Dynamic:   dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()),
		Namespace: "test", Enabled: true,
		Ceiling: Ceiling{PortableKubernetesWrites: true, LonghornWrites: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	var readers sync.WaitGroup
	for range 8 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for range 500 {
				snapshot := manager.Snapshot()
				if snapshot.Effective.LonghornWrites && !snapshot.Effective.AcceptNewOperations {
					t.Errorf("observed torn Longhorn snapshot: %+v", snapshot)
					return
				}
				if snapshot.Effective.PortableKubernetesWrites && !snapshot.Effective.AcceptNewOperations {
					t.Errorf("observed torn portable snapshot: %+v", snapshot)
					return
				}
			}
		}()
	}
	for generation := 1; generation <= 100; generation++ {
		enabled := generation%2 == 0
		manager.observe(policyObject(strconv.Itoa(generation), int64(generation), StoragePolicy{
			AcceptNewOperations: enabled, PortableKubernetesWrites: enabled, LonghornWrites: enabled,
		}))
	}
	readers.Wait()
}

func TestRuntimeManagerMalformedPolicyFailsClosed(t *testing.T) {
	manager, err := NewManager(Config{
		Dynamic:   dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()),
		Namespace: "test", Enabled: true, Ceiling: Ceiling{LonghornWrites: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	manager.observe(&unstructured.Unstructured{Object: map[string]any{
		"apiVersion": APIVersion, "kind": Kind,
		"metadata": map[string]any{"name": Name, "resourceVersion": "1"},
		"spec": map[string]any{"storage": map[string]any{
			"acceptNewOperations": false, "longhornWrites": true,
		}},
	}})
	snapshot := manager.Snapshot()
	if snapshot.Source != "unavailable" || !snapshot.Stale || !Equal(snapshot.Effective, StoragePolicy{}) {
		t.Fatalf("malformed policy did not fail closed: %+v", snapshot)
	}
}

func TestPortableProviderScopeNormalizationAndValidation(t *testing.T) {
	legacy := Normalize(StoragePolicy{AcceptNewOperations: true, PortableKubernetesWrites: true})
	if len(legacy.PortableKubernetesProviderIDs) != 1 || legacy.PortableKubernetesProviderIDs[0] != "*" {
		t.Fatalf("legacy policy was not normalized to wildcard: %+v", legacy)
	}
	explicit := Normalize(StoragePolicy{
		AcceptNewOperations: true, PortableKubernetesWrites: true,
		PortableKubernetesProviderIDs: []string{"rook-ceph", "longhorn", "rook-ceph"},
	})
	if !Equal(explicit, StoragePolicy{
		AcceptNewOperations: true, PortableKubernetesWrites: true,
		PortableKubernetesProviderIDs: []string{"longhorn", "rook-ceph"},
	}) {
		t.Fatalf("provider scopes were not sorted and deduplicated: %+v", explicit)
	}
	for _, invalid := range []StoragePolicy{
		{PortableKubernetesProviderIDs: []string{"longhorn"}},
		{AcceptNewOperations: true, PortableKubernetesWrites: true, PortableKubernetesProviderIDs: []string{}},
		{AcceptNewOperations: true, PortableKubernetesWrites: true, PortableKubernetesProviderIDs: []string{"*", "longhorn"}},
		{AcceptNewOperations: true, PortableKubernetesWrites: true, PortableKubernetesProviderIDs: []string{"NOT_VALID"}},
	} {
		if err := Validate(invalid); err == nil {
			t.Fatalf("invalid provider policy accepted: %+v", invalid)
		}
	}
}

func TestPortableProviderAuthorization(t *testing.T) {
	explicit := StoragePolicy{
		AcceptNewOperations: true, PortableKubernetesWrites: true,
		PortableKubernetesProviderIDs: []string{"longhorn"},
	}
	if !explicit.AllowsPortableProvider("longhorn") || explicit.AllowsPortableProvider("rook-ceph") || explicit.AllowsPortableProvider("") {
		t.Fatalf("explicit scope authorization is incorrect: %+v", explicit)
	}
	legacy := StoragePolicy{AcceptNewOperations: true, PortableKubernetesWrites: true}
	if !legacy.AllowsPortableProvider("csi-detected-driver") {
		t.Fatal("legacy wildcard did not preserve portable provider access")
	}
	disabled := explicit
	disabled.AcceptNewOperations = false
	if disabled.AllowsPortableProvider("longhorn") {
		t.Fatal("global gate did not fail closed")
	}
}

func TestSnapshotClonePreservesExplicitEmptyProviderScope(t *testing.T) {
	snapshot := cloneSnapshot(Snapshot{
		Requested: StoragePolicy{PortableKubernetesProviderIDs: []string{}},
		Effective: StoragePolicy{PortableKubernetesProviderIDs: []string{}},
	})
	if snapshot.Requested.PortableKubernetesProviderIDs == nil || snapshot.Effective.PortableKubernetesProviderIDs == nil {
		t.Fatalf("explicit empty scope became legacy nil: %#v", snapshot)
	}
}

func TestPolicyStatusComparisonIgnoresTransitionTimestampOnly(t *testing.T) {
	current := map[string]any{
		"observedGeneration": int64(3),
		"effective":          structMap(StoragePolicy{AcceptNewOperations: true}),
		"conditions": []any{map[string]any{
			"type": "Ready", "status": "True", "reason": "PolicyObserved",
			"message":            "runtime storage policy is observed",
			"lastTransitionTime": "2026-07-16T12:00:00Z",
		}},
	}
	desired := map[string]any{
		"observedGeneration": int64(3),
		"effective":          structMap(StoragePolicy{AcceptNewOperations: true}),
		"conditions": []any{map[string]any{
			"type": "Ready", "status": "True", "reason": "PolicyObserved",
			"message":            "runtime storage policy is observed",
			"lastTransitionTime": "2026-07-16T13:00:00Z",
		}},
	}
	if !statusSemanticallyEqual(current, desired) {
		t.Fatal("timestamp-only status change would trigger an update loop")
	}
	desired["observedGeneration"] = int64(4)
	if statusSemanticallyEqual(current, desired) {
		t.Fatal("observed-generation change was ignored")
	}
}

func FuzzPortableProviderNormalization(f *testing.F) {
	f.Add("longhorn,rook-ceph,longhorn")
	f.Add("*,longhorn")
	f.Add("Vendor.EXAMPLE.CSI")
	f.Fuzz(func(t *testing.T, raw string) {
		parts := strings.Split(raw, ",")
		if len(parts) > 128 {
			parts = parts[:128]
		}
		value := Normalize(StoragePolicy{
			AcceptNewOperations: true, PortableKubernetesWrites: true,
			PortableKubernetesProviderIDs: parts,
		})
		second := Normalize(value)
		if !Equal(value, second) {
			t.Fatalf("normalization is not idempotent: %#v then %#v", value, second)
		}
		for index := 1; index < len(value.PortableKubernetesProviderIDs); index++ {
			if value.PortableKubernetesProviderIDs[index-1] >= value.PortableKubernetesProviderIDs[index] {
				t.Fatalf("provider IDs are not strictly sorted and unique: %#v", value.PortableKubernetesProviderIDs)
			}
		}
	})
}

func policyObject(resourceVersion string, generation int64, requested StoragePolicy) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": APIVersion, "kind": Kind,
		"metadata": map[string]any{
			"name": Name, "resourceVersion": resourceVersion, "generation": generation,
		},
		"spec": map[string]any{"storage": structMap(requested)},
	}}
}
