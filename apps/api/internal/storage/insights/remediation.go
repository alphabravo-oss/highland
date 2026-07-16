package insights

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

type ActionSurface string

const (
	SurfaceHighland      ActionSurface = "highland"
	SurfaceRookCR        ActionSurface = "rook-cr"
	SurfaceCephDashboard ActionSurface = "ceph-dashboard"
	SurfaceCephCLI       ActionSurface = "ceph-cli"
	SurfaceRunbook       ActionSurface = "runbook"
	SurfaceObserveOnly   ActionSurface = "observe-only"
)

type EscalationLevel string

const (
	EscalationOperator EscalationLevel = "operator"
	EscalationAdmin    EscalationLevel = "admin"
	EscalationStorage  EscalationLevel = "storage-specialist"
	EscalationVendor   EscalationLevel = "vendor"
)

type RemediationEvidence struct {
	Source     string           `json:"source"`
	Strength   EvidenceStrength `json:"strength"`
	ObservedAt time.Time        `json:"observedAt"`
	Reference  string           `json:"reference,omitempty"`
	Summary    string           `json:"summary"`
}

type ObservedCondition struct {
	Code       string                `json:"code"`
	ProviderID string                `json:"providerId"`
	Resource   *ResourceIdentity     `json:"resource,omitempty"`
	Severity   TimelineSeverity      `json:"severity"`
	Evidence   []RemediationEvidence `json:"evidence"`
}

type VersionProfile struct {
	ProviderKind     string `json:"providerKind"`
	ProviderVersion  string `json:"providerVersion,omitempty"`
	DashboardVersion string `json:"dashboardVersion,omitempty"`
}

type RemediationDefinition struct {
	ID                   string           `json:"id"`
	ConditionCode        string           `json:"conditionCode"`
	Title                string           `json:"title"`
	Explanation          string           `json:"explanation"`
	Surface              ActionSurface    `json:"surface"`
	HighlandActionID     string           `json:"highlandActionId,omitempty"`
	DashboardDestination string           `json:"dashboardDestination,omitempty"`
	RunbookURL           string           `json:"runbookUrl,omitempty"`
	Prerequisites        []string         `json:"prerequisites"`
	Risks                []string         `json:"risks"`
	Escalation           EscalationLevel  `json:"escalation"`
	ReviewedProfiles     []VersionProfile `json:"reviewedProfiles,omitempty"`
}

type Remediation struct {
	ID                    string                `json:"id"`
	ConditionCode         string                `json:"conditionCode"`
	ProviderID            string                `json:"providerId"`
	Resource              *ResourceIdentity     `json:"resource,omitempty"`
	Title                 string                `json:"title"`
	Explanation           string                `json:"explanation"`
	Surface               ActionSurface         `json:"surface"`
	HighlandActionID      string                `json:"highlandActionId,omitempty"`
	DashboardDestination  string                `json:"dashboardDestination,omitempty"`
	RunbookURL            string                `json:"runbookUrl,omitempty"`
	Prerequisites         []string              `json:"prerequisites"`
	Risks                 []string              `json:"risks"`
	Escalation            EscalationLevel       `json:"escalation"`
	Evidence              []RemediationEvidence `json:"evidence"`
	Fresh                 bool                  `json:"fresh"`
	CompatibilityReviewed bool                  `json:"compatibilityReviewed"`
	ReadOnly              bool                  `json:"readOnly"`
}

type RemediationContext struct {
	Profile               VersionProfile
	AvailableCapabilities []string
	Now                   time.Time
	MaximumEvidenceAge    time.Duration
	MaximumResults        int
}

type RemediationResult struct {
	Recommendations []Remediation       `json:"recommendations"`
	Conditions      []CapacityCondition `json:"conditions,omitempty"`
}

type RemediationBuilder struct {
	Definitions    []RemediationDefinition
	MaximumResults int
}

func (b RemediationBuilder) Build(conditions []ObservedCondition, context RemediationContext) (RemediationResult, error) {
	maximum := b.MaximumResults
	if maximum <= 0 {
		maximum = 100
	}
	if context.MaximumResults > 0 {
		if context.MaximumResults > maximum {
			return RemediationResult{}, fmt.Errorf("remediation result limit %d exceeds maximum %d", context.MaximumResults, maximum)
		}
		maximum = context.MaximumResults
	}
	maxAge := context.MaximumEvidenceAge
	if maxAge <= 0 {
		maxAge = 15 * time.Minute
	}
	if context.Now.IsZero() {
		context.Now = time.Now()
	}
	for _, definition := range b.Definitions {
		if err := validateRemediationDefinition(definition); err != nil {
			return RemediationResult{}, err
		}
	}

	result := RemediationResult{Recommendations: []Remediation{}}
	for _, condition := range conditions {
		if condition.Code == "" || condition.ProviderID == "" {
			return RemediationResult{}, fmt.Errorf("observed remediation conditions require code and provider")
		}
		if len(condition.Evidence) == 0 {
			result.Conditions = appendUniqueCondition(result.Conditions, CapacityCondition{
				Code: "insufficient-evidence", Message: "remediation guidance was withheld because source evidence is unavailable",
			})
			continue
		}
		for _, evidence := range condition.Evidence {
			if evidence.Source == "" || evidence.Summary == "" {
				return RemediationResult{}, fmt.Errorf("remediation evidence requires source and summary")
			}
		}
		definitions := matchingDefinitions(b.Definitions, condition.Code)
		if len(definitions) == 0 {
			result.Conditions = appendUniqueCondition(result.Conditions, CapacityCondition{
				Code: "guidance-unavailable", Message: "no reviewed remediation guidance is available for condition " + condition.Code,
			})
			continue
		}
		sort.SliceStable(definitions, func(i, j int) bool {
			return remediationPreference(definitions[i], context.AvailableCapabilities) >
				remediationPreference(definitions[j], context.AvailableCapabilities)
		})
		for _, definition := range definitions {
			recommendation := Remediation{
				ID: definition.ID, ConditionCode: condition.Code,
				ProviderID: condition.ProviderID, Resource: condition.Resource,
				Title: definition.Title, Explanation: definition.Explanation,
				Surface: definition.Surface, HighlandActionID: definition.HighlandActionID,
				DashboardDestination:  definition.DashboardDestination,
				RunbookURL:            definition.RunbookURL,
				Prerequisites:         append([]string(nil), definition.Prerequisites...),
				Risks:                 append([]string(nil), definition.Risks...),
				Escalation:            definition.Escalation,
				Evidence:              append([]RemediationEvidence(nil), condition.Evidence...),
				Fresh:                 evidenceIsFresh(condition.Evidence, context.Now, maxAge),
				CompatibilityReviewed: profileReviewed(context.Profile, definition.ReviewedProfiles),
				ReadOnly:              true,
			}
			if definition.Surface == SurfaceHighland &&
				!contains(context.AvailableCapabilities, definition.HighlandActionID) {
				recommendation.Surface = SurfaceObserveOnly
				recommendation.HighlandActionID = ""
				recommendation.Risks = append(recommendation.Risks, "The required Highland capability is not currently enabled.")
			}
			if (definition.Surface == SurfaceCephDashboard || definition.Surface == SurfaceCephCLI) &&
				len(definition.ReviewedProfiles) > 0 && !recommendation.CompatibilityReviewed {
				recommendation.DashboardDestination = ""
				recommendation.Risks = append(recommendation.Risks, "Native guidance has not been reviewed for the current provider version.")
			}
			result.Recommendations = append(result.Recommendations, recommendation)
			if len(result.Recommendations) > maximum {
				return RemediationResult{}, fmt.Errorf("remediation result exceeds maximum %d", maximum)
			}
		}
	}
	return result, nil
}

func validateRemediationDefinition(definition RemediationDefinition) error {
	if definition.ID == "" || definition.ConditionCode == "" || definition.Title == "" || definition.Explanation == "" {
		return fmt.Errorf("remediation definitions require id, condition code, title, and explanation")
	}
	switch definition.Surface {
	case SurfaceHighland, SurfaceRookCR, SurfaceCephDashboard, SurfaceCephCLI,
		SurfaceRunbook, SurfaceObserveOnly:
	default:
		return fmt.Errorf("remediation %q has unsupported action surface %q", definition.ID, definition.Surface)
	}
	if definition.Surface == SurfaceHighland && definition.HighlandActionID == "" {
		return fmt.Errorf("Highland remediation %q requires a typed action id", definition.ID)
	}
	if definition.Surface != SurfaceHighland && definition.HighlandActionID != "" {
		return fmt.Errorf("non-Highland remediation %q must not declare a Highland action", definition.ID)
	}
	if definition.RunbookURL != "" {
		parsed, err := url.Parse(definition.RunbookURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return fmt.Errorf("remediation %q has an unsafe runbook URL", definition.ID)
		}
	}
	text := strings.ToLower(strings.Join(append(
		[]string{definition.Title, definition.Explanation, definition.DashboardDestination},
		append(definition.Prerequisites, definition.Risks...)...,
	), " "))
	for _, prohibited := range []string{
		"force delete", "force-delete", "remove finalizer", "remove-finalizer",
		"cluster purge", "ceph purge", "rm -rf", "kubectl patch", "ceph osd",
	} {
		if strings.Contains(text, prohibited) {
			return fmt.Errorf("remediation %q contains prohibited destructive or executable guidance", definition.ID)
		}
	}
	if definition.Surface == SurfaceCephDashboard && definition.DashboardDestination == "" {
		return fmt.Errorf("Ceph Dashboard remediation %q requires a destination registry key", definition.ID)
	}
	if definition.DashboardDestination != "" &&
		(strings.ContainsAny(definition.DashboardDestination, "?#") ||
			strings.Contains(definition.DashboardDestination, "://") ||
			strings.HasPrefix(definition.DashboardDestination, "/")) {
		return fmt.Errorf("remediation %q dashboard destination must be a registry key, not a URL or route", definition.ID)
	}
	return nil
}

func matchingDefinitions(definitions []RemediationDefinition, code string) []RemediationDefinition {
	out := []RemediationDefinition{}
	for _, definition := range definitions {
		if definition.ConditionCode == code {
			out = append(out, definition)
		}
	}
	return out
}

func remediationPreference(definition RemediationDefinition, capabilities []string) int {
	switch definition.Surface {
	case SurfaceHighland:
		if contains(capabilities, definition.HighlandActionID) {
			return 60
		}
		return 5
	case SurfaceRookCR:
		return 50
	case SurfaceCephDashboard:
		return 40
	case SurfaceRunbook:
		return 30
	case SurfaceCephCLI:
		return 20
	default:
		return 10
	}
}

func evidenceIsFresh(evidence []RemediationEvidence, now time.Time, maximumAge time.Duration) bool {
	if len(evidence) == 0 {
		return false
	}
	for _, item := range evidence {
		if item.ObservedAt.IsZero() || item.ObservedAt.After(now.Add(time.Minute)) ||
			now.Sub(item.ObservedAt) > maximumAge {
			return false
		}
	}
	return true
}

func profileReviewed(current VersionProfile, reviewed []VersionProfile) bool {
	if len(reviewed) == 0 {
		return true
	}
	for _, profile := range reviewed {
		if profile.ProviderKind == current.ProviderKind &&
			(profile.ProviderVersion == "" || profile.ProviderVersion == current.ProviderVersion) &&
			(profile.DashboardVersion == "" || profile.DashboardVersion == current.DashboardVersion) {
			return true
		}
	}
	return false
}
