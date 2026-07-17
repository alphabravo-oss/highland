package storage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/highland-io/highland/apps/api/internal/storage/insights"
)

const (
	defaultPageSize = 100
	maxPageSize     = 500
)

type InventoryReader interface {
	Ready() bool
	LastSync() time.Time
	SnapshotAvailable() bool
	DiscoveredDriverNames() ([]string, error)
	Drivers(context.Context) ([]DriverSummary, error)
	StorageClasses() ([]StorageClassSummary, error)
	Claims(context.Context) ([]ClaimSummary, error)
	Volumes(context.Context) ([]PersistentVolumeSummary, error)
	Snapshots() ([]SnapshotSummary, error)
	Attachments() ([]AttachmentSummary, error)
	Capacities() ([]CapacitySummary, error)
	Events() ([]StorageEvent, error)
}

type coreConditionReader interface {
	CoreConditions() []Condition
}

// HTTPAPI serves provider-neutral and typed provider read endpoints.
type HTTPAPI struct {
	inventory      InventoryReader
	registry       *Registry
	observer       Observer
	context        *ContextEngine
	insightMu      sync.Mutex
	insightHistory *insightHistory
}

func (a *HTTPAPI) SetObserver(observer Observer) { a.observer = observer }

func (a *HTTPAPI) SetContextSources(operations ContextOperationSource, audits ContextAuditSource) {
	if a == nil || a.context == nil {
		return
	}
	a.context.SetSources(operations, audits)
}

// ImpactAnalyzer returns the dependency engine used by the public impact API
// so operation preflight can consume the identical result.
func (a *HTTPAPI) ImpactAnalyzer() ImpactAnalyzer {
	if a == nil {
		return nil
	}
	return a.context
}

// Ready reports whether the required Kubernetes informer cache has synced.
func (a *HTTPAPI) Ready() bool { return a != nil && a.inventory != nil && a.inventory.Ready() }

// Status returns bounded storage-core and provider health facts for readiness
// and the Status page without exposing inventory objects.
func (a *HTTPAPI) Status(ctx context.Context) map[string]any {
	status := map[string]any{"ready": a.Ready()}
	if a == nil || a.inventory == nil {
		status["condition"] = "storage inventory is not configured"
		return status
	}
	status["lastSync"] = a.inventory.LastSync()
	status["snapshotApi"] = a.inventory.SnapshotAvailable()
	if reader, ok := a.inventory.(coreConditionReader); ok {
		status["conditions"] = reader.CoreConditions()
	}
	drivers, err := a.inventory.DiscoveredDriverNames()
	if err != nil {
		status["condition"] = err.Error()
		return status
	}
	providers, observedAt, stale := a.registry.DescriptorSnapshot(ctx, drivers)
	status["providers"] = providers
	status["providersObservedAt"] = observedAt
	status["providersStale"] = stale
	return status
}

// ProviderHealthy reports configured provider health for readiness policy.
func (a *HTTPAPI) ProviderHealthy(ctx context.Context, id string) bool {
	if a == nil || a.registry == nil {
		return false
	}
	provider, ok := a.registry.Provider(id)
	if !ok {
		return false
	}
	return provider.Health(ctx).Status != SeverityError
}

func NewHTTPAPI(inventory InventoryReader, registry *Registry) *HTTPAPI {
	if registry == nil {
		registry = NewRegistry()
	}
	return &HTTPAPI{
		inventory: inventory, registry: registry, context: NewContextEngine(inventory, registry),
		insightHistory: &insightHistory{samples: map[string][]insights.MetricSample{}},
	}
}

func (a *HTTPAPI) Mount(r chi.Router) {
	r.Get("/api/v1/storage/providers", a.ListProviders)
	r.Get("/api/v1/storage/providers/{providerId}", a.GetProvider)
	r.Get("/api/v1/storage/drivers", a.ListDrivers)
	r.Get("/api/v1/storage/classes", a.ListStorageClasses)
	r.Get("/api/v1/storage/claims", a.ListClaims)
	r.Get("/api/v1/storage/claims/{namespace}/{name}", a.GetClaim)
	r.Get("/api/v1/storage/volumes", a.ListVolumes)
	r.Get("/api/v1/storage/volumes/{name}", a.GetVolume)
	r.Get("/api/v1/storage/snapshots", a.ListSnapshots)
	r.Get("/api/v1/storage/attachments", a.ListAttachments)
	r.Get("/api/v1/storage/capacity", a.ListCapacity)
	r.Get("/api/v1/storage/events", a.ListEvents)
	r.Get("/api/v1/storage/timeline", a.GetStorageTimeline)
	r.Get("/api/v1/storage/capacity/ownership", a.GetCapacityOwnership)
	r.Get("/api/v1/storage/comparison", a.GetProviderComparison)
	r.Get("/api/v1/storage/remediations", a.GetStorageRemediations)
	r.Get("/api/v1/storage/relationships", a.ListRelationships)
	r.Get("/api/v1/storage/resources/{kind}/{id}/relationships", a.GetResourceRelationships)
	r.Get("/api/v1/storage/impact", a.GetImpact)
	r.Get("/api/v1/providers/{providerId}/summary", a.GetProviderSummary)
	r.Get("/api/v1/providers/{providerId}/health", a.GetProviderHealth)
	r.Get("/api/v1/providers/{providerId}/relationships", a.GetProviderRelationships)
	r.Get("/api/v1/providers/{providerId}/drift", a.GetProviderDrift)
	r.Get("/api/v1/providers/{providerId}/capacity/forecast", a.GetCapacityForecast)
	r.Get("/api/v1/providers/{providerId}/resources/{kind}", a.ListProviderResources)
	r.Get("/api/v1/providers/{providerId}/resources/{kind}/{id}", a.GetProviderResource)
}

func (a *HTTPAPI) ListProviders(w http.ResponseWriter, r *http.Request) {
	if !a.ensureReady(w, r) {
		return
	}
	drivers, err := a.inventory.DiscoveredDriverNames()
	if err != nil {
		a.writeInventoryError(w, r, err)
		return
	}
	providers, observedAt, stale := a.registry.DescriptorSnapshot(r.Context(), drivers)
	for _, provider := range providers {
		a.observerSetProvider(provider)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": providers,
		"meta": map[string]any{
			"lastSync": a.inventory.LastSync(), "snapshotApi": a.inventory.SnapshotAvailable(),
			"conditions": a.coreConditions(), "observedAt": observedAt, "stale": stale, "partial": false,
			"requestId": chimw.GetReqID(r.Context()),
		},
	})
}

func (a *HTTPAPI) GetProvider(w http.ResponseWriter, r *http.Request) {
	if !a.ensureReady(w, r) {
		return
	}
	id := chi.URLParam(r, "providerId")
	drivers, err := a.inventory.DiscoveredDriverNames()
	if err != nil {
		a.writeInventoryError(w, r, err)
		return
	}
	providers, _, _ := a.registry.DescriptorSnapshot(r.Context(), drivers)
	for _, provider := range providers {
		if provider.ID == id {
			writeJSON(w, http.StatusOK, provider)
			return
		}
	}
	writeAPIError(w, r, http.StatusNotFound, "PROVIDER_NOT_FOUND", "storage provider not found", false, map[string]any{"providerId": id})
}

func (a *HTTPAPI) GetProviderHealth(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "providerId")
	if provider, ok := a.registry.Provider(id); ok {
		writeJSON(w, http.StatusOK, provider.Health(r.Context()))
		return
	}
	writeAPIError(w, r, http.StatusNotFound, "PROVIDER_NOT_FOUND", "storage provider not found", false, map[string]any{"providerId": id})
}

func (a *HTTPAPI) GetProviderSummary(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "providerId")
	provider, ok := a.registry.Provider(id)
	if !ok {
		a.GetProvider(w, r)
		return
	}
	reader, ok := provider.(ProviderSummaryReader)
	if !ok {
		a.GetProvider(w, r)
		return
	}
	started := time.Now()
	summary, err := reader.ProviderSummary(r.Context())
	if a.observer != nil {
		a.observer.ObserveStorageProviderRequest(id, "summary", time.Since(started))
	}
	if err != nil {
		if a.observer != nil {
			a.observer.IncStorageProviderError(id, "read_failed")
		}
		writeAPIError(w, r, http.StatusServiceUnavailable, "PROVIDER_READ_FAILED", err.Error(), true, nil)
		return
	}
	if typed, ok := summary.(map[string]any); ok && a.context != nil {
		if report, driftErr := a.context.driftReport(r.Context(), id); driftErr == nil {
			typed["driftSummary"] = report.Summary
			typed["driftObservedAt"] = report.ObservedAt
			typed["driftIncomplete"] = report.Incomplete
		}
	}
	writeJSON(w, http.StatusOK, summary)
}

func (a *HTTPAPI) ListDrivers(w http.ResponseWriter, r *http.Request) {
	listResponse(a, w, r, "drivers", func() ([]DriverSummary, error) { return a.inventory.Drivers(r.Context()) }, func(v DriverSummary, f listFilters) bool {
		return matches(f.Provider, v.ProviderID) && matches(f.Driver, v.Name) && search(f.Search, v.Name, v.ProviderID)
	})
}

func (a *HTTPAPI) ListStorageClasses(w http.ResponseWriter, r *http.Request) {
	listResponse(a, w, r, "classes", a.inventory.StorageClasses, func(v StorageClassSummary, f listFilters) bool {
		return matches(f.Provider, v.ProviderID) && matches(f.Driver, v.Provisioner) && search(f.Search, v.Name, v.Provisioner, v.ProviderID)
	})
}

func (a *HTTPAPI) ListClaims(w http.ResponseWriter, r *http.Request) {
	listResponse(a, w, r, "claims", func() ([]ClaimSummary, error) { return a.inventory.Claims(r.Context()) }, func(v ClaimSummary, f listFilters) bool {
		return matches(f.Provider, v.ProviderID) && matches(f.Driver, v.Driver) && matches(f.Namespace, v.Namespace) && matches(f.Status, v.Phase) &&
			search(f.Search, v.Namespace, v.Name, v.PVName, v.StorageClass, v.Driver)
	})
}

func (a *HTTPAPI) GetClaim(w http.ResponseWriter, r *http.Request) {
	if !a.ensureReady(w, r) {
		return
	}
	namespace, name := chi.URLParam(r, "namespace"), chi.URLParam(r, "name")
	claims, err := a.inventory.Claims(r.Context())
	if err != nil {
		a.writeInventoryError(w, r, err)
		return
	}
	for _, claim := range claims {
		if claim.Namespace == namespace && claim.Name == name {
			writeJSON(w, http.StatusOK, claim)
			return
		}
	}
	writeAPIError(w, r, http.StatusNotFound, "CLAIM_NOT_FOUND", "persistent volume claim not found", false, map[string]any{"namespace": namespace, "name": name})
}

func (a *HTTPAPI) ListVolumes(w http.ResponseWriter, r *http.Request) {
	listResponse(a, w, r, "volumes", func() ([]PersistentVolumeSummary, error) { return a.inventory.Volumes(r.Context()) }, func(v PersistentVolumeSummary, f listFilters) bool {
		return matches(f.Provider, v.ProviderID) && matches(f.Driver, v.Driver) && matches(f.Namespace, v.ClaimNamespace) && matches(f.Status, v.Phase) &&
			search(f.Search, v.Name, v.ClaimNamespace, v.ClaimName, v.StorageClass, v.Driver)
	})
}

func (a *HTTPAPI) GetVolume(w http.ResponseWriter, r *http.Request) {
	if !a.ensureReady(w, r) {
		return
	}
	name := chi.URLParam(r, "name")
	volumes, err := a.inventory.Volumes(r.Context())
	if err != nil {
		a.writeInventoryError(w, r, err)
		return
	}
	for _, volume := range volumes {
		if volume.Name == name {
			writeJSON(w, http.StatusOK, volume)
			return
		}
	}
	writeAPIError(w, r, http.StatusNotFound, "VOLUME_NOT_FOUND", "persistent volume not found", false, map[string]any{"name": name})
}

func (a *HTTPAPI) ListSnapshots(w http.ResponseWriter, r *http.Request) {
	listResponse(a, w, r, "snapshots", a.inventory.Snapshots, func(v SnapshotSummary, f listFilters) bool {
		return matches(f.Provider, v.ProviderID) && matches(f.Driver, v.Driver) && matches(f.Namespace, v.Namespace) &&
			search(f.Search, v.Namespace, v.Name, v.SourcePVC, v.SnapshotClass, v.Driver)
	})
}

func (a *HTTPAPI) ListAttachments(w http.ResponseWriter, r *http.Request) {
	listResponse(a, w, r, "attachments", a.inventory.Attachments, func(v AttachmentSummary, f listFilters) bool {
		status := "detached"
		if v.Attached {
			status = "attached"
		}
		return matches(f.Provider, v.ProviderID) && matches(f.Driver, v.Driver) && matches(f.Status, status) && search(f.Search, v.Name, v.PVName, v.NodeName, v.Driver)
	})
}

func (a *HTTPAPI) ListCapacity(w http.ResponseWriter, r *http.Request) {
	listResponse(a, w, r, "capacity", a.inventory.Capacities, func(v CapacitySummary, f listFilters) bool {
		return matches(f.Provider, v.ProviderID) && matches(f.Driver, v.Driver) && search(f.Search, v.StorageClass, v.Driver, v.ProviderID)
	})
}

func (a *HTTPAPI) ListEvents(w http.ResponseWriter, r *http.Request) {
	listResponse(a, w, r, "events", a.inventory.Events, func(v StorageEvent, f listFilters) bool {
		return matches(f.Namespace, v.Namespace) && matches(f.Status, v.Type) && search(f.Search, v.Reason, v.Message, v.RegardingKind, v.RegardingName)
	})
}

func (a *HTTPAPI) ListProviderResources(w http.ResponseWriter, r *http.Request) {
	providerID, kind := chi.URLParam(r, "providerId"), chi.URLParam(r, "kind")
	provider, ok := a.registry.Provider(providerID)
	if !ok {
		writeAPIError(w, r, http.StatusNotFound, "PROVIDER_NOT_FOUND", "storage provider not found", false, nil)
		return
	}
	reader, ok := provider.(ProviderResourceReader)
	if !ok || !contains(reader.ResourceKinds(r.Context()), kind) {
		writeAPIError(w, r, http.StatusNotFound, "RESOURCE_KIND_UNSUPPORTED", "provider resource kind is not supported", false, map[string]any{"kind": kind})
		return
	}
	page, err := parsePageRequest(r)
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "INVALID_PAGE", err.Error(), false, nil)
		return
	}
	started := time.Now()
	data, meta, err := reader.ListProviderResources(r.Context(), kind, page)
	if a.observer != nil {
		a.observer.ObserveStorageProviderRequest(providerID, "resource_list", time.Since(started))
	}
	if err != nil {
		if a.observer != nil {
			a.observer.IncStorageProviderError(providerID, "read_failed")
		}
		writeAPIError(w, r, http.StatusBadGateway, "PROVIDER_READ_FAILED", err.Error(), true, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "page": meta, "meta": a.responseMeta(r, nil)})
}

func (a *HTTPAPI) GetProviderResource(w http.ResponseWriter, r *http.Request) {
	providerID, kind, id := chi.URLParam(r, "providerId"), chi.URLParam(r, "kind"), chi.URLParam(r, "id")
	provider, ok := a.registry.Provider(providerID)
	if !ok {
		writeAPIError(w, r, http.StatusNotFound, "PROVIDER_NOT_FOUND", "storage provider not found", false, nil)
		return
	}
	reader, ok := provider.(ProviderResourceReader)
	if !ok || !contains(reader.ResourceKinds(r.Context()), kind) {
		writeAPIError(w, r, http.StatusNotFound, "RESOURCE_KIND_UNSUPPORTED", "provider resource kind is not supported", false, nil)
		return
	}
	started := time.Now()
	data, err := reader.GetProviderResource(r.Context(), kind, id)
	if a.observer != nil {
		a.observer.ObserveStorageProviderRequest(providerID, "resource_get", time.Since(started))
	}
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeAPIError(w, r, http.StatusNotFound, "PROVIDER_RESOURCE_NOT_FOUND", "provider resource not found", false, nil)
			return
		}
		if a.observer != nil {
			a.observer.IncStorageProviderError(providerID, "read_failed")
		}
		writeAPIError(w, r, http.StatusBadGateway, "PROVIDER_READ_FAILED", err.Error(), true, nil)
		return
	}
	writeJSON(w, http.StatusOK, data)
}

var ErrNotFound = errors.New("not found")

type listFilters struct {
	Provider  string
	Driver    string
	Namespace string
	Status    string
	Search    string
}

func filtersFromRequest(r *http.Request) listFilters {
	q := r.URL.Query()
	return listFilters{
		Provider: strings.TrimSpace(q.Get("provider")), Driver: strings.TrimSpace(q.Get("driver")),
		Namespace: strings.TrimSpace(q.Get("namespace")), Status: strings.TrimSpace(q.Get("status")),
		Search: strings.TrimSpace(q.Get("search")),
	}
}

func listResponse[T any](a *HTTPAPI, w http.ResponseWriter, r *http.Request, kind string, load func() ([]T, error), keep func(T, listFilters) bool) {
	if !a.ensureReady(w, r) {
		return
	}
	page, err := parsePageRequest(r)
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "INVALID_PAGE", err.Error(), false, nil)
		return
	}
	data, err := load()
	if err != nil {
		a.writeInventoryError(w, r, err)
		return
	}
	if a.observer != nil {
		a.observer.SetStorageInventoryObjects(kind, "all", len(data))
	}
	filters := filtersFromRequest(r)
	filtered := make([]T, 0, len(data))
	for _, item := range data {
		if keep(item, filters) {
			filtered = append(filtered, item)
		}
	}
	paged, meta := paginate(filtered, page)
	conditions := []Condition{}
	conditions = append(conditions, a.coreConditions()...)
	if kind == "snapshots" && !a.inventory.SnapshotAvailable() {
		conditions = append(conditions, Condition{Type: "SnapshotAPIAvailable", Status: "False", Severity: SeverityInfo, Reason: "APIAbsent", Message: "snapshot.storage.k8s.io/v1 is not served; common inventory remains available.", ObservedAt: time.Now().UTC()})
	}
	writeJSON(w, http.StatusOK, Page[T]{Data: paged, Page: meta, Meta: a.responseMeta(r, conditions), Conditions: conditions})
}

func (a *HTTPAPI) responseMeta(r *http.Request, conditions []Condition) ResponseMeta {
	observedAt := time.Now().UTC()
	if a != nil && a.inventory != nil && !a.inventory.LastSync().IsZero() {
		observedAt = a.inventory.LastSync()
	}
	partial := false
	for _, condition := range conditions {
		if condition.Severity == SeverityWarning || condition.Severity == SeverityError {
			partial = true
			break
		}
	}
	return ResponseMeta{
		ObservedAt: observedAt,
		Stale:      time.Since(observedAt) > 2*time.Minute,
		Partial:    partial,
		RequestID:  chimw.GetReqID(r.Context()),
	}
}

func (a *HTTPAPI) coreConditions() []Condition {
	if a == nil || a.inventory == nil {
		return nil
	}
	if reader, ok := a.inventory.(coreConditionReader); ok {
		return reader.CoreConditions()
	}
	return nil
}

func (a *HTTPAPI) observerSetProvider(provider ProviderDescriptor) {
	if a == nil || a.observer == nil {
		return
	}
	a.observer.SetStorageProviderUp(provider.ID, provider.Kind, provider.Health.Status != SeverityError)
}

func (a *HTTPAPI) ensureReady(w http.ResponseWriter, r *http.Request) bool {
	if a == nil || a.inventory == nil {
		writeAPIError(w, r, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "storage inventory is not configured", true, nil)
		return false
	}
	if !a.inventory.Ready() {
		writeAPIError(w, r, http.StatusServiceUnavailable, "STORAGE_CACHE_NOT_READY", "storage inventory cache is not ready", true, nil)
		return false
	}
	return true
}

func (a *HTTPAPI) writeInventoryError(w http.ResponseWriter, r *http.Request, err error) {
	writeAPIError(w, r, http.StatusServiceUnavailable, "STORAGE_READ_FAILED", err.Error(), true, nil)
}

func parsePageRequest(r *http.Request) (PageRequest, error) {
	q := r.URL.Query()
	for _, key := range []string{"provider", "driver", "namespace", "status"} {
		if len(q.Get(key)) > 253 {
			return PageRequest{}, fmt.Errorf("%s filter must not exceed 253 characters", key)
		}
	}
	limit := defaultPageSize
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 || n > maxPageSize {
			return PageRequest{}, fmt.Errorf("limit must be between 1 and %d", maxPageSize)
		}
		limit = n
	}
	page := PageRequest{Limit: limit, Continue: q.Get("continue"), Search: strings.TrimSpace(q.Get("search"))}
	if len(page.Search) > 256 {
		return PageRequest{}, fmt.Errorf("search must not exceed 256 characters")
	}
	if len(page.Continue) > 128 {
		return PageRequest{}, fmt.Errorf("continue token is too long")
	}
	offset, err := continuationOffset(page.Continue)
	if err != nil {
		return PageRequest{}, err
	}
	page.Offset = offset
	return page, nil
}

func paginate[T any](items []T, req PageRequest) ([]T, PageMeta) {
	offset, _ := continuationOffset(req.Continue)
	if offset > len(items) {
		offset = len(items)
	}
	end := offset + req.Limit
	if end > len(items) {
		end = len(items)
	}
	meta := PageMeta{Limit: req.Limit, Total: len(items)}
	if end < len(items) {
		meta.Continue = base64.RawURLEncoding.EncodeToString([]byte("v1:" + strconv.Itoa(end)))
	}
	return items[offset:end], meta
}

func continuationOffset(token string) (int, error) {
	if token == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || !strings.HasPrefix(string(raw), "v1:") {
		return 0, fmt.Errorf("invalid continue token")
	}
	offset, err := strconv.Atoi(strings.TrimPrefix(string(raw), "v1:"))
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid continue token")
	}
	return offset, nil
}

// DecodePageOffset accepts the common opaque continuation token for native
// Highland collections that live outside the storage package.
func DecodePageOffset(token string) (int, error) {
	return continuationOffset(token)
}

// EncodePageOffset gives provider adapters the common opaque continuation
// format without exposing its representation as part of the provider contract.
func EncodePageOffset(offset int) string {
	if offset < 0 {
		offset = 0
	}
	return base64.RawURLEncoding.EncodeToString([]byte("v1:" + strconv.Itoa(offset)))
}

func matches(filter, value string) bool {
	return filter == "" || strings.EqualFold(filter, value)
}

func search(query string, values ...string) bool {
	if query == "" {
		return true
	}
	query = strings.ToLower(query)
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func writeAPIError(w http.ResponseWriter, r *http.Request, status int, code, message string, retryable bool, details map[string]any) {
	requestID := ""
	if r != nil {
		requestID = chimw.GetReqID(r.Context())
	}
	writeJSON(w, status, ErrorEnvelope{Error: APIError{Code: code, Message: message, Details: details, Retryable: retryable, RequestID: requestID}})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
