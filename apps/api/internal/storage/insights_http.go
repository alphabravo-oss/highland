package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/highland-io/highland/apps/api/internal/storage/insights"
)

const (
	maxInsightLimit       = 500
	defaultForecastWindow = 30 * 24 * time.Hour
	maxForecastWindow     = 90 * 24 * time.Hour
)

type insightHistory struct {
	samples map[string][]insights.MetricSample
}

type graphAttributionResolver struct {
	byUID map[string]insights.Attribution
}

func (r graphAttributionResolver) ResolveProvider(identity insights.ResourceIdentity) insights.Attribution {
	if identity.UID == "" {
		return insights.Attribution{Evidence: insights.EvidenceUnknown, Reason: "the Kubernetes event has no exact involved-object UID"}
	}
	if attribution, ok := r.byUID[identity.UID]; ok {
		return attribution
	}
	return insights.Attribution{Evidence: insights.EvidenceUnknown, Reason: "the involved-object UID is not present in the current storage relationship graph"}
}

func (a *HTTPAPI) GetStorageTimeline(w http.ResponseWriter, r *http.Request) {
	if !a.ensureReady(w, r) {
		return
	}
	filter, err := parseTimelineFilter(r)
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "INVALID_TIMELINE_QUERY", err.Error(), false, nil)
		return
	}
	observations, err := a.timelineObservations(r)
	if err != nil {
		writeAPIError(w, r, http.StatusServiceUnavailable, "TIMELINE_BUILD_FAILED", err.Error(), true, nil)
		return
	}
	result, err := (insights.TimelineBuilder{
		MaximumEntries: maxInsightLimit, MaximumObservations: 10_000, ClockSkewLimit: 5 * time.Minute,
	}).Build(observations, filter)
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "INVALID_TIMELINE_QUERY", err.Error(), false, nil)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *HTTPAPI) GetCapacityOwnership(w http.ResponseWriter, r *http.Request) {
	if !a.ensureReady(w, r) {
		return
	}
	query, err := parseCapacityOwnershipQuery(r)
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "INVALID_CAPACITY_QUERY", err.Error(), false, nil)
		return
	}
	observations, err := a.capacityObservations(r)
	if err != nil {
		writeAPIError(w, r, http.StatusServiceUnavailable, "CAPACITY_BUILD_FAILED", err.Error(), true, nil)
		return
	}
	result, err := (insights.CapacityBuilder{MaximumGroups: maxInsightLimit}).Build(observations, query)
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "INVALID_CAPACITY_QUERY", err.Error(), false, nil)
		return
	}
	a.recordCapacityHistory(result)
	writeJSON(w, http.StatusOK, result)
}

func (a *HTTPAPI) GetCapacityForecast(w http.ResponseWriter, r *http.Request) {
	if !a.ensureReady(w, r) {
		return
	}
	providerID := strings.TrimSpace(chi.URLParam(r, "providerId"))
	measure := insights.CapacityMeasure(strings.TrimSpace(r.URL.Query().Get("measure")))
	if providerID == "" || !measure.Valid() {
		writeAPIError(w, r, http.StatusBadRequest, "INVALID_FORECAST_QUERY", "provider and a supported capacity measure are required", false, nil)
		return
	}
	horizon := defaultForecastWindow
	if raw := strings.TrimSpace(r.URL.Query().Get("horizon")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 || parsed > maxForecastWindow {
			writeAPIError(w, r, http.StatusBadRequest, "INVALID_FORECAST_QUERY", "horizon must be a positive Go duration no greater than 2160h", false, nil)
			return
		}
		horizon = parsed
	}

	provider, exists := a.registry.Provider(providerID)
	if !exists {
		writeAPIError(w, r, http.StatusNotFound, "PROVIDER_NOT_FOUND", "storage provider not found", false, map[string]any{"providerId": providerID})
		return
	}
	history, ok := provider.(CapacityHistoryReader)
	if !ok {
		result := unavailableForecast(providerID, measure, "prometheus-history-unavailable", "this provider does not expose a reviewed Prometheus history source")
		if observer, ok := a.observer.(ContextObserver); ok {
			observer.SetStorageForecastSufficient(providerID, string(measure), false)
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	now := time.Now().UTC()
	rawSamples, historyErr := history.CapacityHistory(r.Context(), string(measure), now.Add(-30*24*time.Hour), now, time.Hour)
	if historyErr != nil {
		result := unavailableForecast(providerID, measure, "prometheus-history-unavailable", historyErr.Error())
		if observer, ok := a.observer.(ContextObserver); ok {
			observer.SetStorageForecastSufficient(providerID, string(measure), false)
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	samples := make([]insights.MetricSample, 0, len(rawSamples))
	for _, sample := range rawSamples {
		samples = append(samples, insights.MetricSample{Timestamp: sample.Timestamp, Bytes: sample.Bytes})
	}
	result, err := insights.Forecast(providerID, measure, samples, horizon, time.Now().UTC(), insights.ForecastPolicy{
		MinimumSamples: 12, MinimumWindow: 6 * time.Hour, MaximumWindow: maxForecastWindow,
		MaximumAge: 15 * time.Minute, MaximumSamples: 10_000, MaximumGap: 2 * time.Hour,
	})
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "FORECAST_FAILED", err.Error(), false, nil)
		return
	}
	if observer, ok := a.observer.(ContextObserver); ok {
		observer.SetStorageForecastSufficient(providerID, string(measure), result.Status == insights.ForecastAvailable)
	}
	writeJSON(w, http.StatusOK, result)
}

func unavailableForecast(providerID string, measure insights.CapacityMeasure, code, message string) insights.CapacityForecast {
	return insights.CapacityForecast{
		ProviderID: providerID, Measure: measure, Status: insights.ForecastUnavailable,
		Conditions: []insights.CapacityCondition{{Code: code, Message: message}},
	}
}

func (a *HTTPAPI) GetProviderComparison(w http.ResponseWriter, r *http.Request) {
	if !a.ensureReady(w, r) {
		return
	}
	limit, err := insightLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "INVALID_COMPARISON_QUERY", err.Error(), false, nil)
		return
	}
	policy, err := parsePlacementPolicy(r)
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "INVALID_COMPARISON_QUERY", err.Error(), false, nil)
		return
	}
	candidates, err := a.placementCandidates(r)
	if err != nil {
		writeAPIError(w, r, http.StatusServiceUnavailable, "COMPARISON_BUILD_FAILED", err.Error(), true, nil)
		return
	}
	providers := cleanQueryValues(r.URL.Query()["provider"])
	classes := cleanQueryValues(r.URL.Query()["storageClass"])
	filtered := candidates[:0]
	for _, candidate := range candidates {
		if len(providers) > 0 && !containsString(providers, candidate.ProviderID) {
			continue
		}
		if len(classes) > 0 && !containsString(classes, candidate.StorageClass) {
			continue
		}
		filtered = append(filtered, candidate)
	}
	result, err := (insights.ComparisonBuilder{MaximumCandidates: maxInsightLimit}).Build(filtered, policy)
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "INVALID_COMPARISON_QUERY", err.Error(), false, nil)
		return
	}
	if len(result.Assessments) > limit {
		result.Assessments = result.Assessments[:limit]
		result.Conditions = append(result.Conditions, insights.CapacityCondition{
			Code: "result-truncated", Message: fmt.Sprintf("comparison was limited to %d candidates", limit),
		})
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *HTTPAPI) GetStorageRemediations(w http.ResponseWriter, r *http.Request) {
	if !a.ensureReady(w, r) {
		return
	}
	limit, err := insightLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "INVALID_REMEDIATION_QUERY", err.Error(), false, nil)
		return
	}
	providerFilter := strings.TrimSpace(r.URL.Query().Get("provider"))
	requestedConditions := cleanQueryValues(r.URL.Query()["condition"])
	requestedSeverities := cleanQueryValues(r.URL.Query()["severity"])
	drivers, err := a.inventory.DiscoveredDriverNames()
	if err != nil {
		a.writeInventoryError(w, r, err)
		return
	}
	var observed []insights.ObservedCondition
	var selected *ProviderDescriptor
	for _, descriptor := range a.registry.Descriptors(r.Context(), drivers) {
		if providerFilter != "" && descriptor.ID != providerFilter {
			continue
		}
		copy := descriptor
		if selected == nil {
			selected = &copy
		}
		report, reportErr := a.context.driftReport(r.Context(), descriptor.ID)
		if reportErr == nil {
			for _, record := range report.Data {
				code := string(record.Category)
				severity := timelineSeverity(record.Severity)
				if len(requestedConditions) > 0 && !containsString(requestedConditions, code) {
					continue
				}
				if len(requestedSeverities) > 0 && !containsString(requestedSeverities, string(severity)) {
					continue
				}
				observed = append(observed, insights.ObservedCondition{
					Code: code, ProviderID: descriptor.ID, Severity: severity,
					Resource: &insights.ResourceIdentity{
						Kind: record.Resource.Kind, Namespace: record.Resource.Namespace,
						Name: record.Resource.Name, UID: record.Resource.UID,
					},
					Evidence: []insights.RemediationEvidence{{
						Source: record.ActionSurface, Strength: insights.EvidenceAuthoritative,
						ObservedAt: record.LastObserved, Reference: record.ID, Summary: record.Message,
					}},
				})
			}
		}
		for _, condition := range descriptor.Health.Conditions {
			severity := timelineSeverity(condition.Severity)
			if severity != insights.TimelineWarning && severity != insights.TimelineError && severity != insights.TimelineCritical {
				continue
			}
			if len(requestedConditions) > 0 && !containsString(requestedConditions, "provider-health") {
				continue
			}
			if len(requestedSeverities) > 0 && !containsString(requestedSeverities, string(severity)) {
				continue
			}
			observed = append(observed, insights.ObservedCondition{
				Code: "provider-health", ProviderID: descriptor.ID, Severity: severity,
				Evidence: []insights.RemediationEvidence{{
					Source: "provider-health", Strength: insights.EvidenceAuthoritative,
					ObservedAt: condition.ObservedAt, Reference: condition.Type, Summary: condition.Message,
				}},
			})
		}
	}
	if providerFilter != "" && selected == nil {
		writeAPIError(w, r, http.StatusNotFound, "PROVIDER_NOT_FOUND", "storage provider not found", false, map[string]any{"providerId": providerFilter})
		return
	}
	profile := insights.VersionProfile{}
	capabilities := []string{}
	if selected != nil {
		profile.ProviderKind = selected.Kind
		profile.ProviderVersion = selected.Version
		profile.DashboardVersion = selected.Metadata["cephVersion"]
		for _, capability := range selected.Capabilities {
			capabilities = append(capabilities, string(capability))
		}
	}
	result, err := (insights.RemediationBuilder{
		Definitions: defaultRemediationDefinitions(), MaximumResults: maxInsightLimit,
	}).Build(observed, insights.RemediationContext{
		Profile: profile, AvailableCapabilities: capabilities, Now: time.Now().UTC(),
		MaximumEvidenceAge: 15 * time.Minute, MaximumResults: limit,
	})
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "REMEDIATION_BUILD_FAILED", err.Error(), false, nil)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *HTTPAPI) placementCandidates(r *http.Request) ([]insights.PlacementCandidate, error) {
	drivers, err := a.inventory.Drivers(r.Context())
	if err != nil {
		return nil, fmt.Errorf("read CSI drivers: %w", err)
	}
	classes, err := a.inventory.StorageClasses()
	if err != nil {
		return nil, fmt.Errorf("read StorageClasses: %w", err)
	}
	discovered := make([]string, 0, len(drivers))
	driverByName := map[string]DriverSummary{}
	for _, driver := range drivers {
		discovered = append(discovered, driver.Name)
		driverByName[driver.Name] = driver
	}
	descriptors := map[string]ProviderDescriptor{}
	headrooms := map[string]*insights.HeadroomFact{}
	for _, descriptor := range a.registry.Descriptors(r.Context(), discovered) {
		descriptors[descriptor.ID] = descriptor
		if provider, ok := a.registry.Provider(descriptor.ID); ok {
			if total, used, observedAt, available := providerCapacityFacts(r.Context(), provider); available && total >= used {
				headrooms[descriptor.ID] = &insights.HeadroomFact{
					Percent: float64(total-used) / float64(total) * 100,
					Evidence: insights.ComparisonEvidence{
						Source: "provider-capacity-metrics", Strength: insights.EvidenceAuthoritative,
						ObservedAt: observedAt, Stale: descriptor.Health.Stale,
					},
				}
			}
		}
	}
	result := make([]insights.PlacementCandidate, 0, len(classes))
	for _, class := range classes {
		descriptor, ok := descriptors[class.ProviderID]
		if !ok {
			continue
		}
		driver := driverByName[class.Provisioner]
		evidence := insights.ComparisonEvidence{
			Source: "storage-inventory", Strength: insights.EvidenceAuthoritative,
			ObservedAt: a.inventory.LastSync(), Stale: descriptor.Health.Stale,
		}
		capabilities := make([]insights.CapabilityFact, 0, len(descriptor.Capabilities)+2)
		for _, capability := range descriptor.Capabilities {
			capabilities = append(capabilities, insights.CapabilityFact{
				ID: string(capability), State: insights.FactSupported,
				Verified: descriptor.SupportLevel != SupportDetected, Evidence: evidence,
			})
		}
		capabilities = append(capabilities, insights.CapabilityFact{
			ID: "volume.expand", State: boolFact(class.AllowVolumeExpansion),
			Verified: true, Evidence: insights.ComparisonEvidence{
				Source: "storageclass.allowVolumeExpansion", Strength: insights.EvidenceAuthoritative,
				ObservedAt: a.inventory.LastSync(),
			},
		})
		if len(class.SnapshotClasses) > 0 {
			capabilities = append(capabilities, insights.CapabilityFact{
				ID: "snapshot.create", State: insights.FactSupported, Verified: true,
				Evidence: insights.ComparisonEvidence{
					Source: "VolumeSnapshotClass driver match", Strength: insights.EvidenceAuthoritative,
					ObservedAt: a.inventory.LastSync(),
				},
			})
		}
		topology := make([]string, 0, len(class.AllowedTopologies))
		for _, term := range class.AllowedTopologies {
			topology = append(topology, term.Key)
		}
		operations := []insights.OperationalSurface{}
		for _, capability := range descriptor.Capabilities {
			if strings.Contains(string(capability), ".create") || strings.Contains(string(capability), ".delete") ||
				strings.Contains(string(capability), ".expand") {
				operations = append(operations, insights.OperationalSurface{
					Capability: string(capability), Surface: "highland",
					Detail: "typed, policy-gated Highland operation when enabled",
				})
			}
		}
		if descriptor.Kind == "rook-ceph" {
			operations = append(operations, insights.OperationalSurface{
				Capability: "ceph.native.administration", Surface: "ceph-dashboard",
				Detail: "separate native application and authorization boundary",
			})
		}
		result = append(result, insights.PlacementCandidate{
			ProviderID: descriptor.ID, ProviderName: descriptor.DisplayName,
			StorageClass: class.Name, SupportLevel: comparisonSupport(descriptor.SupportLevel),
			Profile: insights.TestedProfile{
				ProviderKind: descriptor.Kind, ProviderVersion: descriptor.Version,
				Driver: class.Provisioner, DriverVersion: driver.Metadata["version"],
			},
			Health: &insights.HealthFact{Status: string(descriptor.Health.Status), Evidence: insights.ComparisonEvidence{
				Source: "provider-health", Strength: insights.EvidenceAuthoritative,
				ObservedAt: descriptor.Health.ObservedAt, Stale: descriptor.Health.Stale,
			}},
			Capabilities: capabilities, TopologyKeys: topology,
			ReclaimPolicy: class.ReclaimPolicy, Headroom: headrooms[descriptor.ID], Operations: operations,
		})
	}
	return result, nil
}

func parsePlacementPolicy(r *http.Request) (insights.PlacementPolicy, error) {
	policy := insights.PlacementPolicy{
		RequiredAccessMode: strings.TrimSpace(r.URL.Query().Get("accessMode")),
		RequiredTopology:   cleanQueryValues(r.URL.Query()["topology"]),
		MinimumSupport:     insights.ComparisonSupportLevel(strings.TrimSpace(r.URL.Query().Get("minimumSupport"))),
	}
	var err error
	for raw, target := range map[string]*bool{
		"snapshot": &policy.RequireSnapshot, "clone": &policy.RequireClone,
		"encryption": &policy.RequireEncryption, "expansion": &policy.RequireExpansion,
		"healthy": &policy.RequireHealthy,
	} {
		value := strings.TrimSpace(r.URL.Query().Get(raw))
		if value == "" {
			continue
		}
		*target, err = strconv.ParseBool(value)
		if err != nil {
			return policy, fmt.Errorf("%s must be true or false", raw)
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("minimumHeadroom")); raw != "" {
		policy.MinimumHeadroom, err = strconv.ParseFloat(raw, 64)
		if err != nil || policy.MinimumHeadroom < 0 || policy.MinimumHeadroom > 100 {
			return policy, fmt.Errorf("minimumHeadroom must be between 0 and 100")
		}
	}
	return policy, nil
}

func defaultRemediationDefinitions() []insights.RemediationDefinition {
	definition := func(id, code, title, explanation string, surface insights.ActionSurface, escalation insights.EscalationLevel) insights.RemediationDefinition {
		return insights.RemediationDefinition{
			ID: id, ConditionCode: code, Title: title, Explanation: explanation,
			Surface: surface, Escalation: escalation,
			Prerequisites: []string{"Refresh Highland and confirm the evidence is current.", "Review affected workloads and provider health before changing desired state."},
			Risks:         []string{"Changing storage desired state can affect availability and data protection."},
		}
	}
	return []insights.RemediationDefinition{
		definition("inspect-missing-runtime", string(DriftMissingRuntime), "Inspect Rook reconciliation", "The Rook resource exists but fresh Ceph runtime evidence does not show its expected backend object. Review Rook conditions and operator events before editing desired state.", insights.SurfaceRookCR, insights.EscalationStorage),
		definition("inspect-unexpected-runtime", string(DriftUnexpectedRuntime), "Reconcile unexpected Ceph runtime state", "Ceph reports a runtime object without matching Rook desired state. Establish ownership and lifecycle intent in native Ceph and Rook records.", insights.SurfaceCephCLI, insights.EscalationStorage),
		definition("inspect-rook-readiness", string(DriftRookNotReady), "Review Rook readiness conditions", "Rook has not reported the resource ready. Inspect status conditions, related Kubernetes events, and operator health.", insights.SurfaceRookCR, insights.EscalationOperator),
		definition("inspect-spec-status", string(DriftSpecStatus), "Review desired and observed generations", "Rook status has not caught up with the current desired generation. Confirm reconciliation is progressing before another change.", insights.SurfaceRookCR, insights.EscalationOperator),
		definition("inspect-stalled-reconciliation", string(DriftReconciliationStalled), "Escalate stalled reconciliation", "Rook reports a failed or stalled state. Preserve evidence and involve a storage specialist before changing the resource.", insights.SurfaceObserveOnly, insights.EscalationStorage),
		definition("review-version-profile", string(DriftVersionUnsupported), "Review provider compatibility", "The current Rook or Ceph profile is outside Highland's reviewed compatibility matrix. Use native documentation and specialist review.", insights.SurfaceObserveOnly, insights.EscalationStorage),
		definition("refresh-runtime-evidence", string(DriftRuntimeStale), "Restore fresh Ceph runtime evidence", "Highland cannot compare desired and runtime state while native evidence is stale or unavailable. Restore the configured reader before planning changes.", insights.SurfaceObserveOnly, insights.EscalationAdmin),
		definition("verify-post-operation", string(DriftPostOperation), "Verify post-operation state", "Observed provider state does not match the completed operation's expected result. Preserve the operation and provider evidence for review.", insights.SurfaceObserveOnly, insights.EscalationStorage),
		definition("review-provider-health", "provider-health", "Review provider health evidence", "The provider reports an active warning or error. Inspect the cited condition and affected relationships before selecting an action surface.", insights.SurfaceObserveOnly, insights.EscalationOperator),
	}
}

func (a *HTTPAPI) timelineObservations(r *http.Request) ([]insights.TimelineObservation, error) {
	events, err := a.inventory.Events()
	if err != nil {
		return nil, fmt.Errorf("read Kubernetes events: %w", err)
	}
	resolver, err := a.eventAttributionResolver(r)
	if err != nil {
		return nil, err
	}
	normalized := make([]insights.KubernetesEvent, 0, len(events))
	for _, event := range events {
		normalized = append(normalized, insights.KubernetesEvent{
			UID: event.Name, Name: event.Name, Namespace: event.Namespace,
			Type: event.Type, Reason: event.Reason, Message: event.Message,
			Regarding: insights.ResourceIdentity{
				Kind: event.RegardingKind, Namespace: event.Namespace,
				Name: event.RegardingName, UID: event.RegardingUID,
			},
			Count: int64(event.Count), FirstObservedAt: event.FirstObservedAt,
			LastObservedAt: event.LastObservedAt, CollectionTime: a.inventory.LastSync(),
		})
	}
	observations := insights.NormalizeKubernetesEvents(normalized, resolver)

	drivers, err := a.inventory.DiscoveredDriverNames()
	if err != nil {
		return nil, fmt.Errorf("discover providers: %w", err)
	}
	records := make([]insights.ProviderTimelineRecord, 0)
	for _, descriptor := range a.registry.Descriptors(r.Context(), drivers) {
		for index, condition := range descriptor.Health.Conditions {
			source := insights.SourceProvider
			lower := strings.ToLower(condition.Type + " " + condition.Reason)
			if descriptor.Kind == "rook-ceph" && strings.Contains(lower, "rook") {
				source = insights.SourceRookCondition
			} else if descriptor.Kind == "rook-ceph" && (strings.Contains(lower, "ceph") || strings.Contains(lower, "dashboard")) {
				source = insights.SourceCephHealth
			}
			records = append(records, insights.ProviderTimelineRecord{
				ID:         fmt.Sprintf("%s-health-%d-%s", descriptor.ID, index, condition.Type),
				ProviderID: descriptor.ID, Severity: timelineSeverity(condition.Severity),
				Source: source, Reason: condition.Reason,
				Message: condition.Message, Count: 1, LastOccurredAt: condition.ObservedAt,
				ObservedAt:        descriptor.Health.ObservedAt,
				DeduplicationKey:  descriptor.ID + "\x00" + condition.Type + "\x00" + condition.Reason,
				AttributionReason: "provider adapter health condition",
			})
		}
	}
	if a.context.operations != nil {
		operations, operationErr := a.context.operations(r.Context(), maxInsightLimit)
		if operationErr != nil {
			return nil, fmt.Errorf("read storage operations: %w", operationErr)
		}
		for _, operation := range operations {
			if operation.ProviderID == "" {
				continue
			}
			records = append(records, insights.ProviderTimelineRecord{
				ID: operation.ID, ProviderID: operation.ProviderID, Namespace: operation.Namespace,
				Resource: &insights.ResourceIdentity{
					Kind: operation.TargetKind, Namespace: operation.Namespace,
					Name: operation.TargetName, UID: operation.TargetUID,
				},
				Severity: operationSeverity(operation.Phase), Source: insights.SourceOperation,
				Action: operation.ActionID, Reason: operation.Phase, Message: operation.Message,
				Count: 1, FirstOccurredAt: operation.RequestedAt, LastOccurredAt: operation.ObservedAt,
				ObservedAt: operation.ObservedAt, DeduplicationKey: "operation:" + operation.ID,
				AttributionReason: "durable StorageOperation provider identity",
				Links:             []insights.Link{{Kind: "operation", Href: "/storage/operations/" + operation.ID}},
			})
		}
	}
	if a.context.audits != nil {
		for _, event := range a.context.audits(maxInsightLimit) {
			providerID := event.ProviderID
			source := insights.SourceAudit
			if providerID == "" {
				lower := strings.ToLower(event.Action)
				if !strings.Contains(lower, "config") && !strings.Contains(lower, "credential") &&
					!strings.Contains(lower, "oidc") && !strings.Contains(lower, "sso") &&
					!strings.Contains(lower, "user") {
					continue
				}
				providerID = "highland-control-plane"
				source = insights.SourceConfiguration
			}
			records = append(records, insights.ProviderTimelineRecord{
				ID: event.ID, ProviderID: providerID, Namespace: event.Namespace,
				Resource: &insights.ResourceIdentity{
					Kind: event.TargetKind, Namespace: event.Namespace,
					Name: event.TargetName, UID: event.TargetUID,
				},
				Severity: auditSeverity(event.Result), Source: source,
				Action: event.Action, Reason: event.Result, Message: event.Message,
				Count: 1, FirstOccurredAt: event.ObservedAt, LastOccurredAt: event.ObservedAt,
				ObservedAt: event.ObservedAt, DeduplicationKey: "audit:" + event.ID,
				AttributionReason: "durable Highland audit provider identity",
			})
		}
	}
	providerObservations, err := insights.NormalizeProviderRecords(records)
	if err != nil {
		return nil, fmt.Errorf("normalize provider timeline: %w", err)
	}
	return append(observations, providerObservations...), nil
}

func (a *HTTPAPI) eventAttributionResolver(r *http.Request) (graphAttributionResolver, error) {
	resolver := graphAttributionResolver{byUID: map[string]insights.Attribution{}}
	drivers, err := a.inventory.DiscoveredDriverNames()
	if err != nil {
		return resolver, fmt.Errorf("discover providers: %w", err)
	}
	for _, descriptor := range a.registry.Descriptors(r.Context(), drivers) {
		graph, buildErr := a.context.build(r.Context(), descriptor.ID)
		if buildErr != nil {
			return resolver, fmt.Errorf("build %s relationship graph: %w", descriptor.ID, buildErr)
		}
		for _, node := range graph.nodes {
			if node.UID == "" {
				continue
			}
			resolver.byUID[node.UID] = insights.Attribution{
				ProviderID: descriptor.ID, Evidence: insights.EvidenceAuthoritative,
				Reason: "exact Kubernetes UID matched the storage relationship graph",
			}
		}
	}
	return resolver, nil
}

func (a *HTTPAPI) capacityObservations(r *http.Request) ([]insights.CapacityObservation, error) {
	claims, err := a.inventory.Claims(r.Context())
	if err != nil {
		return nil, fmt.Errorf("read claims: %w", err)
	}
	volumes, err := a.inventory.Volumes(r.Context())
	if err != nil {
		return nil, fmt.Errorf("read volumes: %w", err)
	}
	observedAt := a.inventory.LastSync()
	result := make([]insights.CapacityObservation, 0, len(claims)+len(volumes)*2)
	for _, claim := range claims {
		bytes, ok := quantityBytes(claim.RequestedCapacity)
		if !ok || claim.ProviderID == "" {
			continue
		}
		dimensions := insights.CapacityDimensions{
			ProviderID: claim.ProviderID, Driver: claim.Driver, StorageClass: claim.StorageClass,
			Namespace: claim.Namespace, ReclaimPolicy: claim.ReclaimPolicy,
		}
		if len(claim.Workloads) == 1 {
			dimensions.WorkloadKind = claim.Workloads[0].Kind
			dimensions.Workload = claim.Workloads[0].Name
		}
		result = append(result, insights.CapacityObservation{
			ID: "pvc:" + claim.UID, Measure: insights.CapacityPVCRequested, Bytes: bytes,
			Dimensions: dimensions, Evidence: insights.CapacityEvidence{
				Strength: insights.EvidenceAuthoritative, Source: "kubernetes-pvc-spec", ObservedAt: observedAt, Reference: claim.UID,
			},
		})
	}
	for _, volume := range volumes {
		if volume.ProviderID == "" {
			continue
		}
		dimensions := insights.CapacityDimensions{
			ProviderID: volume.ProviderID, Driver: volume.Driver, StorageClass: volume.StorageClass,
			Namespace: volume.ClaimNamespace, ReclaimPolicy: volume.ReclaimPolicy,
		}
		if bytes, ok := quantityBytes(volume.Capacity); ok {
			result = append(result, insights.CapacityObservation{
				ID: "pv:" + volume.UID, Measure: insights.CapacityPVProvisioned, Bytes: bytes,
				Dimensions: dimensions, Evidence: insights.CapacityEvidence{
					Strength: insights.EvidenceAuthoritative, Source: "kubernetes-pv-spec", ObservedAt: observedAt, Reference: volume.UID,
				},
			})
		}
		if bytes, ok := quantityBytes(volume.BackendAllocated); ok && volume.ProviderRef != nil && volume.ProviderRef.ID != "" {
			result = append(result, insights.CapacityObservation{
				ID: "backend:" + volume.ProviderRef.ID, Measure: insights.CapacityBackendAllocated, Bytes: bytes,
				Dimensions: dimensions, Evidence: insights.CapacityEvidence{
					Strength: insights.EvidenceAuthoritative, Source: "provider-inventory-enrichment", ObservedAt: observedAt, Reference: volume.ProviderRef.ID,
				},
			})
		}
	}
	drivers, err := a.inventory.DiscoveredDriverNames()
	if err != nil {
		return nil, fmt.Errorf("discover providers: %w", err)
	}
	for _, descriptor := range a.registry.Descriptors(r.Context(), drivers) {
		provider, ok := a.registry.Provider(descriptor.ID)
		if !ok {
			continue
		}
		total, used, providerObservedAt, available := providerCapacityFacts(r.Context(), provider)
		if !available {
			continue
		}
		dimensions := insights.CapacityDimensions{ProviderID: descriptor.ID}
		result = append(result,
			insights.CapacityObservation{
				ID: "provider:" + descriptor.ID + ":cluster-raw", Measure: insights.CapacityClusterRaw,
				Bytes: total, Dimensions: dimensions, Evidence: insights.CapacityEvidence{
					Strength: insights.EvidenceAuthoritative, Source: "provider-capacity-metrics", ObservedAt: providerObservedAt,
				},
			},
			insights.CapacityObservation{
				ID: "provider:" + descriptor.ID + ":backend-allocated", Measure: insights.CapacityBackendAllocated,
				Bytes: used, Dimensions: dimensions, Evidence: insights.CapacityEvidence{
					Strength: insights.EvidenceAuthoritative, Source: "provider-capacity-metrics", ObservedAt: providerObservedAt,
				},
			},
		)
	}
	return result, nil
}

func providerCapacityFacts(ctx context.Context, provider Provider) (total, used uint64, observedAt time.Time, available bool) {
	reader, ok := provider.(ProviderSummaryReader)
	if !ok {
		return 0, 0, time.Time{}, false
	}
	raw, err := reader.ProviderSummary(ctx)
	if err != nil {
		return 0, 0, time.Time{}, false
	}
	var summary struct {
		ObservedAt        time.Time `json:"observedAt"`
		RuntimeObservedAt time.Time `json:"runtimeObservedAt"`
		Metrics           struct {
			Values     map[string]string `json:"values"`
			ObservedAt time.Time         `json:"observedAt"`
		} `json:"metrics"`
		RuntimeHealth struct {
			DF struct {
				Stats struct {
					TotalBytes   uint64 `json:"total_bytes"`
					TotalUsedRaw uint64 `json:"total_used_raw_bytes"`
				} `json:"stats"`
			} `json:"df"`
		} `json:"runtimeHealth"`
	}
	encoded, err := json.Marshal(raw)
	if err != nil || json.Unmarshal(encoded, &summary) != nil {
		return 0, 0, time.Time{}, false
	}
	total, totalOK := decimalBytes(summary.Metrics.Values["totalBytes"])
	used, usedOK := decimalBytes(summary.Metrics.Values["usedBytes"])
	if !totalOK || !usedOK || total == 0 {
		total = summary.RuntimeHealth.DF.Stats.TotalBytes
		used = summary.RuntimeHealth.DF.Stats.TotalUsedRaw
		if total == 0 {
			return 0, 0, time.Time{}, false
		}
	}
	observedAt = summary.Metrics.ObservedAt
	if observedAt.IsZero() {
		observedAt = summary.ObservedAt
	}
	if observedAt.IsZero() {
		observedAt = summary.RuntimeObservedAt
	}
	return total, used, observedAt, true
}

func decimalBytes(value string) (uint64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	return parsed, err == nil
}

func (a *HTTPAPI) recordCapacityHistory(ownership insights.CapacityOwnership) {
	if a == nil || a.insightHistory == nil {
		return
	}
	timestamp := ownership.ObservedAt
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}
	totals := map[string]uint64{}
	for _, group := range ownership.Groups {
		key := group.Dimensions.ProviderID + "\x00" + string(group.Measure)
		totals[key] += group.Bytes
	}
	a.insightMu.Lock()
	defer a.insightMu.Unlock()
	for key, bytes := range totals {
		samples := append(a.insightHistory.samples[key], insights.MetricSample{Timestamp: timestamp, Bytes: bytes})
		if len(samples) > 10_000 {
			samples = samples[len(samples)-10_000:]
		}
		a.insightHistory.samples[key] = samples
	}
}

func (a *HTTPAPI) capacityHistory(providerID string, measure insights.CapacityMeasure) []insights.MetricSample {
	if a == nil || a.insightHistory == nil {
		return nil
	}
	a.insightMu.Lock()
	defer a.insightMu.Unlock()
	key := providerID + "\x00" + string(measure)
	return append([]insights.MetricSample(nil), a.insightHistory.samples[key]...)
}

func parseTimelineFilter(r *http.Request) (insights.TimelineFilter, error) {
	limit, err := insightLimit(r.URL.Query().Get("limit"))
	if err != nil {
		return insights.TimelineFilter{}, err
	}
	filter := insights.TimelineFilter{
		ProviderID: strings.TrimSpace(r.URL.Query().Get("provider")),
		Namespaces: cleanQueryValues(r.URL.Query()["namespace"]),
		Workload:   strings.TrimSpace(r.URL.Query().Get("workload")),
		Resource:   strings.TrimSpace(r.URL.Query().Get("resource")),
		Actions:    cleanQueryValues(r.URL.Query()["action"]), Limit: limit,
	}
	for _, value := range cleanQueryValues(r.URL.Query()["severity"]) {
		filter.Severities = append(filter.Severities, insights.TimelineSeverity(value))
	}
	for _, value := range cleanQueryValues(r.URL.Query()["source"]) {
		source := insights.TimelineSource(value)
		if !source.Valid() {
			return filter, fmt.Errorf("unsupported timeline source %q", value)
		}
		filter.Sources = append(filter.Sources, source)
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		filter.Since, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return filter, fmt.Errorf("since must be RFC3339")
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("until")); raw != "" {
		filter.Until, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return filter, fmt.Errorf("until must be RFC3339")
		}
	}
	return filter, nil
}

func parseCapacityOwnershipQuery(r *http.Request) (insights.CapacityOwnershipQuery, error) {
	limit, err := insightLimit(r.URL.Query().Get("limit"))
	if err != nil {
		return insights.CapacityOwnershipQuery{}, err
	}
	query := insights.CapacityOwnershipQuery{
		ProviderID: strings.TrimSpace(r.URL.Query().Get("provider")),
		Namespaces: cleanQueryValues(r.URL.Query()["namespace"]), MaximumGroups: limit,
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("authoritativeOnly")); raw != "" {
		query.AuthoritativeOnly, err = strconv.ParseBool(raw)
		if err != nil {
			return query, fmt.Errorf("authoritativeOnly must be true or false")
		}
	}
	for _, value := range cleanQueryValues(r.URL.Query()["measure"]) {
		measure := insights.CapacityMeasure(value)
		if !measure.Valid() {
			return query, fmt.Errorf("unsupported capacity measure %q", value)
		}
		query.Measures = append(query.Measures, measure)
	}
	return query, nil
}

func insightLimit(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 100, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maxInsightLimit {
		return 0, fmt.Errorf("limit must be between 1 and %d", maxInsightLimit)
	}
	return limit, nil
}

func cleanQueryValues(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func quantityBytes(value string) (uint64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	quantity, err := resource.ParseQuantity(value)
	if err != nil || quantity.Sign() < 0 {
		return 0, false
	}
	parsed, ok := quantity.AsInt64()
	if !ok || parsed < 0 {
		return 0, false
	}
	return uint64(parsed), true
}

func timelineSeverity(severity Severity) insights.TimelineSeverity {
	switch severity {
	case SeverityError:
		return insights.TimelineError
	case SeverityWarning:
		return insights.TimelineWarning
	case SeverityOK, SeverityInfo:
		return insights.TimelineInfo
	default:
		return insights.TimelineUnknown
	}
}

func operationSeverity(phase string) insights.TimelineSeverity {
	switch strings.ToLower(phase) {
	case "failed":
		return insights.TimelineError
	case "cancelled":
		return insights.TimelineWarning
	default:
		return insights.TimelineInfo
	}
}

func auditSeverity(result string) insights.TimelineSeverity {
	switch strings.ToLower(result) {
	case "error", "failed":
		return insights.TimelineError
	case "denied", "warning":
		return insights.TimelineWarning
	default:
		return insights.TimelineInfo
	}
}

func comparisonSupport(level SupportLevel) insights.ComparisonSupportLevel {
	switch level {
	case SupportManaged:
		return insights.ComparisonManaged
	case SupportVerified:
		return insights.ComparisonVerified
	default:
		return insights.ComparisonDetected
	}
}

func boolFact(value bool) insights.FactState {
	if value {
		return insights.FactSupported
	}
	return insights.FactUnsupported
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
