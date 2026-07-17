// Package rookceph provides a read-only Rook/Ceph storage provider. Kubernetes
// and Rook CRDs remain the desired-state truth; Dashboard and Prometheus add
// bounded runtime observations and never receive mutation requests.
package rookceph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/highland-io/highland/apps/api/internal/storage"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

var rookResources = map[string]string{
	"clusters": "cephclusters", "pools": "cephblockpools", "filesystems": "cephfilesystems", "mirroring": "cephrbdmirrors",
}

var rookOperatorDeploymentGVR = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
var supportedRookVersionPattern = regexp.MustCompile(`^1\.(19|20)\.[0-9]+$`)
var stableCephVersionPattern = regexp.MustCompile(`^([0-9]+)\.([0-9]+)\.([0-9]+)$`)

type Config struct {
	ID                      string
	Namespace               string
	ClusterName             string
	Dynamic                 dynamic.Interface
	Discovery               discovery.DiscoveryInterface
	Dashboard               *DashboardClient
	DashboardPublicURL      string
	Prometheus              *PrometheusClient
	Publisher               storage.ChangePublisher
	Observer                storage.Observer
	Version                 string
	WritesEnabled           bool
	AllowStorageClassDelete bool
	AllowPoolDelete         bool
	WritePolicy             func() (enabled, allowStorageClassDelete, allowPoolDelete bool)
}

type Adapter struct {
	id, namespace, clusterName, version                     string
	dynamic                                                 dynamic.Interface
	discovery                                               discovery.DiscoveryInterface
	dashboard                                               *DashboardClient
	dashboardPublicURL                                      string
	prometheus                                              *PrometheusClient
	publisher                                               storage.ChangePublisher
	observer                                                storage.Observer
	writesEnabled, allowStorageClassDelete, allowPoolDelete bool
	writePolicy                                             func() (bool, bool, bool)

	mu              sync.RWMutex
	retryOnce       sync.Once
	apiVersion      string
	detectedVersion string
	versionObserved time.Time
	served          map[string]bool
	factory         dynamicinformer.DynamicSharedInformerFactory
	informers       map[string]cache.SharedIndexInformer
	started         atomic.Bool
	ready           atomic.Bool
	lastObserved    atomic.Int64
}

func New(cfg Config) (*Adapter, error) {
	if cfg.Dynamic == nil || cfg.Discovery == nil {
		return nil, fmt.Errorf("Rook/Ceph requires Kubernetes dynamic and discovery clients")
	}
	if cfg.ID == "" {
		cfg.ID = "rook-ceph"
	}
	if cfg.Namespace == "" {
		cfg.Namespace = "rook-ceph"
	}
	if cfg.ClusterName == "" {
		cfg.ClusterName = "rook-ceph"
	}
	return &Adapter{id: cfg.ID, namespace: cfg.Namespace, clusterName: cfg.ClusterName, version: cfg.Version, dynamic: cfg.Dynamic, discovery: cfg.Discovery, dashboard: cfg.Dashboard, dashboardPublicURL: cfg.DashboardPublicURL, prometheus: cfg.Prometheus, publisher: cfg.Publisher, observer: cfg.Observer, writesEnabled: cfg.WritesEnabled, allowStorageClassDelete: cfg.AllowStorageClassDelete, allowPoolDelete: cfg.AllowPoolDelete, writePolicy: cfg.WritePolicy, informers: map[string]cache.SharedIndexInformer{}, served: map[string]bool{}}, nil
}

func (a *Adapter) ID() string { return a.id }
func (a *Adapter) Drivers() []string {
	return []string{a.namespace + ".rbd.csi.ceph.com", a.namespace + ".cephfs.csi.ceph.com", "rbd.csi.ceph.com", "cephfs.csi.ceph.com"}
}

func (a *Adapter) Start(ctx context.Context) error {
	if a.started.Swap(true) {
		return nil
	}
	version, served, err := discoverRookResources(a.discovery)
	if err != nil {
		a.started.Store(false)
		a.retryOnce.Do(func() { go a.retryStart(ctx) })
		return err
	}
	a.mu.Lock()
	a.apiVersion = version
	a.served = served
	a.mu.Unlock()
	a.factory = dynamicinformer.NewFilteredDynamicSharedInformerFactory(a.dynamic, 10*time.Minute, a.namespace, nil)
	for kind, resource := range rookResources {
		if !served[kind] {
			continue
		}
		gvr := schema.GroupVersionResource{Group: "ceph.rook.io", Version: version, Resource: resource}
		informer := a.factory.ForResource(gvr).Informer()
		a.informers[kind] = informer
		resourceKind := kind
		_ = informer.SetWatchErrorHandler(func(_ *cache.Reflector, _ error) {
			if a.observer != nil {
				a.observer.IncStorageWatchError(a.id + "/" + resourceKind)
			}
		})
		handler := func(obj any) {
			now := time.Now().UTC()
			a.lastObserved.Store(now.Unix())
			if a.observer != nil {
				a.observer.SetStorageSyncTimestamp(a.id+"/"+resourceKind, now)
			}
			if a.publisher == nil {
				return
			}
			accessor, err := meta.Accessor(obj)
			if err == nil {
				a.publisher.PublishStorageChange(a.id, accessor.GetNamespace(), resourceKind, accessor.GetName())
			}
		}
		_, _ = informer.AddEventHandler(cache.ResourceEventHandlerFuncs{AddFunc: handler, UpdateFunc: func(_, obj any) { handler(obj) }, DeleteFunc: handler})
	}
	a.factory.Start(ctx.Done())
	go func() {
		syncs := make([]cache.InformerSynced, 0, len(a.informers))
		for _, informer := range a.informers {
			syncs = append(syncs, informer.HasSynced)
		}
		if cache.WaitForCacheSync(ctx.Done(), syncs...) {
			a.ready.Store(true)
			a.lastObserved.Store(time.Now().UTC().Unix())
		}
	}()
	return nil
}

func (a *Adapter) retryStart(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.Start(ctx); err == nil {
				return
			}
		}
	}
}

func detectRookVersion(client discovery.DiscoveryInterface) (string, error) {
	version, _, err := discoverRookResources(client)
	return version, err
}

func discoverRookResources(client discovery.DiscoveryInterface) (string, map[string]bool, error) {
	for _, version := range []string{"v1"} {
		resources, err := client.ServerResourcesForGroupVersion("ceph.rook.io/" + version)
		if err != nil {
			continue
		}
		present := map[string]bool{}
		for _, resource := range resources.APIResources {
			present[resource.Name] = true
		}
		if present["cephclusters"] && present["cephblockpools"] && present["cephfilesystems"] {
			served := map[string]bool{}
			for kind, resource := range rookResources {
				served[kind] = present[resource]
			}
			return version, served, nil
		}
	}
	return "", nil, fmt.Errorf("Rook Ceph CRDs are not served")
}

func (a *Adapter) Descriptor(ctx context.Context) (storage.ProviderDescriptor, error) {
	health := a.Health(ctx)
	rookVersion := a.OperatorVersion(ctx)
	cephVersion := a.CephVersion(ctx)
	if rookVersion == "" {
		health.Conditions = append(health.Conditions, storage.Condition{Type: "RookVersionSupported", Status: "Unknown", Severity: storage.SeverityWarning, Reason: "OperatorVersionUnavailable", Message: "Ceph write workflows are withheld until the Rook operator version can be verified.", ObservedAt: time.Now().UTC()})
		if health.Status == storage.SeverityOK {
			health.Status = storage.SeverityWarning
		}
	} else if !supportedRookVersion(rookVersion) {
		health.Conditions = append(health.Conditions, storage.Condition{Type: "RookVersionSupported", Status: "False", Severity: storage.SeverityWarning, Reason: "UntestedRookVersion", Message: "The installed Rook version is outside Highland's current/previous preview matrix; read-only inventory remains available.", ObservedAt: time.Now().UTC()})
		if health.Status == storage.SeverityOK {
			health.Status = storage.SeverityWarning
		}
	}
	if cephVersion == "" {
		health.Conditions = append(health.Conditions, storage.Condition{Type: "CephVersionSupported", Status: "Unknown", Severity: storage.SeverityWarning, Reason: "CephVersionUnavailable", Message: "Ceph write workflows are withheld until the configured Ceph image version can be verified.", ObservedAt: time.Now().UTC()})
		if health.Status == storage.SeverityOK {
			health.Status = storage.SeverityWarning
		}
	} else if !supportedCephVersion(cephVersion) {
		health.Conditions = append(health.Conditions, storage.Condition{Type: "CephVersionSupported", Status: "False", Severity: storage.SeverityWarning, Reason: "UntestedCephVersion", Message: "The configured Ceph version is outside Highland's preview matrix; read-only inventory remains available.", ObservedAt: time.Now().UTC()})
		if health.Status == storage.SeverityOK {
			health.Status = storage.SeverityWarning
		}
	}
	metadata := map[string]string{"clusterName": a.clusterName, "rookApiVersion": a.rookVersion()}
	metadata["rookVersion"] = rookVersion
	metadata["cephVersion"] = cephVersion
	metadata["versionSupported"] = fmt.Sprintf("%t", supportedRookVersion(rookVersion) && supportedCephVersion(cephVersion))
	if a.dashboard == nil {
		metadata["dashboard"] = "not-configured"
		metadata["dashboardAvailability"] = "not-configured"
	} else {
		metadata["dashboard"] = "configured"
		metadata["dashboardGatewayPath"] = "/ceph-dashboard/"
		metadata["dashboardHandoff"] = "highland-gateway-and-compatible-deep-links"
		result, dashboardErr := a.dashboard.Get(ctx, "/api/health/minimal")
		switch {
		case dashboardErr != nil:
			metadata["dashboardAvailability"] = "unavailable"
			health.Conditions = append(health.Conditions, storage.Condition{Type: "DashboardAvailable", Status: "False", Severity: storage.SeverityWarning, Reason: "PrivateReaderUnavailable", Message: "The private Ceph Dashboard reader is unavailable; the public dashboard link was not probed.", ObservedAt: time.Now().UTC()})
			if health.Status == storage.SeverityOK {
				health.Status = storage.SeverityWarning
			}
		case result.Stale:
			metadata["dashboardAvailability"] = "stale"
			health.Stale = true
			health.Conditions = append(health.Conditions, storage.Condition{Type: "DashboardAvailable", Status: "Unknown", Severity: storage.SeverityWarning, Reason: "PrivateReaderStale", Message: "The private Ceph Dashboard reader returned cached data; the public dashboard link was not probed.", ObservedAt: result.ObservedAt})
			if health.Status == storage.SeverityOK {
				health.Status = storage.SeverityWarning
			}
		default:
			metadata["dashboardAvailability"] = "available"
			health.Conditions = append(health.Conditions, storage.Condition{Type: "DashboardAvailable", Status: "True", Severity: storage.SeverityOK, Reason: "PrivateReaderHealthy", Message: "Availability was verified through Highland's private read-only Ceph Dashboard client.", ObservedAt: result.ObservedAt})
		}
	}
	if a.dashboardPublicURL != "" {
		metadata["dashboardPublicUrl"] = a.dashboardPublicURL
		metadata["dashboardHandoff"] = "root-and-compatible-deep-links"
		if strings.HasPrefix(strings.ToLower(a.dashboardPublicURL), "http://") {
			metadata["dashboardPublicUrlSecurity"] = "insecure-lab-http"
		} else {
			metadata["dashboardPublicUrlSecurity"] = "https"
		}
	}
	if a.prometheus == nil {
		metadata["prometheus"] = "not-configured"
	} else {
		metadata["prometheus"] = "configured"
	}
	return storage.ProviderDescriptor{ID: a.id, Kind: "rook-ceph", DisplayName: "Rook / Ceph", SupportLevel: storage.SupportManaged, Drivers: a.Drivers(), Version: rookVersion, Namespace: a.namespace, Capabilities: a.Capabilities(ctx), Health: health, Metadata: metadata}, nil
}

func (a *Adapter) Capabilities(ctx context.Context) []storage.Capability {
	capabilities := []storage.Capability{storage.CapabilityClaimsRead, storage.CapabilityVolumesRead, storage.CapabilityAttachmentsRead, storage.CapabilitySnapshotsRead, storage.CapabilityCapacityRead, storage.CapabilityEventsRead, storage.CapabilityProviderHealth}
	writesEnabled, allowStorageClassDelete, allowPoolDelete := a.writesEnabled, a.allowStorageClassDelete, a.allowPoolDelete
	if a.writePolicy != nil {
		writesEnabled, allowStorageClassDelete, allowPoolDelete = a.writePolicy()
	}
	if writesEnabled && a.WriteSupported(ctx) {
		capabilities = append(capabilities, storage.CapabilityCephClassCreate)
		if allowStorageClassDelete {
			capabilities = append(capabilities, storage.CapabilityCephClassDelete)
		}
		if a.PoolVerificationAvailable() {
			capabilities = append(capabilities, storage.CapabilityCephPoolCreate)
		}
		if allowPoolDelete && a.PoolVerificationAvailable() {
			capabilities = append(capabilities, storage.CapabilityCephPoolDelete)
		}
	}
	return capabilities
}

func (a *Adapter) PoolVerificationAvailable() bool { return a != nil && a.dashboard != nil }

// OperatorVersion uses the trusted, fixed in-cluster Rook operator Deployment
// and never a request-supplied object or URL. A configured version is retained
// for fixture/offline deployments.
func (a *Adapter) OperatorVersion(ctx context.Context) string {
	if a == nil {
		return ""
	}
	if configured := strings.TrimPrefix(strings.TrimSpace(a.version), "v"); configured != "" {
		return configured
	}
	a.mu.RLock()
	if time.Since(a.versionObserved) < 30*time.Second {
		version := a.detectedVersion
		a.mu.RUnlock()
		return version
	}
	a.mu.RUnlock()
	deployment, err := a.dynamic.Resource(rookOperatorDeploymentGVR).Namespace(a.namespace).Get(ctx, "rook-ceph-operator", metav1.GetOptions{})
	if err != nil {
		a.recordOperatorVersion("")
		return ""
	}
	containers, _, _ := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "containers")
	for _, value := range containers {
		container, _ := value.(map[string]any)
		if fmt.Sprint(container["name"]) != "rook-ceph-operator" {
			continue
		}
		image := fmt.Sprint(container["image"])
		image = strings.SplitN(image, "@", 2)[0]
		if index := strings.LastIndex(image, ":"); index >= 0 && index < len(image)-1 {
			version := strings.TrimPrefix(image[index+1:], "v")
			a.recordOperatorVersion(version)
			return version
		}
	}
	a.recordOperatorVersion("")
	return ""
}

func (a *Adapter) recordOperatorVersion(version string) {
	a.mu.Lock()
	a.detectedVersion = version
	a.versionObserved = time.Now()
	a.mu.Unlock()
}

func (a *Adapter) WriteSupported(ctx context.Context) bool {
	return a != nil && supportedRookVersion(a.OperatorVersion(ctx)) && supportedCephVersion(a.CephVersion(ctx))
}

func (a *Adapter) CephVersion(ctx context.Context) string {
	if a == nil {
		return ""
	}
	cluster, err := a.cluster(ctx)
	if err != nil {
		return ""
	}
	image, _, _ := unstructured.NestedString(cluster.Object, "spec", "cephVersion", "image")
	return imageTagVersion(image)
}

func imageTagVersion(image string) string {
	withoutDigest := strings.SplitN(strings.TrimSpace(image), "@", 2)[0]
	if index := strings.LastIndex(withoutDigest, ":"); index >= 0 && index < len(withoutDigest)-1 {
		return strings.TrimPrefix(withoutDigest[index+1:], "v")
	}
	return ""
}

func supportedRookVersion(version string) bool {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	return supportedRookVersionPattern.MatchString(version)
}

func supportedCephVersion(version string) bool {
	matches := stableCephVersionPattern.FindStringSubmatch(strings.TrimPrefix(strings.TrimSpace(version), "v"))
	if len(matches) != 4 {
		return false
	}
	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	patch, _ := strconv.Atoi(matches[3])
	return (major == 19 && minor == 2) || (major == 20 && minor == 2 && patch >= 1)
}

func (a *Adapter) Health(ctx context.Context) storage.ProviderHealth {
	now := time.Now().UTC()
	health := storage.ProviderHealth{Status: storage.SeverityOK, ObservedAt: now}
	cluster, err := a.cluster(ctx)
	if err != nil {
		health.Status = storage.SeverityError
		health.Conditions = []storage.Condition{{Type: "RookClusterReady", Status: "False", Severity: storage.SeverityError, Reason: classifyRookError(err), Message: err.Error(), ObservedAt: now}}
		return health
	}
	state, _, _ := unstructured.NestedString(cluster.Object, "status", "state")
	cephHealth, _, _ := unstructured.NestedString(cluster.Object, "status", "ceph", "health")
	severity := storage.SeverityOK
	if strings.Contains(strings.ToUpper(cephHealth), "ERR") || strings.EqualFold(state, "Error") {
		severity = storage.SeverityError
	}
	if severity == storage.SeverityOK && (strings.Contains(strings.ToUpper(cephHealth), "WARN") || (!strings.EqualFold(state, "Created") && !strings.EqualFold(state, "Ready"))) {
		severity = storage.SeverityWarning
	}
	health.Status = severity
	health.Conditions = append(health.Conditions, storage.Condition{Type: "RookClusterReady", Status: boolString(severity != storage.SeverityError), Severity: severity, Reason: nonempty(state, "UnknownState"), Message: nonempty(cephHealth, "Ceph runtime health is not reported by Rook"), ObservedAt: now})
	if a.dashboard == nil {
		health.Conditions = append(health.Conditions, storage.Condition{Type: "DashboardAvailable", Status: "Unknown", Severity: storage.SeverityInfo, Reason: "NotConfigured", Message: "Runtime OSD, image, and quorum detail is unavailable."})
	}
	if a.dashboard != nil && a.dashboard.Insecure() {
		health.Conditions = append(health.Conditions, storage.Condition{Type: "DashboardTLSVerified", Status: "False", Severity: storage.SeverityWarning, Reason: "InsecureSkipVerify", Message: "Ceph Dashboard TLS verification is explicitly disabled."})
	}
	if a.prometheus == nil {
		health.Conditions = append(health.Conditions, storage.Condition{Type: "PrometheusAvailable", Status: "Unknown", Severity: storage.SeverityInfo, Reason: "NotConfigured", Message: "Ceph time-series metrics are unavailable; inventory remains functional."})
	}
	return health
}

func (a *Adapter) cluster(ctx context.Context) (*unstructured.Unstructured, error) {
	items, err := a.objects(ctx, "clusters")
	if err != nil {
		return nil, err
	}
	if a.clusterName != "" {
		for _, item := range items {
			if item.GetName() == a.clusterName {
				return item, nil
			}
		}
		return nil, fmt.Errorf("configured CephCluster %s/%s was not found", a.namespace, a.clusterName)
	}
	if len(items) != 1 {
		return nil, fmt.Errorf("expected exactly one CephCluster in %s, found %d", a.namespace, len(items))
	}
	return items[0], nil
}

func (a *Adapter) objects(ctx context.Context, kind string) ([]*unstructured.Unstructured, error) {
	if informer := a.informers[kind]; informer != nil && informer.HasSynced() {
		result := []*unstructured.Unstructured{}
		for _, obj := range informer.GetStore().List() {
			if item, ok := obj.(*unstructured.Unstructured); ok {
				result = append(result, item.DeepCopy())
			}
		}
		return result, nil
	}
	version := a.rookVersion()
	if version != "" && !a.resourceServed(kind) {
		return nil, storage.ErrNotFound
	}
	if version == "" {
		var err error
		version, err = detectRookVersion(a.discovery)
		if err != nil {
			return nil, err
		}
	}
	resource := rookResources[kind]
	if resource == "" {
		return nil, storage.ErrNotFound
	}
	list, err := a.dynamic.Resource(schema.GroupVersionResource{Group: "ceph.rook.io", Version: version, Resource: resource}).Namespace(a.namespace).List(ctx, metav1ListOptions)
	if err != nil {
		return nil, fmt.Errorf("read Rook %s: %w", resource, err)
	}
	result := make([]*unstructured.Unstructured, 0, len(list.Items))
	for index := range list.Items {
		result = append(result, list.Items[index].DeepCopy())
	}
	return result, nil
}

func (a *Adapter) ResourceKinds(context.Context) []string {
	result := []string{"clusters", "pools", "filesystems"}
	if a.resourceServed("mirroring") {
		result = append(result, "mirroring")
	}
	if a.dashboard != nil {
		result = append(result, "osds", "rbd-images", "quorum")
	}
	return result
}

func (a *Adapter) ListProviderResources(ctx context.Context, kind string, page storage.PageRequest) (any, storage.PageMeta, error) {
	var data []map[string]any
	var err error
	if kind == "pools" {
		data, err = a.combinedPoolData(ctx)
	} else if _, ok := rookResources[kind]; ok {
		data, err = a.rookData(ctx, kind)
	} else {
		data, err = a.dashboardData(ctx, kind)
	}
	if err != nil {
		return nil, storage.PageMeta{}, err
	}
	if page.Search != "" {
		filtered := data[:0]
		for _, item := range data {
			encoded, _ := json.Marshal(item)
			if strings.Contains(strings.ToLower(string(encoded)), strings.ToLower(page.Search)) {
				filtered = append(filtered, item)
			}
		}
		data = filtered
	}
	start := page.Offset
	if start > len(data) {
		start = len(data)
	}
	end := start + page.Limit
	if end > len(data) {
		end = len(data)
	}
	items := make([]any, 0, end-start)
	for _, item := range data[start:end] {
		items = append(items, item)
	}
	meta := storage.PageMeta{Limit: page.Limit, Total: len(data)}
	if end < len(data) {
		meta.Continue = storage.EncodePageOffset(end)
	}
	return items, meta, nil
}

func (a *Adapter) GetProviderResource(ctx context.Context, kind, id string) (any, error) {
	data, _, err := a.ListProviderResources(ctx, kind, storage.PageRequest{Limit: 500})
	if err != nil {
		return nil, err
	}
	for _, raw := range data.([]any) {
		item := raw.(map[string]any)
		if fmt.Sprint(item["id"]) == id || fmt.Sprint(item["name"]) == id {
			return item, nil
		}
	}
	return nil, storage.ErrNotFound
}

func (a *Adapter) ProviderSummary(ctx context.Context) (any, error) {
	cluster, err := a.cluster(ctx)
	if err != nil {
		return nil, err
	}
	pools, poolErr := a.combinedPoolData(ctx)
	filesystems, fsErr := a.rookData(ctx, "filesystems")
	clusterSummary := normalizeRook(cluster)
	a.addProviderMetadata(ctx, clusterSummary)
	summary := map[string]any{"providerId": a.id, "providerKind": "rook-ceph", "version": a.OperatorVersion(ctx), "cephVersion": a.CephVersion(ctx), "rookApiVersion": a.rookVersion(), "namespace": a.namespace, "cluster": clusterSummary, "health": a.Health(ctx), "pools": pools, "filesystems": filesystems, "observedAt": time.Now().UTC()}
	conditions := []storage.Condition{}
	if poolErr != nil {
		conditions = append(conditions, partialCondition("Pools", poolErr))
	}
	if fsErr != nil {
		conditions = append(conditions, partialCondition("Filesystems", fsErr))
	}
	if a.dashboard != nil {
		if health, getErr := a.dashboard.Get(ctx, "/api/health/minimal"); getErr == nil {
			var value any
			_ = json.Unmarshal(health.Data, &value)
			summary["runtimeHealth"] = value
			summary["runtimeObservedAt"] = health.ObservedAt
			summary["runtimeStale"] = health.Stale
		} else {
			conditions = append(conditions, partialCondition("Dashboard", getErr))
		}
		for _, source := range []struct{ kind, key, label string }{{"osds", "osds", "OSDs"}, {"quorum", "quorum", "Quorum"}} {
			data, getErr := a.dashboardData(ctx, source.kind)
			if getErr != nil {
				conditions = append(conditions, partialCondition(source.label, getErr))
				continue
			}
			summary[source.key] = data
		}
	}
	if a.prometheus != nil {
		if metrics, metricsErr := a.prometheus.Snapshot(ctx); metricsErr == nil {
			summary["metrics"] = metrics
		} else {
			conditions = append(conditions, partialCondition("Prometheus", metricsErr))
		}
	}
	if len(conditions) > 0 {
		summary["conditions"] = conditions
	}
	return summary, nil
}

func (a *Adapter) CapacityHistory(ctx context.Context, measure string, start, end time.Time, step time.Duration) ([]storage.CapacityHistorySample, error) {
	key := ""
	switch measure {
	case "backend-allocated":
		key = "usedBytes"
	case "cluster-raw":
		key = "totalBytes"
	default:
		return nil, fmt.Errorf("capacity measure %q has no reviewed Prometheus history query", measure)
	}
	if a.prometheus == nil {
		return nil, fmt.Errorf("Prometheus history is not configured")
	}
	return a.prometheus.Range(ctx, key, start, end, step)
}

func (a *Adapter) rookData(ctx context.Context, kind string) ([]map[string]any, error) {
	objects, err := a.objects(ctx, kind)
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, 0, len(objects))
	rookVersion, cephVersion, rookAPI := a.providerVersions(ctx)
	if rookAPI == "" && len(objects) > 0 {
		if slash := strings.LastIndex(objects[0].GetAPIVersion(), "/"); slash >= 0 {
			rookAPI = objects[0].GetAPIVersion()[slash+1:]
		}
	}
	for _, object := range objects {
		item := normalizeRook(object)
		addProviderMetadata(item, a.id, rookVersion, cephVersion, rookAPI)
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return fmt.Sprint(result[i]["name"]) < fmt.Sprint(result[j]["name"]) })
	return result, nil
}

func (a *Adapter) dashboardData(ctx context.Context, kind string) ([]map[string]any, error) {
	if a.dashboard == nil {
		return nil, fmt.Errorf("Ceph Dashboard is not configured")
	}
	endpoint := map[string]string{"osds": "/api/osd", "rbd-images": "/api/block/image", "quorum": "/api/health/minimal", "runtime-pools": "/api/pool"}[kind]
	if endpoint == "" {
		return nil, storage.ErrNotFound
	}
	response, err := a.dashboard.Get(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	var raw any
	if err := json.Unmarshal(response.Data, &raw); err != nil {
		return nil, err
	}
	items := normalizeDashboardList(raw)
	if kind == "rbd-images" {
		items = normalizeRBDImageList(raw)
	}
	rookVersion, cephVersion, rookAPI := a.providerVersions(ctx)
	for _, item := range items {
		addDashboardResourceIdentity(kind, item)
		item["source"] = "ceph-dashboard"
		item["observedAt"] = response.ObservedAt
		item["stale"] = response.Stale
		addProviderMetadata(item, a.id, rookVersion, cephVersion, rookAPI)
	}
	return items, nil
}

func (a *Adapter) combinedPoolData(ctx context.Context) ([]map[string]any, error) {
	desired, err := a.rookData(ctx, "pools")
	if err != nil {
		return nil, err
	}
	if a.dashboard == nil {
		for _, pool := range desired {
			pool["runtimeState"] = "not-configured"
		}
		return desired, nil
	}
	runtimePools, runtimeErr := a.dashboardData(ctx, "runtime-pools")
	if runtimeErr != nil {
		for _, pool := range desired {
			pool["runtimeState"] = "unavailable"
		}
		return desired, nil
	}
	merged := mergePoolData(desired, runtimePools)
	rookVersion, cephVersion, rookAPI := a.providerVersions(ctx)
	for _, pool := range merged {
		addProviderMetadata(pool, a.id, rookVersion, cephVersion, rookAPI)
	}
	return merged, nil
}

func (a *Adapter) addProviderMetadata(ctx context.Context, item map[string]any) {
	rookVersion, cephVersion, rookAPI := a.providerVersions(ctx)
	if rookAPI == "" {
		apiVersion := fmt.Sprint(item["apiVersion"])
		if slash := strings.LastIndex(apiVersion, "/"); slash >= 0 {
			rookAPI = apiVersion[slash+1:]
		}
	}
	addProviderMetadata(item, a.id, rookVersion, cephVersion, rookAPI)
}

func (a *Adapter) providerVersions(ctx context.Context) (string, string, string) {
	return a.OperatorVersion(ctx), a.CephVersion(ctx), a.rookVersion()
}

func addProviderMetadata(item map[string]any, providerID, rookVersion, cephVersion, rookAPI string) {
	item["providerId"] = providerID
	item["providerKind"] = "rook-ceph"
	item["providerVersion"] = rookVersion
	item["cephVersion"] = cephVersion
	item["rookApiVersion"] = rookAPI
}

// mergePoolData preserves Rook desired state and nests the matching Ceph
// observation as runtime state, so neither authority overwrites the other.
func mergePoolData(desired, runtimePools []map[string]any) []map[string]any {
	runtimeByName := make(map[string]map[string]any, len(runtimePools))
	for _, runtimePool := range runtimePools {
		name := fmt.Sprint(runtimePool["pool_name"])
		if name == "" || name == "<nil>" {
			name = fmt.Sprint(runtimePool["name"])
		}
		if name != "" && name != "<nil>" {
			runtimeByName[name] = runtimePool
		}
	}
	for _, pool := range desired {
		name := fmt.Sprint(pool["name"])
		runtimePool, ok := runtimeByName[name]
		if !ok {
			pool["runtimeState"] = "not-observed"
			continue
		}
		pool["runtimeState"] = "observed"
		pool["runtime"] = runtimePool
		pool["runtimeObservedAt"] = runtimePool["observedAt"]
		pool["stale"] = runtimePool["stale"]
		pool["source"] = "rook-crd+ceph-dashboard"
		delete(runtimeByName, name)
	}
	for name, runtimePool := range runtimeByName {
		desired = append(desired, map[string]any{"id": "runtime/" + name, "name": name, "source": "ceph-dashboard", "runtimeState": "runtime-only", "runtime": runtimePool, "observedAt": runtimePool["observedAt"]})
	}
	sort.Slice(desired, func(i, j int) bool { return fmt.Sprint(desired[i]["name"]) < fmt.Sprint(desired[j]["name"]) })
	return desired
}

func (a *Adapter) EnrichClaims(ctx context.Context, claims []storage.ClaimSummary) error {
	images := a.imageIndex(ctx)
	for index := range claims {
		if a.ownsRBD(claims[index].Driver) {
			if _, ok := images[claims[index].VolumeHandle]; ok {
				claims[index].ProviderRef = &storage.ProviderReference{Kind: "ceph-rbd-image", ID: claims[index].VolumeHandle}
			}
		} else if a.ownsCephFS(claims[index].Driver) && authoritativeCephFSMetadata(claims[index].VolumeHandle, claims[index].VolumeAttributes) {
			claims[index].ProviderRef = &storage.ProviderReference{Kind: "cephfs-subvolume", ID: claims[index].VolumeHandle}
		}
	}
	return nil
}
func (a *Adapter) EnrichVolumes(ctx context.Context, volumes []storage.PersistentVolumeSummary) error {
	images := a.imageIndex(ctx)
	for index := range volumes {
		if !a.owns(volumes[index].Driver) {
			continue
		}
		if a.ownsRBD(volumes[index].Driver) {
			if image, ok := images[volumes[index].VolumeHandle]; ok {
				volumes[index].ProviderRef = &storage.ProviderReference{Kind: "ceph-rbd-image", ID: volumes[index].VolumeHandle}
				volumes[index].Backend = image
			} else {
				volumes[index].Conditions = append(volumes[index].Conditions, storage.Condition{Type: "BackendCorrelation", Status: "Unknown", Severity: storage.SeverityInfo, Reason: "NoAuthoritativeBackendMatch", Message: "The CSI volume handle did not exactly match a documented Ceph backend identifier."})
			}
		} else if a.ownsCephFS(volumes[index].Driver) && authoritativeCephFSMetadata(volumes[index].VolumeHandle, volumes[index].VolumeAttributes) {
			volumes[index].ProviderRef = &storage.ProviderReference{Kind: "cephfs-subvolume", ID: volumes[index].VolumeHandle}
			volumes[index].Backend = map[string]any{"filesystem": volumes[index].VolumeAttributes["fsName"], "subvolumeName": volumes[index].VolumeAttributes["subvolumeName"], "subvolumePath": volumes[index].VolumeAttributes["subvolumePath"], "source": "csi-volume-attributes"}
		} else {
			volumes[index].Conditions = append(volumes[index].Conditions, storage.Condition{Type: "BackendCorrelation", Status: "Unknown", Severity: storage.SeverityInfo, Reason: "NoAuthoritativeBackendMatch", Message: "The CSI volume metadata did not contain the documented exact CephFS filesystem and subvolume identifiers."})
		}
	}
	return nil
}

func (a *Adapter) imageIndex(ctx context.Context) map[string]map[string]any {
	result := map[string]map[string]any{}
	data, err := a.dashboardData(ctx, "rbd-images")
	if err != nil {
		return result
	}
	for _, image := range data {
		for _, key := range []string{"id", "image_id", "unique_id"} {
			if value := fmt.Sprint(image[key]); value != "" && value != "<nil>" {
				result[value] = image
			}
		}
	}
	return result
}
func (a *Adapter) owns(driver string) bool {
	for _, owned := range a.Drivers() {
		if driver == owned {
			return true
		}
	}
	return false
}
func (a *Adapter) ownsRBD(driver string) bool {
	return a.owns(driver) && strings.Contains(driver, "rbd")
}
func (a *Adapter) ownsCephFS(driver string) bool {
	return a.owns(driver) && strings.Contains(driver, "cephfs")
}
func authoritativeCephFSMetadata(volumeHandle string, attributes map[string]string) bool {
	return volumeHandle != "" && attributes["fsName"] != "" && (attributes["subvolumeName"] != "" || attributes["subvolumePath"] != "")
}

// VerifyPoolEmpty is intentionally fail-closed. It requires fresh Dashboard
// runtime data and checks RBD images plus Rook filesystem/mirroring references;
// any unavailable source blocks deletion.
func (a *Adapter) VerifyPoolEmpty(ctx context.Context, namespace, poolName string) (bool, string, error) {
	if namespace != a.namespace || poolName == "" {
		return false, "pool is outside the configured Rook provider", nil
	}
	if a.dashboard == nil {
		return false, "Ceph Dashboard is required to prove backend pool emptiness", nil
	}
	health, err := a.dashboard.Get(ctx, "/api/health/minimal")
	if err != nil || health.Stale {
		return false, "fresh Ceph health is unavailable", err
	}
	healthPayload := strings.ToUpper(string(health.Data))
	if strings.Contains(healthPayload, "HEALTH_ERR") || strings.Contains(healthPayload, "HEALTH_WARN") {
		return false, "Ceph pool deletion requires fresh HEALTH_OK", nil
	}
	images, err := a.dashboardData(ctx, "rbd-images")
	if err != nil {
		return false, "RBD image inventory is unavailable", err
	}
	for _, image := range images {
		for _, key := range []string{"pool_name", "pool", "poolName"} {
			if fmt.Sprint(image[key]) == poolName {
				return false, "pool still contains or references RBD images", nil
			}
		}
	}
	filesystems, err := a.objects(ctx, "filesystems")
	if err != nil {
		return false, "CephFilesystem dependencies could not be checked", err
	}
	for _, filesystem := range filesystems {
		encoded, _ := json.Marshal(filesystem.Object["spec"])
		if strings.Contains(string(encoded), `"`+poolName+`"`) {
			return false, "pool is referenced by a CephFilesystem", nil
		}
	}
	mirrors, err := a.objects(ctx, "mirroring")
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return false, "CephRBDMirror dependencies could not be checked", err
	}
	for _, mirror := range mirrors {
		encoded, _ := json.Marshal(mirror.Object["spec"])
		if strings.Contains(string(encoded), `"`+poolName+`"`) {
			return false, "pool is referenced by mirroring configuration", nil
		}
	}
	return true, "fresh Rook and Ceph data prove no discovered dependencies", nil
}

// VerifyPoolPresent is the read-only postflight for pool creation. Rook Ready
// is necessary but not sufficient: a fresh Ceph runtime list must also contain
// the exact pool name before Highland reports success.
func (a *Adapter) VerifyPoolPresent(ctx context.Context, namespace, poolName string) (bool, string, error) {
	if namespace != a.namespace || poolName == "" {
		return false, "pool is outside the configured Rook provider", nil
	}
	if a.dashboard == nil {
		return false, "Ceph Dashboard is required for pool postflight verification", fmt.Errorf("Ceph Dashboard is not configured")
	}
	response, err := a.dashboard.Get(ctx, "/api/pool")
	if err != nil {
		return false, "Ceph runtime pool inventory is unavailable", err
	}
	if response.Stale {
		return false, "Ceph runtime pool inventory is stale", fmt.Errorf("Ceph Dashboard pool data is temporarily unavailable")
	}
	var raw any
	if err := json.Unmarshal(response.Data, &raw); err != nil {
		return false, "Ceph runtime pool inventory is malformed", err
	}
	for _, pool := range normalizeDashboardList(raw) {
		for _, key := range []string{"pool_name", "poolName", "name"} {
			if fmt.Sprint(pool[key]) == poolName {
				return true, "fresh Ceph runtime inventory contains the pool", nil
			}
		}
	}
	return false, "Ceph runtime inventory does not contain the pool", nil
}
func (a *Adapter) rookVersion() string { a.mu.RLock(); defer a.mu.RUnlock(); return a.apiVersion }
func (a *Adapter) resourceServed(kind string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.served[kind]
}

func normalizeRook(object *unstructured.Unstructured) map[string]any {
	result := map[string]any{"id": object.GetNamespace() + "/" + object.GetName(), "name": object.GetName(), "namespace": object.GetNamespace(), "kind": object.GetKind(), "apiVersion": object.GetAPIVersion(), "kubernetesUid": string(object.GetUID()), "generation": object.GetGeneration(), "source": "rook-crd", "observedAt": time.Now().UTC()}
	for _, section := range []string{"spec", "status"} {
		if value, ok := object.Object[section]; ok {
			result[section] = boundedObject(value, 0)
		}
	}
	return result
}

func boundedObject(value any, depth int) any {
	if depth > 5 {
		return "[truncated]"
	}
	switch typed := value.(type) {
	case map[string]any:
		result := map[string]any{}
		count := 0
		for key, child := range typed {
			if sensitiveProviderKey(key) {
				continue
			}
			if count >= 100 {
				result["truncated"] = true
				break
			}
			result[key] = boundedObject(child, depth+1)
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
			result = append(result, boundedObject(child, depth+1))
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

func normalizeDashboardList(raw any) []map[string]any {
	var items []any
	switch value := raw.(type) {
	case []any:
		items = value
	case map[string]any:
		if data, ok := value["data"].([]any); ok {
			items = data
		} else {
			items = []any{value}
		}
	}
	result := []map[string]any{}
	for _, item := range items {
		if len(result) >= 500 {
			break
		}
		if object, ok := item.(map[string]any); ok {
			if bounded, ok := boundedObject(object, 0).(map[string]any); ok {
				result = append(result, bounded)
			}
		}
	}
	return result
}

// Ceph Dashboard API v2 groups RBD images by pool as
// [{"pool_name":"pool-a","value":[...images...]}]. Older supported
// releases and fixtures may return a flat image array. Normalize both forms
// to one provider resource per image so list/detail identity and backend
// correlation operate on actual images rather than pool wrapper records.
func normalizeRBDImageList(raw any) []map[string]any {
	var records []any
	switch value := raw.(type) {
	case []any:
		records = value
	case map[string]any:
		if data, ok := value["data"].([]any); ok {
			records = data
		} else {
			records = []any{value}
		}
	}
	result := make([]map[string]any, 0)
	appendRecord := func(value any, poolName any) {
		if len(result) >= 500 {
			return
		}
		object, ok := value.(map[string]any)
		if !ok {
			return
		}
		bounded, ok := boundedObject(object, 0).(map[string]any)
		if !ok {
			return
		}
		if _, exists := bounded["pool_name"]; !exists && poolName != nil {
			bounded["pool_name"] = poolName
		}
		result = append(result, bounded)
	}
	for _, record := range records {
		if len(result) >= 500 {
			break
		}
		group, ok := record.(map[string]any)
		if !ok {
			continue
		}
		if images, grouped := group["value"].([]any); grouped {
			for _, image := range images {
				appendRecord(image, group["pool_name"])
			}
			continue
		}
		appendRecord(group, nil)
	}
	return result
}

func addDashboardResourceIdentity(kind string, item map[string]any) {
	switch kind {
	case "quorum":
		item["id"] = "cluster"
		item["name"] = "Ceph quorum"
		if health, ok := item["health"].(map[string]any); ok {
			item["state"] = health["status"]
		}
	case "osds":
		if _, exists := item["name"]; !exists {
			if id, ok := item["id"]; ok {
				item["name"] = "osd." + fmt.Sprint(id)
			} else if id, ok := item["osd"]; ok {
				item["id"] = id
				item["name"] = "osd." + fmt.Sprint(id)
			}
		}
	case "rbd-images":
		if _, exists := item["id"]; !exists {
			if uniqueID, ok := item["unique_id"]; ok {
				item["id"] = uniqueID
			} else if name, ok := item["name"]; ok {
				item["id"] = name
			}
		}
	}
}

func sensitiveProviderKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
	if normalized == "key" || strings.HasSuffix(normalized, "accesskey") || strings.HasSuffix(normalized, "secretkey") || strings.HasSuffix(normalized, "encryptionkey") {
		return true
	}
	for _, marker := range []string{"secret", "password", "token", "credential", "authorization", "apikey", "privatekey"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}
func classifyRookError(err error) string {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "forbidden") {
		return "PermissionDenied"
	}
	if strings.Contains(message, "not served") {
		return "CRDAbsent"
	}
	if strings.Contains(message, "found") {
		return "ClusterNotFoundOrAmbiguous"
	}
	return "RookUnavailable"
}
func boolString(value bool) string {
	if value {
		return "True"
	}
	return "False"
}
func nonempty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
func partialCondition(source string, err error) storage.Condition {
	return storage.Condition{Type: source + "Available", Status: "False", Severity: storage.SeverityWarning, Reason: "PartialData", Message: err.Error(), ObservedAt: time.Now().UTC()}
}

var metav1ListOptions = metav1.ListOptions{}
var _ storage.Provider = (*Adapter)(nil)
var _ storage.InventoryEnricher = (*Adapter)(nil)
var _ storage.ProviderResourceReader = (*Adapter)(nil)
var _ storage.ProviderSummaryReader = (*Adapter)(nil)
