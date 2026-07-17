package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Role is a Highland RBAC role.
type Role string

const (
	RoleViewer   Role = "viewer"
	RoleOperator Role = "operator"
	RoleAdmin    Role = "admin"
)

// User is the authenticated principal returned by /auth/me.
type User struct {
	Username         string `json:"username"`
	Email            string `json:"email,omitempty"`
	Role             Role   `json:"role"`
	AuthSource       string `json:"authSource,omitempty"`
	MFAEnabled       bool   `json:"mfaEnabled,omitempty"`
	MFASetupRequired bool   `json:"mfaSetupRequired,omitempty"`
	SessionVersion   int    `json:"-"`
}

// Session holds a server-side session.
type Session struct {
	ID        string    `json:"id"`
	User      User      `json:"user"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// Authenticator validates local credentials and issues sessions.
type Authenticator struct {
	users *UserStore
	store *Store
}

// NewAuthenticator creates a local-login authenticator with multi-user support.
func NewAuthenticator(users *UserStore, store *Store) *Authenticator {
	return &Authenticator{users: users, store: store}
}

// Login checks credentials and returns a session ID on success.
func (a *Authenticator) Login(ctx context.Context, username, password string) (string, *User, error) {
	user, err := a.users.Authenticate(ctx, username, password)
	if err != nil {
		return "", nil, err
	}
	id, err := a.store.Create(*user)
	if err != nil {
		return "", nil, err
	}
	return id, user, nil
}

func (a *Authenticator) Authenticate(ctx context.Context, username, password string) (*User, error) {
	return a.users.Authenticate(ctx, username, password)
}

// IssueSession creates a session for an already-authenticated user (OIDC).
func (a *Authenticator) IssueSession(user User) (string, error) {
	return a.store.Create(user)
}

// Store returns the underlying session store.
func (a *Authenticator) Store() *Store {
	return a.store
}

// Users returns the user store.
func (a *Authenticator) Users() *UserStore {
	return a.users
}

func randomID(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// RandomSecret returns nBytes of cryptographically-random data (session signing key).
func RandomSecret(nBytes int) ([]byte, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}
