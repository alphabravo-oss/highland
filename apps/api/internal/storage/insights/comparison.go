package insights

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type ComparisonSupportLevel string

const (
	ComparisonDetected ComparisonSupportLevel = "detected"
	ComparisonVerified ComparisonSupportLevel = "verified"
	ComparisonManaged  ComparisonSupportLevel = "managed"
)

func (s ComparisonSupportLevel) rank() int {
	switch s {
	case ComparisonManaged:
		return 3
	case ComparisonVerified:
		return 2
	case ComparisonDetected:
		return 1
	default:
		return 0
	}
}

type FactState string

const (
	FactSupported   FactState = "supported"
	FactUnsupported FactState = "unsupported"
	FactUnknown     FactState = "unknown"
)

type ComparisonEvidence struct {
	Source     string           `json:"source"`
	Strength   EvidenceStrength `json:"strength"`
	ObservedAt time.Time        `json:"observedAt"`
	Stale      bool             `json:"stale,omitempty"`
	Detail     string           `json:"detail,omitempty"`
}

type CapabilityFact struct {
	ID       string             `json:"id"`
	State    FactState          `json:"state"`
	Verified bool               `json:"verified,omitempty"`
	Evidence ComparisonEvidence `json:"evidence"`
}

type HealthFact struct {
	Status   string             `json:"status"`
	Evidence ComparisonEvidence `json:"evidence"`
}

type HeadroomFact struct {
	Percent  float64            `json:"percent"`
	Evidence ComparisonEvidence `json:"evidence"`
}

// BenchmarkFact is comparable only when Semantic, Unit, Method, and Profile
// are all equal. Backend counters with different meanings remain separate.
type BenchmarkFact struct {
	Semantic string             `json:"semantic"`
	Unit     string             `json:"unit"`
	Method   string             `json:"method"`
	Profile  string             `json:"profile"`
	Value    float64            `json:"value"`
	Evidence ComparisonEvidence `json:"evidence"`
}

type TestedProfile struct {
	ProviderKind    string `json:"providerKind"`
	ProviderVersion string `json:"providerVersion,omitempty"`
	Driver          string `json:"driver"`
	DriverVersion   string `json:"driverVersion,omitempty"`
	Kubernetes      string `json:"kubernetesVersion,omitempty"`
}

type PlacementCandidate struct {
	ProviderID    string                 `json:"providerId"`
	ProviderName  string                 `json:"providerName"`
	StorageClass  string                 `json:"storageClass"`
	SupportLevel  ComparisonSupportLevel `json:"supportLevel"`
	Profile       TestedProfile          `json:"testedProfile"`
	Health        *HealthFact            `json:"health,omitempty"`
	Capabilities  []CapabilityFact       `json:"capabilities"`
	AccessModes   []string               `json:"accessModes,omitempty"`
	TopologyKeys  []string               `json:"topologyKeys,omitempty"`
	ReclaimPolicy string                 `json:"reclaimPolicy,omitempty"`
	Headroom      *HeadroomFact          `json:"headroom,omitempty"`
	Benchmarks    []BenchmarkFact        `json:"benchmarks,omitempty"`
	Operations    []OperationalSurface   `json:"operations,omitempty"`
}

type OperationalSurface struct {
	Capability string `json:"capability"`
	Surface    string `json:"surface"`
	ReadOnly   bool   `json:"readOnly,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

type PlacementPolicy struct {
	RequiredAccessMode string                 `json:"requiredAccessMode,omitempty"`
	RequiredTopology   []string               `json:"requiredTopology,omitempty"`
	RequireSnapshot    bool                   `json:"requireSnapshot,omitempty"`
	RequireClone       bool                   `json:"requireClone,omitempty"`
	RequireEncryption  bool                   `json:"requireEncryption,omitempty"`
	RequireExpansion   bool                   `json:"requireExpansion,omitempty"`
	RequireHealthy     bool                   `json:"requireHealthy,omitempty"`
	MinimumHeadroom    float64                `json:"minimumHeadroomPercent,omitempty"`
	MinimumSupport     ComparisonSupportLevel `json:"minimumSupportLevel,omitempty"`
}

type CriterionResult struct {
	Criterion string             `json:"criterion"`
	State     FactState          `json:"state"`
	Reason    string             `json:"reason"`
	Evidence  ComparisonEvidence `json:"evidence,omitempty"`
}

type CandidateAssessment struct {
	Candidate   PlacementCandidate  `json:"candidate"`
	Eligibility string              `json:"eligibility"`
	Criteria    []CriterionResult   `json:"criteria"`
	Conditions  []CapacityCondition `json:"conditions,omitempty"`
}

type ProviderComparison struct {
	Assessments []CandidateAssessment `json:"assessments"`
	Policy      PlacementPolicy       `json:"policy"`
	Conditions  []CapacityCondition   `json:"conditions,omitempty"`
	ObservedAt  time.Time             `json:"observedAt"`
}

type ComparisonBuilder struct {
	MaximumCandidates int
}

func (b ComparisonBuilder) Build(candidates []PlacementCandidate, policy PlacementPolicy) (ProviderComparison, error) {
	maximum := b.MaximumCandidates
	if maximum <= 0 {
		maximum = 100
	}
	if len(candidates) > maximum {
		return ProviderComparison{}, fmt.Errorf("comparison has %d candidates, exceeding maximum %d", len(candidates), maximum)
	}
	if policy.MinimumHeadroom < 0 || policy.MinimumHeadroom > 100 {
		return ProviderComparison{}, fmt.Errorf("minimum headroom must be between 0 and 100")
	}
	if policy.MinimumSupport != "" && policy.MinimumSupport.rank() == 0 {
		return ProviderComparison{}, fmt.Errorf("unsupported minimum support level %q", policy.MinimumSupport)
	}

	result := ProviderComparison{
		Assessments: make([]CandidateAssessment, 0, len(candidates)),
		Policy:      policy,
	}
	for _, candidate := range candidates {
		if candidate.ProviderID == "" || candidate.StorageClass == "" {
			return ProviderComparison{}, fmt.Errorf("comparison candidates require provider and storage class identity")
		}
		assessment := assessCandidate(candidate, policy)
		result.Assessments = append(result.Assessments, assessment)
		for _, fact := range candidate.Capabilities {
			if fact.Evidence.ObservedAt.After(result.ObservedAt) {
				result.ObservedAt = fact.Evidence.ObservedAt
			}
		}
		if candidate.Health != nil && candidate.Health.Evidence.ObservedAt.After(result.ObservedAt) {
			result.ObservedAt = candidate.Health.Evidence.ObservedAt
		}
		if candidate.Headroom != nil && candidate.Headroom.Evidence.ObservedAt.After(result.ObservedAt) {
			result.ObservedAt = candidate.Headroom.Evidence.ObservedAt
		}
	}
	sort.SliceStable(result.Assessments, func(i, j int) bool {
		left, right := eligibilityRank(result.Assessments[i].Eligibility), eligibilityRank(result.Assessments[j].Eligibility)
		if left != right {
			return left > right
		}
		if result.Assessments[i].Candidate.ProviderID != result.Assessments[j].Candidate.ProviderID {
			return result.Assessments[i].Candidate.ProviderID < result.Assessments[j].Candidate.ProviderID
		}
		return result.Assessments[i].Candidate.StorageClass < result.Assessments[j].Candidate.StorageClass
	})
	if hasNonComparableBenchmarks(candidates) {
		result.Conditions = append(result.Conditions, CapacityCondition{
			Code:    "non-comparable-benchmarks",
			Message: "some benchmark or backend metrics use different semantics, units, methods, or profiles and were not ranked against each other",
		})
	}
	return result, nil
}

func assessCandidate(candidate PlacementCandidate, policy PlacementPolicy) CandidateAssessment {
	assessment := CandidateAssessment{Candidate: candidate}
	add := func(criterion string, state FactState, reason string, evidence ComparisonEvidence) {
		assessment.Criteria = append(assessment.Criteria, CriterionResult{
			Criterion: criterion, State: state, Reason: reason, Evidence: evidence,
		})
	}
	profileState, profileReason := testedProfileState(candidate)
	add("tested-profile", profileState, profileReason, ComparisonEvidence{Source: "compatibility-matrix"})
	if policy.RequiredAccessMode != "" {
		state := FactUnsupported
		if contains(candidate.AccessModes, policy.RequiredAccessMode) {
			state = FactSupported
		} else if len(candidate.AccessModes) == 0 {
			state = FactUnknown
		}
		add("access-mode", state, fmt.Sprintf("requires access mode %s", policy.RequiredAccessMode), ComparisonEvidence{Source: "storage-class"})
	}
	for _, topology := range policy.RequiredTopology {
		state := FactUnsupported
		if contains(candidate.TopologyKeys, topology) {
			state = FactSupported
		} else if len(candidate.TopologyKeys) == 0 {
			state = FactUnknown
		}
		add("topology:"+topology, state, fmt.Sprintf("requires topology key %s", topology), ComparisonEvidence{Source: "csi"})
	}
	for _, requirement := range []struct {
		required   bool
		criterion  string
		capability string
	}{
		{policy.RequireSnapshot, "snapshot", "snapshot.create"},
		{policy.RequireClone, "clone", "volume.clone"},
		{policy.RequireEncryption, "encryption", "volume.encryption"},
		{policy.RequireExpansion, "expansion", "volume.expand"},
	} {
		if !requirement.required {
			continue
		}
		fact, found := findCapability(candidate.Capabilities, requirement.capability)
		if !found {
			add(requirement.criterion, FactUnknown, "capability evidence is unavailable", ComparisonEvidence{})
			continue
		}
		state := fact.State
		if fact.Evidence.Stale && state == FactSupported {
			state = FactUnknown
		}
		if state == FactSupported && candidate.SupportLevel.rank() >= ComparisonVerified.rank() && !fact.Verified {
			state = FactUnknown
		}
		add(requirement.criterion, state, "requires "+requirement.capability, fact.Evidence)
	}
	if policy.RequireHealthy {
		if candidate.Health == nil || candidate.Health.Evidence.Stale {
			add("health", FactUnknown, "fresh provider health is unavailable", evidenceOrZero(candidate.Health))
		} else {
			status := strings.ToLower(candidate.Health.Status)
			state := FactUnsupported
			if status == "ok" || status == "healthy" {
				state = FactSupported
			} else if status == "" || status == "unknown" {
				state = FactUnknown
			}
			add("health", state, "requires currently healthy provider evidence", candidate.Health.Evidence)
		}
	}
	if policy.MinimumHeadroom > 0 {
		if candidate.Headroom == nil || candidate.Headroom.Evidence.Stale {
			add("headroom", FactUnknown, "fresh usable-capacity headroom is unavailable", headroomEvidence(candidate.Headroom))
		} else {
			state := FactUnsupported
			if candidate.Headroom.Percent >= policy.MinimumHeadroom {
				state = FactSupported
			}
			add("headroom", state, fmt.Sprintf("requires at least %.1f%% usable headroom", policy.MinimumHeadroom), candidate.Headroom.Evidence)
		}
	}
	if policy.MinimumSupport != "" {
		state := FactUnsupported
		if candidate.SupportLevel.rank() >= policy.MinimumSupport.rank() {
			state = FactSupported
		} else if candidate.SupportLevel.rank() == 0 {
			state = FactUnknown
		}
		add("support-level", state, "requires Highland support level "+string(policy.MinimumSupport), ComparisonEvidence{Source: "compatibility-matrix"})
	}

	assessment.Eligibility = "eligible"
	for _, criterion := range assessment.Criteria {
		if criterion.State == FactUnsupported {
			assessment.Eligibility = "ineligible"
			break
		}
		if criterion.State == FactUnknown {
			assessment.Eligibility = "unknown"
		}
	}
	if len(assessment.Criteria) == 1 && assessment.Criteria[0].Criterion == "tested-profile" {
		assessment.Eligibility = "unknown"
		assessment.Conditions = append(assessment.Conditions, CapacityCondition{
			Code: "no-placement-policy", Message: "no placement requirements were supplied; Highland did not rank this candidate",
		})
	}
	return assessment
}

func testedProfileState(candidate PlacementCandidate) (FactState, string) {
	profile := candidate.Profile
	if profile.ProviderKind == "" || profile.Driver == "" {
		return FactUnknown, "provider kind or CSI driver identity is unavailable"
	}
	if candidate.SupportLevel.rank() >= ComparisonVerified.rank() && profile.DriverVersion == "" {
		return FactUnknown, "the tested CSI driver version is unavailable"
	}
	if candidate.SupportLevel == ComparisonManaged && profile.ProviderVersion == "" {
		return FactUnknown, "the tested managed-provider version is unavailable"
	}
	return FactSupported, "exact tested provider and driver profile is recorded"
}

func findCapability(facts []CapabilityFact, id string) (CapabilityFact, bool) {
	for _, fact := range facts {
		if fact.ID == id {
			return fact, true
		}
	}
	return CapabilityFact{}, false
}

func evidenceOrZero(fact *HealthFact) ComparisonEvidence {
	if fact == nil {
		return ComparisonEvidence{}
	}
	return fact.Evidence
}

func headroomEvidence(fact *HeadroomFact) ComparisonEvidence {
	if fact == nil {
		return ComparisonEvidence{}
	}
	return fact.Evidence
}

func eligibilityRank(value string) int {
	switch value {
	case "eligible":
		return 3
	case "unknown":
		return 2
	case "ineligible":
		return 1
	default:
		return 0
	}
}

func hasNonComparableBenchmarks(candidates []PlacementCandidate) bool {
	keys := map[string]map[string]struct{}{}
	for _, candidate := range candidates {
		for _, benchmark := range candidate.Benchmarks {
			if keys[benchmark.Semantic] == nil {
				keys[benchmark.Semantic] = map[string]struct{}{}
			}
			signature := strings.Join([]string{benchmark.Unit, benchmark.Method, benchmark.Profile}, "\x00")
			keys[benchmark.Semantic][signature] = struct{}{}
		}
	}
	for _, signatures := range keys {
		if len(signatures) > 1 {
			return true
		}
	}
	return false
}
