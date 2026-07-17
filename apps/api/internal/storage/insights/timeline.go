// Package insights contains provider-neutral storage context models and
// builders. It intentionally has no dependency on HTTP handlers, informers, or
// provider adapters so the same attribution and aggregation rules can be used
// by cached collectors, APIs, and operation preflight.
package insights

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type TimelineSource string

const (
	SourceKubernetesEvent TimelineSource = "kubernetes-event"
	SourceRookCondition   TimelineSource = "rook-condition"
	SourceCephHealth      TimelineSource = "ceph-health"
	SourceProvider        TimelineSource = "provider"
	SourceOperation       TimelineSource = "storage-operation"
	SourceAudit           TimelineSource = "audit"
	SourceConfiguration   TimelineSource = "configuration"
)

func (s TimelineSource) Valid() bool {
	switch s {
	case SourceKubernetesEvent, SourceRookCondition, SourceCephHealth, SourceProvider,
		SourceOperation, SourceAudit, SourceConfiguration:
		return true
	default:
		return false
	}
}

type RetentionClass string

const (
	RetentionTransient RetentionClass = "transient"
	RetentionDurable   RetentionClass = "durable"
	RetentionAudit     RetentionClass = "audit"
)

type TimelineSeverity string

const (
	TimelineInfo     TimelineSeverity = "info"
	TimelineWarning  TimelineSeverity = "warning"
	TimelineError    TimelineSeverity = "error"
	TimelineCritical TimelineSeverity = "critical"
	TimelineUnknown  TimelineSeverity = "unknown"
)

type EvidenceStrength string

const (
	EvidenceAuthoritative EvidenceStrength = "authoritative"
	EvidenceDerived       EvidenceStrength = "derived"
	EvidencePotential     EvidenceStrength = "potential"
	EvidenceUnknown       EvidenceStrength = "unknown"
)

type ResourceIdentity struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
	UID        string `json:"uid,omitempty"`
}

func (r ResourceIdentity) Key() string {
	if r.UID != "" {
		return "uid:" + r.UID
	}
	return strings.Join([]string{r.APIVersion, r.Kind, r.Namespace, r.Name}, "/")
}

type WorkloadIdentity struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	UID       string `json:"uid,omitempty"`
}

type Link struct {
	Kind string `json:"kind"`
	Href string `json:"href"`
}

type Attribution struct {
	ProviderID string           `json:"providerId,omitempty"`
	Evidence   EvidenceStrength `json:"evidence"`
	Reason     string           `json:"reason,omitempty"`
}

// ObjectAttributionResolver must only return authoritative when the involved
// Kubernetes object has been correlated through an exact UID or another
// documented, exact identity. Names, messages, reasons, and "storage-shaped"
// heuristics are not authoritative.
type ObjectAttributionResolver interface {
	ResolveProvider(ResourceIdentity) Attribution
}

type KubernetesEvent struct {
	UID             string
	Name            string
	Namespace       string
	Type            string
	Reason          string
	Action          string
	Message         string
	Regarding       ResourceIdentity
	Workload        *WorkloadIdentity
	Count           int64
	FirstObservedAt time.Time
	LastObservedAt  time.Time
	CollectionTime  time.Time
	GraphLink       string
}

type TimelineObservation struct {
	ID               string
	ProviderID       string
	Namespace        string
	Workload         *WorkloadIdentity
	Resource         *ResourceIdentity
	Severity         TimelineSeverity
	Source           TimelineSource
	Action           string
	Reason           string
	Message          string
	Count            int64
	FirstOccurredAt  time.Time
	LastOccurredAt   time.Time
	ObservedAt       time.Time
	Attribution      Attribution
	Links            []Link
	DeduplicationKey string
	Retention        RetentionClass
}

type Ordering string

const (
	OrderingKnown   Ordering = "known"
	OrderingSkewed  Ordering = "clock-skew"
	OrderingUnknown Ordering = "unknown"
)

type TimelineEntry struct {
	ID              string            `json:"id"`
	ProviderID      string            `json:"providerId,omitempty"`
	Namespace       string            `json:"namespace,omitempty"`
	Workload        *WorkloadIdentity `json:"workload,omitempty"`
	Resource        *ResourceIdentity `json:"resource,omitempty"`
	Severity        TimelineSeverity  `json:"severity"`
	Source          TimelineSource    `json:"source"`
	Action          string            `json:"action,omitempty"`
	Reason          string            `json:"reason,omitempty"`
	Message         string            `json:"message,omitempty"`
	Count           int64             `json:"count"`
	FirstOccurredAt time.Time         `json:"firstOccurredAt,omitempty"`
	LastOccurredAt  time.Time         `json:"lastOccurredAt,omitempty"`
	ObservedAt      time.Time         `json:"observedAt"`
	Ordering        Ordering          `json:"ordering"`
	ClockSkew       time.Duration     `json:"clockSkew,omitempty"`
	Attribution     Attribution       `json:"attribution"`
	Links           []Link            `json:"links,omitempty"`
	Retention       RetentionClass    `json:"retention"`
}

type TimelineFilter struct {
	ProviderID string
	Namespaces []string
	Workload   string
	Resource   string
	Severities []TimelineSeverity
	Sources    []TimelineSource
	Actions    []string
	Since      time.Time
	Until      time.Time
	Limit      int
}

type Timeline struct {
	Entries   []TimelineEntry `json:"entries"`
	Total     int             `json:"total"`
	Truncated bool            `json:"truncated,omitempty"`
}

type TimelineBuilder struct {
	MaximumEntries      int
	MaximumObservations int
	ClockSkewLimit      time.Duration
}

func NormalizeKubernetesEvents(events []KubernetesEvent, resolver ObjectAttributionResolver) []TimelineObservation {
	out := make([]TimelineObservation, 0, len(events))
	for _, event := range events {
		attribution := Attribution{Evidence: EvidenceUnknown, Reason: "involved object is not correlated to a storage provider"}
		if resolver != nil {
			resolved := resolver.ResolveProvider(event.Regarding)
			// Provider attribution for a Kubernetes Event is intentionally
			// fail-closed. Derived or potential relationships may be useful in
			// a graph, but are not sufficient to place an event in a
			// provider-filtered incident timeline.
			if resolved.ProviderID != "" && resolved.Evidence == EvidenceAuthoritative {
				attribution = resolved
			}
		}
		count := event.Count
		if count < 1 {
			count = 1
		}
		first, last := event.FirstObservedAt, event.LastObservedAt
		if last.IsZero() {
			last = first
		}
		if first.IsZero() {
			first = last
		}
		resource := event.Regarding
		links := []Link{}
		if event.GraphLink != "" {
			links = append(links, Link{Kind: "resource-graph", Href: event.GraphLink})
		}
		dedupIdentity := event.UID
		if dedupIdentity == "" {
			dedupIdentity = strings.Join([]string{
				resource.Key(), event.Reason, event.Action, event.Message,
			}, "\x00")
		}
		out = append(out, TimelineObservation{
			ID: event.UID, ProviderID: attribution.ProviderID,
			Namespace: event.Namespace, Workload: event.Workload,
			Resource: &resource, Severity: kubernetesEventSeverity(event.Type),
			Source: SourceKubernetesEvent, Action: event.Action,
			Reason: event.Reason, Message: event.Message, Count: count,
			FirstOccurredAt: first, LastOccurredAt: last,
			ObservedAt: event.CollectionTime, Attribution: attribution,
			Links: links, DeduplicationKey: "k8s-event:" + dedupIdentity,
			Retention: RetentionTransient,
		})
	}
	return out
}

// ProviderTimelineRecord normalizes sources that already have an explicit
// provider boundary: Rook state, Ceph runtime health, provider adapter errors,
// durable operations, audit records, and configuration changes. Kubernetes
// Events are deliberately rejected here; they must pass through involved
// object correlation in NormalizeKubernetesEvents.
type ProviderTimelineRecord struct {
	ID                string
	ProviderID        string
	Namespace         string
	Workload          *WorkloadIdentity
	Resource          *ResourceIdentity
	Severity          TimelineSeverity
	Source            TimelineSource
	Action            string
	Reason            string
	Message           string
	Count             int64
	FirstOccurredAt   time.Time
	LastOccurredAt    time.Time
	ObservedAt        time.Time
	Links             []Link
	DeduplicationKey  string
	AttributionReason string
}

func NormalizeProviderRecords(records []ProviderTimelineRecord) ([]TimelineObservation, error) {
	out := make([]TimelineObservation, 0, len(records))
	for _, record := range records {
		if !record.Source.Valid() || record.Source == SourceKubernetesEvent {
			return nil, fmt.Errorf("source %q must not be normalized as a provider record", record.Source)
		}
		if record.ProviderID == "" {
			return nil, fmt.Errorf("%s timeline record %q requires a provider", record.Source, record.ID)
		}
		count := record.Count
		if count < 1 {
			count = 1
		}
		out = append(out, TimelineObservation{
			ID: record.ID, ProviderID: record.ProviderID, Namespace: record.Namespace,
			Workload: record.Workload, Resource: record.Resource,
			Severity: record.Severity, Source: record.Source, Action: record.Action,
			Reason: record.Reason, Message: record.Message, Count: count,
			FirstOccurredAt: record.FirstOccurredAt, LastOccurredAt: record.LastOccurredAt,
			ObservedAt: record.ObservedAt, Links: append([]Link(nil), record.Links...),
			DeduplicationKey: record.DeduplicationKey,
			Attribution: Attribution{
				ProviderID: record.ProviderID, Evidence: EvidenceAuthoritative,
				Reason: record.AttributionReason,
			},
			Retention: retentionForSource(record.Source),
		})
	}
	return out, nil
}

func (b TimelineBuilder) Build(observations []TimelineObservation, filter TimelineFilter) (Timeline, error) {
	maximum := b.MaximumEntries
	if maximum <= 0 {
		maximum = 1000
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > maximum {
		return Timeline{}, fmt.Errorf("timeline limit %d exceeds maximum %d", limit, maximum)
	}
	maximumObservations := b.MaximumObservations
	if maximumObservations <= 0 {
		maximumObservations = 10000
	}
	if len(observations) > maximumObservations {
		return Timeline{}, fmt.Errorf("timeline input has %d observations, exceeding maximum %d", len(observations), maximumObservations)
	}
	if !filter.Until.IsZero() && !filter.Since.IsZero() && filter.Until.Before(filter.Since) {
		return Timeline{}, fmt.Errorf("timeline until must not precede since")
	}

	entriesByKey := map[string]TimelineEntry{}
	for index, observation := range observations {
		if !matchesTimeline(observation, filter) {
			continue
		}
		entry := b.entry(observation, index)
		key := observation.DeduplicationKey
		if key == "" {
			key = entry.ID
		}
		if prior, ok := entriesByKey[key]; ok {
			entriesByKey[key] = mergeTimelineEntries(prior, entry)
		} else {
			entriesByKey[key] = entry
		}
	}
	entries := make([]TimelineEntry, 0, len(entriesByKey))
	for _, entry := range entriesByKey {
		entries = append(entries, entry)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		left, right := entries[i].LastOccurredAt, entries[j].LastOccurredAt
		if left.IsZero() {
			left = entries[i].ObservedAt
		}
		if right.IsZero() {
			right = entries[j].ObservedAt
		}
		if left.Equal(right) {
			return entries[i].ID < entries[j].ID
		}
		return left.After(right)
	})
	result := Timeline{Total: len(entries), Entries: entries}
	if len(result.Entries) > limit {
		result.Entries = result.Entries[:limit]
		result.Truncated = true
	}
	return result, nil
}

func (b TimelineBuilder) entry(observation TimelineObservation, index int) TimelineEntry {
	count := observation.Count
	if count < 1 {
		count = 1
	}
	id := observation.ID
	if id == "" {
		id = fmt.Sprintf("%s-%d", observation.Source, index)
	}
	ordering := OrderingKnown
	var skew time.Duration
	sourceTime := observation.LastOccurredAt
	if sourceTime.IsZero() {
		sourceTime = observation.FirstOccurredAt
	}
	if sourceTime.IsZero() {
		ordering = OrderingUnknown
	} else if !observation.ObservedAt.IsZero() {
		skew = observation.ObservedAt.Sub(sourceTime)
		if skew < 0 {
			skew = -skew
		}
		limit := b.ClockSkewLimit
		if limit <= 0 {
			limit = 5 * time.Minute
		}
		if skew > limit {
			ordering = OrderingSkewed
		}
	}
	return TimelineEntry{
		ID: id, ProviderID: observation.ProviderID,
		Namespace: observation.Namespace, Workload: observation.Workload,
		Resource: observation.Resource, Severity: observation.Severity,
		Source: observation.Source, Action: observation.Action,
		Reason: observation.Reason, Message: observation.Message, Count: count,
		FirstOccurredAt: observation.FirstOccurredAt,
		LastOccurredAt:  observation.LastOccurredAt, ObservedAt: observation.ObservedAt,
		Ordering: ordering, ClockSkew: skew, Attribution: observation.Attribution,
		Links:     append([]Link(nil), observation.Links...),
		Retention: retentionOrDefault(observation.Retention, observation.Source),
	}
}

func matchesTimeline(observation TimelineObservation, filter TimelineFilter) bool {
	if filter.ProviderID != "" {
		if observation.ProviderID != filter.ProviderID {
			return false
		}
		// All provider-filtered records must say how provider attribution was
		// established. Kubernetes Events require authoritative correlation.
		if observation.Attribution.Evidence == EvidenceUnknown || observation.Attribution.Evidence == EvidencePotential {
			return false
		}
		if observation.Source == SourceKubernetesEvent && observation.Attribution.Evidence != EvidenceAuthoritative {
			return false
		}
	}
	if len(filter.Namespaces) > 0 && !contains(filter.Namespaces, observation.Namespace) {
		return false
	}
	if filter.Workload != "" && (observation.Workload == nil ||
		(observation.Workload.Name != filter.Workload && observation.Workload.UID != filter.Workload)) {
		return false
	}
	if filter.Resource != "" && (observation.Resource == nil ||
		(observation.Resource.Name != filter.Resource && observation.Resource.UID != filter.Resource)) {
		return false
	}
	if len(filter.Severities) > 0 && !contains(filter.Severities, observation.Severity) {
		return false
	}
	if len(filter.Sources) > 0 && !contains(filter.Sources, observation.Source) {
		return false
	}
	if len(filter.Actions) > 0 && !contains(filter.Actions, observation.Action) {
		return false
	}
	occurred := observation.LastOccurredAt
	if occurred.IsZero() {
		occurred = observation.ObservedAt
	}
	if !filter.Since.IsZero() && occurred.Before(filter.Since) {
		return false
	}
	if !filter.Until.IsZero() && occurred.After(filter.Until) {
		return false
	}
	return true
}

func mergeTimelineEntries(left, right TimelineEntry) TimelineEntry {
	// A repeated Kubernetes Event is one logical timeline entry. Count uses the
	// maximum because API list/watch observations report a cumulative count;
	// summing them would double-count relists. Other normalized sources may
	// emit deltas, for which summing is appropriate.
	if left.Source == SourceKubernetesEvent {
		if right.Count > left.Count {
			left.Count = right.Count
		}
	} else {
		left.Count += right.Count
	}
	if left.FirstOccurredAt.IsZero() || (!right.FirstOccurredAt.IsZero() && right.FirstOccurredAt.Before(left.FirstOccurredAt)) {
		left.FirstOccurredAt = right.FirstOccurredAt
	}
	if right.LastOccurredAt.After(left.LastOccurredAt) {
		left.LastOccurredAt = right.LastOccurredAt
		left.Message = right.Message
		left.Reason = right.Reason
		left.Action = right.Action
	}
	if right.ObservedAt.After(left.ObservedAt) {
		left.ObservedAt = right.ObservedAt
		left.Ordering = right.Ordering
		left.ClockSkew = right.ClockSkew
	}
	left.Links = mergeLinks(left.Links, right.Links)
	return left
}

func mergeLinks(left, right []Link) []Link {
	seen := map[string]struct{}{}
	out := make([]Link, 0, len(left)+len(right))
	for _, link := range append(append([]Link(nil), left...), right...) {
		key := link.Kind + "\x00" + link.Href
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, link)
	}
	return out
}

func kubernetesEventSeverity(eventType string) TimelineSeverity {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "warning":
		return TimelineWarning
	case "error":
		return TimelineError
	case "critical":
		return TimelineCritical
	case "normal":
		return TimelineInfo
	default:
		return TimelineUnknown
	}
}

func retentionOrDefault(retention RetentionClass, source TimelineSource) RetentionClass {
	if retention != "" {
		return retention
	}
	return retentionForSource(source)
}

func retentionForSource(source TimelineSource) RetentionClass {
	switch source {
	case SourceOperation:
		return RetentionDurable
	case SourceAudit, SourceConfiguration:
		return RetentionAudit
	default:
		return RetentionTransient
	}
}

func contains[T comparable](values []T, wanted T) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
