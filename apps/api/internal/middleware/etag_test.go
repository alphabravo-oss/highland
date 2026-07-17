package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConditionalJSONETag(t *testing.T) {
	requestID := "first"
	handler := ConditionalJSONETag(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[1],"meta":{"requestId":"` + requestID + `","observedAt":"` + requestID + `"}}`))
	}))
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/v1/storage/classes", nil))
	etag := first.Header().Get("ETag")
	if first.Code != http.StatusOK || etag == "" {
		t.Fatalf("first status=%d etag=%q", first.Code, etag)
	}
	secondRequest := httptest.NewRequest(http.MethodGet, "/api/v1/storage/classes", nil)
	secondRequest.Header.Set("If-None-Match", "W/"+etag)
	requestID = "second"
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, secondRequest)
	if second.Code != http.StatusNotModified || second.Body.Len() != 0 {
		t.Fatalf("conditional status=%d body=%q", second.Code, second.Body.String())
	}
}

func TestConditionalJSONETagBypassesStreamAndProxy(t *testing.T) {
	handler := ConditionalJSONETag(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	for _, path := range []string{"/api/v1/events/stream", "/api/v1/lh/volumes"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Header().Get("ETag") != "" {
			t.Fatalf("%s unexpectedly received etag", path)
		}
	}
}
