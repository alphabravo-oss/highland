package operations

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/highland-io/highland/apps/api/internal/auth"
	appmw "github.com/highland-io/highland/apps/api/internal/middleware"
	"github.com/highland-io/highland/apps/api/internal/observability"
	"github.com/highland-io/highland/apps/api/internal/storage"
)

type operationObserverStub struct {
	started, finished int
	result            string
}

func (o *operationObserverStub) OperationStarted(string, string) { o.started++ }
func (o *operationObserverStub) OperationFinished(_, _, result string, _ time.Duration) {
	o.finished++
	o.result = result
}
func (*operationObserverStub) OperationRetry(string, string) {}

type impactAnalyzerStub struct {
	result storage.ImpactResult
	err    error
}

func (s impactAnalyzerStub) AnalyzeImpact(context.Context, string, string, string, int) (storage.ImpactResult, error) {
	return s.result, s.err
}

type poolSafetyStub struct {
	empty, present bool
	reason         string
	err            error
}

func (s poolSafetyStub) VerifyPoolEmpty(context.Context, string, string) (bool, string, error) {
	return s.empty, s.reason, s.err
}
func (s poolSafetyStub) VerifyPoolPresent(context.Context, string, string) (bool, string, error) {
	return s.present, s.reason, s.err
}

func newPlanner(t *testing.T, objects ...runtime.Object) *Planner {
	t.Helper()
	core := kubernetesfake.NewSimpleClientset(objects...)
	dynamic := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	planner, err := NewPlanner(PlannerConfig{Core: core, Dynamic: dynamic, Scope: storage.NewScope("cluster", nil), Secret: []byte("0123456789abcdef0123456789abcdef")})
	if err != nil {
		t.Fatal(err)
	}
	return planner
}

func TestActionRegistryAuthorizationMatrix(t *testing.T) {
	for _, action := range Actions() {
		if err := Authorize(action, auth.RoleViewer); err == nil {
			t.Fatalf("viewer unexpectedly authorized for %s", action.ID)
		}
		operatorErr := Authorize(action, auth.RoleOperator)
		if action.MinimumRole == "admin" && operatorErr == nil {
			t.Fatalf("operator unexpectedly authorized for admin action %s", action.ID)
		}
		if action.MinimumRole == "operator" && operatorErr != nil {
			t.Fatalf("operator rejected for %s: %v", action.ID, operatorErr)
		}
		if err := Authorize(action, auth.RoleAdmin); err != nil {
			t.Fatalf("admin rejected for %s: %v", action.ID, err)
		}
	}
}

func TestTargetFromBodyTreatsRestoreAsNewPVC(t *testing.T) {
	tests := map[string]string{
		"create-snapshot":              "VolumeSnapshot",
		"restore-snapshot":             "PersistentVolumeClaim",
		"clone-pvc":                    "PersistentVolumeClaim",
		"create-ceph-rbd-storageclass": "StorageClass",
		"create-ceph-blockpool":        "CephBlockPool",
	}
	for actionID, wantKind := range tests {
		request := Request{ActionID: actionID}
		targetFromBody(nil, &request)
		if request.Target.Kind != wantKind {
			t.Fatalf("action %s target kind=%s, want %s", actionID, request.Target.Kind, wantKind)
		}
	}
}

func TestCephFeatureGateRequiresSupportedProviderVersion(t *testing.T) {
	action, _ := ActionByID("create-ceph-rbd-storageclass")
	unsafe := NewAPI(APIConfig{WritesEnabled: true, CephWritesEnabled: true, CephPoolVerified: true, CephVersionSafe: false})
	if unsafe.featureEnabled(context.Background(), action) {
		t.Fatal("Ceph workflow enabled without a supported operator version")
	}
	safe := NewAPI(APIConfig{WritesEnabled: true, CephWritesEnabled: true, CephPoolVerified: true, CephVersionSafe: true})
	if !safe.featureEnabled(context.Background(), action) {
		t.Fatal("Ceph workflow remained disabled for a supported operator version")
	}
}

func TestCephStorageClassDeleteHasIndependentDestructiveGate(t *testing.T) {
	action, ok := ActionByID("delete-ceph-storageclass")
	if !ok {
		t.Fatal("delete Ceph StorageClass action is missing")
	}
	createOnly := NewAPI(APIConfig{
		WritesEnabled:           true,
		CephWritesEnabled:       true,
		CephVersionSafe:         true,
		AllowStorageClassDelete: false,
	})
	if createOnly.featureEnabled(context.Background(), action) {
		t.Fatal("StorageClass deletion was enabled by the create-only Ceph gate")
	}
	deleteEnabled := NewAPI(APIConfig{
		WritesEnabled:           true,
		CephWritesEnabled:       true,
		CephVersionSafe:         true,
		AllowStorageClassDelete: true,
	})
	if !deleteEnabled.featureEnabled(context.Background(), action) {
		t.Fatal("StorageClass deletion remained disabled after its explicit gate was enabled")
	}
}

func TestDestructivePlanUsesSharedImpactResultAndFailsClosed(t *testing.T) {
	class := &storagev1.StorageClass{
		ObjectMeta:  metav1.ObjectMeta{Name: "ceph-rbd", UID: "class-uid", ResourceVersion: "7"},
		Provisioner: "rook-ceph.rbd.csi.ceph.com",
	}
	core := kubernetesfake.NewSimpleClientset(class)
	dynamic := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	targetID := storage.CanonicalGraphID("storageclass", "rook-ceph", "", class.Name)
	complete := storage.ImpactResult{
		ProviderID: "rook-ceph",
		Target:     storage.GraphNode{ID: targetID, Kind: "storageclass", ProviderID: "rook-ceph", Name: class.Name},
		Confirmed: []storage.ImpactResource{{
			Node:       storage.GraphNode{ID: "pod-id", Kind: "pod", ProviderID: "rook-ceph", Namespace: "team-a", Name: "database-0", UID: "pod-uid"},
			Confidence: storage.ConfidenceAuthoritative,
		}},
		Summary: storage.ImpactSummary{WorkloadCount: 1, PodCount: 1, NamespaceCount: 1},
	}
	planner, err := NewPlanner(PlannerConfig{
		Core: core, Dynamic: dynamic, Scope: storage.NewScope("cluster", nil),
		Secret:         []byte("0123456789abcdef0123456789abcdef"),
		ImpactAnalyzer: impactAnalyzerStub{result: complete}, RequireImpactAnalysis: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planner.Plan(context.Background(), "admin", Request{
		ActionID: "delete-ceph-storageclass", ProviderID: "rook-ceph",
		Target: ResourceTarget{Kind: "StorageClass", Name: class.Name},
	})
	if err == nil {
		// The existing class-dependency guard must remain authoritative and
		// normally blocks before shared impact when a real Pod/PVC exists. This
		// synthetic graph-only dependency proves the result is still surfaced.
		found := false
		for _, check := range plan.Checks {
			found = found || check.ID == "shared-impact-analysis"
		}
		if !found || len(plan.Dependencies) != 1 || plan.Dependencies[0].Name != "database-0" {
			t.Fatalf("shared impact was not reused: %#v", plan)
		}
	} else {
		t.Fatal(err)
	}

	incompletePlanner, err := NewPlanner(PlannerConfig{
		Core: core, Dynamic: dynamic, Scope: storage.NewScope("cluster", nil),
		Secret:                []byte("0123456789abcdef0123456789abcdef"),
		ImpactAnalyzer:        impactAnalyzerStub{result: storage.ImpactResult{Target: complete.Target, Incomplete: true}},
		RequireImpactAnalysis: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = incompletePlanner.Plan(context.Background(), "admin", Request{
		ActionID: "delete-ceph-storageclass", ProviderID: "rook-ceph",
		Target: ResourceTarget{Kind: "StorageClass", Name: class.Name},
	})
	var planErr *PlanError
	if !errors.As(err, &planErr) || planErr.Code != "IMPACT_ANALYSIS_INCOMPLETE" {
		t.Fatalf("incomplete impact must fail closed, err=%v", err)
	}
}

func TestWriteRouteRemainsDisabledForAdminWhenFeatureGateIsOff(t *testing.T) {
	dynamic := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{OperationGVR: "StorageOperationList"})
	store, _ := NewStore(dynamic, "highland-system")
	planner, _ := NewPlanner(PlannerConfig{Core: kubernetesfake.NewSimpleClientset(), Dynamic: dynamic, Scope: storage.NewScope("cluster", nil), Secret: []byte("0123456789abcdef0123456789abcdef")})
	api := NewAPI(APIConfig{Store: store, Planner: planner, WritesEnabled: false})
	sessions := auth.NewStore(time.Hour)
	sessionID, err := sessions.Create(auth.User{Username: "admin", Role: auth.RoleAdmin})
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	router.Use(appmw.SessionAuth(sessions, "highland_session", observability.New()))
	api.Mount(router)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/storage/claims", strings.NewReader(`{"target":{"kind":"PersistentVolumeClaim","namespace":"default","name":"data"}}`))
	req.AddCookie(&http.Cookie{Name: "highland_session", Value: sessionID})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "ACTION_FORBIDDEN") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestNamespaceWriteScopeFailsClosed(t *testing.T) {
	core := kubernetesfake.NewSimpleClientset(&storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "fast"}, Provisioner: "example.csi.io"})
	planner, err := NewPlanner(PlannerConfig{Core: core, Dynamic: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()), Scope: storage.NewScope("namespaces", []string{"team-a"}), Secret: []byte("0123456789abcdef0123456789abcdef")})
	if err != nil {
		t.Fatal(err)
	}
	request := Request{ActionID: "create-pvc", Target: ResourceTarget{Kind: "PersistentVolumeClaim", Namespace: "team-b", Name: "data"}, Parameters: map[string]any{"storageClass": "fast", "size": "1Gi"}}
	_, err = planner.Plan(context.Background(), "alice", request)
	var planError *PlanError
	if !errors.As(err, &planError) || planError.Code != "NAMESPACE_NOT_ALLOWED" {
		t.Fatalf("out-of-scope error=%v", err)
	}
	request.Target.Namespace = "team-a"
	if _, err := planner.Plan(context.Background(), "alice", request); err != nil {
		t.Fatalf("allowlisted namespace rejected: %v", err)
	}
}

func TestTransientDependencyErrorsAreRetryableInPlanAndEnvelope(t *testing.T) {
	core := kubernetesfake.NewSimpleClientset(&storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "fast"}, Provisioner: "example.csi.io"})
	core.PrependReactor("get", "persistentvolumeclaims", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewTooManyRequests("temporarily throttled", 1)
	})
	planner, err := NewPlanner(PlannerConfig{Core: core, Dynamic: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()), Scope: storage.NewScope("cluster", nil), Secret: []byte("0123456789abcdef0123456789abcdef")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = planner.Plan(context.Background(), "alice", Request{ActionID: "create-pvc", Target: ResourceTarget{Kind: "PersistentVolumeClaim", Namespace: "default", Name: "data"}, Parameters: map[string]any{"storageClass": "fast", "size": "1Gi"}})
	var planError *PlanError
	if !errors.As(err, &planError) || !planError.Retryable || !retryable(err) {
		t.Fatalf("transient plan error=%#v", err)
	}
	recorder := httptest.NewRecorder()
	NewAPI(APIConfig{}).writePlanError(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/storage/plans", nil), "kubernetes", err)
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), `"retryable":true`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestDependencyAuthorizationErrorsAreTerminalAndExplicit(t *testing.T) {
	core := kubernetesfake.NewSimpleClientset()
	core.PrependReactor("get", "persistentvolumeclaims", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "persistentvolumeclaims"}, "data", errors.New("denied"))
	})
	planner, err := NewPlanner(PlannerConfig{Core: core, Dynamic: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()), Scope: storage.NewScope("cluster", nil), Secret: []byte("0123456789abcdef0123456789abcdef")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = planner.Plan(context.Background(), "alice", Request{ActionID: "expand-pvc", Target: ResourceTarget{Kind: "PersistentVolumeClaim", Namespace: "default", Name: "data"}, Parameters: map[string]any{"size": "2Gi"}})
	var planError *PlanError
	if !errors.As(err, &planError) || planError.Code != "DEPENDENCY_PERMISSION_DENIED" || planError.Retryable || retryable(err) {
		t.Fatalf("authorization plan error=%#v", err)
	}
}

func TestSnapshotActionPrerequisiteTracksDiscovery(t *testing.T) {
	core := kubernetesfake.NewSimpleClientset()
	planner, err := NewPlanner(PlannerConfig{Core: core, Dynamic: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()), Scope: storage.NewScope("cluster", nil), Secret: []byte("0123456789abcdef0123456789abcdef")})
	if err != nil {
		t.Fatal(err)
	}
	action, _ := ActionByID("create-snapshot")
	if available, _ := planner.ActionPrerequisite(context.Background(), action); available {
		t.Fatal("snapshot action advertised without the snapshot API")
	}
	discovery := core.Discovery().(*fakediscovery.FakeDiscovery)
	discovery.Resources = []*metav1.APIResourceList{{GroupVersion: "snapshot.storage.k8s.io/v1", APIResources: []metav1.APIResource{{Name: "volumesnapshots"}, {Name: "volumesnapshotclasses"}, {Name: "volumesnapshotcontents"}}}}
	if available, reason := planner.ActionPrerequisite(context.Background(), action); !available {
		t.Fatalf("served snapshot API rejected: %s", reason)
	}
}

func TestPlanChallengeBindsUserPlanAndExpiry(t *testing.T) {
	allow := true
	planner := newPlanner(t, &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "fast", UID: types.UID("class-uid"), ResourceVersion: "7"}, Provisioner: "example.csi.io", AllowVolumeExpansion: &allow})
	request := Request{ActionID: "create-pvc", Target: ResourceTarget{Kind: "PersistentVolumeClaim", Namespace: "tenant-a", Name: "data"}, Parameters: map[string]any{"storageClass": "fast", "size": "9007199254740993", "accessModes": []any{"ReadWriteOnce"}}}
	plan, err := planner.Plan(context.Background(), "alice", request)
	if err != nil {
		t.Fatal(err)
	}
	request.Confirmation.Challenge = plan.Challenge
	if err := planner.Verify("alice", request, plan); err != nil {
		t.Fatalf("valid confirmation rejected: %v", err)
	}
	if err := planner.Verify("mallory", request, plan); err == nil {
		t.Fatal("confirmation replay by another user was accepted")
	}
	changed := plan
	changed.Hash = "changed"
	if err := planner.Verify("alice", request, changed); err == nil {
		t.Fatal("confirmation accepted after plan changed")
	}
	if plan.Resources[0].Manifest["spec"].(map[string]any)["resources"].(map[string]any)["requests"].(map[string]any)["storage"] != "9007199254740993" {
		t.Fatal("quantity lost exact string representation")
	}
}

func TestPlanWarningsRequireSeparateAcknowledgement(t *testing.T) {
	planner := newPlanner(t, &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "fast"}, Provisioner: "example.csi.io"})
	request := Request{ActionID: "create-pvc", Target: ResourceTarget{Kind: "PersistentVolumeClaim", Namespace: "tenant-a", Name: "data"}, Parameters: map[string]any{"storageClass": "fast", "size": "1Gi"}}
	plan, err := planner.Plan(context.Background(), "alice", request)
	if err != nil {
		t.Fatal(err)
	}
	// Warning acknowledgement is independent of the ordinary confirmation
	// challenge and must be an explicit user decision.
	plan.Warnings = []string{"backend data may be retained"}
	request.Confirmation.Challenge = plan.Challenge
	var planError *PlanError
	if err := planner.Verify("alice", request, plan); !errors.As(err, &planError) || planError.Code != "WARNING_ACKNOWLEDGEMENT_REQUIRED" {
		t.Fatalf("unacknowledged warning error=%v", err)
	}
	request.Confirmation.WarningsAcknowledged = true
	if err := planner.Verify("alice", request, plan); err != nil {
		t.Fatalf("acknowledged warning rejected: %v", err)
	}
}

func TestPlanRejectsUnknownSensitiveParametersAndForgedTargets(t *testing.T) {
	planner := newPlanner(t, &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "fast"}, Provisioner: "example.csi.io"})
	request := Request{ActionID: "create-pvc", Target: ResourceTarget{Kind: "PersistentVolumeClaim", Namespace: "tenant-a", Name: "data"}, Parameters: map[string]any{"storageClass": "fast", "size": "1Gi", "password": "must-not-persist"}}
	_, err := planner.Plan(context.Background(), "alice", request)
	var planError *PlanError
	if !errors.As(err, &planError) || planError.Code != "INVALID_PARAMETER" {
		t.Fatalf("sensitive parameter error=%v", err)
	}
	request.Parameters = map[string]any{"storageClass": "fast", "size": "1Gi"}
	request.Target.Name = "../../escape"
	if _, err = planner.Plan(context.Background(), "alice", request); !errors.As(err, &planError) || planError.Code != "INVALID_PARAMETER" {
		t.Fatalf("path traversal target error=%v", err)
	}
	request = Request{ActionID: "create-ceph-rbd-storageclass", ProviderID: "attacker", Target: ResourceTarget{Kind: "StorageClass", Name: "fast"}}
	if _, err = planner.Plan(context.Background(), "admin", request); !errors.As(err, &planError) || planError.Code != "PROVIDER_MISMATCH" {
		t.Fatalf("forged provider error=%v", err)
	}
	request = Request{ActionID: "create-pvc", Target: ResourceTarget{Kind: "Secret", Namespace: "tenant-a", Name: "data"}, Parameters: map[string]any{"storageClass": "fast", "size": "1Gi"}}
	if _, err = planner.Plan(context.Background(), "alice", request); !errors.As(err, &planError) || planError.Code != "TARGET_KIND_MISMATCH" {
		t.Fatalf("forged target kind error=%v", err)
	}
}

func TestPlanningPerformsServerDryRunAndSurfacesAdmissionFailure(t *testing.T) {
	core := kubernetesfake.NewSimpleClientset(&storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "fast"}, Provisioner: "example.csi.io"})
	dynamic := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	dynamic.PrependReactor("patch", "persistentvolumeclaims", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("admission denied by quota")
	})
	planner, err := NewPlanner(PlannerConfig{Core: core, Dynamic: dynamic, Scope: storage.NewScope("cluster", nil), Secret: []byte("0123456789abcdef0123456789abcdef"), PlanDryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = planner.Plan(context.Background(), "alice", Request{ActionID: "create-pvc", Target: ResourceTarget{Kind: "PersistentVolumeClaim", Namespace: "tenant-a", Name: "data"}, Parameters: map[string]any{"storageClass": "fast", "size": "1Gi"}})
	var planError *PlanError
	if !errors.As(err, &planError) || planError.Code != "SERVER_DRY_RUN_FAILED" {
		t.Fatalf("dry-run error=%v", err)
	}
}

func TestDeletePVCBlockedByLiveWorkload(t *testing.T) {
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "tenant-a", UID: types.UID("pvc-uid"), ResourceVersion: "3"}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "tenant-a"}, Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	planner := newPlanner(t, claim, pod)
	_, err := planner.Plan(context.Background(), "admin", Request{ActionID: "delete-pvc", Target: ResourceTarget{Kind: "PersistentVolumeClaim", Namespace: "tenant-a", Name: "data"}})
	var planError *PlanError
	if err == nil || !errors.As(err, &planError) || planError.Code != "LIVE_WORKLOAD_REFERENCES_CLAIM" {
		t.Fatalf("expected live workload block, got %v", err)
	}
}

func TestPoolCreationFailsClosedOnRuntimeAndHealthChecks(t *testing.T) {
	cluster := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "ceph.rook.io/v1", "kind": "CephCluster",
		"metadata": map[string]any{"name": "rook-ceph", "namespace": "rook-ceph", "uid": "cluster-uid", "resourceVersion": "4"},
		"status":   map[string]any{"state": "Ready", "ceph": map[string]any{"health": "HEALTH_OK"}},
	}}
	nodes := []runtime.Object{
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-b"}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-c"}},
	}
	request := Request{ActionID: "create-ceph-blockpool", ProviderID: "rook-ceph", Target: ResourceTarget{Kind: "CephBlockPool", Namespace: "rook-ceph", Name: "scratch"}, Parameters: map[string]any{"replicatedSize": 3.0, "failureDomain": "host"}}

	makePlanner := func(safety poolSafetyStub, health string, nodeObjects []runtime.Object) *Planner {
		t.Helper()
		copyCluster := cluster.DeepCopy()
		_ = unstructured.SetNestedField(copyCluster.Object, health, "status", "ceph", "health")
		core := kubernetesfake.NewSimpleClientset(nodeObjects...)
		dynamic := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), copyCluster)
		planner, err := NewPlanner(PlannerConfig{Core: core, Dynamic: dynamic, Scope: storage.NewScope("cluster", nil), Secret: []byte("0123456789abcdef0123456789abcdef"), Safety: safety})
		if err != nil {
			t.Fatal(err)
		}
		return planner
	}

	assertCode := func(err error, expected string) {
		t.Helper()
		var planError *PlanError
		if !errors.As(err, &planError) || planError.Code != expected {
			t.Fatalf("error=%v, want %s", err, expected)
		}
	}
	_, err := makePlanner(poolSafetyStub{err: errors.New("dashboard unavailable"), reason: "fresh pool inventory unavailable"}, "HEALTH_OK", nodes).Plan(context.Background(), "admin", request)
	assertCode(err, "POOL_POSTFLIGHT_UNAVAILABLE")
	_, err = makePlanner(poolSafetyStub{}, "HEALTH_ERR", nodes).Plan(context.Background(), "admin", request)
	assertCode(err, "CEPH_HEALTH_ERR")
	_, err = makePlanner(poolSafetyStub{}, "HEALTH_OK", nodes[:2]).Plan(context.Background(), "admin", request)
	assertCode(err, "INSUFFICIENT_FAILURE_DOMAINS")
	plan, err := makePlanner(poolSafetyStub{}, "HEALTH_OK", nodes).Plan(context.Background(), "admin", request)
	if err != nil || len(plan.Resources) != 1 || plan.Resources[0].Kind != "CephBlockPool" {
		t.Fatalf("safe pool plan=%#v err=%v", plan, err)
	}
}

func TestPoolDeleteBlocksWhenEmptinessCannotBeProved(t *testing.T) {
	pool := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "ceph.rook.io/v1", "kind": "CephBlockPool",
		"metadata": map[string]any{"name": "scratch", "namespace": "rook-ceph", "uid": "pool-uid", "resourceVersion": "8"},
	}}
	core := kubernetesfake.NewSimpleClientset()
	dynamic := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), pool)
	planner, err := NewPlanner(PlannerConfig{Core: core, Dynamic: dynamic, Scope: storage.NewScope("cluster", nil), Secret: []byte("0123456789abcdef0123456789abcdef"), Safety: poolSafetyStub{empty: false, reason: "RBD inventory is stale"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = planner.Plan(context.Background(), "admin", Request{ActionID: "delete-ceph-blockpool", ProviderID: "rook-ceph", Target: ResourceTarget{Kind: "CephBlockPool", Namespace: "rook-ceph", Name: "scratch"}})
	var planError *PlanError
	if !errors.As(err, &planError) || planError.Code != "CANNOT_PROVE_EMPTY" {
		t.Fatalf("error=%v, want CANNOT_PROVE_EMPTY", err)
	}
}

func TestCephInfrastructureActionsCannotEscapeProviderOwnership(t *testing.T) {
	nonCephClass := &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "foreign"}, Provisioner: "example.csi.io"}
	planner := newPlanner(t, nonCephClass)
	_, err := planner.Plan(context.Background(), "admin", Request{ActionID: "delete-ceph-storageclass", ProviderID: "rook-ceph", Target: ResourceTarget{Kind: "StorageClass", Name: "foreign"}})
	var planError *PlanError
	if !errors.As(err, &planError) || planError.Code != "PROVIDER_MISMATCH" {
		t.Fatalf("foreign StorageClass delete error=%v", err)
	}

	planner = newPlanner(t)
	_, err = planner.Plan(context.Background(), "admin", Request{ActionID: "create-ceph-blockpool", ProviderID: "rook-ceph", Target: ResourceTarget{Kind: "CephBlockPool", Namespace: "other-rook", Name: "escape"}, Parameters: map[string]any{"replicatedSize": 3, "failureDomain": "host"}})
	if !errors.As(err, &planError) || planError.Code != "PROVIDER_SCOPE_MISMATCH" {
		t.Fatalf("cross-provider pool error=%v", err)
	}
}

func TestStorePersistsImmutableRequestAndStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	dynamic := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{OperationGVR: "StorageOperationList"})
	store, _ := NewStore(dynamic, "highland-system")
	spec := Spec{ActionID: "create-pvc", Target: ResourceTarget{Kind: "PersistentVolumeClaim", Namespace: "default", Name: "data"}, ParameterHash: strings.Repeat("a", 64), PlanHash: strings.Repeat("b", 64), Requester: "alice", RequesterRole: "operator", RequestedAt: time.Now().UTC(), Resources: []PlannedResource{{APIVersion: "v1", Kind: "PersistentVolumeClaim", Namespace: "default", Name: "data", Operation: "server-side-apply"}}}
	created, err := store.Create(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC()
	created.Status = Status{Phase: "Running", StartedAt: &started}
	updated, err := store.UpdateStatus(context.Background(), created.Name, created.Status)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Spec.ParameterHash != spec.ParameterHash || updated.Status.Phase != "Running" {
		t.Fatalf("durable operation mismatch: %#v", updated)
	}
	list, err := store.List(context.Background(), map[string]string{"user": "alice"}, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%#v err=%v", list, err)
	}
}

func TestStoreCreateIsIdempotentAcrossConcurrentReplicas(t *testing.T) {
	scheme := runtime.NewScheme()
	dynamic := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{OperationGVR: "StorageOperationList"})
	store, _ := NewStore(dynamic, "highland-system")
	spec := Spec{
		ActionID:        "create-pvc",
		Target:          ResourceTarget{Kind: "PersistentVolumeClaim", Namespace: "default", Name: "data"},
		ParameterHash:   strings.Repeat("a", 64),
		PlanHash:        strings.Repeat("b", 64),
		IdempotencyHash: strings.Repeat("c", 64),
		Requester:       "alice",
		RequesterRole:   "operator",
		RequestedAt:     time.Now().UTC(),
		Resources:       []PlannedResource{{APIVersion: "v1", Kind: "PersistentVolumeClaim", Namespace: "default", Name: "data", Operation: "server-side-apply"}},
	}

	const replicas = 12
	var wg sync.WaitGroup
	var mu sync.Mutex
	created, duplicates, unexpected := 0, 0, []error{}
	for range replicas {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Create(context.Background(), spec)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				created++
			case apierrors.IsAlreadyExists(err):
				duplicates++
			default:
				unexpected = append(unexpected, err)
			}
		}()
	}
	wg.Wait()
	if created != 1 || duplicates != replicas-1 || len(unexpected) != 0 {
		t.Fatalf("created=%d duplicates=%d unexpected=%v", created, duplicates, unexpected)
	}
	operations, err := store.List(context.Background(), nil, 100)
	if err != nil || len(operations) != 1 || operations[0].Name != "storage-"+strings.Repeat("c", 24) {
		t.Fatalf("operations=%#v err=%v", operations, err)
	}
}

func TestOperationRetentionDeletesOnlyExpiredTerminalObjects(t *testing.T) {
	dynamic := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{OperationGVR: "StorageOperationList"})
	store, _ := NewStore(dynamic, "highland-system")
	now := time.Now().UTC()
	createAt := func(hash string, created time.Time, phase string) *Operation {
		t.Helper()
		operation, err := store.Create(context.Background(), Spec{ActionID: "create-pvc", Target: ResourceTarget{Kind: "PersistentVolumeClaim", Namespace: "default", Name: "data-" + hash}, ParameterHash: strings.Repeat(hash, 64), PlanHash: strings.Repeat("b", 64), IdempotencyHash: strings.Repeat(hash, 64), Requester: "alice", RequesterRole: "operator", RequestedAt: created, Resources: []PlannedResource{{APIVersion: "v1", Kind: "PersistentVolumeClaim", Namespace: "default", Name: "data-" + hash, Operation: "server-side-apply"}}})
		if err != nil {
			t.Fatal(err)
		}
		object, err := dynamic.Resource(OperationGVR).Namespace("highland-system").Get(context.Background(), operation.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		object.SetCreationTimestamp(metav1.NewTime(created))
		if _, err = dynamic.Resource(OperationGVR).Namespace("highland-system").Update(context.Background(), object, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		operation.CreationTimestamp = created
		operation.Status.Phase = phase
		if _, err = store.UpdateStatus(context.Background(), operation.Name, operation.Status); err != nil {
			t.Fatal(err)
		}
		return operation
	}
	oldTerminal := createAt("c", now.Add(-31*24*time.Hour), "Succeeded")
	recentTerminal := createAt("d", now.Add(-2*24*time.Hour), "Failed")
	oldActive := createAt("e", now.Add(-40*24*time.Hour), "Running")

	deleted, err := store.DeleteTerminalBefore(context.Background(), now.Add(-30*24*time.Hour))
	if err != nil || deleted != 1 {
		t.Fatalf("deleted=%d err=%v", deleted, err)
	}
	if _, err := store.Get(context.Background(), oldTerminal.Name); !apierrors.IsNotFound(err) {
		t.Fatalf("expired terminal operation still exists: %v", err)
	}
	for _, retained := range []*Operation{recentTerminal, oldActive} {
		if _, err := store.Get(context.Background(), retained.Name); err != nil {
			t.Fatalf("retained operation %s missing: %v", retained.Name, err)
		}
	}
}

func TestOperationRetentionRequiresMatchingDurableAuditEvidence(t *testing.T) {
	dynamic := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{OperationGVR: "StorageOperationList"})
	store, _ := NewStore(dynamic, "highland-system")
	created := time.Now().UTC().Add(-31 * 24 * time.Hour)
	makeTerminal := func(hash string) *Operation {
		t.Helper()
		operation, err := store.Create(context.Background(), Spec{ActionID: "create-pvc", Target: ResourceTarget{Kind: "PersistentVolumeClaim", Namespace: "default", Name: "data-" + hash}, ParameterHash: strings.Repeat(hash, 64), PlanHash: strings.Repeat("b", 64), IdempotencyHash: strings.Repeat(hash, 64), Requester: "alice", RequesterRole: "operator", RequestedAt: created, Resources: []PlannedResource{{APIVersion: "v1", Kind: "PersistentVolumeClaim", Namespace: "default", Name: "data-" + hash, Operation: "server-side-apply"}}})
		if err != nil {
			t.Fatal(err)
		}
		object, _ := dynamic.Resource(OperationGVR).Namespace("highland-system").Get(context.Background(), operation.Name, metav1.GetOptions{})
		object.SetCreationTimestamp(metav1.NewTime(created))
		_, _ = dynamic.Resource(OperationGVR).Namespace("highland-system").Update(context.Background(), object, metav1.UpdateOptions{})
		operation.CreationTimestamp = created
		operation.Status.Phase = "Succeeded"
		operation, err = store.UpdateStatus(context.Background(), operation.Name, operation.Status)
		if err != nil {
			t.Fatal(err)
		}
		return operation
	}
	withAudit := makeTerminal("c")
	withoutAudit := makeTerminal("d")
	deleted, err := store.DeleteTerminalBeforeWhere(context.Background(), time.Now().UTC().Add(-30*24*time.Hour), func(operation Operation) bool {
		return operation.Name == withAudit.Name
	})
	if err != nil || deleted != 1 {
		t.Fatalf("deleted=%d err=%v", deleted, err)
	}
	if _, err := store.Get(context.Background(), withAudit.Name); !apierrors.IsNotFound(err) {
		t.Fatalf("audited operation retained: %v", err)
	}
	if _, err := store.Get(context.Background(), withoutAudit.Name); err != nil {
		t.Fatalf("operation without durable terminal evidence was deleted: %v", err)
	}
}

func TestRunningOperationRecoversAndCompletesAfterRestart(t *testing.T) {
	scheme := runtime.NewScheme()
	pvcGVR := schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}
	pvc := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "PersistentVolumeClaim", "metadata": map[string]any{"name": "data", "namespace": "default", "uid": "pvc-uid"}, "status": map[string]any{"phase": "Bound"}}}
	dynamic := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{OperationGVR: "StorageOperationList", pvcGVR: "PersistentVolumeClaimList"}, pvc)
	store, _ := NewStore(dynamic, "highland-system")
	spec := Spec{ActionID: "create-pvc", Target: ResourceTarget{Kind: "PersistentVolumeClaim", Namespace: "default", Name: "data"}, ParameterHash: strings.Repeat("a", 64), PlanHash: strings.Repeat("b", 64), Requester: "alice", RequesterRole: "operator", RequestedAt: time.Now().UTC(), Resources: []PlannedResource{{APIVersion: "v1", Kind: "PersistentVolumeClaim", Namespace: "default", Name: "data", Operation: "server-side-apply"}}}
	operation, err := store.Create(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now().Add(-time.Minute).UTC()
	operation.Status = Status{Phase: "Running", Step: "WaitingForReconciliation", StartedAt: &started, Result: &ResultReference{Kind: "PersistentVolumeClaim", Namespace: "default", Name: "data", UID: "pvc-uid"}}
	operation, err = store.UpdateStatus(context.Background(), operation.Name, operation.Status)
	if err != nil {
		t.Fatal(err)
	}
	core := kubernetesfake.NewSimpleClientset()
	planner, _ := NewPlanner(PlannerConfig{Core: core, Dynamic: dynamic, Scope: storage.NewScope("cluster", nil), Secret: []byte("0123456789abcdef0123456789abcdef")})
	observer := &operationObserverStub{}
	controller, _ := NewController(core, dynamic, store, planner, "highland-system", observer, nil)
	if err := controller.Reconcile(context.Background(), operation); err != nil {
		t.Fatal(err)
	}
	finished, err := store.Get(context.Background(), operation.Name)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status.Phase != "Succeeded" {
		t.Fatalf("phase=%s, want Succeeded", finished.Status.Phase)
	}
	if observer.started != 1 || observer.finished != 1 || observer.result != "succeeded" {
		t.Fatalf("recovered operation observations = started:%d finished:%d result:%q", observer.started, observer.finished, observer.result)
	}
}

func TestControllerFailsMalformedStoredOperationsWithoutMutation(t *testing.T) {
	tests := []struct {
		name      string
		actionID  string
		resources []PlannedResource
		wantCode  string
	}{
		{name: "unknown action", actionID: "forged-action", resources: []PlannedResource{{APIVersion: "v1", Kind: "PersistentVolumeClaim", Namespace: "default", Name: "data", Operation: "delete"}}, wantCode: "ACTION_NOT_SUPPORTED"},
		{name: "missing resource", actionID: "delete-pvc", wantCode: "INVALID_OPERATION_SPEC"},
	}
	for index, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dynamic := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{OperationGVR: "StorageOperationList"})
			store, _ := NewStore(dynamic, "highland-system")
			operation, err := store.Create(context.Background(), Spec{ActionID: tc.actionID, Target: ResourceTarget{Kind: "PersistentVolumeClaim", Namespace: "default", Name: "data"}, ParameterHash: strings.Repeat("a", 64), PlanHash: strings.Repeat("b", 64), IdempotencyHash: strings.Repeat(string(rune('c'+index)), 64), Resources: tc.resources, Requester: "alice", RequesterRole: "operator", RequestedAt: time.Now().UTC()})
			if err != nil {
				t.Fatal(err)
			}
			core := kubernetesfake.NewSimpleClientset()
			planner, _ := NewPlanner(PlannerConfig{Core: core, Dynamic: dynamic, Scope: storage.NewScope("cluster", nil), Secret: []byte("0123456789abcdef0123456789abcdef")})
			controller, _ := NewController(core, dynamic, store, planner, "highland-system", nil, nil)
			if err := controller.Reconcile(context.Background(), operation); err != nil {
				t.Fatal(err)
			}
			failed, err := store.Get(context.Background(), operation.Name)
			if err != nil || failed.Status.Phase != "Failed" || failed.Status.ErrorCode != tc.wantCode {
				t.Fatalf("operation=%#v err=%v", failed, err)
			}
		})
	}
}

func TestRetryingOperationRerunsPreflightAndMutationAfterLeaderTakeover(t *testing.T) {
	scheme := runtime.NewScheme()
	pvcGVR := schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}
	dynamic := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{OperationGVR: "StorageOperationList", pvcGVR: "PersistentVolumeClaimList"})
	core := kubernetesfake.NewSimpleClientset(&storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "fast", UID: "class-uid", ResourceVersion: "1"}, Provisioner: "example.csi.io"})
	planner, err := NewPlanner(PlannerConfig{Core: core, Dynamic: dynamic, Scope: storage.NewScope("cluster", nil), Secret: []byte("0123456789abcdef0123456789abcdef")})
	if err != nil {
		t.Fatal(err)
	}
	request := Request{ActionID: "create-pvc", Target: ResourceTarget{Kind: "PersistentVolumeClaim", Namespace: "default", Name: "data"}, Parameters: map[string]any{"storageClass": "fast", "size": "1Gi", "accessModes": []any{"ReadWriteOnce"}}}
	plan, err := planner.Plan(context.Background(), "alice", request)
	if err != nil {
		t.Fatal(err)
	}
	store, _ := NewStore(dynamic, "highland-system")
	operation, err := store.Create(context.Background(), Spec{ActionID: request.ActionID, Target: plan.Target, Parameters: request.Parameters, ParameterHash: hashValue(request.Parameters), PlanHash: plan.Hash, IdempotencyHash: strings.Repeat("f", 64), Resources: plan.Resources, Dependencies: plan.Dependencies, Requester: "alice", RequesterRole: "operator", RequestedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	var patches int
	dynamic.PrependReactor("patch", "persistentvolumeclaims", func(ktesting.Action) (bool, runtime.Object, error) {
		patches++
		applied := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "PersistentVolumeClaim", "metadata": map[string]any{"name": "data", "namespace": "default", "uid": "pvc-uid", "labels": map[string]any{"app.kubernetes.io/managed-by": "highland"}}, "status": map[string]any{"phase": "Pending"}}}
		if patches == 2 {
			return true, nil, errors.New("connection reset by peer")
		}
		return true, applied, nil
	})
	controller, err := NewController(core, dynamic, store, planner, "highland-system", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Reconcile(context.Background(), operation); err != nil {
		t.Fatal(err)
	}
	retrying, err := store.Get(context.Background(), operation.Name)
	if err != nil || retrying.Status.Step != "Retrying" || retrying.Status.Retries != 1 {
		t.Fatalf("retrying operation=%#v err=%v", retrying, err)
	}
	if err := controller.Reconcile(context.Background(), retrying); err != nil {
		t.Fatal(err)
	}
	waiting, err := store.Get(context.Background(), operation.Name)
	if err != nil || waiting.Status.Step != "WaitingForReconciliation" || waiting.Status.Result == nil || patches != 4 {
		t.Fatalf("recovered operation=%#v patches=%d err=%v", waiting, patches, err)
	}
}

func TestPoolDeleteWaitsForRuntimeAbsenceAfterRookObjectDisappears(t *testing.T) {
	for _, test := range []struct {
		name        string
		present     bool
		wantDone    bool
		wantMessage string
	}{
		{name: "runtime still present", present: true, wantDone: false, wantMessage: "runtime still contains pool"},
		{name: "runtime absent", present: false, wantDone: true, wantMessage: "PoolDeletedAndRuntimeVerified"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dynamic := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{OperationGVR: "StorageOperationList", poolGVR: "CephBlockPoolList"})
			core := kubernetesfake.NewSimpleClientset()
			store, _ := NewStore(dynamic, "highland-system")
			planner, _ := NewPlanner(PlannerConfig{Core: core, Dynamic: dynamic, Scope: storage.NewScope("cluster", nil), Secret: []byte("0123456789abcdef0123456789abcdef"), Safety: poolSafetyStub{present: test.present, reason: "runtime still contains pool"}})
			controller, _ := NewController(core, dynamic, store, planner, "highland-system", nil, nil)
			operation := &Operation{Spec: Spec{ActionID: "delete-ceph-blockpool", Target: ResourceTarget{Kind: "CephBlockPool", Namespace: "rook-ceph", Name: "scratch", UID: "old-pool"}}}
			plan := Plan{Target: operation.Spec.Target, Resources: []PlannedResource{{APIVersion: "ceph.rook.io/v1", Kind: "CephBlockPool", Namespace: "rook-ceph", Name: "scratch", Operation: "delete"}}}
			done, failed, message, err := controller.inspect(context.Background(), operation, plan)
			if err != nil || failed || done != test.wantDone || message != test.wantMessage {
				t.Fatalf("done=%t failed=%t message=%q err=%v", done, failed, message, err)
			}
		})
	}
}

func TestDeleteInspectionIgnoresSameNameRecreatedResource(t *testing.T) {
	pvcGVR := schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}
	recreated := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "PersistentVolumeClaim", "metadata": map[string]any{"namespace": "default", "name": "data", "uid": "new-uid"}}}
	dynamic := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{OperationGVR: "StorageOperationList", pvcGVR: "PersistentVolumeClaimList"}, recreated)
	core := kubernetesfake.NewSimpleClientset()
	store, _ := NewStore(dynamic, "highland-system")
	planner, _ := NewPlanner(PlannerConfig{Core: core, Dynamic: dynamic, Scope: storage.NewScope("cluster", nil), Secret: []byte("0123456789abcdef0123456789abcdef")})
	controller, _ := NewController(core, dynamic, store, planner, "highland-system", nil, nil)
	operation := &Operation{Spec: Spec{ActionID: "delete-pvc", Target: ResourceTarget{Kind: "PersistentVolumeClaim", Namespace: "default", Name: "data", UID: "old-uid"}}}
	plan := Plan{Target: operation.Spec.Target, Resources: []PlannedResource{{APIVersion: "v1", Kind: "PersistentVolumeClaim", Namespace: "default", Name: "data", Operation: "delete"}}}
	done, failed, message, err := controller.inspect(context.Background(), operation, plan)
	if err != nil || failed || !done || message != "ResourceDeleted" {
		t.Fatalf("done=%t failed=%t message=%q err=%v", done, failed, message, err)
	}
}
