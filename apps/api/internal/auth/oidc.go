package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OIDCConfig is required for real IdP login.
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	// RoleClaim is the ID token claim mapped to Highland role (default: highland_role or groups).
	RoleClaim string
	// DefaultRole when claim missing.
	DefaultRole Role
}

// oidcStateStore persists short-lived OIDC CSRF state. Redis-backed when Redis
// is configured (multi-replica), in-memory otherwise (single replica).
type oidcStateStore interface {
	put(state string, ttl time.Duration)
	consume(state string) bool
}

// memoryStateStore keeps CSRF state in-process (single-replica / offline).
type memoryStateStore struct {
	mu     sync.Mutex
	states map[string]time.Time
}

func newMemoryStateStore() *memoryStateStore {
	return &memoryStateStore{states: map[string]time.Time{}}
}

func (m *memoryStateStore) put(state string, ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[state] = time.Now().Add(ttl)
	// prune expired entries
	for k, exp := range m.states {
		if time.Now().After(exp) {
			delete(m.states, k)
		}
	}
}

func (m *memoryStateStore) consume(state string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	exp, ok := m.states[state]
	delete(m.states, state)
	return ok && !time.Now().After(exp)
}

// newOIDCStateStore selects Redis when the session backend is Redis-backed,
// reusing that client; otherwise falls back to in-memory state.
func newOIDCStateStore(auth *Authenticator) oidcStateStore {
	if auth != nil && auth.store != nil {
		if rb, ok := auth.store.backend.(*RedisBackend); ok {
			return newRedisStateStore(rb.rdb)
		}
	}
	return newMemoryStateStore()
}

// OIDCProvider handles authorize + callback.
type OIDCProvider struct {
	cfg      OIDCConfig
	provider *oidc.Provider
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
	// state stores short-lived CSRF state (Redis or memory).
	state oidcStateStore
	// sessions issued via authenticator
	auth *Authenticator
}

// NewOIDCProvider discovers the issuer. Returns error if issuer unreachable (call at startup only if configured).
func NewOIDCProvider(ctx context.Context, cfg OIDCConfig, auth *Authenticator) (*OIDCProvider, error) {
	if cfg.Issuer == "" || cfg.ClientID == "" || cfg.RedirectURL == "" {
		return nil, fmt.Errorf("oidc: issuer, clientID, redirectURL required")
	}
	if cfg.DefaultRole == "" {
		cfg.DefaultRole = RoleOperator
	}
	if cfg.RoleClaim == "" {
		cfg.RoleClaim = "highland_role"
	}
	p, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discover: %w", err)
	}
	oauth := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     p.Endpoint(),
		RedirectURL:  cfg.RedirectURL,
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
	return &OIDCProvider{
		cfg:      cfg,
		provider: p,
		oauth:    oauth,
		verifier: p.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		state:    newOIDCStateStore(auth),
		auth:     auth,
	}, nil
}

// AuthCodeURL starts the OIDC flow and returns redirect URL + state cookie value.
func (o *OIDCProvider) AuthCodeURL() (url, state string, err error) {
	state, err = randomState()
	if err != nil {
		return "", "", err
	}
	o.state.put(state, 10*time.Minute)
	return o.oauth.AuthCodeURL(state), state, nil
}

// HandleCallback exchanges code, verifies ID token, issues Highland session.
func (o *OIDCProvider) HandleCallback(ctx context.Context, state, code string) (sessionID string, user *User, err error) {
	if !o.state.consume(state) {
		return "", nil, fmt.Errorf("invalid or expired state")
	}
	tok, err := o.oauth.Exchange(ctx, code)
	if err != nil {
		return "", nil, fmt.Errorf("token exchange: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		return "", nil, fmt.Errorf("no id_token in token response")
	}
	idTok, err := o.verifier.Verify(ctx, rawID)
	if err != nil {
		return "", nil, fmt.Errorf("verify id_token: %w", err)
	}
	var claims map[string]any
	if err := idTok.Claims(&claims); err != nil {
		return "", nil, err
	}
	username := claimString(claims, "email")
	if username == "" {
		username = claimString(claims, "preferred_username")
	}
	if username == "" {
		username = idTok.Subject
	}
	role := o.cfg.DefaultRole
	if v := claimString(claims, o.cfg.RoleClaim); v != "" {
		role = ParseRole(v)
	} else if groups, ok := claims["groups"].([]any); ok {
		role = roleFromGroups(groups)
	}
	u := User{Username: username, Email: username, Role: role, AuthSource: "oidc"}
	sid, err := o.auth.IssueSession(u)
	if err != nil {
		return "", nil, err
	}
	return sid, &u, nil
}

func roleFromGroups(groups []any) Role {
	for _, g := range groups {
		s := strings.ToLower(fmt.Sprint(g))
		if strings.Contains(s, "admin") {
			return RoleAdmin
		}
	}
	for _, g := range groups {
		s := strings.ToLower(fmt.Sprint(g))
		if strings.Contains(s, "operator") || strings.Contains(s, "edit") {
			return RoleOperator
		}
	}
	return RoleViewer
}

func claimString(m map[string]any, k string) string {
	v, ok := m[k]
	if !ok || v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// SetOIDCStateCookie stores CSRF state.
func SetOIDCStateCookie(w http.ResponseWriter, name, state string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
}

// ClearOIDCStateCookie removes state cookie.
func ClearOIDCStateCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1})
}

// EncodeBasic is unused helper kept for tests.
func EncodeBasic(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}
