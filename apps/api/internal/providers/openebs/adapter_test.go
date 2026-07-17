package openebs

import (
	"context"
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

func TestDescriptorDetectsInstalledEnginesAndHealthyComponents(t *testing.T) {
	adapter := testAdapter(t,
		component("Deployment", "openebs-localpv-provisioner", int64(1), int64(1), "openebs/provisioner-localpv:4.5.1"),
		storageClass("openebs-hostpath", HostPathDriver),
		csiDriver(LVMDriver),
		openEBSObject("local.openebs.io/v1alpha1", "LVMVolume", "pvc-lvm", "openebs", map[string]any{"state": "Ready"}),
	)
	descriptor, err := adapter.Descriptor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.Kind != "openebs" || descriptor.SupportLevel != storage.SupportManaged {
		t.Fatalf("descriptor=%#v", descriptor)
	}
	if descriptor.Metadata["engine.hostpath"] != "true" || descriptor.Metadata["engine.lvm"] != "true" {
		t.Fatalf("engine metadata=%#v", descriptor.Metadata)
	}
	if descriptor.Metadata["engine.mayastor"] != "false" || descriptor.Health.Status != storage.SeverityOK {
		t.Fatalf("unexpected descriptor health=%#v metadata=%#v", descriptor.Health, descriptor.Metadata)
	}
}

func TestProviderResourcesAreNormalizedBoundedAndSearchable(t *testing.T) {
	volume := openEBSObject("local.openebs.io/v1alpha1", "LVMVolume", "pvc-lvm", "openebs", map[string]any{"state": "Ready"})
	volume.Object["spec"] = map[string]any{
		"volName": "handle-lvm", "vgName": "fast-vg", "capacity": "10Gi",
		"secretName": "must-not-leak",
	}
	adapter := testAdapter(t, volume)
	raw, page, err := adapter.ListProviderResources(context.Background(), "lvm-volumes", storage.PageRequest{Limit: 20, Search: "fast-vg"})
	if err != nil || page.Total != 1 {
		t.Fatalf("raw=%#v page=%#v err=%v", raw, page, err)
	}
	item := raw.([]any)[0].(map[string]any)
	if item["engine"] != "lvm" || item["volumeHandle"] != "handle-lvm" || item["volumeGroup"] != "fast-vg" {
		t.Fatalf("normalized=%#v", item)
	}
	spec := item["spec"].(map[string]any)
	if _, leaked := spec["secretName"]; leaked {
		t.Fatalf("sensitive field leaked: %#v", spec)
	}
}

func TestHostPathInventoryAndExactCorrelation(t *testing.T) {
	adapter := testAdapter(t,
		storageClass("openebs-hostpath", HostPathDriver),
		hostPathPV("pvc-hostpath", "openebs-hostpath", "node-a", "/var/openebs/local/pvc-hostpath"),
	)
	raw, page, err := adapter.ListProviderResources(context.Background(), "hostpath-volumes", storage.PageRequest{Limit: 20})
	if err != nil || page.Total != 1 {
		t.Fatalf("raw=%#v page=%#v err=%v", raw, page, err)
	}
	item := raw.([]any)[0].(map[string]any)
	if item["node"] != "node-a" || item["path"] != "/var/openebs/local/pvc-hostpath" {
		t.Fatalf("hostpath=%#v", item)
	}
	volumes := []storage.PersistentVolumeSummary{{Name: "pvc-hostpath", Driver: HostPathDriver}}
	claims := []storage.ClaimSummary{{PVName: "pvc-hostpath", Driver: HostPathDriver}}
	if err := adapter.EnrichVolumes(context.Background(), volumes); err != nil {
		t.Fatal(err)
	}
	if err := adapter.EnrichClaims(context.Background(), claims); err != nil {
		t.Fatal(err)
	}
	if volumes[0].ProviderRef == nil || volumes[0].ProviderRef.Kind != "openebs-hostpath-volume" || claims[0].ProviderRef == nil {
		t.Fatalf("volume=%#v claim=%#v", volumes[0], claims[0])
	}
}

func TestUnreadyComponentMakesProviderHealthActionable(t *testing.T) {
	adapter := testAdapter(t,
		component("Deployment", "openebs-localpv-provisioner", int64(1), int64(0), "openebs/provisioner-localpv:4.5.1"),
		storageClass("openebs-hostpath", HostPathDriver),
	)
	health := adapter.Health(context.Background())
	if health.Status != storage.SeverityError {
		t.Fatalf("health=%#v", health)
	}
	found := false
	for _, condition := range health.Conditions {
		if condition.Type == "ComponentsReady" && condition.Reason == "UnavailableComponents" {
			found = true
		}
	}
	if !found {
		t.Fatalf("conditions=%#v", health.Conditions)
	}
}

func TestAbsentOptionalCRDsReturnEmptyResources(t *testing.T) {
	adapter := testAdapter(t, storageClass("openebs-hostpath", HostPathDriver))
	raw, page, err := adapter.ListProviderResources(context.Background(), "zfs-volumes", storage.PageRequest{Limit: 20})
	if err != nil || page.Total != 0 || len(raw.([]any)) != 0 {
		t.Fatalf("raw=%#v page=%#v err=%v", raw, page, err)
	}
}

func testAdapter(t *testing.T, objects ...runtime.Object) *Adapter {
	t.Helper()
	listKinds := map[schema.GroupVersionResource]string{
		{Group: "apps", Version: "v1", Resource: "deployments"}:                    "DeploymentList",
		{Group: "apps", Version: "v1", Resource: "daemonsets"}:                     "DaemonSetList",
		{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"}:       "StorageClassList",
		{Group: "storage.k8s.io", Version: "v1", Resource: "csidrivers"}:           "CSIDriverList",
		{Version: "v1", Resource: "persistentvolumes"}:                             "PersistentVolumeList",
		{Group: "openebs.io", Version: "v1beta3", Resource: "diskpools"}:           "DiskPoolList",
		{Group: "local.openebs.io", Version: "v1alpha1", Resource: "lvmnodes"}:     "LVMNodeList",
		{Group: "local.openebs.io", Version: "v1alpha1", Resource: "lvmvolumes"}:   "LVMVolumeList",
		{Group: "local.openebs.io", Version: "v1alpha1", Resource: "lvmsnapshots"}: "LVMSnapshotList",
		{Group: "zfs.openebs.io", Version: "v1alpha1", Resource: "zfsnodes"}:       "ZFSNodeList",
		{Group: "zfs.openebs.io", Version: "v1alpha1", Resource: "zfsvolumes"}:     "ZFSVolumeList",
		{Group: "zfs.openebs.io", Version: "v1alpha1", Resource: "zfssnapshots"}:   "ZFSSnapshotList",
		{Group: "zfs.openebs.io", Version: "v1alpha1", Resource: "zfsbackups"}:     "ZFSBackupList",
		{Group: "zfs.openebs.io", Version: "v1alpha1", Resource: "zfsrestores"}:    "ZFSRestoreList",
	}
	dynamic := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, objects...)
	discovery := &fakediscovery.FakeDiscovery{Fake: &ktesting.Fake{}}
	discovery.Resources = []*metav1.APIResourceList{
		{GroupVersion: "local.openebs.io/v1alpha1", APIResources: []metav1.APIResource{
			{Name: "lvmnodes", Namespaced: true}, {Name: "lvmvolumes", Namespaced: true}, {Name: "lvmsnapshots", Namespaced: true},
		}},
	}
	adapter, err := New(Config{Namespace: "openebs", Dynamic: dynamic, Discovery: discovery})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func component(kind, name string, desired, ready int64, image string) *unstructured.Unstructured {
	status := map[string]any{"readyReplicas": ready}
	if kind == "DaemonSet" {
		status = map[string]any{"desiredNumberScheduled": desired, "numberReady": ready}
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1", "kind": kind,
		"metadata": map[string]any{"name": name, "namespace": "openebs", "labels": map[string]any{"app.kubernetes.io/part-of": "openebs"}},
		"spec": map[string]any{"replicas": desired, "template": map[string]any{"spec": map[string]any{"containers": []any{
			map[string]any{"name": "controller", "image": image},
		}}}},
		"status": status,
	}}
}

func storageClass(name, provisioner string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "storage.k8s.io/v1", "kind": "StorageClass",
		"metadata": map[string]any{"name": name}, "provisioner": provisioner,
	}}
}

func csiDriver(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "storage.k8s.io/v1", "kind": "CSIDriver", "metadata": map[string]any{"name": name},
	}}
}

func openEBSObject(apiVersion, kind, name, namespace string, status map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion, "kind": kind,
		"metadata": map[string]any{"name": name, "namespace": namespace, "uid": name + "-uid"},
		"status":   status,
	}}
}

func hostPathPV(name, className, node, path string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "PersistentVolume", "metadata": map[string]any{"name": name},
		"spec": map[string]any{
			"storageClassName": className, "capacity": map[string]any{"storage": "10Gi"},
			"local": map[string]any{"path": path},
			"nodeAffinity": map[string]any{"required": map[string]any{"nodeSelectorTerms": []any{
				map[string]any{"matchExpressions": []any{
					map[string]any{"key": "kubernetes.io/hostname", "operator": "In", "values": []any{node}},
				}},
			}}},
		},
		"status": map[string]any{"phase": "Bound"},
	}}
}
