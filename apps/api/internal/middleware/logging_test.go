package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func statusHandler(code int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) })
}

func lines(buf *bytes.Buffer) []map[string]any {
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err == nil {
			out = append(out, m)
		}
	}
	return out
}

func TestRequestLoggerLevelsAndSkips(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := chi.NewRouter()
	r.Use(RequestLogger(logger, 1))
	r.Get("/api/v1/volumes/{name}", statusHandler(http.StatusOK).ServeHTTP)
	r.Get("/boom", statusHandler(http.StatusInternalServerError).ServeHTTP)
	r.Get("/healthz", statusHandler(http.StatusOK).ServeHTTP)
	r.Get("/metrics", statusHandler(http.StatusOK).ServeHTTP)

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/volumes/alpha", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/boom", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/metrics", nil))

	got := lines(&buf)
	if len(got) != 2 {
		t.Fatalf("expected 2 log lines (health/metrics skipped), got %d: %s", len(got), buf.String())
	}
	// First: 200 on route template.
	if got[0]["level"] != "INFO" || got[0]["route"] != "/api/v1/volumes/{name}" || got[0]["status"].(float64) != 200 {
		t.Errorf("unexpected first line: %v", got[0])
	}
	// Second: 500 must escalate to ERROR.
	if got[1]["level"] != "ERROR" || got[1]["status"].(float64) != 500 {
		t.Errorf("500 should log at ERROR: %v", got[1])
	}
}

func TestRequestLoggerSamplesInfoButNotErrors(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	r := chi.NewRouter()
	r.Use(RequestLogger(logger, 10)) // 1-in-10 Info sampling
	r.Get("/ok", statusHandler(http.StatusOK).ServeHTTP)
	r.Get("/err", statusHandler(http.StatusInternalServerError).ServeHTTP)

	for i := 0; i < 30; i++ {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ok", nil))
	}
	for i := 0; i < 5; i++ {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/err", nil))
	}

	var info, errs int
	for _, l := range lines(&buf) {
		switch l["level"] {
		case "INFO":
			info++
		case "ERROR":
			errs++
		}
	}
	if errs != 5 {
		t.Errorf("all 5 errors must log regardless of sampling, got %d", errs)
	}
	if info != 3 { // 30/10
		t.Errorf("expected 3 sampled info lines out of 30, got %d", info)
	}
}
