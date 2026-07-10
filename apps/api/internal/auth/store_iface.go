package auth

import "time"

// SessionBackend stores sessions for single- or multi-replica API.
type SessionBackend interface {
	Create(user User, ttl time.Duration) (id string, err error)
	Get(id string) (*Session, bool)
	Delete(id string)
}

// Store wraps a SessionBackend with a fixed TTL (used by handlers).
type Store struct {
	backend SessionBackend
	ttl     time.Duration
}

// NewStoreFromBackend builds the session Store used by the API.
func NewStoreFromBackend(b SessionBackend, ttl time.Duration) *Store {
	return &Store{backend: b, ttl: ttl}
}

// NewStore is in-memory (single replica / offline).
func NewStore(ttl time.Duration) *Store {
	return NewStoreFromBackend(NewMemoryBackend(), ttl)
}

func (s *Store) Create(user User) (string, error) {
	return s.backend.Create(user, s.ttl)
}

func (s *Store) Get(id string) (*Session, bool) {
	return s.backend.Get(id)
}

func (s *Store) Delete(id string) {
	s.backend.Delete(id)
}
