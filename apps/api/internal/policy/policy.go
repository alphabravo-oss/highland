package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

const (
	Name       = "highland"
	APIVersion = "highland.io/v1alpha1"
	Kind       = "HighlandPolicy"
)

var GVR = schema.GroupVersionResource{Group: "highland.io", Version: "v1alpha1", Resource: "highlandpolicies"}

type StoragePolicy struct {
	AcceptNewOperations           bool     `json:"acceptNewOperations"`
	PortableKubernetesWrites      bool     `json:"portableKubernetesWrites"`
	PortableKubernetesProviderIDs []string `json:"portableKubernetesProviderIds"`
	LonghornWrites                bool     `json:"longhornWrites"`
	RookCephWrites                bool     `json:"rookCephWrites"`
	AllowCephStorageClassDelete   bool     `json:"allowCephStorageClassDelete"`
	AllowCephPoolDelete           bool     `json:"allowCephPoolDelete"`
}

type Ceiling struct {
	PortableKubernetesWrites    bool `json:"portableKubernetesWrites"`
	LonghornWrites              bool `json:"longhornWrites"`
	RookCephWrites              bool `json:"rookCephWrites"`
	AllowCephStorageClassDelete bool `json:"allowCephStorageClassDelete"`
	AllowCephPoolDelete         bool `json:"allowCephPoolDelete"`
}

type Condition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	Reason             string    `json:"reason,omitempty"`
	Message            string    `json:"message,omitempty"`
	LastTransitionTime time.Time `json:"lastTransitionTime"`
}

type ChangeMetadata struct {
	Username  string    `json:"username,omitempty"`
	RequestID string    `json:"requestId,omitempty"`
	At        time.Time `json:"at,omitempty"`
}

type Snapshot struct {
	Requested          StoragePolicy  `json:"requested"`
	Effective          StoragePolicy  `json:"effective"`
	Ceiling            Ceiling        `json:"ceiling"`
	Conditions         []Condition    `json:"conditions"`
	Source             string         `json:"source"`
	Generation         int64          `json:"generation"`
	ResourceVersion    string         `json:"resourceVersion,omitempty"`
	ObservedGeneration int64          `json:"observedGeneration"`
	ObservedAt         time.Time      `json:"observedAt"`
	Stale              bool           `json:"stale"`
	Partial            bool           `json:"partial"`
	LastChange         ChangeMetadata `json:"lastChange,omitempty"`
}

type Publisher interface {
	PublishHighlandChange(eventType string, keys []string, resource, name string, entity any)
}

type Observer interface {
	PolicyObserved(capabilities map[string]bool, generation int64, observedAt time.Time, ceilingMismatch bool)
	PolicyUpdate(result string)
}

type Config struct {
	Dynamic         dynamic.Interface
	Namespace       string
	Enabled         bool
	Ceiling         Ceiling
	StaticRequested StoragePolicy
	Publisher       Publisher
	Observer        Observer
	Now             func() time.Time
}

type Manager struct {
	dynamic   dynamic.Interface
	namespace string
	enabled   bool
	ceiling   Ceiling
	publisher Publisher
	observer  Observer
	now       func() time.Time
	changeMu  sync.RWMutex
	onChange  func()
	snapshot  atomic.Value
}

func NewManager(cfg Config) (*Manager, error) {
	if cfg.Namespace == "" {
		return nil, fmt.Errorf("policy namespace is required")
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	manager := &Manager{
		dynamic: cfg.Dynamic, namespace: cfg.Namespace, enabled: cfg.Enabled,
		ceiling: cfg.Ceiling, publisher: cfg.Publisher, observer: cfg.Observer, now: now,
	}
	cfg.StaticRequested = Normalize(cfg.StaticRequested)
	if cfg.Enabled {
		manager.snapshot.Store(Snapshot{
			Ceiling: cfg.Ceiling, Source: "unavailable", ObservedAt: now(),
			Stale: true, Partial: true,
			Conditions: []Condition{{
				Type: "Ready", Status: "False", Reason: "PolicyNotObserved",
				Message:            "runtime policy has not been observed; storage writes fail closed",
				LastTransitionTime: now(),
			}},
		})
	} else {
		effective, conditions := Intersect(cfg.StaticRequested, cfg.Ceiling, now())
		snapshot := Snapshot{
			Requested: cfg.StaticRequested, Effective: effective, Ceiling: cfg.Ceiling,
			Conditions: conditions, Source: "static-helm", ObservedAt: now(),
		}
		manager.snapshot.Store(snapshot)
		if manager.observer != nil {
			manager.observer.PolicyObserved(capabilityMap(snapshot.Effective), 0, snapshot.ObservedAt, len(CeilingViolations(snapshot.Requested, snapshot.Ceiling)) > 0)
		}
	}
	return manager, nil
}

func (m *Manager) Enabled() bool { return m != nil && m.enabled }

func (m *Manager) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{Source: "unavailable", Stale: true, Partial: true}
	}
	return cloneSnapshot(m.snapshot.Load().(Snapshot))
}

func (m *Manager) Start(ctx context.Context) error {
	if m == nil || !m.enabled {
		return nil
	}
	if m.dynamic == nil {
		return fmt.Errorf("dynamic Kubernetes client is unavailable")
	}
	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(m.dynamic, 10*time.Minute, m.namespace, nil)
	informer := factory.ForResource(GVR).Informer()
	_ = informer.SetWatchErrorHandler(func(_ *cache.Reflector, err error) {
		m.failClosed("PolicyWatchError", "runtime policy watch failed: "+sanitizePolicyError(err))
	})
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    m.observe,
		UpdateFunc: func(_, current any) { m.observe(current) },
		DeleteFunc: func(any) { m.failClosed("PolicyDeleted", "runtime policy was deleted") },
	})
	factory.Start(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), informer.HasSynced) {
		return fmt.Errorf("HighlandPolicy informer did not synchronize")
	}
	return nil
}

func (m *Manager) observe(raw any) {
	object, ok := raw.(*unstructured.Unstructured)
	if !ok || object.GetName() != Name {
		return
	}
	requested, err := requestedFromObject(object)
	if err != nil {
		m.failClosed("PolicyInvalid", err.Error())
		return
	}
	now := m.now()
	effective, conditions := Intersect(requested, m.ceiling, now)
	snapshot := Snapshot{
		Requested: requested, Effective: effective, Ceiling: m.ceiling,
		Conditions: conditions, Source: "runtime-policy",
		Generation: object.GetGeneration(), ResourceVersion: object.GetResourceVersion(),
		ObservedGeneration: object.GetGeneration(), ObservedAt: now,
		LastChange: ChangeMetadata{
			Username:  object.GetAnnotations()["highland.io/last-changed-by"],
			RequestID: object.GetAnnotations()["highland.io/last-change-request-id"],
			At:        parseTime(object.GetAnnotations()["highland.io/last-changed-at"]),
		},
	}
	previous := m.Snapshot()
	m.snapshot.Store(snapshot)
	if m.observer != nil {
		m.observer.PolicyObserved(capabilityMap(snapshot.Effective), snapshot.ObservedGeneration, snapshot.ObservedAt, len(CeilingViolations(snapshot.Requested, snapshot.Ceiling)) > 0)
		if providerObserver, ok := m.observer.(interface{ PolicyProvidersObserved([]string) }); ok {
			providerObserver.PolicyProvidersObserved(append([]string(nil), snapshot.Effective.PortableKubernetesProviderIDs...))
		}
	}
	if m.publisher != nil && !reflect.DeepEqual(previous, snapshot) {
		m.publisher.PublishHighlandChange("policy.updated", []string{"storage-actions", "admin-storage-policy"}, "policy", Name, snapshot)
	}
	if onChange := m.changeCallback(); onChange != nil && !reflect.DeepEqual(previous.Effective, snapshot.Effective) {
		onChange()
	}
	go m.updateStatus(object.DeepCopy(), snapshot)
}

func capabilityMap(value StoragePolicy) map[string]bool {
	return map[string]bool{
		"accept-new-operations":    value.AcceptNewOperations,
		"portable-kubernetes":      value.PortableKubernetesWrites,
		"longhorn":                 value.LonghornWrites,
		"rook-ceph":                value.RookCephWrites,
		"ceph-storageclass-delete": value.AllowCephStorageClassDelete,
		"ceph-pool-delete":         value.AllowCephPoolDelete,
	}
}

func (m *Manager) SetOnChange(onChange func()) {
	if m != nil {
		m.changeMu.Lock()
		defer m.changeMu.Unlock()
		m.onChange = onChange
	}
}

func (m *Manager) failClosed(reason, message string) {
	now := m.now()
	previous := m.Snapshot()
	snapshot := Snapshot{
		Ceiling: m.ceiling, Source: "unavailable", ObservedAt: now, Stale: true, Partial: true,
		Conditions: []Condition{{
			Type: "Ready", Status: "False", Reason: reason, Message: message, LastTransitionTime: now,
		}},
	}
	m.snapshot.Store(snapshot)
	if m.observer != nil {
		m.observer.PolicyObserved(capabilityMap(StoragePolicy{}), 0, now, false)
		if providerObserver, ok := m.observer.(interface{ PolicyProvidersObserved([]string) }); ok {
			providerObserver.PolicyProvidersObserved([]string{})
		}
	}
	if m.publisher != nil && !reflect.DeepEqual(previous, snapshot) {
		m.publisher.PublishHighlandChange("policy.updated", []string{"storage-actions", "admin-storage-policy"}, "policy", Name, snapshot)
	}
	if onChange := m.changeCallback(); onChange != nil && !reflect.DeepEqual(previous.Effective, snapshot.Effective) {
		onChange()
	}
}

func (m *Manager) changeCallback() func() {
	m.changeMu.RLock()
	defer m.changeMu.RUnlock()
	return m.onChange
}

func (m *Manager) updateStatus(object *unstructured.Unstructured, snapshot Snapshot) {
	status := map[string]any{
		"effective":          structMap(snapshot.Effective),
		"observedGeneration": snapshot.ObservedGeneration,
		"conditions":         sliceMaps(snapshot.Conditions),
	}
	current, _, _ := unstructured.NestedMap(object.Object, "status")
	if statusSemanticallyEqual(current, status) {
		return
	}
	object.Object["status"] = status
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = m.dynamic.Resource(GVR).Namespace(m.namespace).UpdateStatus(ctx, object, metav1.UpdateOptions{})
}

func (m *Manager) Update(ctx context.Context, requested StoragePolicy, resourceVersion, username, requestID string) (*unstructured.Unstructured, error) {
	if m == nil || !m.enabled {
		return nil, ErrControlDisabled
	}
	if m.dynamic == nil {
		return nil, fmt.Errorf("runtime policy store is unavailable")
	}
	requested = Normalize(requested)
	if err := Validate(requested); err != nil {
		return nil, err
	}
	resource := m.dynamic.Resource(GVR).Namespace(m.namespace)
	current, err := resource.Get(ctx, Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get HighlandPolicy: %w", err)
	}
	if resourceVersion == "" || current.GetResourceVersion() != resourceVersion {
		return nil, ErrStale
	}
	if violations := CeilingViolations(requested, m.ceiling); len(violations) > 0 {
		return nil, &CeilingError{Capabilities: violations}
	}
	if err := unstructured.SetNestedMap(current.Object, structMap(requested), "spec", "storage"); err != nil {
		return nil, err
	}
	annotations := current.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations["highland.io/last-changed-by"] = username
	annotations["highland.io/last-change-request-id"] = requestID
	annotations["highland.io/last-changed-at"] = m.now().Format(time.RFC3339Nano)
	current.SetAnnotations(annotations)
	updated, err := resource.Update(ctx, current, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("update HighlandPolicy: %w", err)
	}
	return updated, nil
}

func (m *Manager) WaitObserved(ctx context.Context, generation int64) (Snapshot, error) {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		snapshot := m.Snapshot()
		if snapshot.ObservedGeneration >= generation && snapshot.Source == "runtime-policy" {
			return snapshot, nil
		}
		select {
		case <-ctx.Done():
			return snapshot, ctx.Err()
		case <-ticker.C:
		}
	}
}

var (
	ErrControlDisabled = errors.New("runtime policy control is disabled")
	ErrStale           = errors.New("policy resource version is stale")
)

type CeilingError struct{ Capabilities []string }

func (e *CeilingError) Error() string {
	return "requested policy exceeds installed permission ceiling: " + strings.Join(e.Capabilities, ", ")
}

func Validate(requested StoragePolicy) error {
	if !requested.PortableKubernetesWrites && len(requested.PortableKubernetesProviderIDs) > 0 {
		return fmt.Errorf("portable Kubernetes provider IDs require portable Kubernetes writes")
	}
	requested = Normalize(requested)
	if !requested.AcceptNewOperations && (requested.PortableKubernetesWrites || requested.LonghornWrites || requested.RookCephWrites || requested.AllowCephStorageClassDelete || requested.AllowCephPoolDelete) {
		return fmt.Errorf("provider writes require acceptNewOperations")
	}
	if requested.AllowCephStorageClassDelete && !requested.RookCephWrites {
		return fmt.Errorf("Ceph StorageClass deletion requires Rook/Ceph writes")
	}
	if requested.AllowCephPoolDelete && !requested.RookCephWrites {
		return fmt.Errorf("Ceph pool deletion requires Rook/Ceph writes")
	}
	if requested.PortableKubernetesWrites && len(requested.PortableKubernetesProviderIDs) == 0 {
		return fmt.Errorf("portable Kubernetes writes require at least one provider ID")
	}
	if len(requested.PortableKubernetesProviderIDs) > 64 {
		return fmt.Errorf("portable Kubernetes provider IDs cannot contain more than 64 entries")
	}
	for index, providerID := range requested.PortableKubernetesProviderIDs {
		if providerID == "*" {
			if len(requested.PortableKubernetesProviderIDs) != 1 {
				return fmt.Errorf("legacy wildcard provider ID must be the only provider scope")
			}
			continue
		}
		if messages := validation.IsDNS1123Subdomain(providerID); len(messages) > 0 {
			return fmt.Errorf("portable Kubernetes provider ID %d is invalid", index)
		}
	}
	return nil
}

func Intersect(requested StoragePolicy, ceiling Ceiling, now time.Time) (StoragePolicy, []Condition) {
	requested = Normalize(requested)
	effective := StoragePolicy{
		AcceptNewOperations:           requested.AcceptNewOperations,
		PortableKubernetesWrites:      requested.AcceptNewOperations && requested.PortableKubernetesWrites && ceiling.PortableKubernetesWrites,
		PortableKubernetesProviderIDs: []string{},
		LonghornWrites:                requested.AcceptNewOperations && requested.LonghornWrites && ceiling.LonghornWrites,
		RookCephWrites:                requested.AcceptNewOperations && requested.RookCephWrites && ceiling.RookCephWrites,
		AllowCephStorageClassDelete:   requested.AcceptNewOperations && requested.RookCephWrites && requested.AllowCephStorageClassDelete && ceiling.RookCephWrites && ceiling.AllowCephStorageClassDelete,
		AllowCephPoolDelete:           requested.AcceptNewOperations && requested.RookCephWrites && requested.AllowCephPoolDelete && ceiling.RookCephWrites && ceiling.AllowCephPoolDelete,
	}
	if effective.PortableKubernetesWrites {
		effective.PortableKubernetesProviderIDs = append([]string(nil), requested.PortableKubernetesProviderIDs...)
	}
	violations := CeilingViolations(requested, ceiling)
	conditions := []Condition{{
		Type: "Ready", Status: "True", Reason: "PolicyObserved",
		Message: "runtime storage policy is observed", LastTransitionTime: now,
	}}
	if len(violations) > 0 {
		conditions = append(conditions, Condition{
			Type: "CeilingSatisfied", Status: "False", Reason: "PermissionCeiling",
			Message:            "requested capabilities are not installed: " + strings.Join(violations, ", "),
			LastTransitionTime: now,
		})
	} else {
		conditions = append(conditions, Condition{
			Type: "CeilingSatisfied", Status: "True", Reason: "WithinCeiling",
			Message:            "requested capabilities are within the installed permission ceiling",
			LastTransitionTime: now,
		})
	}
	return effective, conditions
}

func CeilingViolations(requested StoragePolicy, ceiling Ceiling) []string {
	var result []string
	if requested.PortableKubernetesWrites && !ceiling.PortableKubernetesWrites {
		result = append(result, "portableKubernetesWrites")
	}
	if requested.LonghornWrites && !ceiling.LonghornWrites {
		result = append(result, "longhornWrites")
	}
	if requested.RookCephWrites && !ceiling.RookCephWrites {
		result = append(result, "rookCephWrites")
	}
	if requested.AllowCephStorageClassDelete && !ceiling.AllowCephStorageClassDelete {
		result = append(result, "allowCephStorageClassDelete")
	}
	if requested.AllowCephPoolDelete && !ceiling.AllowCephPoolDelete {
		result = append(result, "allowCephPoolDelete")
	}
	return result
}

func requestedFromObject(object *unstructured.Unstructured) (StoragePolicy, error) {
	raw, found, err := unstructured.NestedMap(object.Object, "spec", "storage")
	if err != nil || !found {
		return StoragePolicy{}, fmt.Errorf("spec.storage is required")
	}
	encoded, _ := json.Marshal(raw)
	var requested StoragePolicy
	if err := json.Unmarshal(encoded, &requested); err != nil {
		return StoragePolicy{}, fmt.Errorf("decode spec.storage: %w", err)
	}
	requested = Normalize(requested)
	if err := Validate(requested); err != nil {
		return StoragePolicy{}, err
	}
	return requested, nil
}

func structMap(value any) map[string]any {
	encoded, _ := json.Marshal(value)
	result := map[string]any{}
	_ = json.Unmarshal(encoded, &result)
	return result
}

func sliceMaps(values []Condition) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		result = append(result, structMap(value))
	}
	return result
}

func parseTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.Requested.PortableKubernetesProviderIDs = cloneStrings(snapshot.Requested.PortableKubernetesProviderIDs)
	snapshot.Effective.PortableKubernetesProviderIDs = cloneStrings(snapshot.Effective.PortableKubernetesProviderIDs)
	snapshot.Conditions = append([]Condition(nil), snapshot.Conditions...)
	return snapshot
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string{}, values...)
}

// Normalize gives policy lists one deterministic representation. A nil list on
// an enabled legacy object means the pre-provider-scope behavior and is
// represented explicitly as the compatibility wildcard. New clients always
// write an explicit list.
func Normalize(value StoragePolicy) StoragePolicy {
	if value.PortableKubernetesWrites && value.PortableKubernetesProviderIDs == nil {
		value.PortableKubernetesProviderIDs = []string{"*"}
	}
	if !value.PortableKubernetesWrites {
		value.PortableKubernetesProviderIDs = []string{}
		return value
	}
	seen := map[string]struct{}{}
	providers := make([]string, 0, len(value.PortableKubernetesProviderIDs))
	for _, providerID := range value.PortableKubernetesProviderIDs {
		providerID = strings.TrimSpace(strings.ToLower(providerID))
		if providerID == "" {
			continue
		}
		if _, exists := seen[providerID]; exists {
			continue
		}
		seen[providerID] = struct{}{}
		providers = append(providers, providerID)
	}
	sort.Strings(providers)
	value.PortableKubernetesProviderIDs = providers
	return value
}

func Equal(left, right StoragePolicy) bool {
	return reflect.DeepEqual(Normalize(left), Normalize(right))
}

func (value StoragePolicy) AllowsPortableProvider(providerID string) bool {
	value = Normalize(value)
	if !value.AcceptNewOperations || !value.PortableKubernetesWrites || providerID == "" {
		return false
	}
	for _, allowed := range value.PortableKubernetesProviderIDs {
		if allowed == "*" || allowed == providerID {
			return true
		}
	}
	return false
}

func sanitizePolicyError(err error) string {
	if err == nil {
		return "unknown error"
	}
	message := strings.ReplaceAll(strings.TrimSpace(err.Error()), "\n", " ")
	if len(message) > 256 {
		message = message[:256]
	}
	return message
}

func statusSemanticallyEqual(current, desired map[string]any) bool {
	return reflect.DeepEqual(normalizePolicyStatus(current), normalizePolicyStatus(desired))
}

func normalizePolicyStatus(value map[string]any) map[string]any {
	encoded, _ := json.Marshal(value)
	normalized := map[string]any{}
	_ = json.Unmarshal(encoded, &normalized)
	conditions, _ := normalized["conditions"].([]any)
	for _, raw := range conditions {
		if condition, ok := raw.(map[string]any); ok {
			delete(condition, "lastTransitionTime")
		}
	}
	return normalized
}
