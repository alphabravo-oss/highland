// Package audit provides failure-aware audit storage for Highland.
//
// Call sites must classify appends as required (fail closed) or best-effort.
// See docs/adr/0004-production-durable-audit-and-ha-profiles.md.
package audit

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// SchemaVersion is the current audit event schema version for encoding.
const SchemaVersion = 1

// Common typed errors for sink operations.
var (
	ErrNotDurable     = errors.New("audit sink is not durable")
	ErrUnavailable    = errors.New("audit sink unavailable")
	ErrInvalidEvent   = errors.New("invalid audit event")
	ErrSecretRejected = errors.New("audit event rejected: secret material")
	ErrDuplicateEvent = errors.New("duplicate audit event id")
)

// Decision classifies whether an append is a precondition for a mutation.
type AppendClass int

const (
	// AppendBestEffort records evidence when possible; failures are observable
	// via metrics but do not block the request path.
	AppendBestEffort AppendClass = iota
	// AppendRequired must succeed before a privileged mutation is admitted.
	AppendRequired
)

// Query filters and paginates audit events (newest-first by default).
type Query struct {
	Limit      int
	Cursor     string // opaque; empty = start
	Action     string
	Result     string
	ProviderID string
	OperationID string
	Username   string // bounded exact match only
	Since      time.Time
	Until      time.Time
}

// Page is a list result with an optional next cursor.
type Page struct {
	Events     []Event
	NextCursor string
}

// Health describes sink readiness for status APIs and readiness probes.
type Health struct {
	Status    string `json:"status"` // ok | degraded | unavailable
	Durable   bool   `json:"durable"`
	Backend   string `json:"backend"`
	Message   string `json:"message,omitempty"`
	QueueDepth int   `json:"queueDepth,omitempty"`
}

// Sink is the multi-replica-aware audit contract.
type Sink interface {
	Append(ctx context.Context, event Event) error
	List(ctx context.Context, query Query) (Page, error)
	Health(ctx context.Context) Health
	Durable() bool
	Close(ctx context.Context) error
}

// TerminalEvidence is optional: durable sinks that can prove terminal operation
// audit events for StorageOperation garbage collection (fail closed if absent).
type TerminalEvidence interface {
	DurableTerminalOperationIDs() (map[string]bool, error)
}

// ListRecent is the shared query helper used by handlers (replica-independent
// when the sink is shared Postgres / SharedMemory).
func ListRecent(ctx context.Context, s Sink, limit int) []Event {
	if s == nil {
		return nil
	}
	if limit <= 0 {
		limit = 100
	}
	page, err := s.List(ctx, Query{Limit: limit})
	if err != nil {
		return nil
	}
	return page.Events
}

// EventBuilder constructs validated, redacted events.
type EventBuilder struct {
	event Event
	err   error
}

// NewEvent starts a builder with schema version and UTC timestamp.
func NewEvent() *EventBuilder {
	return &EventBuilder{event: Event{
		SchemaVersion: SchemaVersion,
		Timestamp:     time.Now().UTC(),
	}}
}

func (b *EventBuilder) ID(id string) *EventBuilder          { b.event.ID = id; return b }
func (b *EventBuilder) Username(u string) *EventBuilder      { b.event.Username = u; return b }
func (b *EventBuilder) Role(r string) *EventBuilder          { b.event.Role = r; return b }
func (b *EventBuilder) Action(a string) *EventBuilder        { b.event.Action = a; return b }
func (b *EventBuilder) Target(t string) *EventBuilder        { b.event.Target = t; return b }
func (b *EventBuilder) Method(m string) *EventBuilder        { b.event.Method = m; return b }
func (b *EventBuilder) Path(p string) *EventBuilder          { b.event.Path = p; return b }
func (b *EventBuilder) Result(r string) *EventBuilder        { b.event.Result = r; return b }
func (b *EventBuilder) SourceIP(ip string) *EventBuilder     { b.event.SourceIP = ip; return b }
func (b *EventBuilder) Message(m string) *EventBuilder       { b.event.Message = redactMessage(m); return b }
func (b *EventBuilder) OperationID(id string) *EventBuilder  { b.event.OperationID = id; return b }
func (b *EventBuilder) ActionID(id string) *EventBuilder     { b.event.ActionID = id; return b }
func (b *EventBuilder) ProviderID(id string) *EventBuilder   { b.event.ProviderID = id; return b }
func (b *EventBuilder) ProviderKind(k string) *EventBuilder  { b.event.ProviderKind = k; return b }
func (b *EventBuilder) TargetKind(k string) *EventBuilder    { b.event.TargetKind = k; return b }
func (b *EventBuilder) TargetNamespace(n string) *EventBuilder {
	b.event.TargetNamespace = n
	return b
}
func (b *EventBuilder) TargetName(n string) *EventBuilder { b.event.TargetName = n; return b }
func (b *EventBuilder) TargetUID(u string) *EventBuilder  { b.event.TargetUID = u; return b }
func (b *EventBuilder) PlanHash(h string) *EventBuilder   { b.event.PlanHash = h; return b }
func (b *EventBuilder) CorrelationID(id string) *EventBuilder {
	b.event.CorrelationID = id
	return b
}
func (b *EventBuilder) HTTPCorrelationID(id string) *EventBuilder {
	b.event.HTTPCorrelationID = id
	return b
}

// Build validates and returns the event or an error.
func (b *EventBuilder) Build() (Event, error) {
	if b.err != nil {
		return Event{}, b.err
	}
	if err := ValidateEvent(b.event); err != nil {
		return Event{}, err
	}
	return b.event, nil
}

// ValidateEvent rejects empty required fields and obvious secret material.
func ValidateEvent(e Event) error {
	if e.Action == "" {
		return fmt.Errorf("%w: action is required", ErrInvalidEvent)
	}
	if e.Result == "" {
		return fmt.Errorf("%w: result is required", ErrInvalidEvent)
	}
	if containsSecretMarkers(e.Message) || containsSecretMarkers(e.Target) {
		return ErrSecretRejected
	}
	return nil
}

// RequireAppend appends and wraps failure for required admission paths.
func RequireAppend(ctx context.Context, sink Sink, event Event) error {
	if sink == nil {
		return fmt.Errorf("%w: sink is nil", ErrUnavailable)
	}
	if err := sink.Append(ctx, event); err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return nil
}

// BestEffortAppend appends without failing the caller; returns the sink error
// for metrics only.
func BestEffortAppend(ctx context.Context, sink Sink, event Event) error {
	if sink == nil {
		return nil
	}
	return sink.Append(ctx, event)
}
