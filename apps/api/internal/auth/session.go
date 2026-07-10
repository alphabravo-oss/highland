package auth

import (
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
	Username string `json:"username"`
	Role     Role   `json:"role"`
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
func (a *Authenticator) Login(username, password string) (string, *User, error) {
	user, ok := a.users.Authenticate(username, password)
	if !ok {
		return "", nil, ErrInvalidCredentials
	}
	id, err := a.store.Create(*user)
	if err != nil {
		return "", nil, err
	}
	return id, user, nil
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

// ErrInvalidCredentials is returned on failed login.
var ErrInvalidCredentials = errInvalidCredentials{}

type errInvalidCredentials struct{}

func (errInvalidCredentials) Error() string { return "invalid credentials" }

func randomID(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
