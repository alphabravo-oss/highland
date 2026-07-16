package operations

import "time"

const (
	APIVersion = "highland.io/v1alpha1"
	Kind       = "StorageOperation"
)

type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

type ConfirmationMode string

const (
	ConfirmSummary   ConfirmationMode = "summary"
	ConfirmTypedName ConfirmationMode = "typed-name"
)

type Action struct {
	ID              string           `json:"id"`
	Capability      string           `json:"capability"`
	MinimumRole     string           `json:"minimumRole"`
	ProviderKind    string           `json:"providerKind,omitempty"`
	Risk            RiskLevel        `json:"risk"`
	Confirmation    ConfirmationMode `json:"confirmation"`
	FeatureFlag     string           `json:"featureFlag"`
	PreflightChecks []string         `json:"preflightChecks"`
	AuditAction     string           `json:"auditAction"`
}

type ResourceTarget struct {
	APIVersion      string `json:"apiVersion,omitempty"`
	Kind            string `json:"kind"`
	Namespace       string `json:"namespace,omitempty"`
	Name            string `json:"name"`
	UID             string `json:"uid,omitempty"`
	ResourceVersion string `json:"resourceVersion,omitempty"`
}

type Confirmation struct {
	Challenge            string `json:"challenge"`
	TypedName            string `json:"typedName,omitempty"`
	WarningsAcknowledged bool   `json:"warningsAcknowledged,omitempty"`
}

type Request struct {
	ActionID     string         `json:"actionId"`
	ProviderID   string         `json:"providerId,omitempty"`
	Target       ResourceTarget `json:"target"`
	Parameters   map[string]any `json:"parameters,omitempty"`
	Confirmation Confirmation   `json:"confirmation,omitempty"`
}

type Check struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type PlannedResource struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Namespace  string         `json:"namespace,omitempty"`
	Name       string         `json:"name"`
	Operation  string         `json:"operation"`
	Manifest   map[string]any `json:"manifest,omitempty"`
}

type Plan struct {
	Action             Action            `json:"action"`
	ProviderID         string            `json:"providerId,omitempty"`
	Target             ResourceTarget    `json:"target"`
	Resources          []PlannedResource `json:"resources"`
	Dependencies       []ResourceTarget  `json:"dependencies,omitempty"`
	Checks             []Check           `json:"checks"`
	Warnings           []string          `json:"warnings,omitempty"`
	BlastRadius        string            `json:"blastRadius"`
	Hash               string            `json:"hash"`
	Challenge          string            `json:"challenge"`
	ChallengeExpiresAt time.Time         `json:"challengeExpiresAt"`
	ObservedAt         time.Time         `json:"observedAt"`
}

type Spec struct {
	ActionID        string            `json:"actionId"`
	ProviderID      string            `json:"providerId,omitempty"`
	Target          ResourceTarget    `json:"target"`
	Parameters      map[string]any    `json:"parameters,omitempty"`
	ParameterHash   string            `json:"parameterHash"`
	PlanHash        string            `json:"planHash"`
	IdempotencyHash string            `json:"idempotencyHash,omitempty"`
	Resources       []PlannedResource `json:"resources"`
	Dependencies    []ResourceTarget  `json:"dependencies,omitempty"`
	Requester       string            `json:"requester"`
	RequesterRole   string            `json:"requesterRole"`
	RequestedAt     time.Time         `json:"requestedAt"`
}

type OperationCondition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	Reason             string    `json:"reason,omitempty"`
	Message            string    `json:"message,omitempty"`
	LastTransitionTime time.Time `json:"lastTransitionTime"`
}

type ResultReference struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name,omitempty"`
	UID        string `json:"uid,omitempty"`
}

type Status struct {
	Phase         string               `json:"phase"`
	Step          string               `json:"step,omitempty"`
	Conditions    []OperationCondition `json:"conditions,omitempty"`
	StartedAt     *time.Time           `json:"startedAt,omitempty"`
	FinishedAt    *time.Time           `json:"finishedAt,omitempty"`
	LastAttemptAt *time.Time           `json:"lastAttemptAt,omitempty"`
	Retries       int                  `json:"retries,omitempty"`
	Result        *ResultReference     `json:"result,omitempty"`
	ErrorCode     string               `json:"errorCode,omitempty"`
	Diagnostics   string               `json:"diagnostics,omitempty"`
}

type Operation struct {
	APIVersion        string    `json:"apiVersion"`
	Kind              string    `json:"kind"`
	Name              string    `json:"name"`
	UID               string    `json:"uid,omitempty"`
	ResourceVersion   string    `json:"resourceVersion,omitempty"`
	CreationTimestamp time.Time `json:"creationTimestamp"`
	Spec              Spec      `json:"spec"`
	Status            Status    `json:"status"`
}
