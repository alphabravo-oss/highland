package auth

import (
	"sync"
	"time"
)

// MemoryBackend is the default session store (fine for 1 replica).
type MemoryBackend struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{sessions: map[string]*Session{}}
}

func (m *MemoryBackend) Create(user User, ttl time.Duration) (string, error) {
	id, err := randomID(32)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[id] = &Session{ID: id, User: user, ExpiresAt: time.Now().Add(ttl)}
	return id, nil
}

func (m *MemoryBackend) Get(id string) (*Session, bool) {
	m.mu.RLock()
	sess, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Now().After(sess.ExpiresAt) {
		m.Delete(id)
		return nil, false
	}
	return sess, true
}

func (m *MemoryBackend) Delete(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}
