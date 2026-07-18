// Package observability exposes Prometheus metrics + a scrape handler for the
// Highland BFF's own operation (request rates/latencies, Longhorn proxy health,
// auth/CSRF outcomes, and live SSE clients). All record methods are nil-safe so
// callers/tests can pass a nil *Metrics.
package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const namespace = "highland"

var latencyBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}

// Metrics owns a private registry and every collector.
type Metrics struct {
	registry *prometheus.Registry

	httpRequests                  *prometheus.CounterVec // {method, route, status_class}
	httpDuration                  *prometheus.HistogramVec
	httpInFlight                  prometheus.Gauge
	proxyRequests                 *prometheus.CounterVec // {method, status_class}
	proxyDuration                 *prometheus.HistogramVec
	proxyErrors                   *prometheus.CounterVec // {reason}
	loginAttempts                 *prometheus.CounterVec // {result}
	sessionFails                  *prometheus.CounterVec // {reason}
	authzDenials                  *prometheus.CounterVec // {reason}
	csrfRejects                   prometheus.Counter
	watchErrors                   prometheus.Counter
	storageProviderUp             *prometheus.GaugeVec
	storageInventoryObjects       *prometheus.GaugeVec
	storageSyncTimestamp          *prometheus.GaugeVec
	storageWatchErrors            *prometheus.CounterVec
	storageProviderDuration       *prometheus.HistogramVec
	storageProviderErrors         *prometheus.CounterVec
	storageOperations             *prometheus.CounterVec
	storageOperationDuration      *prometheus.HistogramVec
	storageOperationsInProgress   *prometheus.GaugeVec
	storageOperationRetries       *prometheus.CounterVec
	storageOperationAuthFailures  *prometheus.CounterVec
	storagePreflightDenials       *prometheus.CounterVec
	storagePostflightMismatches   *prometheus.CounterVec
	storageOperationLeader        prometheus.Gauge
	storageGraphDuration          *prometheus.HistogramVec
	storageUnresolvedRelations    *prometheus.GaugeVec
	storageDriftRecords           *prometheus.GaugeVec
	storageImpactFailures         *prometheus.CounterVec
	storageForecastSufficient     *prometheus.GaugeVec
	storagePolicyEnabled          *prometheus.GaugeVec
	storagePolicyUpdates          *prometheus.CounterVec
	storagePolicyGeneration       prometheus.Gauge
	storagePolicyObservedAt       prometheus.Gauge
	storagePolicyCeilingMismatch  prometheus.Gauge
	storagePolicyPortableProvider *prometheus.GaugeVec
	storagePolicyLegacyWildcard   prometheus.Gauge
	// HA / security-state (ADR-0004 / ADR-0005)
	auditAppendTotal   *prometheus.CounterVec // {result}
	auditSinkUp        *prometheus.GaugeVec   // {backend,durable}
	loginLimiterUp     prometheus.Gauge
	loginLimiterErrors prometheus.Counter
}

// New builds and registers all collectors (plus Go/process runtime metrics).
func New() *Metrics {
	m := &Metrics{registry: prometheus.NewRegistry()}
	m.httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "http_requests_total",
		Help: "Total HTTP requests served by the BFF, by chi route template and status class.",
	}, []string{"method", "route", "status_class"})
	m.httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace, Name: "http_request_duration_seconds",
		Help: "HTTP request latency in seconds by route template.", Buckets: latencyBuckets,
	}, []string{"method", "route"})
	m.httpInFlight = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace, Name: "http_requests_in_flight", Help: "In-flight HTTP requests.",
	})
	m.proxyRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "longhorn_proxy_requests_total",
		Help: "Requests proxied to the Longhorn manager.",
	}, []string{"method", "status_class"})
	m.proxyDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace, Name: "longhorn_proxy_request_duration_seconds",
		Help: "Latency of proxied Longhorn manager requests.", Buckets: latencyBuckets,
	}, []string{"method"})
	m.proxyErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "longhorn_proxy_errors_total",
		Help: "Longhorn proxy transport errors.",
	}, []string{"reason"})
	m.loginAttempts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "login_attempts_total",
		Help: "Local login attempts by outcome.",
	}, []string{"result"})
	m.sessionFails = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "session_auth_failures_total",
		Help: "Session-cookie auth rejections.",
	}, []string{"reason"})
	m.authzDenials = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "authz_denials_total",
		Help: "RBAC denials.",
	}, []string{"reason"})
	m.csrfRejects = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace, Name: "csrf_rejections_total",
		Help: "Rejected requests failing CSRF double-submit validation.",
	})
	m.watchErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace, Name: "sse_watch_errors_total",
		Help: "Longhorn informer watch/handler errors.",
	})
	m.storageProviderUp = prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "storage_provider_up", Help: "Whether a configured storage provider is healthy."}, []string{"provider", "kind"})
	m.storageInventoryObjects = prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "storage_inventory_objects", Help: "Objects in the storage inventory cache."}, []string{"kind", "provider"})
	m.storageSyncTimestamp = prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "storage_sync_timestamp_seconds", Help: "Unix timestamp of the last successful storage source event."}, []string{"source"})
	m.storageWatchErrors = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "storage_watch_errors_total", Help: "Storage informer watch failures."}, []string{"source"})
	m.storageProviderDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: namespace, Name: "storage_provider_request_duration_seconds", Help: "Storage provider request latency.", Buckets: latencyBuckets}, []string{"provider", "operation"})
	m.storageProviderErrors = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "storage_provider_errors_total", Help: "Storage provider errors by bounded reason."}, []string{"provider", "reason"})
	m.storageOperations = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "storage_operations_total", Help: "Durable storage operations by terminal result."}, []string{"provider", "action", "result"})
	m.storageOperationDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: namespace, Name: "storage_operation_duration_seconds", Help: "Durable storage operation duration.", Buckets: []float64{1, 5, 15, 30, 60, 300, 900, 1800}}, []string{"provider", "action"})
	m.storageOperationsInProgress = prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "storage_operations_in_progress", Help: "Storage operations currently reconciling."}, []string{"provider", "action"})
	m.storageOperationRetries = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "storage_operation_retries_total", Help: "Storage operation reconciliation retries."}, []string{"provider", "reason"})
	m.storageOperationAuthFailures = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "storage_operation_authorization_failures_total", Help: "Durable storage operations blocked by missing installed Kubernetes permissions."}, []string{"provider", "action"})
	m.storagePreflightDenials = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "storage_preflight_denials_total", Help: "Storage operation preflight denials."}, []string{"provider", "reason"})
	m.storagePostflightMismatches = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "storage_postflight_mismatches_total", Help: "Storage operations whose authoritative postflight state did not match the approved result."}, []string{"provider", "kind"})
	m.storageOperationLeader = prometheus.NewGauge(prometheus.GaugeOpts{Namespace: namespace, Name: "storage_operation_controller_leader", Help: "Whether this Highland API replica is the elected storage operation controller."})
	m.storageGraphDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: namespace, Name: "storage_graph_build_duration_seconds", Help: "Bounded storage relationship graph build latency.", Buckets: latencyBuckets}, []string{"provider"})
	m.storageUnresolvedRelations = prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "storage_unresolved_relationships", Help: "Storage graph nodes with unresolved authoritative backend correlation."}, []string{"provider"})
	m.storageDriftRecords = prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "storage_drift_records", Help: "Active desired/runtime drift records by bounded severity."}, []string{"provider", "severity"})
	m.storageImpactFailures = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "storage_impact_failures_total", Help: "Impact queries that failed or returned incomplete required evidence."}, []string{"provider", "reason"})
	m.storageForecastSufficient = prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "storage_forecast_sufficient", Help: "Whether a provider capacity measure has enough fresh history for forecasting."}, []string{"provider", "measure"})
	m.storagePolicyEnabled = prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "storage_policy_enabled", Help: "Effective runtime storage policy capability state."}, []string{"capability"})
	m.storagePolicyUpdates = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "storage_policy_updates_total", Help: "Runtime storage policy update attempts by bounded result."}, []string{"result"})
	m.storagePolicyGeneration = prometheus.NewGauge(prometheus.GaugeOpts{Namespace: namespace, Name: "storage_policy_observed_generation", Help: "Latest observed HighlandPolicy generation."})
	m.storagePolicyObservedAt = prometheus.NewGauge(prometheus.GaugeOpts{Namespace: namespace, Name: "storage_policy_observed_timestamp_seconds", Help: "Unix timestamp of the latest authoritative policy observation."})
	m.storagePolicyCeilingMismatch = prometheus.NewGauge(prometheus.GaugeOpts{Namespace: namespace, Name: "storage_policy_ceiling_mismatch", Help: "Whether requested runtime policy exceeds the installed permission ceiling."})
	m.storagePolicyPortableProvider = prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "storage_policy_portable_provider_enabled", Help: "Providers explicitly enabled for portable Kubernetes storage workflows."}, []string{"provider"})
	m.storagePolicyLegacyWildcard = prometheus.NewGauge(prometheus.GaugeOpts{Namespace: namespace, Name: "storage_policy_legacy_wildcard", Help: "Whether portable Kubernetes workflows use the legacy all-provider wildcard."})
	m.auditAppendTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "audit_append_total",
		Help: "Audit append attempts by result (ok|error).",
	}, []string{"result"})
	m.auditSinkUp = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace, Name: "audit_sink_up",
		Help: "Whether the audit sink health check is passing (1) or not (0).",
	}, []string{"backend", "durable"})
	m.loginLimiterUp = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace, Name: "login_limiter_up",
		Help: "Whether the login limiter backend health check is passing (1) or not (0).",
	})
	m.loginLimiterErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace, Name: "login_limiter_errors_total",
		Help: "Login limiter backend errors (no username/IP labels).",
	})

	m.registry.MustRegister(
		m.httpRequests, m.httpDuration, m.httpInFlight,
		m.proxyRequests, m.proxyDuration, m.proxyErrors,
		m.loginAttempts, m.sessionFails, m.authzDenials, m.csrfRejects, m.watchErrors,
		m.storageProviderUp, m.storageInventoryObjects, m.storageSyncTimestamp,
		m.storageWatchErrors, m.storageProviderDuration, m.storageProviderErrors,
		m.storageOperations, m.storageOperationDuration, m.storageOperationsInProgress,
		m.storageOperationRetries, m.storageOperationAuthFailures, m.storagePreflightDenials,
		m.storagePostflightMismatches,
		m.storageOperationLeader,
		m.storageGraphDuration, m.storageUnresolvedRelations, m.storageDriftRecords,
		m.storageImpactFailures, m.storageForecastSufficient,
		m.storagePolicyEnabled, m.storagePolicyUpdates, m.storagePolicyGeneration,
		m.storagePolicyObservedAt, m.storagePolicyCeilingMismatch,
		m.storagePolicyPortableProvider, m.storagePolicyLegacyWildcard,
		m.auditAppendTotal, m.auditSinkUp, m.loginLimiterUp, m.loginLimiterErrors,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return m
}

func (m *Metrics) PolicyProvidersObserved(providerIDs []string) {
	if m == nil {
		return
	}
	m.storagePolicyPortableProvider.Reset()
	m.storagePolicyLegacyWildcard.Set(0)
	for index, providerID := range providerIDs {
		if index >= 64 {
			break
		}
		if providerID == "*" {
			m.storagePolicyLegacyWildcard.Set(1)
			continue
		}
		m.storagePolicyPortableProvider.WithLabelValues(providerID).Set(1)
	}
}

func (m *Metrics) PolicyObserved(capabilities map[string]bool, generation int64, observedAt time.Time, ceilingMismatch bool) {
	if m == nil {
		return
	}
	for _, capability := range []string{"accept-new-operations", "portable-kubernetes", "longhorn", "rook-ceph", "ceph-storageclass-delete", "ceph-pool-delete"} {
		value := 0.0
		if capabilities[capability] {
			value = 1
		}
		m.storagePolicyEnabled.WithLabelValues(capability).Set(value)
	}
	m.storagePolicyGeneration.Set(float64(generation))
	if !observedAt.IsZero() {
		m.storagePolicyObservedAt.Set(float64(observedAt.Unix()))
	}
	if ceilingMismatch {
		m.storagePolicyCeilingMismatch.Set(1)
	} else {
		m.storagePolicyCeilingMismatch.Set(0)
	}
}

func (m *Metrics) PolicyUpdate(result string) {
	if m == nil {
		return
	}
	switch result {
	case "ok", "denied", "conflict", "error":
	default:
		result = "error"
	}
	m.storagePolicyUpdates.WithLabelValues(result).Inc()
}

// Handler serves the Prometheus exposition format.
func (m *Metrics) Handler() http.Handler {
	if m == nil {
		return http.NotFoundHandler()
	}
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// RegisterSSEClientSource wires a live gauge to the SSE hub's client count.
func (m *Metrics) RegisterSSEClientSource(count func() int) {
	if m == nil || count == nil {
		return
	}
	m.registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: namespace, Name: "sse_clients", Help: "Currently connected SSE watch clients.",
	}, func() float64 { return float64(count()) }))
}

// sseStreamPath and metricsPath are excluded from request instrumentation:
// /metrics self-instruments the scraper, and the SSE stream is a long-lived
// connection (up to 30m) that would pin the in-flight gauge and dump its whole
// duration into the +Inf latency bucket. Matched on the raw URL path, which is
// known before routing (so the in-flight gauge can be skipped up front).
const (
	metricsPath   = "/metrics"
	sseStreamPath = "/api/v1/events/stream"
)

// InstrumentHandler records request count, latency, and in-flight per route
// TEMPLATE and normalized method (both bounded cardinality). The /metrics and
// SSE routes are not self-instrumented.
func (m *Metrics) InstrumentHandler() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if m == nil {
				next.ServeHTTP(w, r)
				return
			}
			skip := r.URL.Path == metricsPath || r.URL.Path == sseStreamPath
			if !skip {
				m.httpInFlight.Inc()
				defer m.httpInFlight.Dec()
			}
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			if skip {
				return
			}
			route := routePattern(r)
			method := normalizeMethod(r.Method)
			m.httpRequests.WithLabelValues(method, route, statusClass(ww.Status())).Inc()
			m.httpDuration.WithLabelValues(method, route).Observe(time.Since(start).Seconds())
		})
	}
}

// ObserveManagerRequest records a proxied Longhorn manager request.
func (m *Metrics) ObserveManagerRequest(method string, status int, d time.Duration) {
	if m == nil {
		return
	}
	method = normalizeMethod(method)
	m.proxyRequests.WithLabelValues(method, statusClass(status)).Inc()
	m.proxyDuration.WithLabelValues(method).Observe(d.Seconds())
}

// The nil check MUST come before touching a CounterVec field: on a nil *Metrics,
// evaluating m.proxyErrors as an argument would panic before any helper's guard.
func (m *Metrics) IncManagerError(reason string) {
	if m == nil {
		return
	}
	m.proxyErrors.WithLabelValues(reason).Inc()
}

func (m *Metrics) IncLoginAttempt(result string) {
	if m == nil {
		return
	}
	m.loginAttempts.WithLabelValues(result).Inc()
}

func (m *Metrics) IncSessionAuthFailure(reason string) {
	if m == nil {
		return
	}
	m.sessionFails.WithLabelValues(reason).Inc()
}

func (m *Metrics) IncAuthzDenial(reason string) {
	if m == nil {
		return
	}
	m.authzDenials.WithLabelValues(reason).Inc()
}

func (m *Metrics) IncCSRFRejection() {
	if m == nil {
		return
	}
	m.csrfRejects.Inc()
}

func (m *Metrics) IncWatchError() {
	if m == nil {
		return
	}
	m.watchErrors.Inc()
}

func (m *Metrics) SetStorageProviderUp(provider, kind string, up bool) {
	if m == nil {
		return
	}
	value := 0.0
	if up {
		value = 1
	}
	m.storageProviderUp.WithLabelValues(provider, kind).Set(value)
}

func (m *Metrics) SetStorageInventoryObjects(kind, provider string, count int) {
	if m == nil {
		return
	}
	m.storageInventoryObjects.WithLabelValues(kind, provider).Set(float64(count))
}

func (m *Metrics) SetStorageSyncTimestamp(source string, observedAt time.Time) {
	if m == nil || observedAt.IsZero() {
		return
	}
	m.storageSyncTimestamp.WithLabelValues(source).Set(float64(observedAt.Unix()))
}

func (m *Metrics) IncStorageWatchError(source string) {
	if m == nil {
		return
	}
	m.storageWatchErrors.WithLabelValues(source).Inc()
}

func (m *Metrics) ObserveStorageProviderRequest(provider, operation string, duration time.Duration) {
	if m == nil {
		return
	}
	m.storageProviderDuration.WithLabelValues(provider, operation).Observe(duration.Seconds())
}

func (m *Metrics) IncStorageProviderError(provider, reason string) {
	if m == nil {
		return
	}
	m.storageProviderErrors.WithLabelValues(provider, reason).Inc()
}

func (m *Metrics) OperationStarted(provider, action string) {
	if m == nil {
		return
	}
	m.storageOperationsInProgress.WithLabelValues(provider, action).Inc()
}

func (m *Metrics) OperationFinished(provider, action, result string, duration time.Duration) {
	if m == nil {
		return
	}
	m.storageOperations.WithLabelValues(provider, action, result).Inc()
	m.storageOperationDuration.WithLabelValues(provider, action).Observe(duration.Seconds())
	m.storageOperationsInProgress.WithLabelValues(provider, action).Dec()
}

func (m *Metrics) OperationRetry(provider, reason string) {
	if m == nil {
		return
	}
	m.storageOperationRetries.WithLabelValues(provider, reason).Inc()
}

func (m *Metrics) OperationAuthorizationFailure(provider, action string) {
	if m == nil {
		return
	}
	m.storageOperationAuthFailures.WithLabelValues(provider, action).Inc()
}

func (m *Metrics) PreflightDenied(provider, reason string) {
	if m == nil {
		return
	}
	m.storagePreflightDenials.WithLabelValues(provider, reason).Inc()
}

func (m *Metrics) OperationPostflightMismatch(provider, kind string) {
	if m == nil {
		return
	}
	m.storagePostflightMismatches.WithLabelValues(provider, kind).Inc()
}

func (m *Metrics) OperationControllerLeader(leader bool) {
	if m == nil {
		return
	}
	if leader {
		m.storageOperationLeader.Set(1)
		return
	}
	m.storageOperationLeader.Set(0)
}

func (m *Metrics) ObserveStorageGraphBuild(provider string, duration time.Duration, unresolved int) {
	if m == nil {
		return
	}
	m.storageGraphDuration.WithLabelValues(provider).Observe(duration.Seconds())
	m.storageUnresolvedRelations.WithLabelValues(provider).Set(float64(unresolved))
}

func (m *Metrics) SetStorageDriftRecords(provider, severity string, count int) {
	if m == nil {
		return
	}
	m.storageDriftRecords.WithLabelValues(provider, severity).Set(float64(count))
}

func (m *Metrics) IncStorageImpactFailure(provider, reason string) {
	if m == nil {
		return
	}
	m.storageImpactFailures.WithLabelValues(provider, reason).Inc()
}

func (m *Metrics) SetStorageForecastSufficient(provider, measure string, sufficient bool) {
	if m == nil {
		return
	}
	value := 0.0
	if sufficient {
		value = 1
	}
	m.storageForecastSufficient.WithLabelValues(provider, measure).Set(value)
}

// IncAuditAppend records audit append outcomes without high-cardinality labels.
func (m *Metrics) IncAuditAppend(result string) {
	if m == nil {
		return
	}
	if result == "" {
		result = "error"
	}
	m.auditAppendTotal.WithLabelValues(result).Inc()
}

// SetAuditSinkUp reports sink health (backend name, durable flag).
func (m *Metrics) SetAuditSinkUp(backend string, durable, up bool) {
	if m == nil {
		return
	}
	if backend == "" {
		backend = "unknown"
	}
	d := "false"
	if durable {
		d = "true"
	}
	v := 0.0
	if up {
		v = 1
	}
	m.auditSinkUp.WithLabelValues(backend, d).Set(v)
}

// SetLoginLimiterUp reports shared/local limiter backend health.
func (m *Metrics) SetLoginLimiterUp(up bool) {
	if m == nil {
		return
	}
	if up {
		m.loginLimiterUp.Set(1)
		return
	}
	m.loginLimiterUp.Set(0)
}

// IncLoginLimiterError increments backend error count (no IP/username labels).
func (m *Metrics) IncLoginLimiterError() {
	if m == nil {
		return
	}
	m.loginLimiterErrors.Inc()
}

func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if p := rc.RoutePattern(); p != "" {
			return p
		}
	}
	return "other"
}

// knownMethods bounds the `method` label. Go's net/http accepts ANY RFC-7230
// token as a request method and dispatches it to the handler, so an
// unauthenticated client could otherwise mint an unbounded set of label values
// (a metrics-cardinality DoS). Anything outside this set collapses to "other".
var knownMethods = map[string]struct{}{
	http.MethodGet: {}, http.MethodHead: {}, http.MethodPost: {}, http.MethodPut: {},
	http.MethodPatch: {}, http.MethodDelete: {}, http.MethodConnect: {},
	http.MethodOptions: {}, http.MethodTrace: {},
}

func normalizeMethod(method string) string {
	if _, ok := knownMethods[method]; ok {
		return method
	}
	return "other"
}

func statusClass(code int) string {
	if code == 0 {
		code = http.StatusOK
	}
	return strconv.Itoa(code/100) + "xx"
}
