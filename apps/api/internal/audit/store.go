package audit

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// Event is an audit log entry.
type Event struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Method    string    `json:"method"`
	Path      string    `json:"path"`
	Result    string    `json:"result"` // ok | denied | error
	SourceIP  string    `json:"sourceIp"`
	Message   string    `json:"message,omitempty"`
}

// Store is an in-memory ring buffer with optional append-only file.
type Store struct {
	mu       sync.RWMutex
	events   []Event
	max      int
	filePath string
	seq      int
}

// NewStore creates an audit store. max=0 defaults to 2000.
func NewStore(max int, filePath string) *Store {
	if max <= 0 {
		max = 2000
	}
	return &Store{events: make([]Event, 0, 256), max: max, filePath: filePath}
}

// Append records an event.
func (s *Store) Append(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	if e.ID == "" {
		e.ID = time.Now().UTC().Format("20060102T150405") + "-" + itoa(s.seq)
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	s.events = append(s.events, e)
	if len(s.events) > s.max {
		s.events = s.events[len(s.events)-s.max:]
	}
	if s.filePath != "" {
		f, err := os.OpenFile(s.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			_ = json.NewEncoder(f).Encode(e)
			_ = f.Close()
		}
	}
}

// List returns newest-first events, optionally limited.
func (s *Store) List(limit int) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := len(s.events)
	if limit <= 0 || limit > n {
		limit = n
	}
	out := make([]Event, limit)
	for i := 0; i < limit; i++ {
		out[i] = s.events[n-1-i]
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
