package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// RuntimeOIDCSettings is enterprise SSO config that can be updated at runtime
// (in-memory, optionally persisted to HIGHLAND_OIDC_CONFIG_FILE).
type RuntimeOIDCSettings struct {
	Enabled      bool   `json:"enabled"`
	IssuerURL    string `json:"issuerURL"`
	ClientID     string `json:"clientID"`
	ClientSecret string `json:"clientSecret,omitempty"`
	RedirectURL  string `json:"redirectURL"`
	RoleClaim    string `json:"roleClaim"`
}

// PublicOIDCConfig is the admin GET response shape (never includes the secret).
type PublicOIDCConfig struct {
	Enabled     bool   `json:"enabled"`
	IssuerURL   string `json:"issuerURL"`
	ClientID    string `json:"clientID"`
	RedirectURL string `json:"redirectURL"`
	RoleClaim   string `json:"roleClaim"`
	SecretSet   bool   `json:"secretSet"`
	Ready       bool   `json:"ready"`
	// InitError is set when discovery/re-init last failed (settings still stored).
	InitError string `json:"initError,omitempty"`
}

// OIDCRuntime holds mutable OIDC settings and the live provider (if discovery succeeded).
type OIDCRuntime struct {
	mu         sync.RWMutex
	settings   RuntimeOIDCSettings
	provider   *OIDCProvider
	auth       *Authenticator
	configPath string
	initError  string
}

// NewOIDCRuntime creates a runtime holder. configPath may be empty (no persist).
func NewOIDCRuntime(authenticator *Authenticator, configPath string) *OIDCRuntime {
	return &OIDCRuntime{
		auth:       authenticator,
		configPath: configPath,
		settings: RuntimeOIDCSettings{
			RoleClaim: "highland_role",
		},
	}
}

// SeedFromEnv loads initial values from bootstrap env/config (does not discover yet).
func (r *OIDCRuntime) SeedFromEnv(issuer, clientID, secret, redirect, roleClaim string, enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if issuer != "" {
		r.settings.IssuerURL = issuer
	}
	if clientID != "" {
		r.settings.ClientID = clientID
	}
	if secret != "" {
		r.settings.ClientSecret = secret
	}
	if redirect != "" {
		r.settings.RedirectURL = redirect
	}
	if roleClaim != "" {
		r.settings.RoleClaim = roleClaim
	} else if r.settings.RoleClaim == "" {
		r.settings.RoleClaim = "highland_role"
	}
	r.settings.Enabled = enabled
}

// LoadFile overlays settings from the optional JSON config file (if present).
func (r *OIDCRuntime) LoadFile() error {
	if r.configPath == "" {
		return nil
	}
	raw, err := os.ReadFile(r.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read oidc config file: %w", err)
	}
	var s RuntimeOIDCSettings
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("parse oidc config file: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// File wins over env seed for fields it sets.
	r.settings.Enabled = s.Enabled
	if s.IssuerURL != "" {
		r.settings.IssuerURL = s.IssuerURL
	}
	if s.ClientID != "" {
		r.settings.ClientID = s.ClientID
	}
	if s.ClientSecret != "" {
		r.settings.ClientSecret = s.ClientSecret
	}
	if s.RedirectURL != "" {
		r.settings.RedirectURL = s.RedirectURL
	}
	if s.RoleClaim != "" {
		r.settings.RoleClaim = s.RoleClaim
	}
	return nil
}

// Init tries OIDC discovery when enabled and required fields are set.
func (r *OIDCRuntime) Init(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reinitLocked(ctx)
}

// Provider returns the live provider (nil if not ready).
func (r *OIDCRuntime) Provider() *OIDCProvider {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.provider
}

// IsEnabled reports whether admin has SSO enabled (regardless of discovery readiness).
func (r *OIDCRuntime) IsEnabled() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.settings.Enabled
}

// IsReady is true when a provider is live.
func (r *OIDCRuntime) IsReady() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.provider != nil
}

// Public returns secret-safe config for GET responses.
func (r *OIDCRuntime) Public() PublicOIDCConfig {
	if r == nil {
		return PublicOIDCConfig{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return PublicOIDCConfig{
		Enabled:     r.settings.Enabled,
		IssuerURL:   r.settings.IssuerURL,
		ClientID:    r.settings.ClientID,
		RedirectURL: r.settings.RedirectURL,
		RoleClaim:   r.settings.RoleClaim,
		SecretSet:   r.settings.ClientSecret != "",
		Ready:       r.provider != nil,
		InitError:   r.initError,
	}
}

// UpdateRequest is the admin PUT body. Empty ClientSecret means keep existing.
type UpdateRequest struct {
	Enabled      bool   `json:"enabled"`
	IssuerURL    string `json:"issuerURL"`
	ClientID     string `json:"clientID"`
	ClientSecret string `json:"clientSecret"`
	RedirectURL  string `json:"redirectURL"`
	RoleClaim    string `json:"roleClaim"`
}

// Update applies admin changes, optionally persists, and re-inits the provider if possible.
func (r *OIDCRuntime) Update(ctx context.Context, req UpdateRequest) (PublicOIDCConfig, error) {
	if r == nil {
		return PublicOIDCConfig{}, fmt.Errorf("oidc runtime not available")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.settings.Enabled = req.Enabled
	r.settings.IssuerURL = req.IssuerURL
	r.settings.ClientID = req.ClientID
	r.settings.RedirectURL = req.RedirectURL
	if req.RoleClaim != "" {
		r.settings.RoleClaim = req.RoleClaim
	} else if r.settings.RoleClaim == "" {
		r.settings.RoleClaim = "highland_role"
	}
	// Empty secret on PUT = leave unchanged (UI placeholder "unchanged if empty").
	if req.ClientSecret != "" {
		r.settings.ClientSecret = req.ClientSecret
	}

	if err := r.persistLocked(); err != nil {
		return PublicOIDCConfig{}, err
	}

	// Best-effort re-init; keep settings even if discovery fails.
	_ = r.reinitLocked(ctx)

	return PublicOIDCConfig{
		Enabled:     r.settings.Enabled,
		IssuerURL:   r.settings.IssuerURL,
		ClientID:    r.settings.ClientID,
		RedirectURL: r.settings.RedirectURL,
		RoleClaim:   r.settings.RoleClaim,
		SecretSet:   r.settings.ClientSecret != "",
		Ready:       r.provider != nil,
		InitError:   r.initError,
	}, nil
}

func (r *OIDCRuntime) reinitLocked(ctx context.Context) error {
	r.provider = nil
	r.initError = ""

	if !r.settings.Enabled {
		return nil
	}
	if r.settings.IssuerURL == "" || r.settings.ClientID == "" || r.settings.RedirectURL == "" {
		r.initError = "issuerURL, clientID, and redirectURL are required when enabled"
		return fmt.Errorf("%s", r.initError)
	}
	if r.auth == nil {
		r.initError = "authenticator not configured"
		return fmt.Errorf("%s", r.initError)
	}

	// Bound discovery if caller didn't.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
	}

	p, err := NewOIDCProvider(ctx, OIDCConfig{
		Issuer:       r.settings.IssuerURL,
		ClientID:     r.settings.ClientID,
		ClientSecret: r.settings.ClientSecret,
		RedirectURL:  r.settings.RedirectURL,
		RoleClaim:    r.settings.RoleClaim,
		DefaultRole:  RoleOperator,
	}, r.auth)
	if err != nil {
		r.initError = err.Error()
		return err
	}
	r.provider = p
	return nil
}

func (r *OIDCRuntime) persistLocked() error {
	if r.configPath == "" {
		return nil
	}
	raw, err := json.MarshalIndent(r.settings, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.configPath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("write oidc config: %w", err)
	}
	if err := os.Rename(tmp, r.configPath); err != nil {
		// Fallback for filesystems without atomic rename across volumes.
		if err2 := os.WriteFile(r.configPath, raw, 0o600); err2 != nil {
			return fmt.Errorf("persist oidc config: %w", err2)
		}
		_ = os.Remove(tmp)
	}
	return nil
}
