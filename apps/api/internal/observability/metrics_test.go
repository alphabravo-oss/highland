package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// A nil *Metrics must be safe for every record method and the handler — tests
// and non-instrumented callers pass nil, and the login/auth paths would 500 if
// any of these panicked (the argument-evaluation bug this guards against).
func TestNilMetricsIsSafe(t *testing.T) {
	var m *Metrics
	// None of these may panic.
	m.IncManagerError("x")
	m.IncLoginAttempt("success")
	m.IncSessionAuthFailure("missing_cookie")
	m.IncAuthzDenial("unauthorized")
	m.IncCSRFRejection()
	m.IncWatchError()
	m.ObserveManagerRequest("GET", 200, time.Millisecond)
	m.RegisterSSEClientSource(func() int { return 1 })

	// Handler on nil serves 404 rather than dereferencing a nil registry.
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("nil Handler should 404, got %d", rec.Code)
	}

	// InstrumentHandler on nil must pass through to the next handler.
	called := false
	h := m.InstrumentHandler()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !called {
		t.Fatal("nil InstrumentHandler must call next")
	}
}

func TestHandlerExposesRecordedMetrics(t *testing.T) {
	m := New()
	m.IncLoginAttempt("success")
	m.IncManagerError("upstream_unavailable")
	m.IncCSRFRejection()
	m.ObserveManagerRequest("GET", 200, 5*time.Millisecond)

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("Handler should 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`highland_login_attempts_total{result="success"} 1`,
		`highland_longhorn_proxy_errors_total{reason="upstream_unavailable"} 1`,
		"highland_csrf_rejections_total 1",
		`highland_longhorn_proxy_requests_total{method="GET",status_class="2xx"} 1`,
		"go_goroutines", // Go collector registered
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
}

func TestInstrumentHandlerRecordsRouteTemplate(t *testing.T) {
	m := New()
	r := chi.NewRouter()
	r.Use(m.InstrumentHandler())
	r.Get("/api/v1/volumes/{name}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// Two distinct names must collapse to one bounded route-template label.
	for _, name := range []string{"alpha", "beta"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/volumes/"+name, nil))
	}

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	want := `highland_http_requests_total{method="GET",route="/api/v1/volumes/{name}",status_class="2xx"} 2`
	if !strings.Contains(body, want) {
		t.Errorf("expected bounded-cardinality route counter %q in:\n%s", want, body)
	}
}

func TestInstrumentHandlerBoundsMethodLabel(t *testing.T) {
	m := New()
	r := chi.NewRouter()
	r.Use(m.InstrumentHandler())
	r.HandleFunc("/x", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	// A bogus but valid HTTP-token method must collapse to "other", not mint a
	// new series (this is the cardinality-DoS guard).
	req := httptest.NewRequest("PROPFIND", "/x", nil)
	r.ServeHTTP(httptest.NewRecorder(), req)
	req = httptest.NewRequest("ZZZ12345", "/x", nil)
	r.ServeHTTP(httptest.NewRecorder(), req)
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	// chi rejects the non-standard verbs (405, route "other"), but the point is
	// that BOTH bogus verbs fold into a single method="other" series rather than
	// minting one series each — that is the cardinality-DoS guard.
	if !strings.Contains(body, `highland_http_requests_total{method="other",route="other",status_class="4xx"} 2`) {
		t.Errorf("bogus methods should collapse to a single method=\"other\" series (count 2):\n%s", body)
	}
	if !strings.Contains(body, `highland_http_requests_total{method="GET",route="/x",status_class="2xx"} 1`) {
		t.Errorf("known method GET should be preserved:\n%s", body)
	}
	if strings.Contains(body, "PROPFIND") || strings.Contains(body, "ZZZ12345") {
		t.Errorf("raw bogus method leaked into a label:\n%s", body)
	}
}

func TestSSERouteNotInstrumented(t *testing.T) {
	m := New()
	r := chi.NewRouter()
	r.Use(m.InstrumentHandler())
	r.Get("/api/v1/events/stream", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/events/stream", nil))

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if strings.Contains(rec.Body.String(), "/api/v1/events/stream") {
		t.Errorf("SSE route must be excluded from request metrics:\n%s", rec.Body.String())
	}
}

func TestRegisterSSEClientSourceGauge(t *testing.T) {
	m := New()
	m.RegisterSSEClientSource(func() int { return 3 })
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(rec.Body.String(), "highland_sse_clients 3") {
		t.Errorf("sse_clients gauge should reflect source count 3:\n%s", rec.Body.String())
	}
}
