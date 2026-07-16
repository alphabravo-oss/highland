// Package openebs provides a read-only, engine-aware OpenEBS storage provider.
// Kubernetes and OpenEBS CRDs remain authoritative; the adapter never calls CSI
// sockets and never exposes arbitrary dynamic resources selected by a request.
package openebs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/highland-io/highland/apps/api/internal/storage"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
)

const (
	ProviderID      = "openebs"
	HostPathDriver  = "openebs.io/local"
	LVMDriver       = "local.csi.openebs.io"
	ZFSDriver       = "zfs.csi.openebs.io"
	MayastorDriver  = "io.openebs.csi-mayastor"
	RawFileDriver   = "rawfile.csi.openebs.io"
	discoveryTTL    = 30 * time.Second
	maxProviderList = 500
)

type Config struct {
	ID        string
	Namespace string
	Dynamic   dynamic.Interface
	Discovery discovery.DiscoveryInterface
	Observer  storage.Observer
}

type resourceSpec struct {
	Kind       string
	Engine     string
	Group      string
	Resource   string
	Versions   []string
	Namespaced bool
}

var resourceSpecs = map[string]resourceSpec{
	"disk-pools":    {Kind: "disk-pools", Engine: "mayastor", Group: "openebs.io", Resource: "diskpools", Versions: []string{"v1beta3", "v1beta2", "v1alpha1"}},
	"lvm-nodes":     {Kind: "lvm-nodes", Engine: "lvm", Group: "local.openebs.io", Resource: "lvmnodes", Versions: []string{"v1alpha1"}, Namespaced: true},
	"lvm-volumes":   {Kind: "lvm-volumes", Engine: "lvm", Group: "local.openebs.io", Resource: "lvmvolumes", Versions: []string{"v1alpha1"}, Namespaced: true},
	"lvm-snapshots": {Kind: "lvm-snapshots", Engine: "lvm", Group: "local.openebs.io", Resource: "lvmsnapshots", Versions: []string{"v1alpha1"}, Namespaced: true},
	"zfs-nodes":     {Kind: "zfs-nodes", Engine: "zfs", Group: "zfs.openebs.io", Resource: "zfsnodes", Versions: []string{"v1alpha1"}, Namespaced: true},
	"zfs-volumes":   {Kind: "zfs-volumes", Engine: "zfs", Group: "zfs.openebs.io", Resource: "zfsvolumes", Versions: []string{"v1alpha1"}, Namespaced: true},
	"zfs-snapshots": {Kind: "zfs-snapshots", Engine: "zfs", Group: "zfs.openebs.io", Resource: "zfssnapshots", Versions: []string{"v1alpha1"}, Namespaced: true},
	"zfs-backups":   {Kind: "zfs-backups", Engine: "zfs", Group: "zfs.openebs.io", Resource: "zfsbackups", Versions: []string{"v1alpha1"}, Namespaced: true},
	"zfs-restores":  {Kind: "zfs-restores", Engine: "zfs", Group: "zfs.openebs.io", Resource: "zfsrestores", Versions: []string{"v1alpha1"}, Namespaced: true},
}

type resolvedResource struct {
	resourceSpec
	GVR schema.GroupVersionResource
}

type Adapter struct {
	id, namespace string
	dynamic       dynamic.Interface
	discovery     discovery.DiscoveryInterface
	observer      storage.Observer

	mu             sync.RWMutex
	resolved       map[string]resolvedResource
	discoveredAt   time.Time
	discoveryError error
}

func New(cfg Config) (*Adapter, error) {
	if cfg.Dynamic == nil || cfg.Discovery == nil {
		return nil, fmt.Errorf("OpenEBS requires Kubernetes dynamic and discovery clients")
	}
	if cfg.ID == "" {
		cfg.ID = ProviderID
	}
	if cfg.Namespace == "" {
		cfg.Namespace = "openebs"
	}
	return &Adapter{
		id: cfg.ID, namespace: cfg.Namespace, dynamic: cfg.Dynamic,
		discovery: cfg.Discovery, observer: cfg.Observer, resolved: map[string]resolvedResource{},
	}, nil
}

func (a *Adapter) ID() string { return a.id }

func (a *Adapter) Drivers() []string {
	return []string{HostPathDriver, LVMDriver, ZFSDriver, MayastorDriver, RawFileDriver}
}

func (a *Adapter) Descriptor(ctx context.Context) (storage.ProviderDescriptor, error) {
	engines, _ := a.engineData(ctx)
	installed := make([]string, 0, len(engines))
	for _, engine := range engines {
		if boolValue(engine["installed"]) {
			installed = append(installed, fmt.Sprint(engine["id"]))
		}
	}
	sort.Strings(installed)
	version := a.detectVersion(ctx)
	metadata := map[string]string{
		"installedEngines": strings.Join(installed, ","),
		"engineCount":      strconv.Itoa(len(installed)),
		"readOnly":         "true",
	}
	for _, engine := range []string{"hostpath", "lvm", "zfs", "mayastor", "rawfile"} {
		metadata["engine."+engine] = fmt.Sprintf("%t", containsString(installed, engine))
	}
	return storage.ProviderDescriptor{
		ID: a.id, Kind: "openebs", DisplayName: "OpenEBS", SupportLevel: storage.SupportManaged,
		Drivers: a.Drivers(), Version: version, Namespace: a.namespace,
		Capabilities: a.Capabilities(ctx), Health: a.Health(ctx), Metadata: metadata,
	}, nil
}

func (a *Adapter) Capabilities(context.Context) []storage.Capability {
	return []storage.Capability{
		storage.CapabilityClaimsRead,
		storage.CapabilityVolumesRead,
		storage.CapabilityAttachmentsRead,
		storage.CapabilitySnapshotsRead,
		storage.CapabilityCapacityRead,
		storage.CapabilityEventsRead,
		storage.CapabilityProviderHealth,
	}
}

func (a *Adapter) Health(ctx context.Context) storage.ProviderHealth {
	now := time.Now().UTC()
	health := storage.ProviderHealth{Status: storage.SeverityOK, ObservedAt: now}
	engines, engineErr := a.engineData(ctx)
	if engineErr != nil {
		health.Status = storage.SeverityError
		health.Conditions = append(health.Conditions, condition("OpenEBSDiscovery", "False", storage.SeverityError, "DiscoveryFailed", engineErr.Error(), now))
		return health
	}
	installed := 0
	for _, engine := range engines {
		if boolValue(engine["installed"]) {
			installed++
		}
	}
	if installed == 0 {
		health.Status = storage.SeverityWarning
		health.Conditions = append(health.Conditions, condition("EngineInstalled", "False", storage.SeverityWarning, "NoEngineObserved", "No OpenEBS engine, provisioner, or controller was observed.", now))
	} else {
		health.Conditions = append(health.Conditions, condition("EngineInstalled", "True", storage.SeverityOK, "EnginesObserved", fmt.Sprintf("%d OpenEBS engine(s) observed.", installed), now))
	}
	components, componentErr := a.componentData(ctx)
	if componentErr != nil {
		health.Status = worse(health.Status, storage.SeverityWarning)
		health.Conditions = append(health.Conditions, condition("ComponentsReady", "Unknown", storage.SeverityWarning, "ComponentReadFailed", componentErr.Error(), now))
	} else {
		unavailable := 0
		for _, component := range components {
			if !boolValue(component["ready"]) {
				unavailable++
			}
		}
		switch {
		case unavailable > 0:
			health.Status = worse(health.Status, storage.SeverityError)
			health.Conditions = append(health.Conditions, condition("ComponentsReady", "False", storage.SeverityError, "UnavailableComponents", fmt.Sprintf("%d of %d OpenEBS components are not ready.", unavailable, len(components)), now))
		case len(components) > 0:
			health.Conditions = append(health.Conditions, condition("ComponentsReady", "True", storage.SeverityOK, "RolloutsReady", fmt.Sprintf("%d OpenEBS components are ready.", len(components)), now))
		}
	}
	pools, _, poolErr := a.listCRD(ctx, resourceSpecs["disk-pools"])
	if poolErr == nil {
		faulted := 0
		for _, pool := range pools {
			state := strings.ToLower(fmt.Sprint(firstValue(pool, "state", "phase", "health")))
			if state != "" && state != "<nil>" && !strings.Contains(state, "online") && !strings.Contains(state, "ready") && !strings.Contains(state, "created") {
				faulted++
			}
		}
		if faulted > 0 {
			health.Status = worse(health.Status, storage.SeverityError)
			health.Conditions = append(health.Conditions, condition("DiskPoolsHealthy", "False", storage.SeverityError, "DiskPoolUnavailable", fmt.Sprintf("%d Mayastor DiskPool(s) are not online.", faulted), now))
		} else if len(pools) > 0 {
			health.Conditions = append(health.Conditions, condition("DiskPoolsHealthy", "True", storage.SeverityOK, "DiskPoolsOnline", fmt.Sprintf("%d Mayastor DiskPool(s) observed.", len(pools)), now))
		}
	}
	return health
}

func (a *Adapter) ProviderSummary(ctx context.Context) (any, error) {
	engines, engineErr := a.engineData(ctx)
	components, componentErr := a.componentData(ctx)
	conditions := []storage.Condition{}
	if engineErr != nil {
		conditions = append(conditions, partialCondition("Engines", engineErr))
	}
	if componentErr != nil {
		conditions = append(conditions, partialCondition("Components", componentErr))
	}
	counts := map[string]int{}
	for kind, spec := range resourceSpecs {
		items, _, err := a.listCRD(ctx, spec)
		if err == nil {
			counts[kind] = len(items)
		} else if !errors.Is(err, storage.ErrNotFound) {
			conditions = append(conditions, partialCondition(kind, err))
		}
	}
	hostpath, hostpathErr := a.hostPathVolumes(ctx)
	if hostpathErr == nil {
		counts["hostpath-volumes"] = len(hostpath)
	} else {
		conditions = append(conditions, partialCondition("HostPath volumes", hostpathErr))
	}
	return map[string]any{
		"providerId": a.id, "providerKind": "openebs", "namespace": a.namespace,
		"version": a.detectVersion(ctx), "health": a.Health(ctx), "engines": engines,
		"components": components, "resourceCounts": counts, "conditions": conditions,
		"observedAt": time.Now().UTC(),
	}, nil
}

func (a *Adapter) ResourceKinds(context.Context) []string {
	return []string{
		"components", "engines", "disk-pools", "lvm-nodes", "lvm-volumes",
		"lvm-snapshots", "zfs-nodes", "zfs-volumes", "zfs-snapshots",
		"zfs-backups", "zfs-restores", "hostpath-volumes",
	}
}

func (a *Adapter) ListProviderResources(ctx context.Context, kind string, page storage.PageRequest) (any, storage.PageMeta, error) {
	var data []map[string]any
	var err error
	switch kind {
	case "components":
		data, err = a.componentData(ctx)
	case "engines":
		data, err = a.engineData(ctx)
	case "hostpath-volumes":
		data, err = a.hostPathVolumes(ctx)
	default:
		spec, ok := resourceSpecs[kind]
		if !ok {
			return nil, storage.PageMeta{}, storage.ErrNotFound
		}
		data, _, err = a.listCRD(ctx, spec)
		if errors.Is(err, storage.ErrNotFound) {
			data, err = []map[string]any{}, nil
		}
	}
	if err != nil {
		return nil, storage.PageMeta{}, err
	}
	data = filterSearch(data, page.Search)
	return paginate(data, page), pageMeta(data, page), nil
}

func (a *Adapter) GetProviderResource(ctx context.Context, kind, id string) (any, error) {
	data, _, err := a.ListProviderResources(ctx, kind, storage.PageRequest{Limit: maxProviderList})
	if err != nil {
		return nil, err
	}
	for _, raw := range data.([]any) {
		item, _ := raw.(map[string]any)
		if fmt.Sprint(item["id"]) == id || fmt.Sprint(item["name"]) == id {
			return item, nil
		}
	}
	return nil, storage.ErrNotFound
}

func (a *Adapter) EnrichClaims(ctx context.Context, claims []storage.ClaimSummary) error {
	index := a.backendIndex(ctx)
	for i := range claims {
		switch claims[i].Driver {
		case HostPathDriver:
			if claims[i].PVName != "" {
				if _, ok := index["hostpath"][claims[i].PVName]; ok {
					claims[i].ProviderRef = &storage.ProviderReference{Kind: "openebs-hostpath-volume", ID: claims[i].PVName}
				}
			}
		case LVMDriver:
			a.enrichClaim(&claims[i], index["lvm"], "openebs-lvm-volume")
		case ZFSDriver:
			a.enrichClaim(&claims[i], index["zfs"], "openebs-zfs-volume")
		}
	}
	return nil
}

func (a *Adapter) EnrichVolumes(ctx context.Context, volumes []storage.PersistentVolumeSummary) error {
	index := a.backendIndex(ctx)
	for i := range volumes {
		switch volumes[i].Driver {
		case HostPathDriver:
			if item, ok := index["hostpath"][volumes[i].Name]; ok {
				volumes[i].ProviderRef = &storage.ProviderReference{Kind: "openebs-hostpath-volume", ID: volumes[i].Name}
				volumes[i].Backend = item
			}
		case LVMDriver:
			a.enrichVolume(&volumes[i], index["lvm"], "openebs-lvm-volume")
		case ZFSDriver:
			a.enrichVolume(&volumes[i], index["zfs"], "openebs-zfs-volume")
		case MayastorDriver:
			volumes[i].Conditions = append(volumes[i].Conditions, condition("BackendCorrelation", "Unknown", storage.SeverityInfo, "MayastorRuntimeNotConfigured", "Mayastor volume, replica, and target correlation requires Highland's versioned Mayastor runtime client.", time.Now().UTC()))
		}
	}
	return nil
}

func (a *Adapter) enrichClaim(claim *storage.ClaimSummary, index map[string]map[string]any, kind string) {
	if claim.VolumeHandle == "" {
		return
	}
	if _, ok := index[claim.VolumeHandle]; ok {
		claim.ProviderRef = &storage.ProviderReference{Kind: kind, ID: claim.VolumeHandle}
	}
}

func (a *Adapter) enrichVolume(volume *storage.PersistentVolumeSummary, index map[string]map[string]any, kind string) {
	if volume.VolumeHandle == "" {
		return
	}
	if item, ok := index[volume.VolumeHandle]; ok {
		volume.ProviderRef = &storage.ProviderReference{Kind: kind, ID: volume.VolumeHandle}
		volume.Backend = item
		return
	}
	volume.Conditions = append(volume.Conditions, condition("BackendCorrelation", "Unknown", storage.SeverityInfo, "NoAuthoritativeBackendMatch", "The CSI volume handle did not exactly match an observed OpenEBS backend resource.", time.Now().UTC()))
}

func (a *Adapter) backendIndex(ctx context.Context) map[string]map[string]map[string]any {
	result := map[string]map[string]map[string]any{"hostpath": {}, "lvm": {}, "zfs": {}}
	for engine, kind := range map[string]string{"lvm": "lvm-volumes", "zfs": "zfs-volumes"} {
		items, _, err := a.listCRD(ctx, resourceSpecs[kind])
		if err != nil {
			continue
		}
		for _, item := range items {
			for _, key := range []string{"id", "name", "volumeHandle", "volumeId", "uuid"} {
				value := cleanString(item[key])
				if value != "" {
					result[engine][value] = item
				}
			}
		}
	}
	if items, err := a.hostPathVolumes(ctx); err == nil {
		for _, item := range items {
			result["hostpath"][fmt.Sprint(item["name"])] = item
		}
	}
	return result
}

func (a *Adapter) engineData(ctx context.Context) ([]map[string]any, error) {
	drivers, err := a.list(ctx, schema.GroupVersionResource{Group: "storage.k8s.io", Version: "v1", Resource: "csidrivers"}, "")
	if err != nil {
		return nil, fmt.Errorf("read CSIDrivers: %w", err)
	}
	driverSet := map[string]bool{}
	for _, item := range drivers {
		driverSet[item.GetName()] = true
	}
	classes, err := a.list(ctx, schema.GroupVersionResource{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"}, "")
	if err != nil {
		return nil, fmt.Errorf("read StorageClasses: %w", err)
	}
	provisioners := map[string]bool{}
	for _, item := range classes {
		value, _, _ := unstructured.NestedString(item.Object, "provisioner")
		provisioners[value] = true
	}
	resolved, _ := a.discover()
	components, _ := a.componentData(ctx)
	componentText, _ := json.Marshal(components)
	componentNames := strings.ToLower(string(componentText))

	type engineDefinition struct {
		id, name, driver, mode, description string
		resourceKinds                       []string
	}
	definitions := []engineDefinition{
		{"mayastor", "Replicated PV / Mayastor", MayastorDriver, "replicated", "Synchronous replicated block storage over NVMe-oF.", []string{"disk-pools"}},
		{"lvm", "LocalPV LVM", LVMDriver, "local", "Node-local logical volumes from LVM volume groups.", []string{"lvm-nodes", "lvm-volumes", "lvm-snapshots"}},
		{"zfs", "LocalPV ZFS", ZFSDriver, "local", "Node-local ZFS datasets and zvols with snapshot capabilities.", []string{"zfs-nodes", "zfs-volumes", "zfs-snapshots", "zfs-backups", "zfs-restores"}},
		{"hostpath", "Dynamic LocalPV HostPath", HostPathDriver, "local", "Non-replicated node-local directories provisioned dynamically.", []string{"hostpath-volumes"}},
		{"rawfile", "LocalPV RawFile", RawFileDriver, "local", "Experimental file-backed local block volumes.", nil},
	}
	now := time.Now().UTC()
	result := make([]map[string]any, 0, len(definitions))
	for _, definition := range definitions {
		served := []string{}
		for _, kind := range definition.resourceKinds {
			if kind == "hostpath-volumes" {
				continue
			}
			if _, ok := resolved[kind]; ok {
				served = append(served, kind)
			}
		}
		componentHint := strings.Contains(componentNames, definition.id)
		if definition.id == "hostpath" {
			componentHint = strings.Contains(componentNames, "localpv-provisioner")
		}
		installed := driverSet[definition.driver] || provisioners[definition.driver] || len(served) > 0 || componentHint
		result = append(result, map[string]any{
			"id": definition.id, "name": definition.name, "engine": definition.id,
			"driver": definition.driver, "mode": definition.mode, "installed": installed,
			"description": definition.description, "resourceKinds": definition.resourceKinds,
			"servedResources": served, "source": "kubernetes-discovery", "observedAt": now,
			"providerId": a.id, "providerKind": "openebs",
		})
	}
	return result, nil
}

func (a *Adapter) componentData(ctx context.Context) ([]map[string]any, error) {
	now := time.Now().UTC()
	result := []map[string]any{}
	for _, source := range []struct {
		kind string
		gvr  schema.GroupVersionResource
	}{
		{"Deployment", schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}},
		{"DaemonSet", schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}},
	} {
		items, err := a.list(ctx, source.gvr, a.namespace)
		if err != nil {
			return nil, fmt.Errorf("read OpenEBS %s: %w", source.kind, err)
		}
		for _, item := range items {
			if !isOpenEBSObject(item) {
				continue
			}
			desired, ready := componentReplicas(source.kind, item)
			images := componentImages(item)
			engine := componentEngine(item.GetName(), images)
			result = append(result, map[string]any{
				"id": item.GetName(), "name": item.GetName(), "namespace": item.GetNamespace(),
				"kind": source.kind, "engine": engine, "desired": desired, "readyReplicas": ready,
				"ready": desired > 0 && ready >= desired, "images": images, "source": "kubernetes-workload",
				"observedAt": now, "providerId": a.id, "providerKind": "openebs",
			})
		}
	}
	sort.Slice(result, func(i, j int) bool { return fmt.Sprint(result[i]["name"]) < fmt.Sprint(result[j]["name"]) })
	return result, nil
}

func (a *Adapter) hostPathVolumes(ctx context.Context) ([]map[string]any, error) {
	classes, err := a.list(ctx, schema.GroupVersionResource{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"}, "")
	if err != nil {
		return nil, err
	}
	hostPathClasses := map[string]bool{}
	for _, item := range classes {
		provisioner, _, _ := unstructured.NestedString(item.Object, "provisioner")
		if provisioner == HostPathDriver {
			hostPathClasses[item.GetName()] = true
		}
	}
	volumes, err := a.list(ctx, schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumes"}, "")
	if err != nil {
		return nil, err
	}
	result := []map[string]any{}
	now := time.Now().UTC()
	for _, volume := range volumes {
		className, _, _ := unstructured.NestedString(volume.Object, "spec", "storageClassName")
		if !hostPathClasses[className] {
			continue
		}
		capacity, _, _ := unstructured.NestedString(volume.Object, "spec", "capacity", "storage")
		phase, _, _ := unstructured.NestedString(volume.Object, "status", "phase")
		path, _, _ := unstructured.NestedString(volume.Object, "spec", "local", "path")
		if path == "" {
			path, _, _ = unstructured.NestedString(volume.Object, "spec", "hostPath", "path")
		}
		node := pvNode(volume)
		claimNamespace, _, _ := unstructured.NestedString(volume.Object, "spec", "claimRef", "namespace")
		claimName, _, _ := unstructured.NestedString(volume.Object, "spec", "claimRef", "name")
		result = append(result, map[string]any{
			"id": volume.GetName(), "name": volume.GetName(), "engine": "hostpath",
			"storageClass": className, "capacity": capacity, "phase": phase, "state": phase,
			"node": node, "path": path, "claimNamespace": claimNamespace, "claimName": claimName,
			"reclaimPolicy": nestedString(volume.Object, "spec", "persistentVolumeReclaimPolicy"),
			"source":        "kubernetes-pv", "observedAt": now, "providerId": a.id, "providerKind": "openebs",
		})
	}
	sort.Slice(result, func(i, j int) bool { return fmt.Sprint(result[i]["name"]) < fmt.Sprint(result[j]["name"]) })
	return result, nil
}

func (a *Adapter) listCRD(ctx context.Context, spec resourceSpec) ([]map[string]any, *resolvedResource, error) {
	resolved, discoveryErr := a.discover()
	if discoveryErr != nil {
		return nil, nil, discoveryErr
	}
	resource, ok := resolved[spec.Kind]
	if !ok {
		return nil, nil, storage.ErrNotFound
	}
	namespace := ""
	if resource.Namespaced {
		namespace = a.namespace
	}
	items, err := a.list(ctx, resource.GVR, namespace)
	if err != nil {
		return nil, &resource, fmt.Errorf("read OpenEBS %s: %w", spec.Resource, err)
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, normalizeResource(a.id, resource, item))
	}
	sort.Slice(result, func(i, j int) bool { return fmt.Sprint(result[i]["name"]) < fmt.Sprint(result[j]["name"]) })
	return result, &resource, nil
}

func (a *Adapter) discover() (map[string]resolvedResource, error) {
	a.mu.RLock()
	if time.Since(a.discoveredAt) < discoveryTTL {
		result, err := copyResolved(a.resolved), a.discoveryError
		a.mu.RUnlock()
		return result, err
	}
	a.mu.RUnlock()

	resolved := map[string]resolvedResource{}
	for kind, spec := range resourceSpecs {
		for _, version := range spec.Versions {
			resources, err := a.discovery.ServerResourcesForGroupVersion(spec.Group + "/" + version)
			if err != nil {
				continue
			}
			for _, resource := range resources.APIResources {
				if resource.Name != spec.Resource || strings.Contains(resource.Name, "/") {
					continue
				}
				copy := spec
				copy.Namespaced = resource.Namespaced
				resolved[kind] = resolvedResource{
					resourceSpec: copy,
					GVR:          schema.GroupVersionResource{Group: spec.Group, Version: version, Resource: spec.Resource},
				}
				break
			}
			if _, ok := resolved[kind]; ok {
				break
			}
		}
	}
	a.mu.Lock()
	a.resolved, a.discoveredAt, a.discoveryError = resolved, time.Now(), nil
	a.mu.Unlock()
	return copyResolved(resolved), nil
}

func (a *Adapter) list(ctx context.Context, gvr schema.GroupVersionResource, namespace string) ([]*unstructured.Unstructured, error) {
	var list *unstructured.UnstructuredList
	var err error
	if namespace == "" {
		list, err = a.dynamic.Resource(gvr).List(ctx, metav1.ListOptions{Limit: maxProviderList})
	} else {
		list, err = a.dynamic.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{Limit: maxProviderList})
	}
	if err != nil {
		return nil, err
	}
	result := make([]*unstructured.Unstructured, 0, len(list.Items))
	for i := range list.Items {
		result = append(result, list.Items[i].DeepCopy())
	}
	return result, nil
}

func (a *Adapter) detectVersion(ctx context.Context) string {
	components, err := a.componentData(ctx)
	if err != nil {
		return ""
	}
	versions := map[string]int{}
	for _, component := range components {
		images, _ := component["images"].([]string)
		for _, image := range images {
			if version := imageVersion(image); version != "" {
				versions[version]++
			}
		}
	}
	best, count := "", 0
	for version, occurrences := range versions {
		if occurrences > count {
			best, count = version, occurrences
		}
	}
	return best
}

func normalizeResource(providerID string, resource resolvedResource, item *unstructured.Unstructured) map[string]any {
	now := time.Now().UTC()
	result := map[string]any{
		"id": item.GetName(), "name": item.GetName(), "namespace": item.GetNamespace(),
		"kubernetesUid": string(item.GetUID()), "kind": item.GetKind(), "apiVersion": item.GetAPIVersion(),
		"engine": resource.Engine, "resourceKind": resource.Kind, "source": "openebs-crd",
		"observedAt": now, "providerId": providerID, "providerKind": "openebs",
	}
	if item.GetNamespace() != "" {
		result["id"] = item.GetNamespace() + "/" + item.GetName()
	}
	for _, section := range []string{"spec", "status"} {
		if value, ok := item.Object[section]; ok {
			result[section] = bounded(value, 0)
		}
	}
	promote(result, item.Object, "state", [][]string{{"status", "state"}, {"status", "phase"}, {"status", "cr_state"}, {"status", "crState"}})
	promote(result, item.Object, "phase", [][]string{{"status", "phase"}, {"status", "state"}})
	promote(result, item.Object, "health", [][]string{{"status", "health"}, {"status", "state"}})
	promote(result, item.Object, "node", [][]string{{"spec", "node"}, {"spec", "nodeName"}, {"status", "node"}, {"status", "nodeName"}})
	promote(result, item.Object, "pool", [][]string{{"spec", "poolName"}, {"spec", "pool"}, {"spec", "zfsPool"}})
	promote(result, item.Object, "volumeGroup", [][]string{{"spec", "vgName"}, {"spec", "volGroup"}, {"spec", "volumeGroup"}})
	promote(result, item.Object, "volumeHandle", [][]string{{"spec", "volumeID"}, {"spec", "volumeId"}, {"spec", "volName"}, {"status", "volumeID"}})
	promote(result, item.Object, "capacity", [][]string{{"status", "capacity"}, {"spec", "capacity"}, {"spec", "size"}})
	promote(result, item.Object, "used", [][]string{{"status", "used"}, {"status", "usedBytes"}})
	promote(result, item.Object, "available", [][]string{{"status", "available"}, {"status", "availableBytes"}})
	promote(result, item.Object, "replicas", [][]string{{"spec", "replicas"}, {"spec", "num_replicas"}})
	promote(result, item.Object, "filesystem", [][]string{{"spec", "fsType"}, {"spec", "fstype"}})
	promote(result, item.Object, "encrypted", [][]string{{"spec", "encrypted"}})
	return result
}

func promote(target map[string]any, object map[string]any, key string, paths [][]string) {
	for _, path := range paths {
		value, found, _ := unstructured.NestedFieldNoCopy(object, path...)
		if found && value != nil && fmt.Sprint(value) != "" {
			target[key] = bounded(value, 0)
			return
		}
	}
}

func bounded(value any, depth int) any {
	if depth > 5 {
		return "[truncated]"
	}
	switch typed := value.(type) {
	case map[string]any:
		result := map[string]any{}
		count := 0
		for key, child := range typed {
			if sensitiveKey(key) {
				continue
			}
			if count >= 100 {
				result["truncated"] = true
				break
			}
			result[key] = bounded(child, depth+1)
			count++
		}
		return result
	case []any:
		limit := len(typed)
		if limit > 100 {
			limit = 100
		}
		result := make([]any, 0, limit)
		for _, child := range typed[:limit] {
			result = append(result, bounded(child, depth+1))
		}
		return result
	case string:
		if len(typed) > 2000 {
			return typed[:2000] + "…"
		}
		return typed
	default:
		return typed
	}
}

func sensitiveKey(key string) bool {
	key = strings.ToLower(key)
	return strings.Contains(key, "secret") || strings.Contains(key, "password") ||
		strings.Contains(key, "credential") || strings.Contains(key, "token") ||
		strings.HasSuffix(key, "key") || strings.Contains(key, "chap")
}

func isOpenEBSObject(item *unstructured.Unstructured) bool {
	encoded, _ := json.Marshal(map[string]any{
		"name": item.GetName(), "labels": item.GetLabels(), "images": componentImages(item),
	})
	text := strings.ToLower(string(encoded))
	return strings.Contains(text, "openebs") || strings.Contains(text, "mayastor")
}

func componentReplicas(kind string, item *unstructured.Unstructured) (int64, int64) {
	if kind == "DaemonSet" {
		desired, _, _ := unstructured.NestedInt64(item.Object, "status", "desiredNumberScheduled")
		ready, _, _ := unstructured.NestedInt64(item.Object, "status", "numberReady")
		return desired, ready
	}
	desired, found, _ := unstructured.NestedInt64(item.Object, "spec", "replicas")
	if !found {
		desired = 1
	}
	ready, _, _ := unstructured.NestedInt64(item.Object, "status", "readyReplicas")
	return desired, ready
}

func componentImages(item *unstructured.Unstructured) []string {
	containers, _, _ := unstructured.NestedSlice(item.Object, "spec", "template", "spec", "containers")
	result := []string{}
	for _, raw := range containers {
		container, _ := raw.(map[string]any)
		image := cleanString(container["image"])
		if image != "" {
			result = append(result, image)
		}
	}
	return result
}

func componentEngine(name string, images []string) string {
	text := strings.ToLower(name + " " + strings.Join(images, " "))
	switch {
	case strings.Contains(text, "mayastor") || strings.Contains(text, "io-engine"):
		return "mayastor"
	case strings.Contains(text, "zfs"):
		return "zfs"
	case strings.Contains(text, "lvm"):
		return "lvm"
	case strings.Contains(text, "rawfile"):
		return "rawfile"
	case strings.Contains(text, "localpv"):
		return "hostpath"
	default:
		return "shared"
	}
}

func imageVersion(image string) string {
	image = strings.SplitN(image, "@", 2)[0]
	index := strings.LastIndex(image, ":")
	if index < 0 || index == len(image)-1 {
		return ""
	}
	value := strings.TrimPrefix(image[index+1:], "v")
	if value == "latest" || value == "develop" {
		return ""
	}
	return value
}

func pvNode(volume *unstructured.Unstructured) string {
	terms, _, _ := unstructured.NestedSlice(volume.Object, "spec", "nodeAffinity", "required", "nodeSelectorTerms")
	for _, termRaw := range terms {
		term, _ := termRaw.(map[string]any)
		expressions, _, _ := unstructured.NestedSlice(term, "matchExpressions")
		for _, expressionRaw := range expressions {
			expression, _ := expressionRaw.(map[string]any)
			key := cleanString(expression["key"])
			if key != "kubernetes.io/hostname" && key != "topology.kubernetes.io/hostname" {
				continue
			}
			values, _ := expression["values"].([]any)
			if len(values) > 0 {
				return cleanString(values[0])
			}
		}
	}
	return ""
}

func nestedString(object map[string]any, path ...string) string {
	value, _, _ := unstructured.NestedString(object, path...)
	return value
}

func filterSearch(data []map[string]any, search string) []map[string]any {
	search = strings.ToLower(strings.TrimSpace(search))
	if search == "" {
		return data
	}
	result := make([]map[string]any, 0, len(data))
	for _, item := range data {
		encoded, _ := json.Marshal(item)
		if strings.Contains(strings.ToLower(string(encoded)), search) {
			result = append(result, item)
		}
	}
	return result
}

func paginate(data []map[string]any, page storage.PageRequest) any {
	start, end := pageBounds(len(data), page)
	result := make([]any, 0, end-start)
	for _, item := range data[start:end] {
		result = append(result, item)
	}
	return result
}

func pageMeta(data []map[string]any, page storage.PageRequest) storage.PageMeta {
	_, end := pageBounds(len(data), page)
	meta := storage.PageMeta{Limit: page.Limit, Total: len(data)}
	if end < len(data) {
		meta.Continue = storage.EncodePageOffset(end)
	}
	return meta
}

func pageBounds(length int, page storage.PageRequest) (int, int) {
	limit := page.Limit
	if limit <= 0 || limit > maxProviderList {
		limit = 100
	}
	start := page.Offset
	if start < 0 {
		start = 0
	}
	if start > length {
		start = length
	}
	end := start + limit
	if end > length {
		end = length
	}
	return start, end
}

func condition(kind, status string, severity storage.Severity, reason, message string, observedAt time.Time) storage.Condition {
	return storage.Condition{Type: kind, Status: status, Severity: severity, Reason: reason, Message: message, ObservedAt: observedAt}
}

func partialCondition(source string, err error) storage.Condition {
	return condition(source+"Available", "False", storage.SeverityWarning, "PartialProviderData", err.Error(), time.Now().UTC())
}

func worse(current, candidate storage.Severity) storage.Severity {
	order := map[storage.Severity]int{
		storage.SeverityUnknown: 0, storage.SeverityOK: 1, storage.SeverityInfo: 2,
		storage.SeverityWarning: 3, storage.SeverityError: 4,
	}
	if order[candidate] > order[current] {
		return candidate
	}
	return current
}

func firstValue(item map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := item[key]; ok {
			return value
		}
	}
	return nil
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		result, _ := strconv.ParseBool(typed)
		return result
	default:
		return false
	}
}

func cleanString(value any) string {
	if value == nil {
		return ""
	}
	result := strings.TrimSpace(fmt.Sprint(value))
	if result == "<nil>" {
		return ""
	}
	return result
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func copyResolved(input map[string]resolvedResource) map[string]resolvedResource {
	result := make(map[string]resolvedResource, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

var _ storage.Provider = (*Adapter)(nil)
var _ storage.ProviderSummaryReader = (*Adapter)(nil)
var _ storage.ProviderResourceReader = (*Adapter)(nil)
var _ storage.InventoryEnricher = (*Adapter)(nil)
