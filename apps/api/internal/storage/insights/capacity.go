package insights

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// CapacityMeasure is intentionally non-additive across values. Requested,
// provisioned, logical, allocated, usable, and raw bytes answer different
// questions and must never be collapsed into a single "total capacity".
type CapacityMeasure string

const (
	CapacityPVCRequested     CapacityMeasure = "pvc-requested"
	CapacityPVProvisioned    CapacityMeasure = "pv-provisioned"
	CapacityBackendLogical   CapacityMeasure = "backend-logical"
	CapacityBackendAllocated CapacityMeasure = "backend-allocated"
	CapacityPoolUsable       CapacityMeasure = "pool-usable"
	CapacityPoolRaw          CapacityMeasure = "pool-raw"
	CapacityClusterRaw       CapacityMeasure = "cluster-raw"
)

func (m CapacityMeasure) Valid() bool {
	switch m {
	case CapacityPVCRequested, CapacityPVProvisioned, CapacityBackendLogical,
		CapacityBackendAllocated, CapacityPoolUsable, CapacityPoolRaw, CapacityClusterRaw:
		return true
	default:
		return false
	}
}

type CapacityMeasurementDefinition struct {
	Measure     CapacityMeasure `json:"measure"`
	Description string          `json:"description"`
	Scope       string          `json:"scope"`
}

func CapacityMeasurementDefinitions() []CapacityMeasurementDefinition {
	return []CapacityMeasurementDefinition{
		{CapacityPVCRequested, "capacity requested by PersistentVolumeClaims", "logical Kubernetes demand"},
		{CapacityPVProvisioned, "capacity provisioned on PersistentVolumes", "logical Kubernetes supply"},
		{CapacityBackendLogical, "logical size of backend images or subvolumes", "backend logical address space"},
		{CapacityBackendAllocated, "backend bytes allocated or used, subject to backend accounting semantics", "backend consumption"},
		{CapacityPoolUsable, "capacity usable by clients after backend redundancy and reservation rules", "pool usable capacity"},
		{CapacityPoolRaw, "raw physical capacity assigned to a pool before redundancy overhead", "pool raw capacity"},
		{CapacityClusterRaw, "physical raw capacity across the storage cluster", "cluster physical capacity"},
	}
}

type CapacityDimensions struct {
	ProviderID    string `json:"providerId"`
	Driver        string `json:"driver,omitempty"`
	StorageClass  string `json:"storageClass,omitempty"`
	Namespace     string `json:"namespace,omitempty"`
	WorkloadKind  string `json:"workloadKind,omitempty"`
	Workload      string `json:"workload,omitempty"`
	ReclaimPolicy string `json:"reclaimPolicy,omitempty"`
	Pool          string `json:"pool,omitempty"`
	Filesystem    string `json:"filesystem,omitempty"`
}

type CapacityEvidence struct {
	Strength   EvidenceStrength `json:"strength"`
	Source     string           `json:"source"`
	ObservedAt time.Time        `json:"observedAt"`
	Reference  string           `json:"reference,omitempty"`
}

type CapacityObservation struct {
	ID         string             `json:"id"`
	Measure    CapacityMeasure    `json:"measure"`
	Bytes      uint64             `json:"bytes"`
	Dimensions CapacityDimensions `json:"dimensions"`
	Evidence   CapacityEvidence   `json:"evidence"`
}

type CapacityCondition struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type CapacityOwnershipGroup struct {
	Measure      CapacityMeasure    `json:"measure"`
	Bytes        uint64             `json:"bytes"`
	Dimensions   CapacityDimensions `json:"dimensions"`
	Observations int                `json:"observations"`
	OldestAt     time.Time          `json:"oldestAt,omitempty"`
	NewestAt     time.Time          `json:"newestAt,omitempty"`
	Evidence     []EvidenceStrength `json:"evidence"`
}

type CapacityOwnership struct {
	Groups     []CapacityOwnershipGroup `json:"groups"`
	Conditions []CapacityCondition      `json:"conditions,omitempty"`
	ObservedAt time.Time                `json:"observedAt"`
}

type CapacityOwnershipQuery struct {
	ProviderID        string
	Namespaces        []string
	Measures          []CapacityMeasure
	AuthoritativeOnly bool
	MaximumGroups     int
}

type CapacityBuilder struct {
	MaximumGroups int
}

func (b CapacityBuilder) Build(observations []CapacityObservation, query CapacityOwnershipQuery) (CapacityOwnership, error) {
	maximum := b.MaximumGroups
	if maximum <= 0 {
		maximum = 1000
	}
	if query.MaximumGroups > 0 {
		if query.MaximumGroups > maximum {
			return CapacityOwnership{}, fmt.Errorf("capacity group limit %d exceeds maximum %d", query.MaximumGroups, maximum)
		}
		maximum = query.MaximumGroups
	}
	for _, measure := range query.Measures {
		if !measure.Valid() {
			return CapacityOwnership{}, fmt.Errorf("unsupported capacity measure %q", measure)
		}
	}

	groups := map[string]CapacityOwnershipGroup{}
	var newest time.Time
	conditions := []CapacityCondition{}
	includedNonAuthoritative := false
	for _, observation := range observations {
		if !observation.Measure.Valid() {
			return CapacityOwnership{}, fmt.Errorf("observation %q has unsupported capacity measure %q", observation.ID, observation.Measure)
		}
		if observation.Dimensions.ProviderID == "" {
			return CapacityOwnership{}, fmt.Errorf("observation %q has no provider", observation.ID)
		}
		if query.ProviderID != "" && observation.Dimensions.ProviderID != query.ProviderID {
			continue
		}
		if len(query.Namespaces) > 0 && !contains(query.Namespaces, observation.Dimensions.Namespace) {
			continue
		}
		if len(query.Measures) > 0 && !contains(query.Measures, observation.Measure) {
			continue
		}
		if query.AuthoritativeOnly && observation.Evidence.Strength != EvidenceAuthoritative {
			continue
		}
		if observation.Evidence.Strength != EvidenceAuthoritative {
			includedNonAuthoritative = true
		}
		key := ownershipKey(observation.Measure, observation.Dimensions)
		group := groups[key]
		if group.Observations == 0 {
			group.Measure = observation.Measure
			group.Dimensions = observation.Dimensions
			group.OldestAt = observation.Evidence.ObservedAt
		}
		if !contains(group.Evidence, observation.Evidence.Strength) {
			group.Evidence = append(group.Evidence, observation.Evidence.Strength)
			sort.Slice(group.Evidence, func(i, j int) bool { return group.Evidence[i] < group.Evidence[j] })
		}
		if math.MaxUint64-group.Bytes < observation.Bytes {
			return CapacityOwnership{}, fmt.Errorf("capacity overflow in %s group", observation.Measure)
		}
		group.Bytes += observation.Bytes
		group.Observations++
		if group.OldestAt.IsZero() || (!observation.Evidence.ObservedAt.IsZero() && observation.Evidence.ObservedAt.Before(group.OldestAt)) {
			group.OldestAt = observation.Evidence.ObservedAt
		}
		if observation.Evidence.ObservedAt.After(group.NewestAt) {
			group.NewestAt = observation.Evidence.ObservedAt
		}
		if observation.Evidence.ObservedAt.After(newest) {
			newest = observation.Evidence.ObservedAt
		}
		groups[key] = group
	}

	result := CapacityOwnership{Groups: make([]CapacityOwnershipGroup, 0, len(groups)), ObservedAt: newest}
	for _, group := range groups {
		result.Groups = append(result.Groups, group)
	}
	sort.Slice(result.Groups, func(i, j int) bool {
		if result.Groups[i].Measure != result.Groups[j].Measure {
			return result.Groups[i].Measure < result.Groups[j].Measure
		}
		return ownershipKey(result.Groups[i].Measure, result.Groups[i].Dimensions) <
			ownershipKey(result.Groups[j].Measure, result.Groups[j].Dimensions)
	})
	if len(result.Groups) > maximum {
		return CapacityOwnership{}, fmt.Errorf("capacity result has %d groups, exceeding maximum %d", len(result.Groups), maximum)
	}
	if includedNonAuthoritative {
		conditions = appendUniqueCondition(conditions, CapacityCondition{
			Code:    "non-authoritative-attribution",
			Message: "one or more capacity records use derived, potential, or unknown ownership attribution",
		})
	}
	result.Conditions = conditions
	return result, nil
}

// Total returns a total for exactly one measurement. Requiring the caller to
// name the measure prevents requested bytes from being added to physical or
// usable capacity.
func (o CapacityOwnership) Total(measure CapacityMeasure) (uint64, error) {
	if !measure.Valid() {
		return 0, fmt.Errorf("unsupported capacity measure %q", measure)
	}
	var total uint64
	for _, group := range o.Groups {
		if group.Measure != measure {
			continue
		}
		if math.MaxUint64-total < group.Bytes {
			return 0, fmt.Errorf("capacity total overflow for %s", measure)
		}
		total += group.Bytes
	}
	return total, nil
}

type CapacityExplanation struct {
	ThinProvisioned bool     `json:"thinProvisioned,omitempty"`
	ReplicaCount    int      `json:"replicaCount,omitempty"`
	ErasureData     int      `json:"erasureDataChunks,omitempty"`
	ErasureCoding   int      `json:"erasureCodingChunks,omitempty"`
	Snapshots       bool     `json:"snapshotsMayShareExtents,omitempty"`
	Clones          bool     `json:"clonesMayShareExtents,omitempty"`
	Compression     string   `json:"compression,omitempty"`
	Notes           []string `json:"notes,omitempty"`
}

type PressureState string

const (
	PressureOK       PressureState = "ok"
	PressureWarning  PressureState = "warning"
	PressureCritical PressureState = "critical"
	PressureUnknown  PressureState = "unknown"
)

type HeadroomPolicy struct {
	WarningPercent   float64 `json:"warningPercent"`
	CriticalPercent  float64 `json:"criticalPercent"`
	MinimumFreeBytes uint64  `json:"minimumFreeBytes,omitempty"`
}

type Headroom struct {
	UsedBytes     uint64        `json:"usedBytes"`
	CapacityBytes uint64        `json:"capacityBytes"`
	FreeBytes     uint64        `json:"freeBytes"`
	UsedPercent   float64       `json:"usedPercent"`
	State         PressureState `json:"state"`
	Reason        string        `json:"reason"`
}

func EvaluateHeadroom(used, capacity uint64, policy HeadroomPolicy) (Headroom, error) {
	if capacity == 0 {
		return Headroom{UsedBytes: used, State: PressureUnknown, Reason: "usable capacity is unavailable"}, nil
	}
	if used > capacity {
		return Headroom{}, fmt.Errorf("used bytes exceed usable capacity")
	}
	if policy.WarningPercent <= 0 {
		policy.WarningPercent = 80
	}
	if policy.CriticalPercent <= 0 {
		policy.CriticalPercent = 90
	}
	if policy.WarningPercent >= policy.CriticalPercent || policy.CriticalPercent > 100 {
		return Headroom{}, fmt.Errorf("headroom thresholds must satisfy 0 < warning < critical <= 100")
	}
	free := capacity - used
	percent := float64(used) / float64(capacity) * 100
	state, reason := PressureOK, "capacity is below the warning threshold"
	if percent >= policy.CriticalPercent || free < policy.MinimumFreeBytes {
		state, reason = PressureCritical, "capacity reached the critical pressure policy"
	} else if percent >= policy.WarningPercent {
		state, reason = PressureWarning, "capacity reached the warning pressure policy"
	}
	return Headroom{
		UsedBytes: used, CapacityBytes: capacity, FreeBytes: free,
		UsedPercent: percent, State: state, Reason: reason,
	}, nil
}

type MetricSample struct {
	Timestamp time.Time `json:"timestamp"`
	Bytes     uint64    `json:"bytes"`
}

type ForecastPolicy struct {
	MinimumSamples  int
	MinimumWindow   time.Duration
	MaximumWindow   time.Duration
	MaximumAge      time.Duration
	MaximumSamples  int
	MaximumGap      time.Duration
	FutureTolerance time.Duration
}

type ForecastStatus string

const (
	ForecastAvailable   ForecastStatus = "available"
	ForecastUnavailable ForecastStatus = "unavailable"
)

type ForecastConfidence string

const (
	ConfidenceLow    ForecastConfidence = "low"
	ConfidenceMedium ForecastConfidence = "medium"
	ConfidenceHigh   ForecastConfidence = "high"
)

type CapacityForecast struct {
	ProviderID       string              `json:"providerId"`
	Measure          CapacityMeasure     `json:"measure"`
	Status           ForecastStatus      `json:"status"`
	CurrentBytes     uint64              `json:"currentBytes,omitempty"`
	SlopeBytesPerDay float64             `json:"slopeBytesPerDay,omitempty"`
	ProjectedBytes   uint64              `json:"projectedBytes,omitempty"`
	ProjectionAt     time.Time           `json:"projectionAt,omitempty"`
	SampleCount      int                 `json:"sampleCount"`
	Window           time.Duration       `json:"window"`
	LatestSampleAt   time.Time           `json:"latestSampleAt,omitempty"`
	RSquared         float64             `json:"rSquared,omitempty"`
	Confidence       ForecastConfidence  `json:"confidence,omitempty"`
	Conditions       []CapacityCondition `json:"conditions,omitempty"`
}

func Forecast(providerID string, measure CapacityMeasure, samples []MetricSample, horizon time.Duration, now time.Time, policy ForecastPolicy) (CapacityForecast, error) {
	if providerID == "" {
		return CapacityForecast{}, fmt.Errorf("forecast provider is required")
	}
	if !measure.Valid() {
		return CapacityForecast{}, fmt.Errorf("unsupported forecast measure %q", measure)
	}
	if horizon <= 0 {
		return CapacityForecast{}, fmt.Errorf("forecast horizon must be positive")
	}
	if policy.MinimumSamples <= 0 {
		policy.MinimumSamples = 12
	}
	if policy.MinimumWindow <= 0 {
		policy.MinimumWindow = 6 * time.Hour
	}
	if policy.MaximumWindow <= 0 {
		policy.MaximumWindow = 90 * 24 * time.Hour
	}
	if policy.MaximumAge <= 0 {
		policy.MaximumAge = 15 * time.Minute
	}
	if policy.MaximumSamples <= 0 {
		policy.MaximumSamples = 10000
	}
	if policy.FutureTolerance <= 0 {
		policy.FutureTolerance = time.Minute
	}
	base := CapacityForecast{ProviderID: providerID, Measure: measure, Status: ForecastUnavailable}
	if len(samples) > policy.MaximumSamples {
		return base, fmt.Errorf("forecast sample count %d exceeds maximum %d", len(samples), policy.MaximumSamples)
	}
	samples = append([]MetricSample(nil), samples...)
	sort.Slice(samples, func(i, j int) bool { return samples[i].Timestamp.Before(samples[j].Timestamp) })
	samples = uniqueSamples(samples)
	base.SampleCount = len(samples)
	if len(samples) > 0 {
		base.CurrentBytes = samples[len(samples)-1].Bytes
		base.LatestSampleAt = samples[len(samples)-1].Timestamp
	}
	if len(samples) < policy.MinimumSamples {
		base.Conditions = append(base.Conditions, CapacityCondition{
			Code:    "insufficient-samples",
			Message: fmt.Sprintf("forecast requires at least %d distinct samples", policy.MinimumSamples),
		})
		return base, nil
	}
	if samples[len(samples)-1].Timestamp.After(now.Add(policy.FutureTolerance)) {
		base.Conditions = append(base.Conditions, CapacityCondition{Code: "future-history", Message: "metric history contains samples too far in the future"})
		return base, nil
	}
	window := samples[len(samples)-1].Timestamp.Sub(samples[0].Timestamp)
	base.Window = window
	if window < policy.MinimumWindow {
		base.Conditions = append(base.Conditions, CapacityCondition{Code: "insufficient-window", Message: "metric history window is too short"})
		return base, nil
	}
	if window > policy.MaximumWindow {
		return base, fmt.Errorf("forecast metric window %s exceeds maximum %s", window, policy.MaximumWindow)
	}
	if now.Sub(samples[len(samples)-1].Timestamp) > policy.MaximumAge {
		base.Conditions = append(base.Conditions, CapacityCondition{Code: "stale-history", Message: "latest metric sample is too old"})
		return base, nil
	}
	if policy.MaximumGap > 0 {
		for index := 1; index < len(samples); index++ {
			if samples[index].Timestamp.Sub(samples[index-1].Timestamp) > policy.MaximumGap {
				base.Conditions = append(base.Conditions, CapacityCondition{
					Code: "missing-history", Message: "metric history contains a gap larger than the configured maximum",
				})
				return base, nil
			}
		}
	}
	slopePerSecond, intercept, rSquared := linearRegression(samples)
	projected := intercept + slopePerSecond*samples[len(samples)-1].Timestamp.Sub(samples[0].Timestamp).Seconds() +
		slopePerSecond*horizon.Seconds()
	if projected < 0 {
		projected = 0
	}
	if projected > math.MaxUint64 {
		return base, fmt.Errorf("forecast projection exceeds uint64 capacity")
	}
	base.Status = ForecastAvailable
	base.SlopeBytesPerDay = slopePerSecond * 86400
	base.ProjectedBytes = uint64(math.Round(projected))
	base.ProjectionAt = samples[len(samples)-1].Timestamp.Add(horizon)
	base.RSquared = rSquared
	base.Confidence = forecastConfidence(rSquared, len(samples), window, policy)
	base.Conditions = append(base.Conditions, CapacityCondition{
		Code:    "trend-not-guarantee",
		Message: "projection is a historical trend, not a capacity guarantee",
	})
	return base, nil
}

func ownershipKey(measure CapacityMeasure, dimensions CapacityDimensions) string {
	return strings.Join([]string{
		string(measure), dimensions.ProviderID, dimensions.Driver,
		dimensions.StorageClass, dimensions.Namespace, dimensions.WorkloadKind,
		dimensions.Workload, dimensions.ReclaimPolicy, dimensions.Pool,
		dimensions.Filesystem,
	}, "\x00")
}

func appendUniqueCondition(conditions []CapacityCondition, condition CapacityCondition) []CapacityCondition {
	for _, existing := range conditions {
		if existing.Code == condition.Code {
			return conditions
		}
	}
	return append(conditions, condition)
}

func uniqueSamples(samples []MetricSample) []MetricSample {
	if len(samples) < 2 {
		return samples
	}
	out := samples[:0]
	for _, sample := range samples {
		if len(out) > 0 && sample.Timestamp.Equal(out[len(out)-1].Timestamp) {
			out[len(out)-1] = sample
			continue
		}
		out = append(out, sample)
	}
	return out
}

func linearRegression(samples []MetricSample) (slope, intercept, rSquared float64) {
	origin := samples[0].Timestamp
	var sumX, sumY float64
	for _, sample := range samples {
		sumX += sample.Timestamp.Sub(origin).Seconds()
		sumY += float64(sample.Bytes)
	}
	meanX, meanY := sumX/float64(len(samples)), sumY/float64(len(samples))
	var covariance, varianceX, varianceY float64
	for _, sample := range samples {
		x := sample.Timestamp.Sub(origin).Seconds() - meanX
		y := float64(sample.Bytes) - meanY
		covariance += x * y
		varianceX += x * x
		varianceY += y * y
	}
	if varianceX == 0 {
		return 0, meanY, 0
	}
	slope = covariance / varianceX
	intercept = meanY - slope*meanX
	if varianceY == 0 {
		return slope, intercept, 1
	}
	rSquared = covariance * covariance / (varianceX * varianceY)
	return slope, intercept, rSquared
}

func forecastConfidence(rSquared float64, count int, window time.Duration, policy ForecastPolicy) ForecastConfidence {
	if rSquared >= .9 && count >= policy.MinimumSamples*2 && window >= policy.MinimumWindow*2 {
		return ConfidenceHigh
	}
	if rSquared >= .65 {
		return ConfidenceMedium
	}
	return ConfidenceLow
}
