package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/highland-io/highland/apps/api/internal/auth"
	"github.com/highland-io/highland/apps/api/internal/middleware"
)

func (a *API) GetAccount(w http.ResponseWriter, r *http.Request) {
	principal, _ := middleware.UserFromContext(r.Context())
	policy, err := a.Users.Policy(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "identity service unavailable"})
		return
	}
	if principal.AuthSource == "oidc" {
		writeJSON(w, http.StatusOK, map[string]any{
			"user": principal, "policy": policy, "managedBy": "oidc",
		})
		return
	}
	profile, _, err := a.Users.Profile(r.Context(), principal.Username)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "identity service unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": profile, "policy": policy, "managedBy": "highland"})
}

func (a *API) ChangePassword(w http.ResponseWriter, r *http.Request) {
	principal, _ := middleware.UserFromContext(r.Context())
	if principal.AuthSource == "oidc" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "password is managed by the configured identity provider"})
		return
	}
	var request struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if !decodeAccountRequest(w, r, &request) {
		return
	}
	if err := a.Users.ChangePassword(r.Context(), principal.Username, request.CurrentPassword, request.NewPassword); err != nil {
		writeAccountMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "password_changed", "reauthenticationRequired": true})
}

func (a *API) ChangeEmail(w http.ResponseWriter, r *http.Request) {
	principal, _ := middleware.UserFromContext(r.Context())
	if principal.AuthSource == "oidc" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "email is managed by the configured identity provider"})
		return
	}
	var request struct {
		CurrentPassword string `json:"currentPassword"`
		Email           string `json:"email"`
	}
	if !decodeAccountRequest(w, r, &request) {
		return
	}
	if err := a.Users.ChangeEmail(r.Context(), principal.Username, request.CurrentPassword, request.Email); err != nil {
		writeAccountMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "email_changed", "reauthenticationRequired": true})
}

func (a *API) BeginMFAEnrollment(w http.ResponseWriter, r *http.Request) {
	principal, _ := middleware.UserFromContext(r.Context())
	if principal.AuthSource == "oidc" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "multi-factor authentication is managed by the configured identity provider"})
		return
	}
	var request struct {
		CurrentPassword string `json:"currentPassword"`
	}
	if !decodeAccountRequest(w, r, &request) {
		return
	}
	enrollment, err := a.Users.BeginMFAEnrollment(r.Context(), principal.Username, request.CurrentPassword)
	if err != nil {
		writeAccountMutationError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	writeJSON(w, http.StatusOK, enrollment)
}

func (a *API) ConfirmMFAEnrollment(w http.ResponseWriter, r *http.Request) {
	principal, _ := middleware.UserFromContext(r.Context())
	var request struct {
		Code string `json:"code"`
	}
	if !decodeAccountRequest(w, r, &request) {
		return
	}
	if err := a.Users.ConfirmMFAEnrollment(r.Context(), principal.Username, request.Code); err != nil {
		writeAccountMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "mfa_enabled", "reauthenticationRequired": true})
}

func (a *API) DisableMFA(w http.ResponseWriter, r *http.Request) {
	principal, _ := middleware.UserFromContext(r.Context())
	var request struct {
		CurrentPassword string `json:"currentPassword"`
		Code            string `json:"code"`
	}
	if !decodeAccountRequest(w, r, &request) {
		return
	}
	if err := a.Users.DisableMFA(r.Context(), principal.Username, request.CurrentPassword, request.Code); err != nil {
		writeAccountMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "mfa_disabled", "reauthenticationRequired": true})
}

func (a *API) GetSecurityPolicy(w http.ResponseWriter, r *http.Request) {
	policy, err := a.Users.Policy(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "identity service unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

func (a *API) PutSecurityPolicy(w http.ResponseWriter, r *http.Request) {
	var policy auth.SecurityPolicy
	if !decodeAccountRequest(w, r, &policy) {
		return
	}
	if err := a.Users.UpdatePolicy(r.Context(), policy); err != nil {
		writeAccountMutationError(w, err)
		return
	}
	updated, _ := a.Users.Policy(r.Context())
	writeJSON(w, http.StatusOK, updated)
}

func decodeAccountRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return false
	}
	return true
}

func writeAccountMutationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "current credentials are invalid"})
	case errors.Is(err, auth.ErrIdentityUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "identity service unavailable"})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
}
