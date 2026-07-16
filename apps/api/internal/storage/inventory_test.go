package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestInventoryCorrelatesKubernetesStorageTruth(t *testing.T) {
	attachRequired := true
	className := "example"
	volumeMode := corev1.PersistentVolumeFilesystem
	controllerOwner := true
	pvName := "pv-data"
	pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: pvName, UID: types.UID("pv-uid"), CreationTimestamp: metav1.Now()}, Spec: corev1.PersistentVolumeSpec{StorageClassName: className, PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain, Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("9007199254740993")}, ClaimRef: &corev1.ObjectReference{Namespace: "tenant-a", Name: "data"}, VolumeMode: &volumeMode, PersistentVolumeSource: corev1.PersistentVolumeSource{CSI: &corev1.CSIPersistentVolumeSource{Driver: "example.csi.io", VolumeHandle: "authoritative-handle", VolumeAttributes: map[string]string{"fsName": "shared", "subvolumeName": "subvol-a", "secretToken": "must-not-leak"}}}}, Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeReleased}}
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "tenant-a", UID: types.UID("pvc-uid")}, Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &className, VolumeName: pvName, AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("8Pi")}}}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound, Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("9Pi")}}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "app-0", Namespace: "tenant-a", OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", Name: "app", Controller: &controllerOwner}}}, Spec: corev1.PodSpec{NodeName: "node-a", Volumes: []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	storageClass := &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: className}, Provisioner: "example.csi.io", ReclaimPolicy: ptr(corev1.PersistentVolumeReclaimRetain), VolumeBindingMode: ptr(storagev1.VolumeBindingImmediate)}
	driver := &storagev1.CSIDriver{ObjectMeta: metav1.ObjectMeta{Name: "example.csi.io"}, Spec: storagev1.CSIDriverSpec{AttachRequired: &attachRequired}}
	attachment := &storagev1.VolumeAttachment{ObjectMeta: metav1.ObjectMeta{Name: "attach-1", UID: types.UID("attach-uid")}, Spec: storagev1.VolumeAttachmentSpec{Attacher: "example.csi.io", NodeName: "node-a", Source: storagev1.VolumeAttachmentSource{PersistentVolumeName: &pvName}}, Status: storagev1.VolumeAttachmentStatus{Attached: true}}
	core := kubernetesfake.NewSimpleClientset(pv, pvc, pod, storageClass, driver, attachment)

	snapshot := unstructured.Unstructured{Object: map[string]any{"apiVersion": "snapshot.storage.k8s.io/v1", "kind": "VolumeSnapshot", "metadata": map[string]any{"name": "snap-1", "namespace": "tenant-a", "uid": "snap-uid"}, "spec": map[string]any{"volumeSnapshotClassName": "example-snap", "source": map[string]any{"persistentVolumeClaimName": "data"}}, "status": map[string]any{"readyToUse": true, "restoreSize": "8Pi", "boundVolumeSnapshotContentName": "content-1"}}}
	snapshotClass := unstructured.Unstructured{Object: map[string]any{"apiVersion": "snapshot.storage.k8s.io/v1", "kind": "VolumeSnapshotClass", "metadata": map[string]any{"name": "example-snap"}, "driver": "example.csi.io", "deletionPolicy": "Delete"}}
	snapshotContent := unstructured.Unstructured{Object: map[string]any{"apiVersion": "snapshot.storage.k8s.io/v1", "kind": "VolumeSnapshotContent", "metadata": map[string]any{"name": "content-1"}, "spec": map[string]any{"volumeSnapshotRef": map[string]any{"name": "snap-1", "namespace": "tenant-a"}, "driver": "example.csi.io", "deletionPolicy": "Delete"}, "status": map[string]any{"snapshotHandle": "snap-handle"}}}
	dynamic := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{snapshotGVR: "VolumeSnapshotList", snapshotClassGVR: "VolumeSnapshotClassList", snapshotContentGVR: "VolumeSnapshotContentList"}, &snapshot, &snapshotClass, &snapshotContent)
	discovery := &fakediscovery.FakeDiscovery{Fake: &ktesting.Fake{}}
	discovery.Resources = []*metav1.APIResourceList{{GroupVersion: "snapshot.storage.k8s.io/v1", APIResources: []metav1.APIResource{{Name: "volumesnapshots"}, {Name: "volumesnapshotclasses"}, {Name: "volumesnapshotcontents"}}}}
	inventory, err := NewInventory(core, dynamic, discovery, NewRegistry(), NewScope("cluster", nil))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inventory.Start(ctx)
	deadline := time.Now().Add(3 * time.Second)
	for !inventory.Ready() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !inventory.Ready() {
		t.Fatal("inventory cache did not sync")
	}
	deadline = time.Now().Add(3 * time.Second)
	for !inventory.SnapshotAvailable() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	claims, err := inventory.Claims(context.Background())
	if err != nil || len(claims) != 1 {
		t.Fatalf("claims=%#v err=%v", claims, err)
	}
	claim := claims[0]
	if claim.Driver != "example.csi.io" || claim.VolumeHandle != "authoritative-handle" || len(claim.Workloads) != 1 || claim.Workloads[0].Kind != "StatefulSet" || len(claim.AttachmentIDs) != 1 {
		t.Fatalf("claim correlation failed: %#v", claim)
	}
	if claim.RequestedCapacity != "8Pi" || claim.Provisioned != "9Pi" {
		t.Fatalf("quantity normalization failed: %#v", claim)
	}
	if claim.VolumeAttributes["fsName"] != "shared" || claim.VolumeAttributes["subvolumeName"] != "subvol-a" || claim.VolumeAttributes["secretToken"] != "" {
		t.Fatalf("safe CSI volume attributes failed: %#v", claim.VolumeAttributes)
	}
	volumes, _ := inventory.Volumes(context.Background())
	if len(volumes) != 1 || volumes[0].Capacity != "9007199254740993" || !hasCondition(volumes[0].Conditions, "OrphanRisk") {
		t.Fatalf("volume normalization failed: %#v", volumes)
	}
	snapshots, err := inventory.Snapshots()
	if err != nil || len(snapshots) != 1 || snapshots[0].Driver != "example.csi.io" || snapshots[0].SnapshotHandle != "snap-handle" {
		t.Fatalf("snapshot correlation failed: %#v err=%v", snapshots, err)
	}
	classes, _ := inventory.StorageClasses()
	if len(classes) != 1 || len(classes[0].SnapshotClasses) != 1 || classes[0].SnapshotClasses[0] != "example-snap" {
		t.Fatalf("class snapshot correlation failed: %#v", classes)
	}
}

func ptr[T any](value T) *T { return &value }
func hasCondition(conditions []Condition, kind string) bool {
	for _, condition := range conditions {
		if condition.Type == kind {
			return true
		}
	}
	return false
}

func TestNamespaceScopeFiltersNamespacedInventory(t *testing.T) {
	scope := NewScope("namespaces", []string{"allowed"})
	if !scope.Allows("allowed") || scope.Allows("denied") {
		t.Fatal("namespace scope policy failed")
	}
	className := "restricted"
	core := kubernetesfake.NewSimpleClientset(
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "visible", Namespace: "allowed"}, Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &className}},
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "hidden", Namespace: "denied"}, Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &className}},
	)
	dynamic := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	discovery := &fakediscovery.FakeDiscovery{Fake: &ktesting.Fake{}}
	inventory, err := NewInventory(core, dynamic, discovery, NewRegistry(), scope)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inventory.Start(ctx)
	deadline := time.Now().Add(3 * time.Second)
	for !inventory.Ready() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !inventory.Ready() {
		t.Fatal("namespace inventory cache did not sync")
	}
	claims, err := inventory.Claims(context.Background())
	if err != nil || len(claims) != 1 || claims[0].Namespace != "allowed" || claims[0].Name != "visible" {
		t.Fatalf("namespace claims=%#v err=%v", claims, err)
	}
	volumes, err := inventory.Volumes(context.Background())
	if err != nil || len(volumes) != 0 {
		t.Fatalf("cluster-scoped PVs must be omitted: %#v err=%v", volumes, err)
	}
	if !hasCondition(inventory.CoreConditions(), "ClusterScopedInventory") {
		t.Fatalf("expected explicit partial inventory condition: %#v", inventory.CoreConditions())
	}
}

func TestNamespaceScopeRequiresAllowlist(t *testing.T) {
	_, err := NewInventory(kubernetesfake.NewSimpleClientset(), dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()), &fakediscovery.FakeDiscovery{Fake: &ktesting.Fake{}}, NewRegistry(), NewScope("namespaces", nil))
	if err == nil {
		t.Fatal("expected empty namespace allowlist to be rejected")
	}
}

func TestWarmTenThousandClaimPageP95(t *testing.T) {
	core := kubernetesfake.NewSimpleClientset()
	inventory, err := NewInventory(core, dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()), &fakediscovery.FakeDiscovery{Fake: &ktesting.Fake{}}, NewRegistry(), NewScope("cluster", nil))
	if err != nil {
		t.Fatal(err)
	}
	className := "scale"
	for index := 0; index < 10_000; index++ {
		name := fmt.Sprintf("claim-%05d", index)
		pvName := fmt.Sprintf("pv-%05d", index)
		if err := inventory.pvc.GetStore().Add(&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "scale"}, Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &className, VolumeName: pvName}}); err != nil {
			t.Fatal(err)
		}
		if err := inventory.pv.GetStore().Add(&corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: pvName}, Spec: corev1.PersistentVolumeSpec{StorageClassName: className, ClaimRef: &corev1.ObjectReference{Namespace: "scale", Name: name}, PersistentVolumeSource: corev1.PersistentVolumeSource{CSI: &corev1.CSIPersistentVolumeSource{Driver: "scale.csi.io", VolumeHandle: pvName}}}}); err != nil {
			t.Fatal(err)
		}
	}
	inventory.ready.Store(true)
	api := NewHTTPAPI(inventory, NewRegistry())
	durations := make([]time.Duration, 20)
	for run := range durations {
		request := httptest.NewRequest("GET", "/api/v1/storage/claims?namespace=scale&limit=100", nil)
		response := httptest.NewRecorder()
		started := time.Now()
		api.ListClaims(response, request)
		durations[run] = time.Since(started)
		if response.Code != 200 {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
		var page Page[ClaimSummary]
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil || len(page.Data) != 100 || page.Page.Total != 10_000 || page.Page.Continue == "" {
			t.Fatalf("page=%#v err=%v", page.Page, err)
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[18]
	if p95 >= 500*time.Millisecond {
		t.Fatalf("warm 10,000-claim paginated API p95=%s, target <500ms", p95)
	}
}
