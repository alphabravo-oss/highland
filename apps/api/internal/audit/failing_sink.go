package audit

import (
	"context"
	"sync"
	"time"
)

// FailingSink is a test double that fails Append with a fixed error.
// When failAfter >= 0, the first failAfter appends succeed.
type FailingSink struct {
	mu        sync.Mutex
	err       error
	failAfter int
	count     int
	Events    []Event
}

func NewFailingSink(err error) *FailingSink {
	return &FailingSink{err: err, failAfter: 0}
}

// NewFailingAfter succeeds n times then fails.
func NewFailingAfter(n int, err error) *FailingSink {
	return &FailingSink{err: err, failAfter: n}
}

func (f *FailingSink) Append(ctx context.Context, event Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.count >= f.failAfter {
		return f.err
	}
	f.count++
	f.Events = append(f.Events, event)
	return nil
}

func (f *FailingSink) List(ctx context.Context, query Query) (Page, error) {
	_ = ctx
	_ = query
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Event, len(f.Events))
	copy(out, f.Events)
	// newest first
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return Page{Events: out}, nil
}

func (f *FailingSink) Health(ctx context.Context) Health {
	_ = ctx
	return Health{Status: "unavailable", Backend: "failing", Message: f.err.Error()}
}

func (f *FailingSink) Durable() bool { return true }

func (f *FailingSink) Close(ctx context.Context) error {
	_ = ctx
	return nil
}

// SharedMemorySink is a process-shared durable-looking sink for multi-replica tests.
type SharedMemorySink struct {
	mu     sync.Mutex
	events []Event
	seq    int
}

func NewSharedMemorySink() *SharedMemorySink {
	return &SharedMemorySink{}
}

func (s *SharedMemorySink) Append(ctx context.Context, e Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateEvent(e); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	if e.ID == "" {
		e.ID = itoa(s.seq)
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	s.events = append(s.events, e)
	return nil
}

func (s *SharedMemorySink) List(ctx context.Context, query Query) (Page, error) {
	if err := ctx.Err(); err != nil {
		return Page{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var filtered []Event
	for i := len(s.events) - 1; i >= 0; i-- {
		if matchQuery(s.events[i], query) {
			filtered = append(filtered, s.events[i])
		}
	}
	limit := query.Limit
	if limit <= 0 || limit > len(filtered) {
		limit = len(filtered)
	}
	return Page{Events: filtered[:limit]}, nil
}

func (s *SharedMemorySink) Health(ctx context.Context) Health {
	_ = ctx
	return Health{Status: "ok", Backend: "shared-memory", Durable: true}
}

func (s *SharedMemorySink) Durable() bool { return true }

func (s *SharedMemorySink) Close(ctx context.Context) error {
	_ = ctx
	return nil
}
