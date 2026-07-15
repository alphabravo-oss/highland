package longhorn_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/highland-io/highland/apps/api/internal/longhorn"
)

type fakeObserver struct {
	mu       sync.Mutex
	requests int
	method   string
	status   int
	errors   int
	errKind  string
}

func (f *fakeObserver) ObserveManagerRequest(method string, status int, _ time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	f.method = method
	f.status = status
}

func (f *fakeObserver) IncManagerError(reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errors++
	f.errKind = reason
}

func TestProxyObservesSuccessfulRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	p, err := longhorn.NewProxy(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	obs := &fakeObserver{}
	p.SetMetrics(obs)

	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/lh/volumes", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	obs.mu.Lock()
	defer obs.mu.Unlock()
	if obs.requests != 1 || obs.method != "GET" || obs.status != http.StatusOK {
		t.Fatalf("ObserveManagerRequest not recorded correctly: %+v", obs)
	}
}

func TestProxyObservesUpstreamError(t *testing.T) {
	// Point at a server we immediately close so the transport errors.
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	p, err := longhorn.NewProxy(deadURL)
	if err != nil {
		t.Fatal(err)
	}
	obs := &fakeObserver{}
	p.SetMetrics(obs)

	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/lh/volumes", nil))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on dead upstream, got %d", rec.Code)
	}
	obs.mu.Lock()
	defer obs.mu.Unlock()
	if obs.errors != 1 || obs.errKind != "upstream_unavailable" {
		t.Fatalf("IncManagerError not recorded: %+v", obs)
	}
}

// A nil observer must not panic (SetMetrics never called path).
func TestProxyWithoutObserver(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	p, err := longhorn.NewProxy(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/lh/volumes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
