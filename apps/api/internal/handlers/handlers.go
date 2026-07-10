package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/highland-io/highland/apps/api/internal/auth"
	"github.com/highland-io/highland/apps/api/internal/config"
	"github.com/highland-io/highland/apps/api/internal/middleware"
)

// API groups HTTP handlers for Highland native endpoints.
type API struct {
	Cfg         *config.Config
	Auth        *auth.Authenticator
	Store       *auth.Store
	Users       *auth.UserStore
	OIDC        *auth.OIDCProvider // legacy pointer; prefer OIDCRuntime
	OIDCRuntime *auth.OIDCRuntime  // runtime-configurable enterprise SSO
	Started     time.Time
}

// oidcProvider returns the live OIDC provider from runtime (preferred) or static field.
func (a *API) oidcProvider() *auth.OIDCProvider {
	if a.OIDCRuntime != nil {
		// Always prefer runtime when present so disable/re-init takes effect immediately.
		return a.OIDCRuntime.Provider()
	}
	return a.OIDC
}

// Healthz returns liveness.
func (a *API) Healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "highland-api",
	})
}

// Readyz returns readiness.
func (a *API) Readyz(w http.ResponseWriter, r *http.Request) {
	if a.Cfg == nil || a.Cfg.ManagerURL == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "not_ready",
			"reason": "manager url not configured",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ready",
		"managerUrl": a.Cfg.ManagerURL,
		"uptime":     time.Since(a.Started).String(),
	})
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Providers handles GET /auth/providers — public, no session.
func (a *API) Providers(w http.ResponseWriter, r *http.Request) {
	oidcOffered := a.oidcProvider() != nil || a.Cfg.OIDCMock
	if a.OIDCRuntime != nil && a.OIDCRuntime.IsEnabled() {
		oidcOffered = true
	} else if a.Cfg.OIDCEnabled() && a.Cfg.OIDCIssuer != "" {
		oidcOffered = true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"mode":        string(a.Cfg.AuthMode),
		"local":       a.Cfg.LocalEnabled(),
		"oidc":        oidcOffered,
		"oidcReady":   a.oidcProvider() != nil,
		"oidcMock":    a.Cfg.OIDCMock,
		"localAlways": a.Cfg.LocalAlways,
		"message":     "Local username/password admin login works without OIDC",
	})
}

// Login handles POST /auth/login (local accounts — no IdP required).
func (a *API) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.Cfg.LocalEnabled() {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "local login disabled; use OIDC or set HIGHLAND_LOCAL_ALWAYS=true",
		})
		return
	}
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return
	}
	id, user, err := a.Auth.Login(req.Username, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "login failed"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     a.Cfg.CookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.Cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(a.Cfg.SessionTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"user": user,
	})
}

// Logout handles POST /auth/logout.
func (a *API) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(a.Cfg.CookieName); err == nil && c.Value != "" {
		a.Store.Delete(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     a.Cfg.CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

// Me handles GET /auth/me.
func (a *API) Me(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user": user,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
