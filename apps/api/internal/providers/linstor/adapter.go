// Package linstor observes an independently managed Piraeus/LINSTOR CSI
// deployment. It never installs, upgrades, configures, or removes that system.
package linstor

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/highland-io/highland/apps/api/internal/storage"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

const ProviderID = "linstor"
const Driver = "linstor.csi.linbit.com"
const maxList = 500

type Config struct {
	ID, Namespace string
	Dynamic       dynamic.Interface
	Client        *Client
	Observer      storage.Observer
}
type Adapter struct {
	id, namespace string
	dynamic       dynamic.Interface
	client        *Client
	observer      storage.Observer
}

var crdByKind = map[string]schema.GroupVersionResource{
	"clusters":                 {Group: "piraeus.io", Version: "v1", Resource: "linstorclusters"},
	"satellites":               {Group: "piraeus.io", Version: "v1", Resource: "linstorsatellites"},
	"satellite-configurations": {Group: "piraeus.io", Version: "v1", Resource: "linstorsatelliteconfigurations"},
	"node-connections":         {Group: "piraeus.io", Version: "v1", Resource: "linstornodeconnections"},
}

func New(cfg Config) (*Adapter, error) {
	if cfg.Dynamic == nil {
		return nil, fmt.Errorf("LINSTOR requires a Kubernetes dynamic client")
	}
	if cfg.ID == "" {
		cfg.ID = ProviderID
	}
	if cfg.Namespace == "" {
		cfg.Namespace = "piraeus-datastore"
	}
	return &Adapter{id: cfg.ID, namespace: cfg.Namespace, dynamic: cfg.Dynamic, client: cfg.Client, observer: cfg.Observer}, nil
}
func (a *Adapter) ID() string        { return a.id }
func (a *Adapter) Drivers() []string { return []string{Driver} }
func (a *Adapter) Capabilities(context.Context) []storage.Capability {
	return []storage.Capability{storage.CapabilityClaimsRead, storage.CapabilityVolumesRead, storage.CapabilityAttachmentsRead, storage.CapabilitySnapshotsRead, storage.CapabilityCapacityRead, storage.CapabilityEventsRead, storage.CapabilityProviderHealth}
}

func (a *Adapter) Descriptor(ctx context.Context) (storage.ProviderDescriptor, error) {
	version := ""
	if a.client != nil {
		if v, err := a.client.Version(ctx); err == nil {
			version = firstString(v, "version", "git_hash")
		}
	}
	return storage.ProviderDescriptor{ID: a.id, Kind: "linstor", DisplayName: "Piraeus / LINSTOR", SupportLevel: storage.SupportManaged, Drivers: a.Drivers(), Version: version, Namespace: a.namespace, Capabilities: a.Capabilities(ctx), Health: a.Health(ctx), Metadata: map[string]string{"managementMode": "external", "readOnly": "true", "controllerConfigured": strconv.FormatBool(a.client != nil)}}, nil
}

func (a *Adapter) Health(ctx context.Context) storage.ProviderHealth {
	now := time.Now().UTC()
	result := storage.ProviderHealth{Status: storage.SeverityOK, ObservedAt: now}
	clusters, err := a.listCRD(ctx, "clusters")
	if err != nil {
		result.Status = storage.SeverityWarning
		result.Conditions = append(result.Conditions, cond("PiraeusClusterObserved", "Unknown", storage.SeverityWarning, "KubernetesReadFailed", err.Error(), now))
	} else if len(clusters) == 0 {
		result.Status = storage.SeverityWarning
		result.Conditions = append(result.Conditions, cond("PiraeusClusterObserved", "False", storage.SeverityWarning, "NoClusterObserved", "No LinstorCluster was observed in the configured namespace.", now))
	} else {
		result.Conditions = append(result.Conditions, cond("PiraeusClusterObserved", "True", storage.SeverityOK, "ClusterObserved", fmt.Sprintf("%d LinstorCluster resource(s) observed.", len(clusters)), now))
	}
	components, cerr := a.components(ctx)
	if cerr != nil {
		result.Status = worse(result.Status, storage.SeverityWarning)
		result.Conditions = append(result.Conditions, cond("ComponentsReady", "Unknown", storage.SeverityWarning, "ComponentReadFailed", cerr.Error(), now))
	} else {
		bad := 0
		for _, c := range components {
			if ready, ok := c["ready"].(bool); ok && !ready {
				bad++
			}
		}
		if bad > 0 {
			result.Status = worse(result.Status, storage.SeverityError)
			result.Conditions = append(result.Conditions, cond("ComponentsReady", "False", storage.SeverityError, "UnavailableComponents", fmt.Sprintf("%d Piraeus/LINSTOR component(s) are not ready.", bad), now))
		} else if len(components) > 0 {
			result.Conditions = append(result.Conditions, cond("ComponentsReady", "True", storage.SeverityOK, "ComponentsAvailable", fmt.Sprintf("%d components are ready.", len(components)), now))
		}
	}
	if a.client == nil {
		result.Status = worse(result.Status, storage.SeverityInfo)
		result.Conditions = append(result.Conditions, cond("ControllerAPI", "False", storage.SeverityInfo, "NotConfigured", "LINSTOR REST is not configured; Kubernetes lifecycle health remains available.", now))
	} else if _, err := a.client.Version(ctx); err != nil {
		result.Status = worse(result.Status, storage.SeverityWarning)
		result.Conditions = append(result.Conditions, cond("ControllerAPI", "False", storage.SeverityWarning, "RequestFailed", err.Error(), now))
	} else {
		result.Conditions = append(result.Conditions, cond("ControllerAPI", "True", storage.SeverityOK, "Reachable", "The LINSTOR controller API is reachable.", now))
		a.appendRuntimeHealth(ctx, &result, now)
	}
	if a.observer != nil {
		a.observer.SetStorageProviderUp(a.id, "linstor", result.Status != storage.SeverityError)
	}
	return result
}

func (a *Adapter) appendRuntimeHealth(ctx context.Context, result *storage.ProviderHealth, now time.Time) {
	nodes, err := a.client.List(ctx, "nodes")
	if err != nil {
		result.Status = worse(result.Status, storage.SeverityWarning)
		result.Conditions = append(result.Conditions, cond("NodesOnline", "Unknown", storage.SeverityWarning, "NodeReadFailed", err.Error(), now))
	} else if len(nodes) > 0 {
		offline := 0
		for _, node := range nodes {
			if !strings.EqualFold(firstString(node, "connection_status"), "ONLINE") {
				offline++
			}
		}
		if offline > 0 {
			result.Status = worse(result.Status, storage.SeverityError)
			result.Conditions = append(result.Conditions, cond("NodesOnline", "False", storage.SeverityError, "OfflineNodes", fmt.Sprintf("%d of %d LINSTOR nodes are not online.", offline, len(nodes)), now))
		} else {
			result.Conditions = append(result.Conditions, cond("NodesOnline", "True", storage.SeverityOK, "AllNodesOnline", fmt.Sprintf("All %d LINSTOR nodes are online.", len(nodes)), now))
		}
	}

	replicas, err := a.client.List(ctx, "resources")
	if err != nil {
		result.Status = worse(result.Status, storage.SeverityWarning)
		result.Conditions = append(result.Conditions, cond("ReplicasCurrent", "Unknown", storage.SeverityWarning, "ReplicaReadFailed", err.Error(), now))
	} else if len(replicas) > 0 {
		unhealthy := 0
		for _, replica := range replicas {
			volumes, _ := replica["volumes"].([]any)
			for _, raw := range volumes {
				volume, _ := raw.(map[string]any)
				state, _ := volume["state"].(map[string]any)
				diskState := firstString(state, "disk_state")
				if diskState != "" && !strings.EqualFold(diskState, "UpToDate") && !strings.EqualFold(diskState, "Diskless") {
					unhealthy++
				}
			}
		}
		if unhealthy > 0 {
			result.Status = worse(result.Status, storage.SeverityError)
			result.Conditions = append(result.Conditions, cond("ReplicasCurrent", "False", storage.SeverityError, "UnhealthyReplicaVolumes", fmt.Sprintf("%d LINSTOR replica volumes are not current.", unhealthy), now))
		} else {
			result.Conditions = append(result.Conditions, cond("ReplicasCurrent", "True", storage.SeverityOK, "AllReplicasCurrent", fmt.Sprintf("All observed replica volumes across %d resources are current.", len(replicas)), now))
		}
	}
}

func (a *Adapter) ProviderSummary(ctx context.Context) (any, error) {
	counts := map[string]int{}
	conditions := []storage.Condition{}
	for _, kind := range a.ResourceKinds(ctx) {
		if kind == "components" {
			continue
		}
		values, err := a.resources(ctx, kind)
		if err == nil {
			counts[kind] = len(values)
		} else if !(a.client == nil && !isCRD(kind)) {
			conditions = append(conditions, cond(kind+"Available", "False", storage.SeverityWarning, "PartialProviderData", err.Error(), time.Now().UTC()))
		}
	}
	components, err := a.components(ctx)
	if err != nil {
		conditions = append(conditions, cond("ComponentsAvailable", "False", storage.SeverityWarning, "PartialProviderData", err.Error(), time.Now().UTC()))
	}
	return map[string]any{"providerId": a.id, "providerKind": "linstor", "namespace": a.namespace, "health": a.Health(ctx), "components": components, "resourceCounts": counts, "conditions": conditions, "managementMode": "external", "observedAt": time.Now().UTC()}, nil
}
func (a *Adapter) ResourceKinds(context.Context) []string {
	return []string{"components", "clusters", "satellites", "satellite-configurations", "node-connections", "nodes", "storage-pools", "resource-groups", "resource-definitions", "resources", "snapshots", "remotes", "schedules", "error-reports"}
}
func isCRD(kind string) bool { _, ok := crdByKind[kind]; return ok }
func (a *Adapter) resources(ctx context.Context, kind string) ([]map[string]any, error) {
	if kind == "components" {
		return a.components(ctx)
	}
	if isCRD(kind) {
		return a.listCRD(ctx, kind)
	}
	if a.client == nil {
		return nil, fmt.Errorf("LINSTOR controller endpoint is not configured")
	}
	raw, err := a.client.List(ctx, kind)
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, 0, len(raw))
	for i, item := range raw {
		result = append(result, normalizeAPI(a.id, kind, i, item))
	}
	sortItems(result)
	return result, nil
}
func (a *Adapter) ListProviderResources(ctx context.Context, kind string, page storage.PageRequest) (any, storage.PageMeta, error) {
	values, err := a.resources(ctx, kind)
	if err != nil {
		return nil, storage.PageMeta{}, err
	}
	values = filter(values, page.Search)
	start, end := bounds(len(values), page)
	out := make([]any, 0, end-start)
	for _, v := range values[start:end] {
		out = append(out, v)
	}
	meta := storage.PageMeta{Limit: page.Limit, Total: len(values)}
	if end < len(values) {
		meta.Continue = storage.EncodePageOffset(end)
	}
	return out, meta, nil
}
func (a *Adapter) GetProviderResource(ctx context.Context, kind, id string) (any, error) {
	values, err := a.resources(ctx, kind)
	if err != nil {
		return nil, err
	}
	for _, v := range values {
		if fmt.Sprint(v["id"]) == id || fmt.Sprint(v["name"]) == id {
			return v, nil
		}
	}
	return nil, storage.ErrNotFound
}

func (a *Adapter) EnrichClaims(ctx context.Context, claims []storage.ClaimSummary) error {
	index, err := a.resourceIndex(ctx)
	if err != nil {
		return nil
	}
	for i := range claims {
		if claims[i].Driver != Driver || claims[i].VolumeHandle == "" {
			continue
		}
		name, _ := parseHandle(claims[i].VolumeHandle)
		if item, ok := index[name]; ok && matchesHandle(item, name) {
			claims[i].ProviderRef = &storage.ProviderReference{Kind: "linstor-resource", ID: name}
		}
	}
	return nil
}
func (a *Adapter) EnrichVolumes(ctx context.Context, volumes []storage.PersistentVolumeSummary) error {
	index, err := a.resourceIndex(ctx)
	if err != nil {
		return nil
	}
	for i := range volumes {
		if volumes[i].Driver != Driver || volumes[i].VolumeHandle == "" {
			continue
		}
		name, volumeNumber := parseHandle(volumes[i].VolumeHandle)
		if item, ok := index[name]; ok && matchesHandle(item, name) {
			volumes[i].ProviderRef = &storage.ProviderReference{Kind: "linstor-resource", ID: name}
			volumes[i].Backend = item
			volumes[i].Backend["csiVolumeNumber"] = volumeNumber
		} else {
			volumes[i].Conditions = append(volumes[i].Conditions, cond("BackendCorrelation", "Unknown", storage.SeverityInfo, "NoAuthoritativeBackendMatch", "The CSI volume handle did not exactly match an observed LINSTOR resource definition.", time.Now().UTC()))
		}
	}
	return nil
}
func parseHandle(handle string) (string, int) {
	parts := strings.Split(handle, "/")
	if len(parts) == 2 {
		if n, err := strconv.Atoi(parts[1]); err == nil {
			return parts[0], n
		}
	}
	return handle, 0
}
func (a *Adapter) resourceIndex(ctx context.Context) (map[string]map[string]any, error) {
	values, err := a.resources(ctx, "resource-definitions")
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]any{}
	for _, v := range values {
		name := firstString(v, "name", "resource_name")
		if name != "" {
			out[name] = v
		}
	}
	return out, nil
}
func matchesHandle(item map[string]any, name string) bool {
	return strings.EqualFold(firstString(item, "name", "resource_name"), name)
}

func (a *Adapter) listCRD(ctx context.Context, kind string) ([]map[string]any, error) {
	gvr := crdByKind[kind]
	// Piraeus Operator v2 lifecycle CRDs are cluster-scoped even though the
	// workloads they reconcile run in the configured Piraeus namespace.
	list, err := a.dynamic.Resource(gvr).List(ctx, metav1.ListOptions{Limit: maxList})
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, normalizeCRD(a.id, kind, &list.Items[i]))
	}
	sortItems(out)
	return out, nil
}
func (a *Adapter) components(ctx context.Context) ([]map[string]any, error) {
	out := []map[string]any{}
	for _, source := range []struct {
		kind string
		gvr  schema.GroupVersionResource
	}{{"Deployment", schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}}, {"StatefulSet", schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}}, {"DaemonSet", schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}}} {
		list, err := a.dynamic.Resource(source.gvr).Namespace(a.namespace).List(ctx, metav1.ListOptions{Limit: maxList})
		if err != nil {
			return nil, err
		}
		for i := range list.Items {
			item := &list.Items[i]
			text := strings.ToLower(item.GetName() + " " + labelsText(item.GetLabels()))
			if !strings.Contains(text, "linstor") && !strings.Contains(text, "piraeus") && !strings.Contains(text, "drbd") {
				continue
			}
			desired, _, _ := unstructured.NestedInt64(item.Object, "spec", "replicas")
			ready, _, _ := unstructured.NestedInt64(item.Object, "status", "readyReplicas")
			if source.kind == "DaemonSet" {
				desired, _, _ = unstructured.NestedInt64(item.Object, "status", "desiredNumberScheduled")
				ready, _, _ = unstructured.NestedInt64(item.Object, "status", "numberReady")
			}
			out = append(out, map[string]any{"id": item.GetName(), "name": item.GetName(), "namespace": a.namespace, "kind": source.kind, "desired": desired, "readyReplicas": ready, "ready": desired > 0 && ready >= desired, "providerId": a.id, "providerKind": "linstor", "source": "kubernetes-workload", "observedAt": time.Now().UTC()})
		}
	}
	sortItems(out)
	return out, nil
}

func normalizeCRD(provider, kind string, item *unstructured.Unstructured) map[string]any {
	result := map[string]any{"id": item.GetName(), "name": item.GetName(), "namespace": item.GetNamespace(), "resourceKind": kind, "providerId": provider, "providerKind": "linstor", "source": "piraeus-crd", "observedAt": time.Now().UTC()}
	if v, ok := item.Object["spec"]; ok {
		result["spec"] = bounded(v, 0)
	}
	if v, ok := item.Object["status"]; ok {
		result["status"] = bounded(v, 0)
	}
	for _, path := range [][]string{{"status", "conditions"}, {"status", "state"}, {"status", "phase"}} {
		if v, found, _ := unstructured.NestedFieldNoCopy(item.Object, path...); found {
			result[path[len(path)-1]] = bounded(v, 0)
		}
	}
	return result
}
func normalizeAPI(provider, kind string, index int, item map[string]any) map[string]any {
	safe, _ := bounded(item, 0).(map[string]any)
	if safe == nil {
		safe = map[string]any{}
	}
	name := firstString(safe, "name", "resource_name", "node_name", "stor_pool_name", "resource_group_name", "snapshot_name", "remote_name", "schedule_name", "error_report_id", "id")
	id := ""
	switch kind {
	case "storage-pools":
		name = firstString(safe, "storage_pool_name", "stor_pool_name", "name")
		id = firstString(safe, "uuid")
		if id == "" {
			id = strings.Trim(firstString(safe, "node_name")+"~"+name, "~")
		}
	case "resources":
		name = firstString(safe, "name", "resource_name")
		id = firstString(safe, "uuid")
	case "error-reports":
		name = firstString(safe, "filename", "error_report_id", "id")
		id = name
	case "snapshots":
		name = firstString(safe, "snapshot_name", "name")
		id = firstString(safe, "uuid")
	}
	if name == "" {
		name = fmt.Sprintf("%s-%d", kind, index+1)
	}
	if id == "" {
		id = name
	}
	safe["id"], safe["name"], safe["resourceKind"] = id, name, kind
	safe["providerId"], safe["providerKind"], safe["source"], safe["observedAt"] = provider, "linstor", "linstor-rest", time.Now().UTC()
	return safe
}
func bounded(value any, depth int) any {
	if depth > 5 {
		return "[truncated]"
	}
	switch v := value.(type) {
	case map[string]any:
		r := map[string]any{}
		n := 0
		for k, c := range v {
			l := strings.ToLower(k)
			if strings.Contains(l, "secret") || strings.Contains(l, "password") || strings.Contains(l, "token") || strings.Contains(l, "credential") {
				continue
			}
			if n >= 100 {
				r["truncated"] = true
				break
			}
			r[k] = bounded(c, depth+1)
			n++
		}
		return r
	case []any:
		limit := len(v)
		if limit > 100 {
			limit = 100
		}
		r := make([]any, 0, limit)
		for _, c := range v[:limit] {
			r = append(r, bounded(c, depth+1))
		}
		return r
	case string:
		if len(v) > 2000 {
			return v[:2000] + "…"
		}
		return v
	default:
		return v
	}
}
func filter(values []map[string]any, search string) []map[string]any {
	search = strings.ToLower(strings.TrimSpace(search))
	if search == "" {
		return values
	}
	out := []map[string]any{}
	for _, v := range values {
		raw, _ := json.Marshal(v)
		if strings.Contains(strings.ToLower(string(raw)), search) {
			out = append(out, v)
		}
	}
	return out
}
func bounds(length int, page storage.PageRequest) (int, int) {
	limit := page.Limit
	if limit <= 0 || limit > maxList {
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
func firstString(value map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := strings.TrimSpace(fmt.Sprint(value[k])); s != "" && s != "<nil>" {
			return s
		}
	}
	return ""
}
func sortItems(values []map[string]any) {
	sort.Slice(values, func(i, j int) bool { return fmt.Sprint(values[i]["name"]) < fmt.Sprint(values[j]["name"]) })
}
func labelsText(labels map[string]string) string {
	parts := []string{}
	for k, v := range labels {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, " ")
}
func cond(kind, status string, severity storage.Severity, reason, message string, now time.Time) storage.Condition {
	return storage.Condition{Type: kind, Status: status, Severity: severity, Reason: reason, Message: message, ObservedAt: now}
}
func worse(a, b storage.Severity) storage.Severity {
	order := map[storage.Severity]int{storage.SeverityUnknown: 0, storage.SeverityOK: 1, storage.SeverityInfo: 2, storage.SeverityWarning: 3, storage.SeverityError: 4}
	if order[b] > order[a] {
		return b
	}
	return a
}

var _ storage.Provider = (*Adapter)(nil)
var _ storage.InventoryEnricher = (*Adapter)(nil)
var _ storage.ProviderResourceReader = (*Adapter)(nil)
var _ storage.ProviderSummaryReader = (*Adapter)(nil)
