package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/highland-io/highland/apps/api/internal/audit"
	"github.com/highland-io/highland/apps/api/internal/auth"
)

// RequireRole rejects requests that fail method-level RBAC or admin-only paths.
func RequireRole(auditStore *audit.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := UserFromContext(r.Context())
			if !ok {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			path := r.URL.Path
			// Admin-only Highland surfaces
			if strings.HasPrefix(path, "/api/v1/audit") ||
				strings.HasPrefix(path, "/api/v1/users") ||
				strings.HasPrefix(path, "/api/v1/auth/oidc-config") {
				if !auth.AdminOnly(user.Role) {
					if auditStore != nil {
						auditStore.Append(audit.Event{
							Username: user.Username,
							Role:     string(user.Role),
							Action:   "access_denied",
							Method:   r.Method,
							Path:     path,
							Result:   "denied",
							SourceIP: r.RemoteAddr,
							Message:  "admin only",
						})
					}
					http.Error(w, `{"error":"forbidden: admin role required"}`, http.StatusForbidden)
					return
				}
			}

			// Settings mutations require admin
			if strings.HasPrefix(path, "/api/v1/lh/settings") && r.Method != http.MethodGet && r.Method != http.MethodHead {
				if user.Role != auth.RoleAdmin {
					if auditStore != nil {
						auditStore.Append(audit.Event{
							Username: user.Username,
							Role:     string(user.Role),
							Action:   "settings_denied",
							Method:   r.Method,
							Path:     path,
							Result:   "denied",
							SourceIP: r.RemoteAddr,
						})
					}
					http.Error(w, `{"error":"forbidden: admin role required for settings"}`, http.StatusForbidden)
					return
				}
			}

			if !auth.MethodAllowed(user.Role, r.Method) {
				if auditStore != nil {
					auditStore.Append(audit.Event{
						Username: user.Username,
						Role:     string(user.Role),
						Action:   "method_denied",
						Method:   r.Method,
						Path:     path,
						Result:   "denied",
						SourceIP: r.RemoteAddr,
						Message:  "viewer cannot mutate",
					})
				}
				http.Error(w, `{"error":"forbidden: role cannot perform this method"}`, http.StatusForbidden)
				return
			}

			// Audit mutating requests based on the actual downstream outcome.
			mutating := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
			if auditStore != nil && mutating {
				sw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
				next.ServeHTTP(sw, r)

				result := "ok"
				if sw.status >= 400 {
					result = "error"
				}
				auditStore.Append(audit.Event{
					Username: user.Username,
					Role:     string(user.Role),
					Action:   "mutate",
					Method:   r.Method,
					Path:     path,
					Target:   path,
					Result:   result,
					SourceIP: r.RemoteAddr,
					Message:  "status " + strconv.Itoa(sw.status),
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// statusRecorder wraps http.ResponseWriter to capture the response status code.
// It defaults to 200 if WriteHeader is never called, and passes Flush through
// to the underlying writer so streaming/SSE-style responses keep working.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wroteHeader {
		s.status = code
		s.wroteHeader = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wroteHeader {
		s.wroteHeader = true
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
