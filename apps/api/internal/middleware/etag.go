package middleware

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

// ConditionalJSONETag adds validators to bounded Highland-native JSON GETs.
// Streaming, proxied Longhorn, and non-API responses bypass buffering.
func ConditionalJSONETag(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet ||
			!strings.HasPrefix(r.URL.Path, "/api/v1/") ||
			strings.HasPrefix(r.URL.Path, "/api/v1/lh") ||
			r.URL.Path == "/api/v1/events/stream" {
			next.ServeHTTP(w, r)
			return
		}

		buffered := &etagResponseWriter{header: make(http.Header), status: http.StatusOK}
		next.ServeHTTP(buffered, r)
		contentType := buffered.header.Get("Content-Type")
		if buffered.status != http.StatusOK || !strings.Contains(contentType, "json") {
			copyHeader(w.Header(), buffered.header)
			w.WriteHeader(buffered.status)
			_, _ = w.Write(buffered.body.Bytes())
			return
		}

		sum := sha256.Sum256(stableETagPayload(buffered.body.Bytes()))
		etag := `"` + hex.EncodeToString(sum[:]) + `"`
		copyHeader(w.Header(), buffered.header)
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "private, no-cache")
		if weakETagMatches(r.Header.Get("If-None-Match"), etag) {
			w.Header().Del("Content-Type")
			w.Header().Del("Content-Length")
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.WriteHeader(buffered.status)
		_, _ = w.Write(buffered.body.Bytes())
	})
}

func weakETagMatches(header, current string) bool {
	current = strings.TrimPrefix(strings.TrimSpace(current), "W/")
	for candidate := range strings.SplitSeq(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || strings.TrimPrefix(candidate, "W/") == current {
			return true
		}
	}
	return false
}

// Request identity is useful response metadata but is different on every
// request and therefore must not invalidate an otherwise identical entity.
func stableETagPayload(payload []byte) []byte {
	var value any
	if json.Unmarshal(payload, &value) != nil {
		return payload
	}
	removeRequestIdentity(value)
	stable, err := json.Marshal(value)
	if err != nil {
		return payload
	}
	return stable
}

func removeRequestIdentity(value any) {
	switch typed := value.(type) {
	case map[string]any:
		delete(typed, "requestId")
		delete(typed, "observedAt")
		for _, child := range typed {
			removeRequestIdentity(child)
		}
	case []any:
		for _, child := range typed {
			removeRequestIdentity(child)
		}
	}
}

type etagResponseWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
	wrote  bool
}

func (w *etagResponseWriter) Header() http.Header { return w.header }

func (w *etagResponseWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.status = status
	w.wrote = true
}

func (w *etagResponseWriter) Write(payload []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(payload)
}

func copyHeader(destination, source http.Header) {
	for key, values := range source {
		destination[key] = append([]string(nil), values...)
	}
}
