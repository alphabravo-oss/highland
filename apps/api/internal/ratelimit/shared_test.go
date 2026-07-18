package ratelimit

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeRedis is a minimal in-process dual-key limiter used to prove that two
// "replicas" share one threshold without a real Redis (ADR-0005).
type sharedCounterLimiter struct {
	mu    sync.Mutex
	inner *LoginLimiter
}

func newSharedCounterLimiter() *sharedCounterLimiter {
	now := time.Unix(1_700_000_000, 0)
	l := New(Options{
		Enabled: true, MaxFailuresUser: 3, MaxFailuresIP: 10,
		LockoutBase: time.Minute, LockoutMax: 15 * time.Minute, FailureWindow: 15 * time.Minute,
	})
	l.Stop()
	l.now = func() time.Time { return now }
	return &sharedCounterLimiter{inner: l}
}

func (s *sharedCounterLimiter) Allow(ctx context.Context, username, clientKey string) (Decision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.Allow(ctx, username, clientKey)
}
func (s *sharedCounterLimiter) RecordFailure(ctx context.Context, username, clientKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.RecordFailure(ctx, username, clientKey)
}
func (s *sharedCounterLimiter) RecordSuccess(ctx context.Context, username, clientKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.RecordSuccess(ctx, username, clientKey)
}
func (s *sharedCounterLimiter) Health(ctx context.Context) Health {
	return Health{Status: "ok", Backend: "shared-test"}
}
func (s *sharedCounterLimiter) Close() error { return nil }

func TestAlternatingReplicasShareThreshold(t *testing.T) {
	shared := newSharedCounterLimiter()
	// Two replicas use the same backend handle.
	replicaA, replicaB := Limiter(shared), Limiter(shared)
	ctx := context.Background()
	const user, ip = "admin", "198.51.100.10:443"

	for i := 0; i < 2; i++ {
		if err := replicaA.RecordFailure(ctx, user, ip); err != nil {
			t.Fatal(err)
		}
		dec, err := replicaB.Allow(ctx, user, ip)
		if err != nil {
			t.Fatal(err)
		}
		if !dec.Allowed {
			t.Fatalf("locked too early at failure %d", i+1)
		}
	}
	if err := replicaB.RecordFailure(ctx, user, ip); err != nil {
		t.Fatal(err)
	}
	// Third failure via B must lock A as well.
	dec, err := replicaA.Allow(ctx, user, ip)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Allowed {
		t.Fatal("shared threshold must lock both replicas")
	}
}

func TestFailClosedOutagePolicy(t *testing.T) {
	// RedisLimiter.onOutage fail-closed returns error Decision.
	l := &RedisLimiter{opt: RedisOptions{OutagePolicy: OutageFailClosed, Options: Options{Enabled: true}}}
	dec, err := l.onOutage(context.DeadlineExceeded)
	if err == nil {
		t.Fatal("fail-closed must return error")
	}
	if dec.Allowed {
		t.Fatal("fail-closed must not allow on outage")
	}
	l.opt.OutagePolicy = OutageFailOpen
	dec, err = l.onOutage(context.DeadlineExceeded)
	if err != nil || !dec.Allowed {
		t.Fatalf("fail-open must allow: dec=%+v err=%v", dec, err)
	}
}
