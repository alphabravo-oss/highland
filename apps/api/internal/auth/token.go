package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// tokenPayload is the signed session claim set carried by the cookie.
type tokenPayload struct {
	U     string `json:"u"`           // username
	R     string `json:"r"`           // role
	S     string `json:"s,omitempty"` // authentication source
	V     int    `json:"v,omitempty"` // local-account session version
	Eml   string `json:"m,omitempty"` // display email
	MFA   bool   `json:"f,omitempty"` // MFA enrolled
	Setup bool   `json:"q,omitempty"` // MFA enrollment required
	E     int64  `json:"e"`           // expiry (unix seconds)
}

// TokenBackend implements SessionBackend with stateless, HMAC-signed tokens.
// The cookie value IS the signed claim set — there is no server-side store, so
// sessions survive API restarts and work across replicas with no shared cache.
// Trade-off: no server-side revocation; rely on short TTLs + cookie clearing.
type TokenBackend struct {
	secret []byte
}

// NewTokenBackend creates a stateless session backend signed with secret.
func NewTokenBackend(secret []byte) *TokenBackend {
	return &TokenBackend{secret: secret}
}

func (b *TokenBackend) sign(msg string) string {
	m := hmac.New(sha256.New, b.secret)
	m.Write([]byte(msg))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// Create returns a signed token for the user (used as the cookie value).
func (b *TokenBackend) Create(user User, ttl time.Duration) (string, error) {
	payload, err := json.Marshal(tokenPayload{
		U:     user.Username,
		R:     string(user.Role),
		S:     user.AuthSource,
		V:     user.SessionVersion,
		Eml:   user.Email,
		MFA:   user.MFAEnabled,
		Setup: user.MFASetupRequired,
		E:     time.Now().Add(ttl).Unix(),
	})
	if err != nil {
		return "", err
	}
	p := base64.RawURLEncoding.EncodeToString(payload)
	return p + "." + b.sign(p), nil
}

// Get verifies the token signature and expiry, returning the session.
func (b *TokenBackend) Get(id string) (*Session, bool) {
	parts := strings.SplitN(id, ".", 2)
	if len(parts) != 2 {
		return nil, false
	}
	if !hmac.Equal([]byte(b.sign(parts[0])), []byte(parts[1])) {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, false
	}
	var pl tokenPayload
	if json.Unmarshal(raw, &pl) != nil {
		return nil, false
	}
	exp := time.Unix(pl.E, 0)
	if time.Now().After(exp) {
		return nil, false
	}
	return &Session{ID: id, User: User{
		Username: pl.U, Email: pl.Eml, Role: Role(pl.R), AuthSource: pl.S,
		SessionVersion: pl.V, MFAEnabled: pl.MFA, MFASetupRequired: pl.Setup,
	}, ExpiresAt: exp}, true
}

// Delete is a no-op — stateless tokens cannot be revoked server-side.
func (b *TokenBackend) Delete(string) {}
