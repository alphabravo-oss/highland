package storage

import (
	"context"
	"time"
)

// GraphAPIVersion is incremented when node identity or edge semantics change.
const GraphAPIVersion = "storage.highland.io/v1alpha1"

// ImpactAnalyzer is the shared read/preflight dependency contract. Mutation
// planners must fail closed when ImpactResult.Incomplete is true.
type ImpactAnalyzer interface {
	AnalyzeImpact(context.Context, string, string, string, int) (ImpactResult, error)
}

type RelationshipConfidence string

const (
	ConfidenceAuthoritative RelationshipConfidence = "authoritative"
	ConfidenceDerived       RelationshipConfidence = "derived"
	ConfidencePotential     RelationshipConfidence = "potential"
	ConfidenceUnknown       RelationshipConfidence = "unknown"
)

type Freshness string

const (
	FreshnessFresh   Freshness = "fresh"
	FreshnessAging   Freshness = "aging"
	FreshnessStale   Freshness = "stale"
	FreshnessUnknown Freshness = "unknown"
)

// RelationshipEvidence records why Highland believes an edge exists. Ref is
// an exact Kubernetes UID, CSI handle, or provider-native identifier; display
// names are never promoted to authoritative backend evidence.
type RelationshipEvidence struct {
	Source     string                 `json:"source"`
	Ref        string                 `json:"ref,omitempty"`
	ObservedAt time.Time              `json:"observedAt,omitempty"`
	Freshness  Freshness              `json:"freshness"`
	Confidence RelationshipConfidence `json:"confidence"`
	Message    string                 `json:"message,omitempty"`
}

type GraphNode struct {
	APIVersion  string             `json:"apiVersion"`
	ID          string             `json:"id"`
	Kind        string             `json:"kind"`
	ProviderID  string             `json:"providerId"`
	Namespace   string             `json:"namespace,omitempty"`
	Name        string             `json:"name"`
	UID         string             `json:"kubernetesUid,omitempty"`
	ProviderRef *ProviderReference `json:"providerRef,omitempty"`
	ObservedAt  time.Time          `json:"observedAt,omitempty"`
	Freshness   Freshness          `json:"freshness"`
	Attributes  map[string]any     `json:"attributes,omitempty"`
	Conditions  []Condition        `json:"conditions,omitempty"`
}

type GraphEdge struct {
	APIVersion string                 `json:"apiVersion"`
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	From       string                 `json:"from"`
	To         string                 `json:"to"`
	Confidence RelationshipConfidence `json:"confidence"`
	Evidence   []RelationshipEvidence `json:"evidence"`
}

type RelationshipGraph struct {
	APIVersion string      `json:"apiVersion"`
	ProviderID string      `json:"providerId"`
	Nodes      []GraphNode `json:"nodes"`
	Edges      []GraphEdge `json:"edges"`
	Page       PageMeta    `json:"page"`
	ObservedAt time.Time   `json:"observedAt"`
	Conditions []Condition `json:"conditions,omitempty"`
	Incomplete bool        `json:"incomplete,omitempty"`
}

type ImpactResource struct {
	Node       GraphNode              `json:"node"`
	Confidence RelationshipConfidence `json:"confidence"`
	Path       []string               `json:"path,omitempty"`
}

type ImpactSummary struct {
	RequestedCapacity   string   `json:"requestedCapacity,omitempty"`
	ProvisionedCapacity string   `json:"provisionedCapacity,omitempty"`
	WorkloadCount       int      `json:"workloadCount"`
	PodCount            int      `json:"podCount"`
	NamespaceCount      int      `json:"namespaceCount"`
	SnapshotCount       int      `json:"snapshotCount"`
	OperationCount      int      `json:"operationCount"`
	AttachedCount       int      `json:"attachedCount"`
	DetachedCount       int      `json:"detachedCount"`
	AccessModes         []string `json:"accessModes,omitempty"`
	ReclaimPolicies     []string `json:"reclaimPolicies,omitempty"`
}

type ImpactResult struct {
	APIVersion string           `json:"apiVersion"`
	ProviderID string           `json:"providerId"`
	Target     GraphNode        `json:"target"`
	Confirmed  []ImpactResource `json:"confirmed"`
	Potential  []ImpactResource `json:"potential"`
	Unknown    []ImpactResource `json:"unknown"`
	BackedBy   []ImpactResource `json:"backedBy"`
	Summary    ImpactSummary    `json:"summary"`
	ObservedAt time.Time        `json:"observedAt"`
	Freshness  Freshness        `json:"freshness"`
	Conditions []Condition      `json:"conditions,omitempty"`
	Incomplete bool             `json:"incomplete,omitempty"`
}

type DriftCategory string

const (
	DriftMissingRuntime        DriftCategory = "missing-runtime-resource"
	DriftUnexpectedRuntime     DriftCategory = "unexpected-runtime-resource"
	DriftRookNotReady          DriftCategory = "rook-not-ready"
	DriftSpecStatus            DriftCategory = "spec-status-divergence"
	DriftReconciliationStalled DriftCategory = "reconciliation-stalled"
	DriftVersionUnsupported    DriftCategory = "version-unsupported"
	DriftRuntimeStale          DriftCategory = "runtime-stale"
	DriftPostOperation         DriftCategory = "post-operation-verification-mismatch"
)

type DriftAuthority struct {
	Source     string         `json:"source"`
	ObservedAt time.Time      `json:"observedAt,omitempty"`
	Freshness  Freshness      `json:"freshness"`
	Fields     map[string]any `json:"fields,omitempty"`
}

type DriftLink struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Relation string `json:"relation"`
}

type DriftRecord struct {
	ID            string          `json:"id"`
	ProviderID    string          `json:"providerId"`
	Category      DriftCategory   `json:"category"`
	Resource      GraphNode       `json:"resource"`
	Desired       *DriftAuthority `json:"desired,omitempty"`
	Runtime       *DriftAuthority `json:"runtime,omitempty"`
	FirstObserved time.Time       `json:"firstObserved"`
	LastObserved  time.Time       `json:"lastObserved"`
	Duration      string          `json:"duration"`
	Severity      Severity        `json:"severity"`
	Actionable    bool            `json:"actionable"`
	ActionSurface string          `json:"actionSurface"`
	Suppressed    bool            `json:"suppressed,omitempty"`
	Message       string          `json:"message"`
	Links         []DriftLink     `json:"links,omitempty"`
}

type DriftSummary struct {
	Total      int `json:"total"`
	Error      int `json:"error"`
	Warning    int `json:"warning"`
	Info       int `json:"info"`
	Suppressed int `json:"suppressed"`
}

type DriftReport struct {
	APIVersion string        `json:"apiVersion"`
	ProviderID string        `json:"providerId"`
	Data       []DriftRecord `json:"data"`
	Page       PageMeta      `json:"page"`
	Summary    DriftSummary  `json:"summary"`
	ObservedAt time.Time     `json:"observedAt"`
	Conditions []Condition   `json:"conditions,omitempty"`
	Incomplete bool          `json:"incomplete,omitempty"`
}
