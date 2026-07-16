package storage

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const (
	resyncPeriod          = 10 * time.Minute
	discoveryRefresh      = 5 * time.Minute
	indexPVCByPV          = "highland.pvcByPV"
	indexPodByPVC         = "highland.podByPVC"
	indexAttachmentByPV   = "highland.attachmentByPV"
	indexSnapshotBySource = "highland.snapshotBySourcePVC"
)

var (
	snapshotGVR        = schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshots"}
	snapshotClassGVR   = schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshotclasses"}
	snapshotContentGVR = schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshotcontents"}
)

// Scope controls which namespaced objects Highland returns. The informer may
// have wider read permission; this policy remains a mandatory response filter.
type Scope struct {
	Mode       string
	Namespaces []string
	allowed    map[string]struct{}
}

func NewScope(mode string, namespaces []string) Scope {
	s := Scope{Mode: strings.ToLower(strings.TrimSpace(mode)), Namespaces: append([]string(nil), namespaces...), allowed: map[string]struct{}{}}
	if s.Mode == "" {
		s.Mode = "cluster"
	}
	for _, ns := range namespaces {
		if ns = strings.TrimSpace(ns); ns != "" {
			s.allowed[ns] = struct{}{}
		}
	}
	return s
}

func (s Scope) Allows(namespace string) bool {
	if s.Mode != "namespaces" {
		return true
	}
	_, ok := s.allowed[namespace]
	return ok
}

// ChangePublisher receives provider-neutral invalidation events.
type ChangePublisher interface {
	PublishStorageChange(providerID, namespace, kind, name string)
}

// Inventory maintains informer caches and builds normalized storage summaries.
type Inventory struct {
	core      kubernetes.Interface
	dynamic   dynamic.Interface
	discovery discovery.DiscoveryInterface
	registry  *Registry
	scope     Scope
	publisher ChangePublisher
	observer  Observer

	factory            informers.SharedInformerFactory
	namespaceFactories []informers.SharedInformerFactory
	pv                 cache.SharedIndexInformer
	pvc                cache.SharedIndexInformer
	pod                cache.SharedIndexInformer
	event              cache.SharedIndexInformer
	sc                 cache.SharedIndexInformer
	driver             cache.SharedIndexInformer
	node               cache.SharedIndexInformer
	capacity           cache.SharedIndexInformer
	attachment         cache.SharedIndexInformer
	volumeAttributes   cache.SharedIndexInformer
	namespacePVC       []cache.SharedIndexInformer
	namespacePod       []cache.SharedIndexInformer
	namespaceEvent     []cache.SharedIndexInformer
	namespaceCapacity  []cache.SharedIndexInformer

	dynamicFactory            dynamicinformer.DynamicSharedInformerFactory
	namespaceDynamicFactories []dynamicinformer.DynamicSharedInformerFactory
	snapshotMu                sync.RWMutex
	snapshot                  cache.SharedIndexInformer
	namespaceSnapshot         []cache.SharedIndexInformer
	snapshotClass             cache.SharedIndexInformer
	snapshotContent           cache.SharedIndexInformer
	snapshotAvailable         atomic.Bool
	discoveryMu               sync.RWMutex
	discoveryError            string

	started  atomic.Bool
	ready    atomic.Bool
	lastSync atomic.Int64
	startMu  sync.Mutex
	stopCh   <-chan struct{}
}

func NewInventory(core kubernetes.Interface, dyn dynamic.Interface, disco discovery.DiscoveryInterface, registry *Registry, scope Scope) (*Inventory, error) {
	if core == nil || dyn == nil || disco == nil {
		return nil, fmt.Errorf("typed, dynamic, and discovery kubernetes clients are required")
	}
	if registry == nil {
		registry = NewRegistry()
	}
	i := &Inventory{
		core: core, dynamic: dyn, discovery: disco, registry: registry, scope: scope,
	}
	if scope.Mode == "namespaces" {
		if len(scope.allowed) == 0 {
			return nil, fmt.Errorf("namespace storage scope requires at least one namespace")
		}
		for namespace := range scope.allowed {
			factory := informers.NewSharedInformerFactoryWithOptions(core, resyncPeriod, informers.WithNamespace(namespace))
			i.namespaceFactories = append(i.namespaceFactories, factory)
			i.namespacePVC = append(i.namespacePVC, factory.Core().V1().PersistentVolumeClaims().Informer())
			i.namespacePod = append(i.namespacePod, factory.Core().V1().Pods().Informer())
			i.namespaceEvent = append(i.namespaceEvent, factory.Core().V1().Events().Informer())
			i.namespaceCapacity = append(i.namespaceCapacity, factory.Storage().V1().CSIStorageCapacities().Informer())
		}
	} else {
		factory := informers.NewSharedInformerFactory(core, resyncPeriod)
		i.factory = factory
		i.pv = factory.Core().V1().PersistentVolumes().Informer()
		i.pvc = factory.Core().V1().PersistentVolumeClaims().Informer()
		i.pod = factory.Core().V1().Pods().Informer()
		i.event = factory.Core().V1().Events().Informer()
		i.sc = factory.Storage().V1().StorageClasses().Informer()
		i.driver = factory.Storage().V1().CSIDrivers().Informer()
		i.node = factory.Storage().V1().CSINodes().Informer()
		i.capacity = factory.Storage().V1().CSIStorageCapacities().Informer()
		i.attachment = factory.Storage().V1().VolumeAttachments().Informer()
		if resourceServed(disco, "storage.k8s.io/v1", "volumeattributesclasses") {
			i.volumeAttributes = factory.Storage().V1().VolumeAttributesClasses().Informer()
		}
		i.dynamicFactory = dynamicinformer.NewDynamicSharedInformerFactory(dyn, resyncPeriod)
	}
	if err := i.addIndexes(); err != nil {
		return nil, err
	}
	return i, nil
}

func (i *Inventory) SetPublisher(p ChangePublisher) { i.publisher = p }
func (i *Inventory) SetObserver(o Observer)         { i.observer = o }

func (i *Inventory) addIndexes() error {
	pvcIndex := cache.Indexers{indexPVCByPV: func(obj any) ([]string, error) {
		pvc, ok := obj.(*corev1.PersistentVolumeClaim)
		if !ok || pvc.Spec.VolumeName == "" {
			return nil, nil
		}
		return []string{pvc.Spec.VolumeName}, nil
	}}
	podIndex := cache.Indexers{indexPodByPVC: func(obj any) ([]string, error) {
		pod, ok := obj.(*corev1.Pod)
		if !ok {
			return nil, nil
		}
		keys := make([]string, 0, len(pod.Spec.Volumes))
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName != "" {
				keys = append(keys, pod.Namespace+"/"+volume.PersistentVolumeClaim.ClaimName)
			}
		}
		return keys, nil
	}}
	attachmentIndex := cache.Indexers{indexAttachmentByPV: func(obj any) ([]string, error) {
		va, ok := obj.(*storagev1.VolumeAttachment)
		if !ok || va.Spec.Source.PersistentVolumeName == nil {
			return nil, nil
		}
		return []string{*va.Spec.Source.PersistentVolumeName}, nil
	}}
	for _, informer := range appendInformer(i.pvc, i.namespacePVC) {
		if err := informer.AddIndexers(pvcIndex); err != nil {
			return fmt.Errorf("add PVC index: %w", err)
		}
	}
	for _, informer := range appendInformer(i.pod, i.namespacePod) {
		if err := informer.AddIndexers(podIndex); err != nil {
			return fmt.Errorf("add Pod index: %w", err)
		}
	}
	for _, informer := range appendInformer(i.attachment, nil) {
		if err := informer.AddIndexers(attachmentIndex); err != nil {
			return fmt.Errorf("add VolumeAttachment index: %w", err)
		}
	}
	return nil
}

func (i *Inventory) Start(ctx context.Context) {
	i.startMu.Lock()
	defer i.startMu.Unlock()
	if i.started.Swap(true) {
		return
	}
	i.stopCh = ctx.Done()
	i.registerCoreChangeHandlers()
	if i.factory != nil {
		i.factory.Start(ctx.Done())
	}
	for _, factory := range i.namespaceFactories {
		factory.Start(ctx.Done())
	}
	i.ensureSnapshotInformers()
	if i.dynamicFactory != nil {
		i.dynamicFactory.Start(ctx.Done())
	}

	go i.waitForInitialSync(ctx)
	go i.refreshDiscoveryLoop(ctx)
}

func (i *Inventory) waitForInitialSync(ctx context.Context) {
	syncFunctions := []cache.InformerSynced{}
	for _, informer := range appendInformer(i.pv, nil) {
		syncFunctions = append(syncFunctions, informer.HasSynced)
	}
	for _, informer := range appendInformer(i.pvc, i.namespacePVC) {
		syncFunctions = append(syncFunctions, informer.HasSynced)
	}
	for _, informer := range appendInformer(i.pod, i.namespacePod) {
		syncFunctions = append(syncFunctions, informer.HasSynced)
	}
	for _, informer := range appendInformer(i.event, i.namespaceEvent) {
		syncFunctions = append(syncFunctions, informer.HasSynced)
	}
	for _, informer := range appendInformer(i.sc, nil) {
		syncFunctions = append(syncFunctions, informer.HasSynced)
	}
	for _, informer := range appendInformer(i.driver, nil) {
		syncFunctions = append(syncFunctions, informer.HasSynced)
	}
	for _, informer := range appendInformer(i.node, nil) {
		syncFunctions = append(syncFunctions, informer.HasSynced)
	}
	for _, informer := range appendInformer(i.capacity, i.namespaceCapacity) {
		syncFunctions = append(syncFunctions, informer.HasSynced)
	}
	for _, informer := range appendInformer(i.attachment, nil) {
		syncFunctions = append(syncFunctions, informer.HasSynced)
	}
	for _, informer := range appendInformer(i.volumeAttributes, nil) {
		syncFunctions = append(syncFunctions, informer.HasSynced)
	}
	coreSynced := len(syncFunctions) > 0 && cache.WaitForCacheSync(ctx.Done(), syncFunctions...)
	if !coreSynced {
		return
	}
	i.ready.Store(true)
	now := time.Now().UTC()
	i.lastSync.Store(now.Unix())
	if i.observer != nil {
		i.observer.SetStorageSyncTimestamp("kubernetes", now)
	}
}

func (i *Inventory) refreshDiscoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(discoveryRefresh)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			i.ensureSnapshotInformers()
			if i.dynamicFactory != nil {
				i.dynamicFactory.Start(ctx.Done())
			}
			for _, factory := range i.namespaceDynamicFactories {
				factory.Start(ctx.Done())
			}
		}
	}
}

func (i *Inventory) ensureSnapshotInformers() {
	resources, err := i.discovery.ServerResourcesForGroupVersion("snapshot.storage.k8s.io/v1")
	if err != nil || resources == nil {
		i.discoveryMu.Lock()
		if err != nil {
			i.discoveryError = err.Error()
		} else {
			i.discoveryError = "snapshot.storage.k8s.io/v1 discovery returned no resources"
		}
		i.discoveryMu.Unlock()
		return
	}
	i.discoveryMu.Lock()
	i.discoveryError = ""
	i.discoveryMu.Unlock()
	present := map[string]bool{}
	for _, resource := range resources.APIResources {
		present[resource.Name] = true
	}
	if !present[snapshotGVR.Resource] {
		return
	}

	i.snapshotMu.Lock()
	defer i.snapshotMu.Unlock()
	if i.snapshot != nil || len(i.namespaceSnapshot) > 0 {
		i.snapshotAvailable.Store(true)
		return
	}
	if i.scope.Mode == "namespaces" {
		for namespace := range i.scope.allowed {
			factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(i.dynamic, resyncPeriod, namespace, nil)
			informer := factory.ForResource(snapshotGVR).Informer()
			_ = informer.AddIndexers(cache.Indexers{indexSnapshotBySource: snapshotSourceIndex})
			i.registerInformerChangeHandler(informer, "snapshots")
			i.namespaceDynamicFactories = append(i.namespaceDynamicFactories, factory)
			i.namespaceSnapshot = append(i.namespaceSnapshot, informer)
			if i.stopCh != nil {
				factory.Start(i.stopCh)
			}
		}
		i.snapshotAvailable.Store(true)
		return
	}
	if !present[snapshotClassGVR.Resource] || !present[snapshotContentGVR.Resource] || i.dynamicFactory == nil {
		return
	}
	i.snapshot = i.dynamicFactory.ForResource(snapshotGVR).Informer()
	i.snapshotClass = i.dynamicFactory.ForResource(snapshotClassGVR).Informer()
	i.snapshotContent = i.dynamicFactory.ForResource(snapshotContentGVR).Informer()
	_ = i.snapshot.AddIndexers(cache.Indexers{indexSnapshotBySource: snapshotSourceIndex})
	i.registerInformerChangeHandler(i.snapshot, "snapshots")
	i.registerInformerChangeHandler(i.snapshotClass, "snapshot-classes")
	i.registerInformerChangeHandler(i.snapshotContent, "snapshots")
	i.snapshotAvailable.Store(true)
}

func (i *Inventory) registerCoreChangeHandlers() {
	for _, informer := range appendInformer(i.pv, nil) {
		i.registerInformerChangeHandler(informer, "volumes")
	}
	for _, informer := range appendInformer(i.pvc, i.namespacePVC) {
		i.registerInformerChangeHandler(informer, "claims")
	}
	for _, informer := range appendInformer(i.pod, i.namespacePod) {
		i.registerInformerChangeHandler(informer, "claims")
	}
	for _, informer := range appendInformer(i.event, i.namespaceEvent) {
		i.registerInformerChangeHandler(informer, "events")
	}
	for _, informer := range appendInformer(i.sc, nil) {
		i.registerInformerChangeHandler(informer, "classes")
	}
	for _, informer := range appendInformer(i.driver, nil) {
		i.registerInformerChangeHandler(informer, "drivers")
	}
	for _, informer := range appendInformer(i.node, nil) {
		i.registerInformerChangeHandler(informer, "drivers")
	}
	for _, informer := range appendInformer(i.capacity, i.namespaceCapacity) {
		i.registerInformerChangeHandler(informer, "capacity")
	}
	for _, informer := range appendInformer(i.attachment, nil) {
		i.registerInformerChangeHandler(informer, "attachments")
	}
	for _, informer := range appendInformer(i.volumeAttributes, nil) {
		i.registerInformerChangeHandler(informer, "classes")
	}
}

func (i *Inventory) registerInformerChangeHandler(informer cache.SharedIndexInformer, kind string) {
	if informer == nil {
		return
	}
	_ = informer.SetWatchErrorHandler(func(_ *cache.Reflector, _ error) {
		if i.observer != nil {
			i.observer.IncStorageWatchError("kubernetes/" + kind)
		}
	})
	handler := func(obj any) {
		now := time.Now().UTC()
		i.lastSync.Store(now.Unix())
		if i.observer != nil {
			i.observer.SetStorageSyncTimestamp("kubernetes", now)
		}
		if i.publisher == nil {
			return
		}
		accessor, err := meta.Accessor(obj)
		if err != nil {
			if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
				accessor, err = meta.Accessor(tombstone.Obj)
			}
		}
		if err == nil {
			i.publisher.PublishStorageChange("", accessor.GetNamespace(), kind, accessor.GetName())
		}
	}
	_, _ = informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    handler,
		UpdateFunc: func(_, newObj any) { handler(newObj) },
		DeleteFunc: handler,
	})
}

func (i *Inventory) Ready() bool { return i != nil && i.ready.Load() }

func (i *Inventory) SnapshotAvailable() bool { return i != nil && i.snapshotAvailable.Load() }

func (i *Inventory) LastSync() time.Time {
	if i == nil {
		return time.Time{}
	}
	unix := i.lastSync.Load()
	if unix == 0 {
		return time.Time{}
	}
	return time.Unix(unix, 0).UTC()
}

func (i *Inventory) Drivers(ctx context.Context) ([]DriverSummary, error) {
	if err := i.requireReady(); err != nil {
		return nil, err
	}
	driverObjects := map[string]*storagev1.CSIDriver{}
	for _, obj := range informerObjects(i.driver, nil) {
		if driver, ok := obj.(*storagev1.CSIDriver); ok {
			driverObjects[driver.Name] = driver
		}
	}
	classCounts := map[string]int{}
	for _, obj := range informerObjects(i.sc, nil) {
		if class, ok := obj.(*storagev1.StorageClass); ok {
			classCounts[class.Provisioner]++
			if _, exists := driverObjects[class.Provisioner]; !exists {
				driverObjects[class.Provisioner] = nil
			}
		}
	}
	volumeCounts := map[string]int{}
	for _, obj := range informerObjects(i.pv, nil) {
		if pv, ok := obj.(*corev1.PersistentVolume); ok && pv.Spec.CSI != nil {
			volumeCounts[pv.Spec.CSI.Driver]++
			if _, exists := driverObjects[pv.Spec.CSI.Driver]; !exists {
				driverObjects[pv.Spec.CSI.Driver] = nil
			}
		}
	}
	nodeCounts := map[string]int{}
	for _, obj := range informerObjects(i.node, nil) {
		if node, ok := obj.(*storagev1.CSINode); ok {
			for _, driver := range node.Spec.Drivers {
				nodeCounts[driver.Name]++
				if _, exists := driverObjects[driver.Name]; !exists {
					driverObjects[driver.Name] = nil
				}
			}
		}
	}

	out := make([]DriverSummary, 0, len(driverObjects))
	for name, obj := range driverObjects {
		summary := DriverSummary{
			Name: name, ProviderID: i.registry.ResolveDriver(name), SupportLevel: SupportDetected,
			NodeCount: nodeCounts[name], StorageClassCount: classCounts[name], PersistentVolCount: volumeCounts[name],
		}
		if provider, ok := i.registry.Provider(summary.ProviderID); ok {
			if desc, err := provider.Descriptor(ctx); err == nil {
				summary.SupportLevel = desc.SupportLevel
			}
		}
		if obj != nil {
			summary.AttachRequired = obj.Spec.AttachRequired
			summary.PodInfoOnMount = obj.Spec.PodInfoOnMount
			summary.StorageCapacity = obj.Spec.StorageCapacity != nil && *obj.Spec.StorageCapacity
			if obj.Spec.FSGroupPolicy != nil {
				summary.FSGroupPolicy = string(*obj.Spec.FSGroupPolicy)
			}
			for _, mode := range obj.Spec.VolumeLifecycleModes {
				summary.VolumeLifecycle = append(summary.VolumeLifecycle, string(mode))
			}
			for _, token := range obj.Spec.TokenRequests {
				if token.Audience != "" {
					summary.TokenRequests = append(summary.TokenRequests, token.Audience)
				}
			}
			summary.RequiresRepublish = obj.Spec.RequiresRepublish
			summary.SELinuxMount = obj.Spec.SELinuxMount
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out, nil
}

func (i *Inventory) DiscoveredDriverNames() ([]string, error) {
	drivers, err := i.Drivers(context.Background())
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(drivers))
	for _, driver := range drivers {
		names = append(names, driver.Name)
	}
	return names, nil
}

func (i *Inventory) StorageClasses() ([]StorageClassSummary, error) {
	if err := i.requireReady(); err != nil {
		return nil, err
	}
	claimCounts, volumeCounts := map[string]int{}, map[string]int{}
	for _, obj := range informerObjects(i.pvc, i.namespacePVC) {
		if pvc, ok := obj.(*corev1.PersistentVolumeClaim); ok && i.scope.Allows(pvc.Namespace) && pvc.Spec.StorageClassName != nil {
			claimCounts[*pvc.Spec.StorageClassName]++
		}
	}
	for _, obj := range informerObjects(i.pv, nil) {
		if pv, ok := obj.(*corev1.PersistentVolume); ok {
			volumeCounts[pv.Spec.StorageClassName]++
		}
	}
	snapshotClasses := i.snapshotClassesByDriver()
	classObjects := informerObjects(i.sc, nil)
	out := make([]StorageClassSummary, 0, len(classObjects))
	for _, obj := range classObjects {
		class, ok := obj.(*storagev1.StorageClass)
		if !ok {
			continue
		}
		summary := StorageClassSummary{
			Name: class.Name, UID: string(class.UID), ProviderID: i.registry.ResolveDriver(class.Provisioner),
			Provisioner: class.Provisioner, Parameters: safeParameters(class.Parameters),
			MountOptions: append([]string(nil), class.MountOptions...), ClaimCount: claimCounts[class.Name],
			VolumeCount: volumeCounts[class.Name], SnapshotClasses: snapshotClasses[class.Provisioner],
			CreatedAt: class.CreationTimestamp.Time,
			Default:   class.Annotations["storageclass.kubernetes.io/is-default-class"] == "true" || class.Annotations["storageclass.beta.kubernetes.io/is-default-class"] == "true",
		}
		if class.ReclaimPolicy != nil {
			summary.ReclaimPolicy = string(*class.ReclaimPolicy)
		}
		if class.VolumeBindingMode != nil {
			summary.VolumeBindingMode = string(*class.VolumeBindingMode)
		}
		if class.AllowVolumeExpansion != nil {
			summary.AllowVolumeExpansion = *class.AllowVolumeExpansion
		}
		for _, term := range class.AllowedTopologies {
			for _, expression := range term.MatchLabelExpressions {
				summary.AllowedTopologies = append(summary.AllowedTopologies, TopologyTerm{Key: expression.Key, Values: append([]string(nil), expression.Values...)})
			}
		}
		if !i.SnapshotAvailable() {
			summary.Conditions = append(summary.Conditions, condition("Snapshots", "False", SeverityInfo, "SnapshotAPIUnavailable", "VolumeSnapshot APIs are not installed"))
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out, nil
}

func (i *Inventory) Claims(ctx context.Context) ([]ClaimSummary, error) {
	if err := i.requireReady(); err != nil {
		return nil, err
	}
	pvs := map[string]*corev1.PersistentVolume{}
	for _, obj := range informerObjects(i.pv, nil) {
		if pv, ok := obj.(*corev1.PersistentVolume); ok {
			pvs[pv.Name] = pv
		}
	}
	classes := map[string]*storagev1.StorageClass{}
	for _, obj := range informerObjects(i.sc, nil) {
		if class, ok := obj.(*storagev1.StorageClass); ok {
			classes[class.Name] = class
		}
	}
	claimObjects := informerObjects(i.pvc, i.namespacePVC)
	out := make([]ClaimSummary, 0, len(claimObjects))
	for _, obj := range claimObjects {
		pvc, ok := obj.(*corev1.PersistentVolumeClaim)
		if !ok || !i.scope.Allows(pvc.Namespace) {
			continue
		}
		summary := ClaimSummary{
			ID:        "cluster/" + pvc.Namespace + "/pvc/" + pvc.Name,
			Namespace: pvc.Namespace, Name: pvc.Name, UID: string(pvc.UID), Phase: string(pvc.Status.Phase),
			RequestedCapacity: quantityString(pvc.Spec.Resources.Requests[corev1.ResourceStorage]),
			AccessModes:       accessModes(pvc.Spec.AccessModes), CreatedAt: pvc.CreationTimestamp.Time,
		}
		if pvc.Spec.StorageClassName != nil {
			summary.StorageClass = *pvc.Spec.StorageClassName
		}
		if pvc.Spec.VolumeMode != nil {
			summary.VolumeMode = string(*pvc.Spec.VolumeMode)
		}
		if qty, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok {
			summary.Provisioned = qty.String()
		}
		if pv := pvs[pvc.Spec.VolumeName]; pv != nil {
			summary.PVName = pv.Name
			summary.ReclaimPolicy = string(pv.Spec.PersistentVolumeReclaimPolicy)
			if pv.Spec.CSI != nil {
				summary.Driver = pv.Spec.CSI.Driver
				summary.VolumeHandle = pv.Spec.CSI.VolumeHandle
				summary.VolumeAttributes = safeVolumeAttributes(pv.Spec.CSI.VolumeAttributes)
			}
		}
		if summary.Driver == "" {
			if class := classes[summary.StorageClass]; class != nil {
				summary.Driver = class.Provisioner
			}
		}
		summary.ProviderID = i.registry.ResolveDriver(summary.Driver)
		summary.Workloads = i.workloadsForClaim(pvc.Namespace, pvc.Name)
		summary.AttachmentIDs = i.attachmentsForPV(summary.PVName)
		if summary.Phase == string(corev1.ClaimLost) {
			summary.Conditions = append(summary.Conditions, condition("Bound", "False", SeverityError, "ClaimLost", "Kubernetes reports the claim as lost"))
		}
		if summary.ReclaimPolicy == string(corev1.PersistentVolumeReclaimRetain) {
			summary.Conditions = append(summary.Conditions, condition("OrphanRisk", "True", SeverityWarning, "RetainPolicy", "Deleting this claim can leave backend storage for manual cleanup"))
		}
		out = append(out, summary)
	}
	for _, p := range i.registeredEnrichers() {
		_ = p.EnrichClaims(ctx, out)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Namespace == out[b].Namespace {
			return out[a].Name < out[b].Name
		}
		return out[a].Namespace < out[b].Namespace
	})
	return out, nil
}

func (i *Inventory) Volumes(ctx context.Context) ([]PersistentVolumeSummary, error) {
	if err := i.requireReady(); err != nil {
		return nil, err
	}
	volumeObjects := informerObjects(i.pv, nil)
	out := make([]PersistentVolumeSummary, 0, len(volumeObjects))
	for _, obj := range volumeObjects {
		pv, ok := obj.(*corev1.PersistentVolume)
		if !ok {
			continue
		}
		if pv.Spec.ClaimRef != nil && !i.scope.Allows(pv.Spec.ClaimRef.Namespace) {
			continue
		}
		summary := PersistentVolumeSummary{
			Name: pv.Name, UID: string(pv.UID), StorageClass: pv.Spec.StorageClassName,
			Phase: string(pv.Status.Phase), Capacity: quantityString(pv.Spec.Capacity[corev1.ResourceStorage]),
			AccessModes: accessModes(pv.Spec.AccessModes), ReclaimPolicy: string(pv.Spec.PersistentVolumeReclaimPolicy),
			AttachmentIDs: i.attachmentsForPV(pv.Name), CreatedAt: pv.CreationTimestamp.Time,
		}
		if pv.Spec.VolumeMode != nil {
			summary.VolumeMode = string(*pv.Spec.VolumeMode)
		}
		if pv.Spec.CSI != nil {
			summary.Driver = pv.Spec.CSI.Driver
			summary.VolumeHandle = pv.Spec.CSI.VolumeHandle
			summary.VolumeAttributes = safeVolumeAttributes(pv.Spec.CSI.VolumeAttributes)
		}
		if pv.Spec.ClaimRef != nil {
			summary.ClaimNamespace = pv.Spec.ClaimRef.Namespace
			summary.ClaimName = pv.Spec.ClaimRef.Name
		}
		summary.ProviderID = i.registry.ResolveDriver(summary.Driver)
		if pv.Status.Phase == corev1.VolumeReleased && pv.Spec.PersistentVolumeReclaimPolicy == corev1.PersistentVolumeReclaimRetain {
			summary.Conditions = append(summary.Conditions, condition("OrphanRisk", "True", SeverityWarning, "ReleasedRetainedVolume", "Released volume is retained and may require manual backend cleanup"))
		}
		out = append(out, summary)
	}
	for _, p := range i.registeredEnrichers() {
		_ = p.EnrichVolumes(ctx, out)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out, nil
}

func (i *Inventory) Attachments() ([]AttachmentSummary, error) {
	if err := i.requireReady(); err != nil {
		return nil, err
	}
	pvDriver := map[string]string{}
	for _, obj := range informerObjects(i.pv, nil) {
		if pv, ok := obj.(*corev1.PersistentVolume); ok && pv.Spec.CSI != nil {
			pvDriver[pv.Name] = pv.Spec.CSI.Driver
		}
	}
	attachmentObjects := informerObjects(i.attachment, nil)
	out := make([]AttachmentSummary, 0, len(attachmentObjects))
	for _, obj := range attachmentObjects {
		va, ok := obj.(*storagev1.VolumeAttachment)
		if !ok {
			continue
		}
		pvName := ""
		if va.Spec.Source.PersistentVolumeName != nil {
			pvName = *va.Spec.Source.PersistentVolumeName
		}
		driver := va.Spec.Attacher
		if driver == "" {
			driver = pvDriver[pvName]
		}
		summary := AttachmentSummary{
			Name: va.Name, UID: string(va.UID), ProviderID: i.registry.ResolveDriver(driver), Driver: driver,
			PVName: pvName, NodeName: va.Spec.NodeName, Attached: va.Status.Attached,
		}
		if va.Status.AttachError != nil {
			summary.AttachError = va.Status.AttachError.Message
			summary.Conditions = append(summary.Conditions, condition("Attached", "False", SeverityError, "AttachError", va.Status.AttachError.Message))
		}
		if va.Status.DetachError != nil {
			summary.DetachError = va.Status.DetachError.Message
			summary.Conditions = append(summary.Conditions, condition("Detached", "False", SeverityError, "DetachError", va.Status.DetachError.Message))
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out, nil
}

func (i *Inventory) Capacities() ([]CapacitySummary, error) {
	if err := i.requireReady(); err != nil {
		return nil, err
	}
	classDriver := map[string]string{}
	for _, obj := range informerObjects(i.sc, nil) {
		if class, ok := obj.(*storagev1.StorageClass); ok {
			classDriver[class.Name] = class.Provisioner
		}
	}
	capacityObjects := informerObjects(i.capacity, i.namespaceCapacity)
	out := make([]CapacitySummary, 0, len(capacityObjects))
	for _, obj := range capacityObjects {
		capacity, ok := obj.(*storagev1.CSIStorageCapacity)
		if !ok || !i.scope.Allows(capacity.Namespace) {
			continue
		}
		driver := classDriver[capacity.StorageClassName]
		summary := CapacitySummary{
			ProviderID: i.registry.ResolveDriver(driver), Driver: driver, StorageClass: capacity.StorageClassName,
			ObservedAt: time.Now().UTC(),
		}
		if capacity.Capacity != nil {
			summary.Capacity = capacity.Capacity.String()
		}
		if capacity.MaximumVolumeSize != nil {
			summary.MaximumSize = capacity.MaximumVolumeSize.String()
		}
		if capacity.NodeTopology != nil {
			for key, value := range capacity.NodeTopology.MatchLabels {
				summary.Topology = append(summary.Topology, TopologyTerm{Key: key, Values: []string{value}})
			}
			for _, expression := range capacity.NodeTopology.MatchExpressions {
				summary.Topology = append(summary.Topology, TopologyTerm{Key: expression.Key, Values: append([]string(nil), expression.Values...)})
			}
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].StorageClass == out[b].StorageClass {
			return fmt.Sprint(out[a].Topology) < fmt.Sprint(out[b].Topology)
		}
		return out[a].StorageClass < out[b].StorageClass
	})
	return out, nil
}

func (i *Inventory) Events() ([]StorageEvent, error) {
	if err := i.requireReady(); err != nil {
		return nil, err
	}
	out := make([]StorageEvent, 0)
	for _, obj := range informerObjects(i.event, i.namespaceEvent) {
		event, ok := obj.(*corev1.Event)
		if !ok || !i.scope.Allows(event.Namespace) || !isStorageEvent(event) {
			continue
		}
		summary := StorageEvent{
			Namespace: event.Namespace, Name: event.Name, Type: event.Type, Reason: event.Reason,
			Message: event.Message, RegardingKind: event.InvolvedObject.Kind,
			RegardingName: event.InvolvedObject.Name, RegardingUID: string(event.InvolvedObject.UID), Count: event.Count,
			FirstObservedAt: event.FirstTimestamp.Time, LastObservedAt: event.LastTimestamp.Time,
		}
		if summary.LastObservedAt.IsZero() && !event.EventTime.IsZero() {
			summary.LastObservedAt = event.EventTime.Time
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].LastObservedAt.After(out[b].LastObservedAt) })
	return out, nil
}

func (i *Inventory) Snapshots() ([]SnapshotSummary, error) {
	if err := i.requireReady(); err != nil {
		return nil, err
	}
	if !i.SnapshotAvailable() {
		return []SnapshotSummary{}, nil
	}
	i.snapshotMu.RLock()
	defer i.snapshotMu.RUnlock()
	if i.snapshot == nil && len(i.namespaceSnapshot) == 0 {
		return []SnapshotSummary{}, nil
	}
	classes := map[string]struct{ driver, policy string }{}
	for _, obj := range informerObjects(i.snapshotClass, nil) {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		class := &snapshotv1.VolumeSnapshotClass{}
		if runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, class) != nil {
			continue
		}
		classes[class.Name] = struct{ driver, policy string }{class.Driver, string(class.DeletionPolicy)}
	}
	contents := map[string]struct{ handle, volumeHandle, driver string }{}
	for _, obj := range informerObjects(i.snapshotContent, nil) {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		content := &snapshotv1.VolumeSnapshotContent{}
		if runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, content) != nil {
			continue
		}
		handle, volumeHandle := "", ""
		if content.Status != nil && content.Status.SnapshotHandle != nil {
			handle = *content.Status.SnapshotHandle
		}
		if content.Spec.Source.VolumeHandle != nil {
			volumeHandle = *content.Spec.Source.VolumeHandle
		}
		contents[content.Name] = struct{ handle, volumeHandle, driver string }{handle, volumeHandle, content.Spec.Driver}
	}
	snapshotObjects := informerObjects(i.snapshot, i.namespaceSnapshot)
	out := make([]SnapshotSummary, 0, len(snapshotObjects))
	for _, obj := range snapshotObjects {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok || !i.scope.Allows(u.GetNamespace()) {
			continue
		}
		snapshot := &snapshotv1.VolumeSnapshot{}
		if runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, snapshot) != nil {
			continue
		}
		className, sourcePVC, sourceContent, bound, restoreSize := "", "", "", "", ""
		if snapshot.Spec.VolumeSnapshotClassName != nil {
			className = *snapshot.Spec.VolumeSnapshotClassName
		}
		if snapshot.Spec.Source.PersistentVolumeClaimName != nil {
			sourcePVC = *snapshot.Spec.Source.PersistentVolumeClaimName
		}
		if snapshot.Spec.Source.VolumeSnapshotContentName != nil {
			sourceContent = *snapshot.Spec.Source.VolumeSnapshotContentName
		}
		if snapshot.Status != nil && snapshot.Status.BoundVolumeSnapshotContentName != nil {
			bound = *snapshot.Status.BoundVolumeSnapshotContentName
		}
		if snapshot.Status != nil && snapshot.Status.RestoreSize != nil {
			restoreSize = snapshot.Status.RestoreSize.String()
		}
		if bound == "" {
			bound = sourceContent
		}
		class := classes[className]
		content := contents[bound]
		driver := class.driver
		if driver == "" {
			driver = content.driver
		}
		summary := SnapshotSummary{
			ID:        "cluster/" + snapshot.Namespace + "/snapshot/" + snapshot.Name,
			Namespace: snapshot.Namespace, Name: snapshot.Name, UID: string(snapshot.UID),
			ProviderID: i.registry.ResolveDriver(driver), Driver: driver, SnapshotClass: className,
			SourcePVC: sourcePVC, SourceVolume: content.volumeHandle, BoundContent: bound,
			SnapshotHandle: content.handle, RestoreSize: restoreSize, DeletionPolicy: class.policy,
		}
		if snapshot.Status != nil && snapshot.Status.ReadyToUse != nil {
			ready := *snapshot.Status.ReadyToUse
			summary.ReadyToUse = &ready
		}
		if snapshot.Status != nil && snapshot.Status.CreationTime != nil {
			summary.CreationTime = snapshot.Status.CreationTime.Time
		}
		if summary.CreationTime.IsZero() {
			summary.CreationTime = snapshot.CreationTimestamp.Time
		}
		if summary.ReadyToUse != nil && !*summary.ReadyToUse {
			summary.Conditions = append(summary.Conditions, condition("Ready", "False", SeverityInfo, "SnapshotPending", "Snapshot is not ready for use"))
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Namespace == out[b].Namespace {
			return out[a].Name < out[b].Name
		}
		return out[a].Namespace < out[b].Namespace
	})
	return out, nil
}

func (i *Inventory) workloadsForClaim(namespace, name string) []WorkloadReference {
	objects := indexedObjects(i.pod, i.namespacePod, indexPodByPVC, namespace+"/"+name)
	out := make([]WorkloadReference, 0, len(objects))
	for _, obj := range objects {
		pod, ok := obj.(*corev1.Pod)
		if !ok || !i.scope.Allows(pod.Namespace) {
			continue
		}
		ref := WorkloadReference{Namespace: pod.Namespace, Kind: "Pod", Name: pod.Name, PodName: pod.Name, PodPhase: string(pod.Status.Phase), NodeName: pod.Spec.NodeName}
		for _, owner := range pod.OwnerReferences {
			if owner.Controller != nil && *owner.Controller {
				ref.Kind = owner.Kind
				ref.Name = owner.Name
				break
			}
		}
		out = append(out, ref)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].PodName < out[b].PodName })
	return out
}

func (i *Inventory) attachmentsForPV(name string) []string {
	if name == "" {
		return nil
	}
	objects := indexedObjects(i.attachment, nil, indexAttachmentByPV, name)
	out := make([]string, 0, len(objects))
	for _, obj := range objects {
		if va, ok := obj.(*storagev1.VolumeAttachment); ok {
			out = append(out, va.Name)
		}
	}
	sort.Strings(out)
	return out
}

func (i *Inventory) snapshotClassesByDriver() map[string][]string {
	out := map[string][]string{}
	if !i.SnapshotAvailable() {
		return out
	}
	i.snapshotMu.RLock()
	defer i.snapshotMu.RUnlock()
	if i.snapshotClass == nil {
		return out
	}
	for _, obj := range informerObjects(i.snapshotClass, nil) {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		class := &snapshotv1.VolumeSnapshotClass{}
		if runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, class) != nil {
			continue
		}
		out[class.Driver] = append(out[class.Driver], class.Name)
	}
	for driver := range out {
		sort.Strings(out[driver])
	}
	return out
}

func (i *Inventory) registeredEnrichers() []InventoryEnricher {
	i.registry.mu.RLock()
	defer i.registry.mu.RUnlock()
	out := []InventoryEnricher{}
	for _, provider := range i.registry.providers {
		if enricher, ok := provider.(InventoryEnricher); ok {
			out = append(out, enricher)
		}
	}
	return out
}

func (i *Inventory) requireReady() error {
	if i == nil {
		return fmt.Errorf("storage inventory unavailable")
	}
	if !i.Ready() {
		return fmt.Errorf("storage inventory cache is not ready")
	}
	return nil
}

// CoreConditions explains intentional partial inventory and discovery
// failures. Namespace mode never attempts cluster-scoped reads, so an empty
// driver/PV/attachment list is not misrepresented as a healthy empty cluster.
func (i *Inventory) CoreConditions() []Condition {
	if i == nil {
		return nil
	}
	now := time.Now().UTC()
	out := []Condition{}
	if i.scope.Mode == "namespaces" {
		out = append(out, Condition{Type: "ClusterScopedInventory", Status: "False", Severity: SeverityInfo, Reason: "LeastPrivilegeNamespaceScope", Message: "PV names, StorageClasses, CSI driver/node metadata, VolumeAttachments, snapshot classes, and snapshot contents are omitted because Highland is running with namespace-scoped RBAC.", ObservedAt: now})
	}
	i.discoveryMu.RLock()
	discoveryError := i.discoveryError
	i.discoveryMu.RUnlock()
	if discoveryError != "" {
		reason := "APIAbsent"
		severity := SeverityInfo
		if strings.Contains(strings.ToLower(discoveryError), "forbidden") || strings.Contains(strings.ToLower(discoveryError), "unauthorized") {
			reason, severity = "DiscoveryPermissionDenied", SeverityWarning
		}
		out = append(out, Condition{Type: "SnapshotDiscovery", Status: "False", Severity: severity, Reason: reason, Message: "VolumeSnapshot discovery is unavailable; common non-snapshot inventory remains available.", ObservedAt: now})
	}
	return out
}

func appendInformer(primary cache.SharedIndexInformer, additional []cache.SharedIndexInformer) []cache.SharedIndexInformer {
	out := make([]cache.SharedIndexInformer, 0, len(additional)+1)
	if primary != nil {
		out = append(out, primary)
	}
	out = append(out, additional...)
	return out
}

func informerObjects(primary cache.SharedIndexInformer, additional []cache.SharedIndexInformer) []any {
	out := []any{}
	for _, informer := range appendInformer(primary, additional) {
		out = append(out, informer.GetStore().List()...)
	}
	return out
}

func indexedObjects(primary cache.SharedIndexInformer, additional []cache.SharedIndexInformer, index, key string) []any {
	out := []any{}
	for _, informer := range appendInformer(primary, additional) {
		objects, err := informer.GetIndexer().ByIndex(index, key)
		if err == nil {
			out = append(out, objects...)
		}
	}
	return out
}

func snapshotSourceIndex(obj any) ([]string, error) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil, nil
	}
	name, _, _ := unstructured.NestedString(u.Object, "spec", "source", "persistentVolumeClaimName")
	if name == "" {
		return nil, nil
	}
	return []string{u.GetNamespace() + "/" + name}, nil
}

func resourceServed(client discovery.DiscoveryInterface, groupVersion, resourceName string) bool {
	resources, err := client.ServerResourcesForGroupVersion(groupVersion)
	if err != nil || resources == nil {
		return false
	}
	for _, candidate := range resources.APIResources {
		if candidate.Name == resourceName {
			return true
		}
	}
	return false
}

func accessModes(modes []corev1.PersistentVolumeAccessMode) []string {
	out := make([]string, 0, len(modes))
	for _, mode := range modes {
		out = append(out, string(mode))
	}
	return out
}

func quantityString(quantity resource.Quantity) string {
	return quantity.String()
}

func condition(kind, status string, severity Severity, reason, message string) Condition {
	now := time.Now().UTC()
	return Condition{Type: kind, Status: status, Severity: severity, Reason: reason, Message: message, ObservedAt: now, LastTransitionTime: now}
}

func safeParameters(parameters map[string]string) map[string]string {
	if len(parameters) == 0 {
		return nil
	}
	out := make(map[string]string, len(parameters))
	for key, value := range parameters {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "password") || strings.Contains(lower, "token") || strings.Contains(lower, "credential") || strings.Contains(lower, "secret") || strings.Contains(lower, "key") {
			out[key] = "[redacted]"
			continue
		}
		out[key] = value
	}
	return out
}

// safeVolumeAttributes exposes only documented, non-secret correlation fields.
// CSI attributes not on this allowlist stay server-side and cannot leak
// arbitrary driver metadata into graph or inventory responses.
func safeVolumeAttributes(attributes map[string]string) map[string]string {
	allowed := map[string]struct{}{
		"clusterID": {}, "fsName": {}, "subvolumeName": {}, "subvolumePath": {},
		"rootPath": {}, "pool": {}, "poolName": {}, "staticVolume": {},
	}
	out := map[string]string{}
	for key, value := range attributes {
		if _, ok := allowed[key]; ok && len(value) <= 1024 {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isStorageEvent(event *corev1.Event) bool {
	if event == nil {
		return false
	}
	switch strings.ToLower(event.InvolvedObject.Kind) {
	case "persistentvolume", "persistentvolumeclaim", "storageclass", "volumeattachment", "volumesnapshot", "volumesnapshotcontent", "csidriver", "csinode":
		return true
	}
	lower := strings.ToLower(event.Reason + " " + event.Message)
	return strings.Contains(lower, "volume") || strings.Contains(lower, "storage") || strings.Contains(lower, "mount") || strings.Contains(lower, "attach")
}
