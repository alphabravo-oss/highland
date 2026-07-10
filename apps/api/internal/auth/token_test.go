package auth

import (
	"testing"
	"time"
)

func TestTokenBackendRoundTrip(t *testing.T) {
	b := NewTokenBackend([]byte("test-secret"))
	tok, err := b.Create(User{Username: "admin", Role: RoleAdmin}, time.Hour)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	sess, ok := b.Get(tok)
	if !ok {
		t.Fatal("valid token rejected")
	}
	if sess.User.Username != "admin" || sess.User.Role != RoleAdmin {
		t.Fatalf("wrong claims: %+v", sess.User)
	}
}

func TestTokenBackendRejectsTamper(t *testing.T) {
	b := NewTokenBackend([]byte("test-secret"))
	tok, _ := b.Create(User{Username: "viewer", Role: RoleViewer}, time.Hour)
	// Flip a character in the payload; signature must no longer match.
	tampered := "x" + tok[1:]
	if _, ok := b.Get(tampered); ok {
		t.Fatal("tampered token accepted")
	}
	// A token signed with a different secret must be rejected.
	other := NewTokenBackend([]byte("other-secret"))
	otherTok, _ := other.Create(User{Username: "admin", Role: RoleAdmin}, time.Hour)
	if _, ok := b.Get(otherTok); ok {
		t.Fatal("token signed with different secret accepted")
	}
}

func TestTokenBackendRejectsExpired(t *testing.T) {
	b := NewTokenBackend([]byte("test-secret"))
	tok, _ := b.Create(User{Username: "admin", Role: RoleAdmin}, -time.Minute)
	if _, ok := b.Get(tok); ok {
		t.Fatal("expired token accepted")
	}
}
