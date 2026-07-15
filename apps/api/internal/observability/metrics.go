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

	httpRequests  *prometheus.CounterVec // {method, route, status_class}
	httpDuration  *prometheus.HistogramVec
	httpInFlight  prometheus.Gauge
	proxyRequests *prometheus.CounterVec // {method, status_class}
	proxyDuration *prometheus.HistogramVec
	proxyErrors   *prometheus.CounterVec // {reason}
	loginAttempts *prometheus.CounterVec // {result}
	sessionFails  *prometheus.CounterVec // {reason}
	authzDenials  *prometheus.CounterVec // {reason}
	csrfRejects   prometheus.Counter
	watchErrors   prometheus.Counter
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

	m.registry.MustRegister(
		m.httpRequests, m.httpDuration, m.httpInFlight,
		m.proxyRequests, m.proxyDuration, m.proxyErrors,
		m.loginAttempts, m.sessionFails, m.authzDenials, m.csrfRejects, m.watchErrors,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return m
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
