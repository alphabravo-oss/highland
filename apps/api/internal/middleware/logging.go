package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

// RequestLogger emits one structured slog line per request (method, route
// template, status, duration, request id, client ip, bytes). Health/metrics
// probes are skipped. Warn/Error (4xx/5xx) always log; Info is sampled 1-in-N
// (sampleN<=1 logs every request).
func RequestLogger(logger *slog.Logger, sampleN int) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	if sampleN < 1 {
		sampleN = 1
	}
	var counter atomic.Uint64
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)

			route := chiRoutePattern(r)
			// Skip probes/scrape, and the long-lived SSE stream — the latter
			// would emit one line per disconnect with a multi-minute duration_ms
			// that reads as request latency rather than connection lifetime.
			if route == "/healthz" || route == "/readyz" || route == "/metrics" ||
				route == "/api/v1/events/stream" {
				return
			}
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}

			level := slog.LevelInfo
			switch {
			case status >= 500:
				level = slog.LevelError
			case status >= 400:
				level = slog.LevelWarn
			}
			if level == slog.LevelInfo && sampleN > 1 && counter.Add(1)%uint64(sampleN) != 0 {
				return
			}

			logger.LogAttrs(r.Context(), level, "http_request",
				slog.String("method", r.Method),
				slog.String("route", route),
				slog.Int("status", status),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
				slog.String("request_id", chimw.GetReqID(r.Context())),
				slog.String("remote_ip", clientHost(r.RemoteAddr)),
				slog.Int("bytes", ww.BytesWritten()),
			)
		})
	}
}

func chiRoutePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if p := rc.RoutePattern(); p != "" {
			return p
		}
	}
	return "other"
}

func clientHost(remoteAddr string) string {
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return h
	}
	return remoteAddr
}
