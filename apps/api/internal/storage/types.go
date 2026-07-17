// Package storage implements Highland's provider-neutral Kubernetes storage
// inventory and provider registry. It deliberately models only portable
// Kubernetes/CSI facts; backend-specific detail belongs to provider adapters.
package storage

import (
	"context"
	"time"
)

// Observer is implemented by the process metrics collector. All labels are
// intentionally bounded to provider IDs, operation names, and resource kinds.
type Observer interface {
	SetStorageProviderUp(provider, kind string, up bool)
	SetStorageInventoryObjects(kind, provider string, count int)
	SetStorageSyncTimestamp(source string, observedAt time.Time)
	IncStorageWatchError(source string)
	ObserveStorageProviderRequest(provider, operation string, duration time.Duration)
	IncStorageProviderError(provider, reason string)
}

type ContextObserver interface {
	ObserveStorageGraphBuild(provider string, duration time.Duration, unresolved int)
	SetStorageDriftRecords(provider, severity string, count int)
	IncStorageImpactFailure(provider, reason string)
	SetStorageForecastSufficient(provider, measure string, sufficient bool)
}

type CapacityHistorySample struct {
	Timestamp time.Time
	Bytes     uint64
}

type CapacityHistoryReader interface {
	CapacityHistory(context.Context, string, time.Time, time.Time, time.Duration) ([]CapacityHistorySample, error)
}

type SupportLevel string

const (
	SupportDetected SupportLevel = "detected"
	SupportVerified SupportLevel = "verified"
	SupportManaged  SupportLevel = "managed"
)

type Severity string

const (
	SeverityOK      Severity = "ok"
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
	SeverityUnknown Severity = "unknown"
)

type Capability string

const (
	CapabilityClaimsRead      Capability = "inventory.claims.read"
	CapabilityVolumesRead     Capability = "inventory.volumes.read"
	CapabilityAttachmentsRead Capability = "inventory.attachments.read"
	CapabilitySnapshotsRead   Capability = "inventory.snapshots.read"
	CapabilityCapacityRead    Capability = "inventory.capacity.read"
	CapabilityEventsRead      Capability = "inventory.events.read"
	CapabilityProviderHealth  Capability = "provider.health.read"
	CapabilityVolumeCreate    Capability = "volume.create"
	CapabilityVolumeExpand    Capability = "volume.expand"
	CapabilityVolumeDelete    Capability = "volume.delete"
	CapabilitySnapshotCreate  Capability = "snapshot.create"
	CapabilitySnapshotDelete  Capability = "snapshot.delete"
	CapabilitySnapshotRestore Capability = "snapshot.restore"
	CapabilityVolumeClone     Capability = "volume.clone"
	CapabilityCephPoolCreate  Capability = "ceph.pool.create"
	CapabilityCephPoolDelete  Capability = "ceph.pool.delete"
	CapabilityCephClassCreate Capability = "ceph.storageclass.create"
	CapabilityCephClassDelete Capability = "ceph.storageclass.delete"
)

type Condition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	Severity           Severity  `json:"severity"`
	Reason             string    `json:"reason,omitempty"`
	Message            string    `json:"message,omitempty"`
	LastTransitionTime time.Time `json:"lastTransitionTime,omitempty"`
	ObservedAt         time.Time `json:"observedAt,omitempty"`
}

type ProviderHealth struct {
	Status     Severity    `json:"status"`
	Conditions []Condition `json:"conditions"`
	ObservedAt time.Time   `json:"observedAt"`
	Stale      bool        `json:"stale,omitempty"`
}

type ProviderDescriptor struct {
	ID           string            `json:"id"`
	Kind         string            `json:"kind"`
	DisplayName  string            `json:"displayName"`
	SupportLevel SupportLevel      `json:"supportLevel"`
	Drivers      []string          `json:"drivers"`
	Version      string            `json:"version,omitempty"`
	Namespace    string            `json:"namespace,omitempty"`
	Capabilities []Capability      `json:"capabilities"`
	Health       ProviderHealth    `json:"health"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type DriverSummary struct {
	Name               string            `json:"name"`
	ProviderID         string            `json:"providerId"`
	SupportLevel       SupportLevel      `json:"supportLevel"`
	AttachRequired     *bool             `json:"attachRequired,omitempty"`
	PodInfoOnMount     *bool             `json:"podInfoOnMount,omitempty"`
	StorageCapacity    bool              `json:"storageCapacity,omitempty"`
	FSGroupPolicy      string            `json:"fsGroupPolicy,omitempty"`
	VolumeLifecycle    []string          `json:"volumeLifecycleModes,omitempty"`
	TokenRequests      []string          `json:"tokenRequestAudiences,omitempty"`
	RequiresRepublish  *bool             `json:"requiresRepublish,omitempty"`
	SELinuxMount       *bool             `json:"seLinuxMount,omitempty"`
	NodeCount          int               `json:"nodeCount"`
	StorageClassCount  int               `json:"storageClassCount"`
	PersistentVolCount int               `json:"persistentVolumeCount"`
	Metadata           map[string]string `json:"metadata,omitempty"`
}

type StorageClassSummary struct {
	Name                 string            `json:"name"`
	UID                  string            `json:"kubernetesUid"`
	ProviderID           string            `json:"providerId"`
	Provisioner          string            `json:"provisioner"`
	ReclaimPolicy        string            `json:"reclaimPolicy"`
	VolumeBindingMode    string            `json:"volumeBindingMode"`
	AllowVolumeExpansion bool              `json:"allowVolumeExpansion"`
	Default              bool              `json:"default"`
	Parameters           map[string]string `json:"parameters,omitempty"`
	MountOptions         []string          `json:"mountOptions,omitempty"`
	AllowedTopologies    []TopologyTerm    `json:"allowedTopologies,omitempty"`
	ClaimCount           int               `json:"claimCount"`
	VolumeCount          int               `json:"volumeCount"`
	SnapshotClasses      []string          `json:"snapshotClasses,omitempty"`
	Conditions           []Condition       `json:"conditions,omitempty"`
	CreatedAt            time.Time         `json:"createdAt,omitempty"`
}

type TopologyTerm struct {
	Key    string   `json:"key"`
	Values []string `json:"values"`
}

type WorkloadReference struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	PodName   string `json:"podName"`
	PodPhase  string `json:"podPhase"`
	NodeName  string `json:"nodeName,omitempty"`
}

type ProviderReference struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type ClaimSummary struct {
	ID                string              `json:"id"`
	Namespace         string              `json:"namespace"`
	Name              string              `json:"name"`
	UID               string              `json:"kubernetesUid"`
	ProviderID        string              `json:"providerId"`
	Driver            string              `json:"driver,omitempty"`
	StorageClass      string              `json:"storageClass,omitempty"`
	PVName            string              `json:"pvName,omitempty"`
	Phase             string              `json:"phase"`
	RequestedCapacity string              `json:"requestedCapacity,omitempty"`
	Provisioned       string              `json:"provisionedCapacity,omitempty"`
	AccessModes       []string            `json:"accessModes,omitempty"`
	VolumeMode        string              `json:"volumeMode,omitempty"`
	VolumeHandle      string              `json:"volumeHandle,omitempty"`
	VolumeAttributes  map[string]string   `json:"volumeAttributes,omitempty"`
	ReclaimPolicy     string              `json:"reclaimPolicy,omitempty"`
	Workloads         []WorkloadReference `json:"workloads,omitempty"`
	AttachmentIDs     []string            `json:"attachmentIds,omitempty"`
	ProviderRef       *ProviderReference  `json:"providerRef,omitempty"`
	Capabilities      []Capability        `json:"capabilities,omitempty"`
	Conditions        []Condition         `json:"conditions,omitempty"`
	CreatedAt         time.Time           `json:"createdAt,omitempty"`
}

type PersistentVolumeSummary struct {
	Name             string             `json:"name"`
	UID              string             `json:"kubernetesUid"`
	ProviderID       string             `json:"providerId"`
	Driver           string             `json:"driver,omitempty"`
	VolumeHandle     string             `json:"volumeHandle,omitempty"`
	VolumeAttributes map[string]string  `json:"volumeAttributes,omitempty"`
	StorageClass     string             `json:"storageClass,omitempty"`
	Phase            string             `json:"phase"`
	Capacity         string             `json:"capacity,omitempty"`
	AccessModes      []string           `json:"accessModes,omitempty"`
	VolumeMode       string             `json:"volumeMode,omitempty"`
	ReclaimPolicy    string             `json:"reclaimPolicy,omitempty"`
	ClaimNamespace   string             `json:"claimNamespace,omitempty"`
	ClaimName        string             `json:"claimName,omitempty"`
	AttachmentIDs    []string           `json:"attachmentIds,omitempty"`
	ProviderRef      *ProviderReference `json:"providerRef,omitempty"`
	BackendAllocated string             `json:"backendAllocatedCapacity,omitempty"`
	Backend          map[string]any     `json:"backend,omitempty"`
	Conditions       []Condition        `json:"conditions,omitempty"`
	CreatedAt        time.Time          `json:"createdAt,omitempty"`
}

type SnapshotSummary struct {
	ID             string      `json:"id"`
	Namespace      string      `json:"namespace"`
	Name           string      `json:"name"`
	UID            string      `json:"kubernetesUid"`
	ProviderID     string      `json:"providerId"`
	Driver         string      `json:"driver,omitempty"`
	SnapshotClass  string      `json:"snapshotClass,omitempty"`
	SourcePVC      string      `json:"sourcePvc,omitempty"`
	SourceVolume   string      `json:"sourceVolume,omitempty"`
	BoundContent   string      `json:"boundContent,omitempty"`
	SnapshotHandle string      `json:"snapshotHandle,omitempty"`
	ReadyToUse     *bool       `json:"readyToUse,omitempty"`
	RestoreSize    string      `json:"restoreSize,omitempty"`
	DeletionPolicy string      `json:"deletionPolicy,omitempty"`
	CreationTime   time.Time   `json:"creationTime,omitempty"`
	Conditions     []Condition `json:"conditions,omitempty"`
}

type AttachmentSummary struct {
	Name        string            `json:"name"`
	UID         string            `json:"kubernetesUid"`
	ProviderID  string            `json:"providerId"`
	Driver      string            `json:"driver"`
	PVName      string            `json:"pvName"`
	NodeName    string            `json:"nodeName"`
	Attached    bool              `json:"attached"`
	AttachError string            `json:"attachError,omitempty"`
	DetachError string            `json:"detachError,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Conditions  []Condition       `json:"conditions,omitempty"`
}

type CapacitySummary struct {
	ProviderID   string         `json:"providerId"`
	Driver       string         `json:"driver"`
	StorageClass string         `json:"storageClass"`
	Capacity     string         `json:"capacity"`
	MaximumSize  string         `json:"maximumVolumeSize,omitempty"`
	Topology     []TopologyTerm `json:"topology,omitempty"`
	ObservedAt   time.Time      `json:"observedAt"`
}

type StorageEvent struct {
	Namespace       string    `json:"namespace,omitempty"`
	Name            string    `json:"name"`
	Type            string    `json:"type,omitempty"`
	Reason          string    `json:"reason,omitempty"`
	Message         string    `json:"message,omitempty"`
	RegardingKind   string    `json:"regardingKind,omitempty"`
	RegardingName   string    `json:"regardingName,omitempty"`
	RegardingUID    string    `json:"regardingUid,omitempty"`
	Count           int32     `json:"count,omitempty"`
	FirstObservedAt time.Time `json:"firstObservedAt,omitempty"`
	LastObservedAt  time.Time `json:"lastObservedAt,omitempty"`
}

type PageMeta struct {
	Limit    int    `json:"limit"`
	Continue string `json:"continue,omitempty"`
	Total    int    `json:"total"`
}

type ResponseMeta struct {
	ObservedAt time.Time `json:"observedAt"`
	Stale      bool      `json:"stale"`
	Partial    bool      `json:"partial"`
	RequestID  string    `json:"requestId,omitempty"`
}

type Page[T any] struct {
	Data       []T          `json:"data"`
	Page       PageMeta     `json:"page"`
	Meta       ResponseMeta `json:"meta"`
	Conditions []Condition  `json:"conditions,omitempty"`
}

type ErrorEnvelope struct {
	Error APIError `json:"error"`
}

type APIError struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
	Retryable bool           `json:"retryable"`
	RequestID string         `json:"requestId,omitempty"`
}
