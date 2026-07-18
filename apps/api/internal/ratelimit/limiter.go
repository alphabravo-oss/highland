package ratelimit

import (
	"context"
	"time"
)

// Decision is the result of Allow.
type Decision struct {
	Allowed bool
	RetryIn time.Duration
}

// Health describes limiter backend status for status APIs.
type Health struct {
	Status  string `json:"status"` // ok | degraded | unavailable
	Backend string `json:"backend"`
	Message string `json:"message,omitempty"`
}

// Limiter is the backend-neutral login throttle contract (ADR-0005).
type Limiter interface {
	Allow(ctx context.Context, username, clientKey string) (Decision, error)
	RecordFailure(ctx context.Context, username, clientKey string) error
	RecordSuccess(ctx context.Context, username, clientKey string) error
	Health(ctx context.Context) Health
	Close() error
}

// OutagePolicy controls behavior when the shared backend errors.
type OutagePolicy string

const (
	// OutageFailClosed denies login when the backend is unavailable (production HA default).
	OutageFailClosed OutagePolicy = "fail_closed"
	// OutageFailOpen allows login when the backend errors (explicit break-glass only).
	OutageFailOpen OutagePolicy = "fail_open"
)
