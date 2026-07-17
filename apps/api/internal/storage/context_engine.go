package storage

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	maxRelationshipDepth      = 6
	maxRelationshipNodes      = 200
	providerReadLimit         = 500
	driftGracePeriod          = 2 * time.Minute
	maxPotentialTopologyEdges = 2_000
	maxImpactResources        = 500
	graphCacheTTL             = 5 * time.Second
)

type graphSnapshot struct {
	providerID string
	nodes      map[string]GraphNode
	edges      map[string]GraphEdge
	conditions []Condition
	incomplete bool
	observedAt time.Time
}

type driftObservation struct {
	first time.Time
	last  time.Time
}

type cachedGraph struct {
	snapshot *graphSnapshot
	builtAt  time.Time
}

// ContextEngine builds a bounded point-in-time graph from already-normalized
// informer and provider snapshots. Provider resources are fetched once per
// resource kind per build, never once per Kubernetes object or relationship.
type ContextEngine struct {
	inventory  InventoryReader
	registry   *Registry
	operations ContextOperationSource
	audits     ContextAuditSource

	driftMu sync.Mutex
	drift   map[string]driftObservation

	graphMu    sync.Mutex
	graphCache map[string]cachedGraph
}

func (e *ContextEngine) SetSources(operations ContextOperationSource, audits ContextAuditSource) {
	if e == nil {
		return
	}
	e.operations = operations
	e.audits = audits
}

func NewContextEngine(inventory InventoryReader, registry *Registry) *ContextEngine {
	return &ContextEngine{inventory: inventory, registry: registry, drift: map[string]driftObservation{}, graphCache: map[string]cachedGraph{}}
}

func canonicalGraphID(kind, provider, namespace, name string) string {
	encode := func(value string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(value))
	}
	return "v1:" + normalizeGraphKind(kind) + ":" + encode(provider) + ":" + encode(namespace) + ":" + encode(name)
}

// CanonicalGraphID returns the stable, URL-safe identity used by graph,
// impact, and drift APIs. Names and namespaces are encoded independently so
// delimiter characters cannot create collisions.
func CanonicalGraphID(kind, provider, namespace, name string) string {
	return canonicalGraphID(kind, provider, namespace, name)
}

func normalizeGraphKind(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	kind = strings.ReplaceAll(kind, "_", "-")
	switch kind {
	case "storageclasses", "class", "classes":
		return "storageclass"
	case "persistentvolumeclaim", "persistentvolumeclaims", "claim", "claims":
		return "pvc"
	case "persistentvolume", "persistentvolumes", "volume", "volumes":
		return "pv"
	case "volumeattachment", "volumeattachments", "attachment", "attachments":
		return "volumeattachment"
	case "volumesnapshot", "volumesnapshots", "snapshot", "snapshots":
		return "volumesnapshot"
	case "drivers", "csi-driver", "csidriver":
		return "csidriver"
	case "pods":
		return "pod"
	case "workloads", "workload-owner":
		return "workload"
	case "nodes":
		return "node"
	case "pools", "cephblockpool":
		return "ceph-block-pool"
	case "filesystems", "cephfilesystem":
		return "ceph-filesystem"
	case "rbd-images", "ceph-rbd-image":
		return "rbd-image"
	case "cephfs-subvolume", "cephfs-subvolumes":
		return "cephfs-subvolume"
	case "osds", "ceph-osd":
		return "osd"
	case "mirroring", "cephrbdmirror":
		return "ceph-rbd-mirror"
	case "clusters", "cephcluster":
		return "ceph-cluster"
	case "storageoperations", "operation":
		return "storage-operation"
	default:
		return kind
	}
}

func freshness(observedAt time.Time, stale bool, now time.Time) Freshness {
	if stale {
		return FreshnessStale
	}
	if observedAt.IsZero() {
		return FreshnessUnknown
	}
	age := now.Sub(observedAt)
	if age < 0 {
		age = 0
	}
	if age <= 2*time.Minute {
		return FreshnessFresh
	}
	if age <= 10*time.Minute {
		return FreshnessAging
	}
	return FreshnessStale
}

func (e *ContextEngine) build(ctx context.Context, providerID string) (*graphSnapshot, error) {
	e.graphMu.Lock()
	defer e.graphMu.Unlock()
	if cached, ok := e.graphCache[providerID]; ok && time.Since(cached.builtAt) <= graphCacheTTL {
		return cached.snapshot, nil
	}
	snapshot, err := e.buildFresh(ctx, providerID)
	if err == nil {
		e.graphCache[providerID] = cachedGraph{snapshot: snapshot, builtAt: time.Now()}
	}
	return snapshot, err
}

func (e *ContextEngine) buildFresh(ctx context.Context, providerID string) (*graphSnapshot, error) {
	if e == nil || e.inventory == nil || !e.inventory.Ready() {
		return nil, fmt.Errorf("storage inventory cache is not ready")
	}
	if strings.TrimSpace(providerID) == "" {
		return nil, fmt.Errorf("provider is required")
	}
	now := time.Now().UTC()
	snapshot := &graphSnapshot{
		providerID: providerID, nodes: map[string]GraphNode{}, edges: map[string]GraphEdge{}, observedAt: now,
	}
	addNode := func(node GraphNode) {
		if node.ID == "" {
			return
		}
		node.APIVersion = GraphAPIVersion
		if node.Freshness == "" {
			node.Freshness = freshness(node.ObservedAt, false, now)
		}
		snapshot.nodes[node.ID] = node
	}
	addEdge := func(edgeType, from, to string, confidence RelationshipConfidence, evidence RelationshipEvidence) {
		if from == "" || to == "" || from == to {
			return
		}
		evidence.Confidence = confidence
		if evidence.Freshness == "" {
			evidence.Freshness = freshness(evidence.ObservedAt, false, now)
		}
		id := edgeType + ":" + from + ":" + to
		snapshot.edges[id] = GraphEdge{
			APIVersion: GraphAPIVersion, ID: id, Type: edgeType, From: from, To: to,
			Confidence: confidence, Evidence: []RelationshipEvidence{evidence},
		}
	}

	provider, configured := e.registry.Provider(providerID)
	if configured {
		desc, err := provider.Descriptor(ctx)
		if err != nil {
			return nil, fmt.Errorf("describe provider: %w", err)
		}
		addNode(GraphNode{
			ID: canonicalGraphID("provider", providerID, "", providerID), Kind: "provider",
			ProviderID: providerID, Name: desc.DisplayName, ObservedAt: desc.Health.ObservedAt,
			Freshness:  freshness(desc.Health.ObservedAt, desc.Health.Stale, now),
			Attributes: map[string]any{"providerKind": desc.Kind, "supportLevel": desc.SupportLevel, "version": desc.Version},
			Conditions: append([]Condition(nil), desc.Health.Conditions...),
		})
	}

	drivers, err := e.inventory.Drivers(ctx)
	if err != nil {
		return nil, fmt.Errorf("read drivers: %w", err)
	}
	classes, err := e.inventory.StorageClasses()
	if err != nil {
		return nil, fmt.Errorf("read storage classes: %w", err)
	}
	claims, err := e.inventory.Claims(ctx)
	if err != nil {
		return nil, fmt.Errorf("read claims: %w", err)
	}
	volumes, err := e.inventory.Volumes(ctx)
	if err != nil {
		return nil, fmt.Errorf("read volumes: %w", err)
	}
	snapshots, snapshotErr := e.inventory.Snapshots()
	if snapshotErr != nil {
		snapshot.incomplete = true
		snapshot.conditions = append(snapshot.conditions, partialContextCondition("VolumeSnapshots", snapshotErr, now))
	}
	attachments, err := e.inventory.Attachments()
	if err != nil {
		return nil, fmt.Errorf("read attachments: %w", err)
	}

	providerNodeID := canonicalGraphID("provider", providerID, "", providerID)
	driverIDs := map[string]string{}
	for _, driver := range drivers {
		if driver.ProviderID != providerID {
			continue
		}
		id := canonicalGraphID("csidriver", providerID, "", driver.Name)
		driverIDs[driver.Name] = id
		addNode(GraphNode{ID: id, Kind: "csidriver", ProviderID: providerID, Name: driver.Name, ObservedAt: e.inventory.LastSync(), Attributes: map[string]any{"nodeCount": driver.NodeCount, "storageClassCount": driver.StorageClassCount, "volumeCount": driver.PersistentVolCount}})
		if configured {
			addEdge("managed-by", id, providerNodeID, ConfidenceAuthoritative, RelationshipEvidence{Source: "provider-registry", Ref: driver.Name, ObservedAt: e.inventory.LastSync()})
		}
	}

	classIDs := map[string]string{}
	for _, class := range classes {
		if class.ProviderID != providerID {
			continue
		}
		id := canonicalGraphID("storageclass", providerID, "", class.Name)
		classIDs[class.Name] = id
		addNode(GraphNode{
			ID: id, Kind: "storageclass", ProviderID: providerID, Name: class.Name, UID: class.UID,
			ObservedAt: e.inventory.LastSync(), Conditions: append([]Condition(nil), class.Conditions...),
			Attributes: map[string]any{"provisioner": class.Provisioner, "reclaimPolicy": class.ReclaimPolicy, "volumeBindingMode": class.VolumeBindingMode, "claimCount": class.ClaimCount, "volumeCount": class.VolumeCount},
		})
		if driverID := driverIDs[class.Provisioner]; driverID != "" {
			addEdge("provisions", id, driverID, ConfidenceAuthoritative, RelationshipEvidence{Source: "kubernetes-storageclass", Ref: class.UID, ObservedAt: e.inventory.LastSync()})
		}
	}

	backendIDs := map[string]string{}
	backendPoolNames := map[string]string{}
	backendFilesystemNames := map[string]string{}
	poolIDs := []string{}
	osdIDs := []string{}
	if configured {
		if reader, ok := provider.(ProviderResourceReader); ok {
			for _, resourceKind := range []string{"clusters", "pools", "filesystems", "mirroring", "osds", "rbd-images", "resource-definitions"} {
				if !contains(reader.ResourceKinds(ctx), resourceKind) {
					continue
				}
				raw, _, readErr := reader.ListProviderResources(ctx, resourceKind, PageRequest{Limit: providerReadLimit})
				if readErr != nil {
					snapshot.incomplete = true
					snapshot.conditions = append(snapshot.conditions, partialContextCondition("Provider "+resourceKind, readErr, now))
					continue
				}
				for _, item := range providerMaps(raw) {
					nativeID := stringField(item, "id")
					if nativeID == "" {
						continue
					}
					name := stringField(item, "name")
					namespace := stringField(item, "namespace")
					kind := providerGraphKind(resourceKind)
					observedAt := timeField(item, "observedAt")
					stale := boolField(item, "stale")
					id := canonicalGraphID(kind, providerID, namespace, nativeID)
					node := GraphNode{
						ID: id, Kind: kind, ProviderID: providerID, Namespace: namespace, Name: name,
						UID: stringField(item, "kubernetesUid"), ProviderRef: &ProviderReference{Kind: kind, ID: nativeID},
						ObservedAt: observedAt, Freshness: freshness(observedAt, stale, now),
						Attributes: providerNodeAttributes(item),
					}
					addNode(node)
					backendIDs[kind+"\x00"+nativeID] = id
					if name != "" {
						backendIDs[kind+"\x00name\x00"+name] = id
					}
					if kind == "ceph-block-pool" {
						backendPoolNames[name] = id
						poolIDs = append(poolIDs, id)
					}
					if kind == "ceph-filesystem" {
						backendFilesystemNames[name] = id
					}
					if kind == "osd" {
						osdIDs = append(osdIDs, id)
					}
				}
			}
		}
	}

	volumeIDs := map[string]string{}
	volumeByHandle := map[string]string{}
	for _, volume := range volumes {
		if volume.ProviderID != providerID {
			continue
		}
		id := canonicalGraphID("pv", providerID, "", volume.Name)
		volumeIDs[volume.Name] = id
		if volume.VolumeHandle != "" {
			volumeByHandle[volume.VolumeHandle] = id
		}
		attributes := map[string]any{
			"capacity": volume.Capacity, "storageClass": volume.StorageClass, "phase": volume.Phase,
			"accessModes": volume.AccessModes, "reclaimPolicy": volume.ReclaimPolicy, "volumeHandle": volume.VolumeHandle,
			"volumeAttributes": volume.VolumeAttributes,
		}
		addNode(GraphNode{ID: id, Kind: "pv", ProviderID: providerID, Name: volume.Name, UID: volume.UID, ProviderRef: volume.ProviderRef, ObservedAt: e.inventory.LastSync(), Attributes: attributes, Conditions: append([]Condition(nil), volume.Conditions...)})
		if classID := classIDs[volume.StorageClass]; classID != "" {
			addEdge("provisions", id, classID, ConfidenceAuthoritative, RelationshipEvidence{Source: "kubernetes-pv", Ref: volume.UID, ObservedAt: e.inventory.LastSync()})
		}
		if volume.ProviderRef != nil && volume.ProviderRef.ID != "" {
			kind := normalizeGraphKind(volume.ProviderRef.Kind)
			backendID := backendIDs[kind+"\x00"+volume.ProviderRef.ID]
			if backendID == "" {
				backendID = canonicalGraphID(kind, providerID, "", volume.ProviderRef.ID)
				backendAttributes := map[string]any{}
				for key, value := range volume.VolumeAttributes {
					backendAttributes[key] = value
				}
				addNode(GraphNode{ID: backendID, Kind: kind, ProviderID: providerID, Name: volume.ProviderRef.ID, ProviderRef: volume.ProviderRef, ObservedAt: e.inventory.LastSync(), Freshness: FreshnessUnknown, Attributes: backendAttributes, Conditions: []Condition{{Type: "ProviderObservation", Status: "Unknown", Severity: SeverityInfo, Reason: "ProviderResourceNotObserved", Message: "An authoritative CSI/provider identifier exists but the provider inventory did not return its detail.", ObservedAt: now}}})
				snapshot.incomplete = true
			}
			addEdge("backed-by", id, backendID, ConfidenceAuthoritative, RelationshipEvidence{Source: "csi-volume-handle", Ref: volume.ProviderRef.ID, ObservedAt: e.inventory.LastSync()})
		} else if strings.Contains(strings.ToLower(volume.Driver), "ceph") {
			node := snapshot.nodes[id]
			node.Conditions = append(node.Conditions, Condition{Type: "BackendCorrelation", Status: "Unknown", Severity: SeverityInfo, Reason: "NoAuthoritativeBackendMatch", Message: "No exact provider identifier was available; Highland did not infer a backend resource from names.", ObservedAt: now})
			snapshot.nodes[id] = node
			snapshot.incomplete = true
		}
	}

	claimIDs := map[string]string{}
	for _, claim := range claims {
		if claim.ProviderID != providerID {
			continue
		}
		id := canonicalGraphID("pvc", providerID, claim.Namespace, claim.Name)
		claimIDs[claim.Namespace+"\x00"+claim.Name] = id
		addNode(GraphNode{
			ID: id, Kind: "pvc", ProviderID: providerID, Namespace: claim.Namespace, Name: claim.Name, UID: claim.UID,
			ProviderRef: claim.ProviderRef, ObservedAt: e.inventory.LastSync(), Conditions: append([]Condition(nil), claim.Conditions...),
			Attributes: map[string]any{"requestedCapacity": claim.RequestedCapacity, "provisionedCapacity": claim.Provisioned, "accessModes": claim.AccessModes, "reclaimPolicy": claim.ReclaimPolicy, "phase": claim.Phase},
		})
		if classID := classIDs[claim.StorageClass]; classID != "" {
			addEdge("uses", id, classID, ConfidenceAuthoritative, RelationshipEvidence{Source: "kubernetes-pvc", Ref: claim.UID, ObservedAt: e.inventory.LastSync()})
		}
		if pvID := volumeIDs[claim.PVName]; pvID != "" {
			addEdge("binds", id, pvID, ConfidenceAuthoritative, RelationshipEvidence{Source: "kubernetes-pvc-volumeName", Ref: claim.UID, ObservedAt: e.inventory.LastSync()})
		}
		for _, workload := range claim.Workloads {
			podID := canonicalGraphID("pod", providerID, workload.Namespace, workload.PodName)
			podConditions := []Condition{}
			if workload.Kind == "" || workload.Kind == "Pod" {
				podConditions = append(podConditions, Condition{Type: "WorkloadOwner", Status: "Unknown", Severity: SeverityInfo, Reason: "StandaloneOrUnresolvedPod", Message: "No controller owner reference was observed for this Pod.", ObservedAt: e.inventory.LastSync()})
			}
			addNode(GraphNode{ID: podID, Kind: "pod", ProviderID: providerID, Namespace: workload.Namespace, Name: workload.PodName, ObservedAt: e.inventory.LastSync(), Attributes: map[string]any{"phase": workload.PodPhase, "nodeName": workload.NodeName}, Conditions: podConditions})
			addEdge("uses", podID, id, ConfidenceAuthoritative, RelationshipEvidence{Source: "kubernetes-pod-volume", Ref: workload.Namespace + "/" + workload.PodName, ObservedAt: e.inventory.LastSync()})
			if workload.NodeName != "" {
				nodeID := canonicalGraphID("node", providerID, "", workload.NodeName)
				addNode(GraphNode{ID: nodeID, Kind: "node", ProviderID: providerID, Name: workload.NodeName, ObservedAt: e.inventory.LastSync()})
				addEdge("attaches", podID, nodeID, ConfidenceDerived, RelationshipEvidence{Source: "kubernetes-pod-spec", Ref: workload.Namespace + "/" + workload.PodName, ObservedAt: e.inventory.LastSync()})
			}
			if workload.Kind != "" && workload.Kind != "Pod" && workload.Name != "" {
				workloadName := workload.Kind + "/" + workload.Name
				workloadID := canonicalGraphID("workload", providerID, workload.Namespace, workloadName)
				addNode(GraphNode{ID: workloadID, Kind: "workload", ProviderID: providerID, Namespace: workload.Namespace, Name: workload.Name, ObservedAt: e.inventory.LastSync(), Freshness: FreshnessUnknown, Attributes: map[string]any{"workloadKind": workload.Kind}, Conditions: []Condition{{Type: "OwnerExistence", Status: "Unknown", Severity: SeverityInfo, Reason: "OwnerReferenceOnly", Message: "The Pod owner reference is authoritative, but this graph snapshot does not independently observe the workload object.", ObservedAt: e.inventory.LastSync()}}})
				addEdge("owned-by", podID, workloadID, ConfidenceDerived, RelationshipEvidence{Source: "kubernetes-owner-reference", Ref: workload.Namespace + "/" + workloadName, ObservedAt: e.inventory.LastSync()})
				addEdge("uses", workloadID, podID, ConfidenceDerived, RelationshipEvidence{Source: "kubernetes-owner-reference", Ref: workload.Namespace + "/" + workloadName, ObservedAt: e.inventory.LastSync(), Message: "The workload controller owns this Pod."})
			}
		}
	}

	for _, attachment := range attachments {
		if attachment.ProviderID != providerID {
			continue
		}
		id := canonicalGraphID("volumeattachment", providerID, "", attachment.Name)
		addNode(GraphNode{ID: id, Kind: "volumeattachment", ProviderID: providerID, Name: attachment.Name, UID: attachment.UID, ObservedAt: e.inventory.LastSync(), Attributes: map[string]any{"attached": attachment.Attached, "nodeName": attachment.NodeName}, Conditions: append([]Condition(nil), attachment.Conditions...)})
		if pvID := volumeIDs[attachment.PVName]; pvID != "" {
			addEdge("attaches", id, pvID, ConfidenceAuthoritative, RelationshipEvidence{Source: "kubernetes-volumeattachment", Ref: attachment.UID, ObservedAt: e.inventory.LastSync()})
		}
		if attachment.NodeName != "" {
			nodeID := canonicalGraphID("node", providerID, "", attachment.NodeName)
			addNode(GraphNode{ID: nodeID, Kind: "node", ProviderID: providerID, Name: attachment.NodeName, ObservedAt: e.inventory.LastSync()})
			addEdge("attaches", id, nodeID, ConfidenceAuthoritative, RelationshipEvidence{Source: "kubernetes-volumeattachment", Ref: attachment.UID, ObservedAt: e.inventory.LastSync()})
		}
	}

	for _, item := range snapshots {
		if item.ProviderID != providerID {
			continue
		}
		id := canonicalGraphID("volumesnapshot", providerID, item.Namespace, item.Name)
		addNode(GraphNode{ID: id, Kind: "volumesnapshot", ProviderID: providerID, Namespace: item.Namespace, Name: item.Name, UID: item.UID, ObservedAt: e.inventory.LastSync(), Attributes: map[string]any{"restoreSize": item.RestoreSize, "readyToUse": item.ReadyToUse, "snapshotHandle": item.SnapshotHandle}, Conditions: append([]Condition(nil), item.Conditions...)})
		if pvcID := claimIDs[item.Namespace+"\x00"+item.SourcePVC]; pvcID != "" {
			addEdge("backed-by", id, pvcID, ConfidenceAuthoritative, RelationshipEvidence{Source: "kubernetes-volumesnapshot-source", Ref: item.UID, ObservedAt: e.inventory.LastSync()})
		} else if pvID := volumeByHandle[item.SourceVolume]; pvID != "" {
			addEdge("backed-by", id, pvID, ConfidenceAuthoritative, RelationshipEvidence{Source: "snapshot-content-volume-handle", Ref: item.SourceVolume, ObservedAt: e.inventory.LastSync()})
		}
	}

	for id, node := range snapshot.nodes {
		switch node.Kind {
		case "rbd-image":
			poolName := stringAttribute(node.Attributes, "pool")
			if poolName == "" {
				poolName = stringAttribute(node.Attributes, "poolName")
			}
			if poolID := backendPoolNames[poolName]; poolID != "" {
				addEdge("belongs-to-pool", id, poolID, ConfidenceDerived, RelationshipEvidence{Source: "ceph-dashboard-rbd-image", Ref: poolName, ObservedAt: node.ObservedAt})
			}
		case "cephfs-subvolume":
			filesystem := stringAttribute(node.Attributes, "fsName")
			if filesystemID := backendFilesystemNames[filesystem]; filesystemID != "" {
				addEdge("belongs-to-filesystem", id, filesystemID, ConfidenceAuthoritative, RelationshipEvidence{Source: "csi-volume-attributes", Ref: filesystem, ObservedAt: node.ObservedAt, Message: "CephFS filesystem and subvolume metadata came from the bound CSI PersistentVolume."})
			}
		}
	}
	if e.operations != nil {
		operations, operationErr := e.operations(ctx, providerReadLimit)
		if operationErr != nil {
			snapshot.incomplete = true
			snapshot.conditions = append(snapshot.conditions, partialContextCondition("StorageOperations", operationErr, now))
		} else {
			for _, operation := range operations {
				if operation.ProviderID == "" || operation.ProviderID != providerID {
					continue
				}
				operationID := canonicalGraphID("storage-operation", providerID, operation.Namespace, operation.ID)
				addNode(GraphNode{
					ID: operationID, Kind: "storage-operation", ProviderID: providerID,
					Namespace: operation.Namespace, Name: operation.ID, ObservedAt: operation.ObservedAt,
					Attributes: map[string]any{
						"actionId": operation.ActionID, "phase": operation.Phase,
						"targetKind": operation.TargetKind, "targetName": operation.TargetName,
					},
				})
				targetKind := normalizeGraphKind(operation.TargetKind)
				targetName := operation.TargetName
				if targetKind == "ceph-block-pool" {
					targetName = operation.Namespace + "/" + operation.TargetName
				}
				targetID := canonicalGraphID(targetKind, providerID, operation.Namespace, targetName)
				if _, exists := snapshot.nodes[targetID]; exists {
					addEdge("affects", operationID, targetID, ConfidenceAuthoritative, RelationshipEvidence{
						Source: "storage-operation", Ref: operation.ID, ObservedAt: operation.ObservedAt,
					})
				}
			}
		}
	}
	potentialEdges := 0
	for _, poolID := range poolIDs {
		for _, osdID := range osdIDs {
			if potentialEdges >= maxPotentialTopologyEdges {
				snapshot.incomplete = true
				snapshot.conditions = append(snapshot.conditions, Condition{Type: "PotentialTopologyComplete", Status: "False", Severity: SeverityInfo, Reason: "QueryCostBound", Message: fmt.Sprintf("Potential pool-to-OSD topology was capped at %d edges; no direct data placement was inferred.", maxPotentialTopologyEdges), ObservedAt: now})
				break
			}
			addEdge("affected-by", poolID, osdID, ConfidencePotential, RelationshipEvidence{Source: "ceph-topology-possibility", ObservedAt: now, Message: "The OSD is part of this Ceph cluster, but Highland has no authoritative PG/object placement evidence for this pool."})
			potentialEdges++
		}
		if potentialEdges >= maxPotentialTopologyEdges {
			break
		}
	}
	return snapshot, nil
}

func providerGraphKind(kind string) string {
	if kind == "resource-definitions" {
		return "linstor-resource"
	}
	return normalizeGraphKind(kind)
}

func providerMaps(raw any) []map[string]any {
	switch items := raw.(type) {
	case []any:
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if value, ok := item.(map[string]any); ok {
				out = append(out, value)
			}
		}
		return out
	case []map[string]any:
		return items
	default:
		return nil
	}
}

func providerNodeAttributes(item map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"kind", "state", "phase", "health", "status", "runtimeState", "pool_name", "pool", "poolName", "fsName", "filesystem", "up", "in", "host", "deviceClass", "generation", "providerVersion", "cephVersion", "rookApiVersion", "resource_group_name", "uuid"} {
		if value, ok := item[key]; ok {
			out[key] = value
		}
	}
	if _, ok := out["pool"]; !ok {
		if value, ok := out["pool_name"]; ok {
			out["pool"] = value
		}
	}
	return out
}

func partialContextCondition(source string, err error, observedAt time.Time) Condition {
	return Condition{Type: "RelationshipSourceAvailable", Status: "False", Severity: SeverityWarning, Reason: "PartialData", Message: source + ": " + err.Error(), ObservedAt: observedAt}
}

func stringField(item map[string]any, key string) string {
	value := fmt.Sprint(item[key])
	if value == "<nil>" {
		return ""
	}
	return value
}

func boolField(item map[string]any, key string) bool {
	value, _ := item[key].(bool)
	return value
}

func timeField(item map[string]any, key string) time.Time {
	switch value := item[key].(type) {
	case time.Time:
		return value
	case string:
		parsed, _ := time.Parse(time.RFC3339Nano, value)
		return parsed
	default:
		return time.Time{}
	}
}

func stringAttribute(item map[string]any, key string) string {
	value := fmt.Sprint(item[key])
	if value == "<nil>" {
		return ""
	}
	return value
}

type graphQuery struct {
	provider  string
	namespace string
	kind      string
	targetID  string
	depth     int
	page      PageRequest
}

func (e *ContextEngine) relationships(ctx context.Context, query graphQuery) (RelationshipGraph, error) {
	snapshot, err := e.build(ctx, query.provider)
	if err != nil {
		return RelationshipGraph{}, err
	}
	kind := normalizeGraphKind(query.kind)
	seeds := map[string]struct{}{}
	for id, node := range snapshot.nodes {
		if query.targetID != "" {
			if id == query.targetID && node.Kind == kind {
				seeds[id] = struct{}{}
			}
			continue
		}
		if node.Kind == kind && (query.namespace == "" || node.Namespace == query.namespace) {
			seeds[id] = struct{}{}
		}
	}
	if query.targetID != "" && len(seeds) == 0 {
		return RelationshipGraph{}, ErrNotFound
	}
	selected := expandGraph(snapshot, seeds, query.depth)
	nodes := make([]GraphNode, 0, len(selected))
	for id := range selected {
		nodes = append(nodes, snapshot.nodes[id])
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	paged, meta := paginate(nodes, query.page)
	visible := map[string]struct{}{}
	for _, node := range paged {
		visible[node.ID] = struct{}{}
	}
	edges := make([]GraphEdge, 0)
	for _, edge := range snapshot.edges {
		if _, ok := visible[edge.From]; !ok {
			continue
		}
		if _, ok := visible[edge.To]; !ok {
			continue
		}
		edges = append(edges, edge)
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	return RelationshipGraph{APIVersion: GraphAPIVersion, ProviderID: query.provider, Nodes: paged, Edges: edges, Page: meta, ObservedAt: snapshot.observedAt, Conditions: snapshot.conditions, Incomplete: snapshot.incomplete}, nil
}

func expandGraph(snapshot *graphSnapshot, seeds map[string]struct{}, depth int) map[string]struct{} {
	selected := map[string]struct{}{}
	frontier := map[string]struct{}{}
	for id := range seeds {
		selected[id] = struct{}{}
		frontier[id] = struct{}{}
	}
	for level := 0; level < depth; level++ {
		next := map[string]struct{}{}
		for _, edge := range snapshot.edges {
			_, from := frontier[edge.From]
			_, to := frontier[edge.To]
			if !from && !to {
				continue
			}
			for _, id := range []string{edge.From, edge.To} {
				if _, exists := selected[id]; !exists {
					selected[id] = struct{}{}
					next[id] = struct{}{}
				}
			}
		}
		frontier = next
		if len(frontier) == 0 || len(selected) >= maxRelationshipNodes {
			break
		}
	}
	return selected
}

func (e *ContextEngine) impact(ctx context.Context, provider, kind, targetID string, depth int) (ImpactResult, error) {
	snapshot, err := e.build(ctx, provider)
	if err != nil {
		return ImpactResult{}, err
	}
	target, ok := snapshot.nodes[targetID]
	if !ok || target.Kind != normalizeGraphKind(kind) {
		return ImpactResult{}, ErrNotFound
	}
	type pathState struct {
		id         string
		confidence RelationshipConfidence
		path       []string
	}
	dependents := []pathState{{id: targetID, confidence: ConfidenceAuthoritative, path: []string{targetID}}}
	seen := map[string]struct{}{targetID: {}}
	results := []ImpactResource{}
	truncated := false
	for level := 0; level < depth && len(dependents) > 0; level++ {
		next := []pathState{}
		for _, state := range dependents {
			for _, edge := range snapshot.edges {
				if edge.To != state.id {
					continue
				}
				if _, exists := seen[edge.From]; exists {
					continue
				}
				seen[edge.From] = struct{}{}
				confidence := weakerConfidence(state.confidence, edge.Confidence)
				path := append(append([]string(nil), state.path...), edge.From)
				next = append(next, pathState{id: edge.From, confidence: confidence, path: path})
				results = append(results, ImpactResource{Node: snapshot.nodes[edge.From], Confidence: confidence, Path: path})
				if len(results) >= maxImpactResources {
					truncated = true
					break
				}
			}
			if truncated {
				break
			}
		}
		if truncated {
			break
		}
		dependents = next
	}
	backing := []ImpactResource{}
	backingTruncated := false
	current := []pathState{{id: targetID, confidence: ConfidenceAuthoritative, path: []string{targetID}}}
	backSeen := map[string]struct{}{targetID: {}}
	for level := 0; level < depth && len(current) > 0; level++ {
		next := []pathState{}
		for _, state := range current {
			for _, edge := range snapshot.edges {
				if edge.From != state.id {
					continue
				}
				if _, exists := backSeen[edge.To]; exists {
					continue
				}
				backSeen[edge.To] = struct{}{}
				confidence := weakerConfidence(state.confidence, edge.Confidence)
				path := append(append([]string(nil), state.path...), edge.To)
				next = append(next, pathState{id: edge.To, confidence: confidence, path: path})
				backing = append(backing, ImpactResource{Node: snapshot.nodes[edge.To], Confidence: confidence, Path: path})
				if len(backing) >= maxImpactResources {
					backingTruncated = true
					break
				}
			}
			if backingTruncated {
				break
			}
		}
		if backingTruncated {
			break
		}
		current = next
	}
	result := ImpactResult{
		APIVersion: GraphAPIVersion, ProviderID: provider, Target: target, BackedBy: backing,
		ObservedAt: snapshot.observedAt, Freshness: freshness(snapshot.observedAt, false, time.Now().UTC()),
		Conditions: append([]Condition(nil), snapshot.conditions...), Incomplete: snapshot.incomplete,
	}
	for _, item := range results {
		switch item.Confidence {
		case ConfidenceAuthoritative, ConfidenceDerived:
			result.Confirmed = append(result.Confirmed, item)
		case ConfidencePotential:
			result.Potential = append(result.Potential, item)
		default:
			result.Unknown = append(result.Unknown, item)
		}
	}
	result.Summary = summarizeImpact(append([]ImpactResource{{Node: target, Confidence: ConfidenceAuthoritative}}, results...))
	sortImpact(result.Confirmed)
	sortImpact(result.Potential)
	sortImpact(result.Unknown)
	sortImpact(result.BackedBy)
	if snapshot.incomplete {
		result.Conditions = append(result.Conditions, Condition{Type: "ImpactComplete", Status: "False", Severity: SeverityWarning, Reason: "RequiredSourceUnavailable", Message: "Impact is read-only and partial. Destructive workflows must fail closed while required sources are unavailable.", ObservedAt: snapshot.observedAt})
	}
	if truncated || backingTruncated {
		result.Incomplete = true
		result.Conditions = append(result.Conditions, Condition{Type: "ImpactComplete", Status: "False", Severity: SeverityWarning, Reason: "QueryCostBound", Message: fmt.Sprintf("Impact traversal was capped at %d related resources.", maxImpactResources), ObservedAt: snapshot.observedAt})
	}
	return result, nil
}

// AnalyzeImpact exposes the same dependency engine used by the read API for
// Phase 4 planners. Destructive callers must reject results with Incomplete
// set or an ImpactComplete=False condition.
func (e *ContextEngine) AnalyzeImpact(ctx context.Context, provider, kind, targetID string, depth int) (ImpactResult, error) {
	if depth <= 0 {
		depth = 5
	}
	if depth > maxRelationshipDepth {
		return ImpactResult{}, fmt.Errorf("depth must not exceed %d", maxRelationshipDepth)
	}
	return e.impact(ctx, provider, kind, targetID, depth)
}

func weakerConfidence(a, b RelationshipConfidence) RelationshipConfidence {
	rank := map[RelationshipConfidence]int{ConfidenceAuthoritative: 0, ConfidenceDerived: 1, ConfidencePotential: 2, ConfidenceUnknown: 3}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func summarizeImpact(items []ImpactResource) ImpactSummary {
	namespaces, workloads, pods, snapshots, operations := map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
	accessModes, policies := map[string]struct{}{}, map[string]struct{}{}
	requested := resource.NewQuantity(0, resource.BinarySI)
	provisioned := resource.NewQuantity(0, resource.BinarySI)
	summary := ImpactSummary{}
	hasPV := false
	for _, item := range items {
		if item.Node.Kind == "pv" {
			hasPV = true
			break
		}
	}
	for _, item := range items {
		node := item.Node
		if node.Namespace != "" {
			namespaces[node.Namespace] = struct{}{}
		}
		switch node.Kind {
		case "workload":
			workloads[node.ID] = struct{}{}
		case "pod":
			pods[node.ID] = struct{}{}
		case "volumesnapshot":
			snapshots[node.ID] = struct{}{}
		case "storage-operation":
			operations[node.ID] = struct{}{}
		case "volumeattachment":
			if attached, _ := node.Attributes["attached"].(bool); attached {
				summary.AttachedCount++
			} else {
				summary.DetachedCount++
			}
		}
		for _, raw := range stringSliceAttribute(node.Attributes["accessModes"]) {
			accessModes[raw] = struct{}{}
		}
		if policy := stringAttribute(node.Attributes, "reclaimPolicy"); policy != "" {
			policies[policy] = struct{}{}
		}
		if node.Kind == "pvc" {
			addQuantity(requested, stringAttribute(node.Attributes, "requestedCapacity"))
			if !hasPV {
				addQuantity(provisioned, stringAttribute(node.Attributes, "provisionedCapacity"))
			}
		}
		if node.Kind == "pv" {
			addQuantity(provisioned, stringAttribute(node.Attributes, "capacity"))
		}
	}
	summary.WorkloadCount, summary.PodCount, summary.NamespaceCount = len(workloads), len(pods), len(namespaces)
	summary.SnapshotCount, summary.OperationCount = len(snapshots), len(operations)
	if !requested.IsZero() {
		summary.RequestedCapacity = requested.String()
	}
	if !provisioned.IsZero() {
		summary.ProvisionedCapacity = provisioned.String()
	}
	summary.AccessModes = sortedKeys(accessModes)
	summary.ReclaimPolicies = sortedKeys(policies)
	return summary
}

func addQuantity(total *resource.Quantity, value string) {
	if value == "" {
		return
	}
	if parsed, err := resource.ParseQuantity(value); err == nil {
		total.Add(parsed)
	}
}

func stringSliceAttribute(value any) []string {
	switch values := value.(type) {
	case []string:
		return values
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			result = append(result, fmt.Sprint(value))
		}
		return result
	default:
		return nil
	}
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func sortImpact(items []ImpactResource) {
	sort.Slice(items, func(i, j int) bool { return items[i].Node.ID < items[j].Node.ID })
}

func (e *ContextEngine) driftReport(ctx context.Context, providerID string) (DriftReport, error) {
	provider, ok := e.registry.Provider(providerID)
	if !ok {
		return DriftReport{}, ErrNotFound
	}
	reader, ok := provider.(ProviderResourceReader)
	if !ok {
		return DriftReport{}, fmt.Errorf("provider does not expose desired/runtime resources")
	}
	now := time.Now().UTC()
	report := DriftReport{APIVersion: GraphAPIVersion, ProviderID: providerID, Data: []DriftRecord{}, ObservedAt: now}
	current := map[string]struct{}{}
	itemsByKind := map[string][]map[string]any{}
	desc, descriptorErr := provider.Descriptor(ctx)
	if descriptorErr != nil {
		report.Incomplete = true
		report.Conditions = append(report.Conditions, partialContextCondition("Provider descriptor", descriptorErr, now))
	}
	if descriptorErr == nil && desc.Kind == "rook-ceph" {
		resourceKinds := reader.ResourceKinds(ctx)
		for _, kind := range []string{"clusters", "pools", "filesystems", "mirroring"} {
			if !contains(resourceKinds, kind) {
				continue
			}
			raw, _, err := reader.ListProviderResources(ctx, kind, PageRequest{Limit: providerReadLimit})
			if err != nil {
				report.Incomplete = true
				report.Conditions = append(report.Conditions, partialContextCondition("Provider "+kind, err, now))
				continue
			}
			itemsByKind[kind] = providerMaps(raw)
		}
		expectedPools := expectedCephFilesystemPools(itemsByKind["filesystems"])
		for _, kind := range []string{"clusters", "pools", "filesystems", "mirroring"} {
			for _, item := range itemsByKind[kind] {
				for _, candidate := range classifyDriftItem(providerID, kind, item, expectedPools, now) {
					current[candidate.ID] = struct{}{}
					candidate.FirstObserved, candidate.LastObserved = e.recordDrift(candidate.ID, now)
					candidate.Duration = candidate.LastObserved.Sub(candidate.FirstObserved).Round(time.Second).String()
					if (candidate.Category == DriftMissingRuntime || candidate.Category == DriftUnexpectedRuntime) && candidate.LastObserved.Sub(candidate.FirstObserved) < driftGracePeriod {
						candidate.Suppressed = true
						candidate.Severity = SeverityInfo
					}
					report.Data = append(report.Data, candidate)
				}
			}
		}
	}
	if descriptorErr == nil {
		for _, condition := range desc.Health.Conditions {
			if condition.Reason != "UnsupportedVersion" {
				continue
			}
			id := "drift:" + providerID + ":version-unsupported"
			first, last := e.recordDrift(id, now)
			report.Data = append(report.Data, DriftRecord{ID: id, ProviderID: providerID, Category: DriftVersionUnsupported, Resource: GraphNode{APIVersion: GraphAPIVersion, ID: canonicalGraphID("provider", providerID, "", providerID), Kind: "provider", ProviderID: providerID, Name: desc.DisplayName}, FirstObserved: first, LastObserved: last, Duration: last.Sub(first).Round(time.Second).String(), Severity: condition.Severity, Actionable: false, ActionSurface: desc.Kind, Message: condition.Message})
			current[id] = struct{}{}
		}
	}
	if e.operations != nil && len(report.Data) > 0 {
		operations, operationErr := e.operations(ctx, providerReadLimit)
		if operationErr != nil {
			report.Incomplete = true
			report.Conditions = append(report.Conditions, partialContextCondition("StorageOperations", operationErr, now))
		} else {
			for recordIndex := range report.Data {
				record := &report.Data[recordIndex]
				for _, operation := range operations {
					if operation.ProviderID != providerID || !contextTargetMatches(record.Resource, operation.TargetKind, operation.Namespace, operation.TargetName, operation.TargetUID) {
						continue
					}
					record.Links = append(record.Links, DriftLink{Kind: "storage-operation", ID: operation.ID, Relation: "observed-during"})
				}
			}
		}
	}
	if e.audits != nil && len(report.Data) > 0 {
		for recordIndex := range report.Data {
			record := &report.Data[recordIndex]
			for _, event := range e.audits(providerReadLimit) {
				if event.ProviderID != providerID || !contextTargetMatches(record.Resource, event.TargetKind, event.Namespace, event.TargetName, event.TargetUID) {
					continue
				}
				record.Links = append(record.Links, DriftLink{Kind: "audit-event", ID: event.ID, Relation: "evidence"})
			}
		}
	}
	e.pruneDrift(current)
	sort.Slice(report.Data, func(i, j int) bool { return report.Data[i].ID < report.Data[j].ID })
	for _, record := range report.Data {
		report.Summary.Total++
		if record.Suppressed {
			report.Summary.Suppressed++
		}
		switch record.Severity {
		case SeverityError:
			report.Summary.Error++
		case SeverityWarning:
			report.Summary.Warning++
		default:
			report.Summary.Info++
		}
	}
	if len(report.Data) == 0 && !report.Incomplete {
		message := "No supported provider drift was observed."
		if descriptorErr == nil && desc.Kind == "rook-ceph" {
			message = "No supported Rook/Ceph desired-versus-runtime drift was observed."
		}
		report.Conditions = append(report.Conditions, Condition{Type: "DesiredRuntimeDrift", Status: "False", Severity: SeverityOK, Reason: "InSync", Message: message, ObservedAt: now})
	}
	return report, nil
}

func contextTargetMatches(node GraphNode, targetKind, namespace, name, uid string) bool {
	if uid != "" && node.UID != "" {
		return uid == node.UID
	}
	if normalizeGraphKind(targetKind) != node.Kind || namespace != node.Namespace {
		return false
	}
	if node.Kind == "ceph-block-pool" && node.ProviderRef != nil {
		return node.ProviderRef.ID == namespace+"/"+name || node.Name == name
	}
	return node.Name == name
}

func classifyDriftItem(providerID, listKind string, item map[string]any, expectedPools map[string]struct{}, now time.Time) []DriftRecord {
	nativeID := stringField(item, "id")
	name := stringField(item, "name")
	namespace := stringField(item, "namespace")
	kind := providerGraphKind(listKind)
	node := GraphNode{APIVersion: GraphAPIVersion, ID: canonicalGraphID(kind, providerID, namespace, nativeID), Kind: kind, ProviderID: providerID, Namespace: namespace, Name: name, UID: stringField(item, "kubernetesUid"), ProviderRef: &ProviderReference{Kind: kind, ID: nativeID}, ObservedAt: timeField(item, "observedAt"), Freshness: freshness(timeField(item, "observedAt"), boolField(item, "stale"), now)}
	desired := &DriftAuthority{Source: "rook-crd", ObservedAt: node.ObservedAt, Freshness: node.Freshness, Fields: mapField(item, "spec")}
	runtime := &DriftAuthority{Source: "ceph-dashboard", ObservedAt: timeField(item, "runtimeObservedAt"), Freshness: freshness(timeField(item, "runtimeObservedAt"), boolField(item, "stale"), now), Fields: mapField(item, "runtime")}
	makeRecord := func(category DriftCategory, severity Severity, actionable bool, surface, message string) DriftRecord {
		return DriftRecord{ID: "drift:" + providerID + ":" + string(category) + ":" + node.ID, ProviderID: providerID, Category: category, Resource: node, Desired: desired, Runtime: runtime, Severity: severity, Actionable: actionable, ActionSurface: surface, Message: message, Links: []DriftLink{{Kind: "relationship-resource", ID: node.ID, Relation: "affected-by"}}}
	}
	out := []DriftRecord{}
	switch stringField(item, "runtimeState") {
	case "not-observed":
		out = append(out, makeRecord(DriftMissingRuntime, SeverityWarning, false, "rook-ceph", "Rook declares this resource but fresh Ceph runtime inventory did not contain it."))
	case "runtime-only":
		if kind == "ceph-block-pool" {
			if strings.HasPrefix(name, ".") {
				break
			}
			if _, expected := expectedPools[name]; expected {
				break
			}
		}
		record := makeRecord(DriftUnexpectedRuntime, SeverityWarning, false, "ceph-dashboard", "Ceph runtime contains this resource but no matching Rook desired-state object was observed.")
		record.Desired = nil
		out = append(out, record)
	case "unavailable":
		out = append(out, makeRecord(DriftRuntimeStale, SeverityWarning, false, "rook-ceph", "Ceph runtime evidence is unavailable; desired state remains separate and impact is incomplete."))
	case "not-configured":
		out = append(out, makeRecord(DriftRuntimeStale, SeverityInfo, false, "highland-configuration", "Ceph runtime comparison is not configured."))
	}
	if boolField(item, "stale") || runtime.Freshness == FreshnessStale {
		out = append(out, makeRecord(DriftRuntimeStale, SeverityWarning, false, "ceph-dashboard", "Ceph runtime evidence is stale."))
	}
	status := mapField(item, "status")
	if status != nil {
		ready, terminal, message := rookReady(status)
		if !ready {
			category := DriftRookNotReady
			severity := SeverityWarning
			if terminal {
				category = DriftReconciliationStalled
				severity = SeverityError
			}
			out = append(out, makeRecord(category, severity, false, "rook-ceph", message))
		}
		generation := int64Field(item, "generation")
		observedGeneration := int64MapField(status, "observedGeneration")
		if generation > 0 && observedGeneration > 0 && observedGeneration < generation {
			out = append(out, makeRecord(DriftSpecStatus, SeverityWarning, false, "rook-ceph", fmt.Sprintf("Rook status observed generation %d but desired generation is %d.", observedGeneration, generation)))
		}
	}
	return out
}

func expectedCephFilesystemPools(filesystems []map[string]any) map[string]struct{} {
	expected := map[string]struct{}{}
	for _, filesystem := range filesystems {
		if stringField(filesystem, "runtimeState") == "runtime-only" {
			continue
		}
		name := stringField(filesystem, "name")
		if name == "" {
			continue
		}
		expected[name+"-metadata"] = struct{}{}
		spec := mapField(filesystem, "spec")
		dataPools, _ := spec["dataPools"].([]any)
		for index, raw := range dataPools {
			pool, _ := raw.(map[string]any)
			poolName := stringAttribute(pool, "name")
			if poolName == "" {
				poolName = fmt.Sprintf("data%d", index)
			}
			expected[name+"-"+poolName] = struct{}{}
		}
	}
	return expected
}

func mapField(item map[string]any, key string) map[string]any {
	value, _ := item[key].(map[string]any)
	return value
}

func int64Field(item map[string]any, key string) int64 {
	switch value := item[key].(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		return int64(value)
	case string:
		parsed, _ := strconv.ParseInt(value, 10, 64)
		return parsed
	default:
		return 0
	}
}

func int64MapField(item map[string]any, key string) int64 {
	return int64Field(item, key)
}

func rookReady(status map[string]any) (ready, terminal bool, message string) {
	state := strings.ToLower(firstNonempty(stringAttribute(status, "phase"), stringAttribute(status, "state")))
	if state == "ready" || state == "connected" || state == "created" {
		return true, false, ""
	}
	if state != "" {
		terminal = strings.Contains(state, "fail") || strings.Contains(state, "error")
		return false, terminal, "Rook reports state " + state + "."
	}
	if conditions, ok := status["conditions"].([]any); ok {
		for _, raw := range conditions {
			condition, ok := raw.(map[string]any)
			if !ok || !strings.EqualFold(stringAttribute(condition, "type"), "Ready") {
				continue
			}
			if strings.EqualFold(stringAttribute(condition, "status"), "True") {
				return true, false, ""
			}
			reason := stringAttribute(condition, "reason")
			terminal = strings.Contains(strings.ToLower(reason), "fail") || strings.Contains(strings.ToLower(reason), "error")
			return false, terminal, firstNonempty(stringAttribute(condition, "message"), "Rook Ready condition is not True.")
		}
	}
	return true, false, ""
}

func (e *ContextEngine) recordDrift(id string, now time.Time) (time.Time, time.Time) {
	e.driftMu.Lock()
	defer e.driftMu.Unlock()
	observation, ok := e.drift[id]
	if !ok {
		observation.first = now
	}
	observation.last = now
	e.drift[id] = observation
	return observation.first, observation.last
}

func (e *ContextEngine) pruneDrift(current map[string]struct{}) {
	e.driftMu.Lock()
	defer e.driftMu.Unlock()
	for id := range e.drift {
		if _, ok := current[id]; !ok {
			delete(e.drift, id)
		}
	}
}
