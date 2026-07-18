package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Event is an audit log entry.
type Event struct {
	SchemaVersion           int       `json:"schemaVersion,omitempty"`
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

// Store is an in-memory ring buffer with optional append-only JSONL file.
// It implements Sink. JSONL is suitable for single-replica durability; production
// HA uses PostgresAuditSink (ADR-0004).
type Store struct {
	mu       sync.RWMutex
	events   []Event
	max      int
	filePath string
	seq      int
	backend  string
}

// Ensure Store satisfies Sink.
var _ Sink = (*Store)(nil)

// NewStore creates an audit store. max=0 defaults to 2000.
func NewStore(max int, filePath string) *Store {
	if max <= 0 {
		max = 2000
	}
	backend := "memory"
	if filePath != "" {
		backend = "jsonl"
	}
	return &Store{events: make([]Event, 0, 256), max: max, filePath: filePath, backend: backend}
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

// Append implements Sink. Failures are returned to callers (no silent ignore).
func (s *Store) Append(ctx context.Context, e Event) error {
	if s == nil {
		return fmt.Errorf("%w: store is nil", ErrUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.SchemaVersion == 0 {
		e.SchemaVersion = SchemaVersion
	}
	// Reject secret material before redaction so required admission cannot
	// launder sensitive fields into durable storage.
	if err := ValidateEvent(e); err != nil {
		return err
	}
	e.Message = redactMessage(e.Message)

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
		if err != nil {
			return fmt.Errorf("%w: open jsonl: %v", ErrUnavailable, err)
		}
		encErr := json.NewEncoder(f).Encode(e)
		closeErr := f.Close()
		if encErr != nil {
			return fmt.Errorf("%w: encode jsonl: %v", ErrUnavailable, encErr)
		}
		if closeErr != nil {
			return fmt.Errorf("%w: close jsonl: %v", ErrUnavailable, closeErr)
		}
	}
	return nil
}

// ListRecent returns newest-first events, optionally limited (legacy helper).
func (s *Store) ListRecent(limit int) []Event {
	page, err := s.List(context.Background(), Query{Limit: limit})
	if err != nil {
		return nil
	}
	return page.Events
}

// List implements Sink with filters and cursor pagination (newest-first).
func (s *Store) List(ctx context.Context, query Query) (Page, error) {
	if s == nil {
		return Page{}, fmt.Errorf("%w: store is nil", ErrUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return Page{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	var filtered []Event
	for i := len(s.events) - 1; i >= 0; i-- {
		e := s.events[i]
		if !matchQuery(e, query) {
			continue
		}
		filtered = append(filtered, e)
	}

	start := 0
	if query.Cursor != "" {
		for i, e := range filtered {
			if e.ID == query.Cursor {
				start = i + 1
				break
			}
		}
	}
	limit := query.Limit
	if limit <= 0 {
		limit = len(filtered)
	}
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	out := make([]Event, end-start)
	copy(out, filtered[start:end])
	page := Page{Events: out}
	if end < len(filtered) && len(out) > 0 {
		page.NextCursor = out[len(out)-1].ID
	}
	return page, nil
}

func matchQuery(e Event, q Query) bool {
	if q.Action != "" && e.Action != q.Action {
		return false
	}
	if q.Result != "" && e.Result != q.Result {
		return false
	}
	if q.ProviderID != "" && e.ProviderID != q.ProviderID {
		return false
	}
	if q.OperationID != "" && e.OperationID != q.OperationID {
		return false
	}
	if q.Username != "" && e.Username != q.Username {
		return false
	}
	if !q.Since.IsZero() && e.Timestamp.Before(q.Since) {
		return false
	}
	if !q.Until.IsZero() && e.Timestamp.After(q.Until) {
		return false
	}
	return true
}

// Health implements Sink.
func (s *Store) Health(ctx context.Context) Health {
	if s == nil {
		return Health{Status: "unavailable", Backend: "none", Message: "nil store"}
	}
	_ = ctx
	s.mu.RLock()
	defer s.mu.RUnlock()
	h := Health{Backend: s.backend, Durable: s.filePath != ""}
	if s.filePath != "" && !s.durableUnlocked() {
		h.Status = "unavailable"
		h.Message = "jsonl not writable"
		return h
	}
	h.Status = "ok"
	return h
}

// Close implements Sink.
func (s *Store) Close(ctx context.Context) error {
	_ = ctx
	return nil
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
