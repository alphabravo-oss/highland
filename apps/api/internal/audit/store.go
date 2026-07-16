package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Event is an audit log entry.
type Event struct {
	ID                      string    `json:"id"`
	Timestamp               time.Time `json:"timestamp"`
	Username                string    `json:"username"`
	Role                    string    `json:"role"`
	Action                  string    `json:"action"`
	Target                  string    `json:"target"`
	Method                  string    `json:"method"`
	Path                    string    `json:"path"`
	Result                  string    `json:"result"` // ok | denied | error
	SourceIP                string    `json:"sourceIp"`
	Message                 string    `json:"message,omitempty"`
	OperationID             string    `json:"operationId,omitempty"`
	ActionID                string    `json:"actionId,omitempty"`
	ProviderID              string    `json:"providerId,omitempty"`
	ProviderKind            string    `json:"providerKind,omitempty"`
	TargetKind              string    `json:"targetKind,omitempty"`
	TargetNamespace         string    `json:"targetNamespace,omitempty"`
	TargetName              string    `json:"targetName,omitempty"`
	TargetUID               string    `json:"targetUid,omitempty"`
	PlanHash                string    `json:"planHash,omitempty"`
	CorrelationID           string    `json:"correlationId,omitempty"`
	HTTPCorrelationID       string    `json:"httpCorrelationId,omitempty"`
	KubernetesCorrelationID string    `json:"kubernetesCorrelationId,omitempty"`
	CephCorrelationID       string    `json:"cephCorrelationId,omitempty"`
}

// Durable reports whether the append-only audit stream is configured and
// writable now. When it is false, durable StorageOperation objects must not be
// garbage-collected because they are the only persistent operation record.
func (s *Store) Durable() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.durableUnlocked()
}

func (s *Store) durableUnlocked() bool {
	if s == nil || s.filePath == "" {
		return false
	}
	f, err := os.OpenFile(s.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return false
	}
	return f.Close() == nil
}

// DurableTerminalOperationIDs returns the operation IDs whose terminal event
// is actually present in the append-only JSONL stream. A malformed or
// unreadable stream fails closed so callers retain the StorageOperation CR.
func (s *Store) DurableTerminalOperationIDs() (map[string]bool, error) {
	if s == nil {
		return nil, fmt.Errorf("audit store is unavailable")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.durableUnlocked() {
		return nil, fmt.Errorf("durable audit stream is not writable")
	}
	file, err := os.Open(s.filePath)
	if err != nil {
		return nil, fmt.Errorf("open durable audit stream: %w", err)
	}
	defer file.Close()
	result := map[string]bool{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2<<20)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, fmt.Errorf("decode durable audit stream: %w", err)
		}
		if event.OperationID != "" && (event.Action == "storage_operation_succeeded" || event.Action == "storage_operation_failed" || event.Action == "storage_operation_cancelled") {
			result[event.OperationID] = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read durable audit stream: %w", err)
	}
	return result, nil
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
