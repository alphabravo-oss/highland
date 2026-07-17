package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/highland-io/highland/apps/api/internal/audit"
	"github.com/highland-io/highland/apps/api/internal/auth"
	"github.com/highland-io/highland/apps/api/internal/config"
	"github.com/highland-io/highland/apps/api/internal/middleware"
	"github.com/highland-io/highland/apps/api/internal/observability"
	"github.com/highland-io/highland/apps/api/internal/ratelimit"
	"github.com/highland-io/highland/apps/api/internal/storage"
)

// API groups HTTP handlers for Highland native endpoints.
type API struct {
	Cfg         *config.Config
	Auth        *auth.Authenticator
	Store       *auth.Store
	Users       *auth.UserStore
	Challenge   *auth.MFAChallengeSigner
	Audit       *audit.Store
	OIDC        *auth.OIDCProvider // legacy pointer; prefer OIDCRuntime
	OIDCRuntime *auth.OIDCRuntime  // runtime-configurable enterprise SSO
	Limiter     *ratelimit.LoginLimiter
	Obs         *observability.Metrics
	Storage     *storage.HTTPAPI
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
	if a.Cfg == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "not_ready",
			"reason": "configuration unavailable",
		})
		return
	}
	if a.Cfg.StorageEnabled && (a.Storage == nil || !a.Storage.Ready()) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "not_ready", "reason": "storage core cache is not ready",
		})
		return
	}
	if a.Cfg.LonghornEnabled && a.Cfg.LonghornRequired && a.Cfg.ManagerURL == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "reason": "required Longhorn manager url not configured"})
		return
	}
	if a.Cfg.LonghornEnabled && a.Cfg.LonghornRequired {
		if err := a.managerReachable(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status": "not_ready", "reason": "required manager unreachable: " + err.Error(), "managerUrl": a.Cfg.ManagerURL,
			})
			return
		}
	}
	for _, providerID := range a.Cfg.RequiredProviders {
		if providerID == "longhorn" {
			continue
		}
		if a.Storage == nil || !a.Storage.ProviderHealthy(r.Context(), providerID) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status": "not_ready", "reason": "required storage provider is unavailable", "providerId": providerID,
			})
			return
		}
	}
	response := map[string]any{
		"status": "ready", "uptime": time.Since(a.Started).String(),
	}
	if a.Cfg.LonghornEnabled {
		response["managerUrl"] = a.Cfg.ManagerURL
	}
	if a.Storage != nil {
		// Readiness must remain a constant-time cache check. Full provider
		// descriptors may call several independent backends and belong on the
		// status/provider endpoints; evaluating them here can make an optional
		// slow provider remove the entire API from Service endpoints.
		response["storage"] = map[string]any{"ready": a.Storage.Ready()}
	}
	writeJSON(w, http.StatusOK, response)
}

// managerReachable performs a short GET against the manager /v1 API to confirm
// the Longhorn manager is answering.
func (a *API) managerReachable(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	target := strings.TrimSuffix(a.Cfg.ManagerURL, "/") + "/v1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
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
	// Brute-force gate: reject before the (expensive) credential check when the
	// account or client IP is locked out. Response is identical regardless of
	// which key tripped or whether the account exists.
	if a.Limiter != nil {
		if ok, retry := a.Limiter.Allow(req.Username, r.RemoteAddr); !ok {
			a.Obs.IncLoginAttempt("locked_out")
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retry.Seconds()))))
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"error": "too many login attempts, please try again later",
			})
			return
		}
	}
	user, err := a.Auth.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) || errors.Is(err, auth.ErrAccountDisabled) {
			if a.Limiter != nil {
				a.Limiter.RecordFailure(req.Username, r.RemoteAddr)
			}
			a.Obs.IncLoginAttempt("invalid_credentials")
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
			return
		}
		// Do NOT record a failure on backend errors — avoids locking admins out
		// during an auth-backend outage.
		a.Obs.IncLoginAttempt("error")
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "identity service unavailable"})
		return
	}
	if user.MFAEnabled {
		challenge, challengeErr := a.Challenge.Issue(user.Username)
		if challengeErr != nil {
			a.Obs.IncLoginAttempt("error")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "login failed"})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"mfaRequired": true, "challengeToken": challenge})
		return
	}
	id, err := a.Auth.IssueSession(*user)
	if err != nil {
		a.Obs.IncLoginAttempt("error")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "login failed"})
		return
	}
	if a.Limiter != nil {
		a.Limiter.RecordSuccess(req.Username, r.RemoteAddr)
	}
	a.Obs.IncLoginAttempt("success")
	a.setSessionCookie(w, id)
	a.Users.RecordAuthentication(context.Background(), user.Username)
	writeJSON(w, http.StatusOK, map[string]any{
		"user": user,
	})
}

func (a *API) VerifyMFA(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ChallengeToken string `json:"challengeToken"`
		Code           string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	username, err := a.Challenge.Verify(request.ChallengeToken)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired verification challenge"})
		return
	}
	if a.Limiter != nil {
		if ok, retry := a.Limiter.Allow(username, r.RemoteAddr); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retry.Seconds()))))
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many verification attempts, please try again later"})
			return
		}
	}
	user, err := a.Users.VerifySecondFactor(r.Context(), username, request.Code)
	if err != nil {
		if a.Limiter != nil {
			a.Limiter.RecordFailure(username, r.RemoteAddr)
		}
		a.Obs.IncLoginAttempt("invalid_mfa")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid verification code"})
		return
	}
	id, err := a.Auth.IssueSession(*user)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "login failed"})
		return
	}
	if a.Limiter != nil {
		a.Limiter.RecordSuccess(username, r.RemoteAddr)
	}
	a.Obs.IncLoginAttempt("success")
	a.setSessionCookie(w, id)
	a.Users.RecordAuthentication(context.Background(), username)
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (a *API) setSessionCookie(w http.ResponseWriter, id string) {
	http.SetCookie(w, &http.Cookie{
		Name: a.Cfg.CookieName, Value: id, Path: "/", HttpOnly: true,
		Secure: a.Cfg.CookieSecure, SameSite: http.SameSiteLaxMode,
		MaxAge: int(a.Cfg.SessionTTL.Seconds()),
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
