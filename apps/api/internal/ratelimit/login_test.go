package ratelimit

import (
	"context"
	"testing"
	"time"
)

func testLimiter(now *time.Time) *LoginLimiter {
	l := New(Options{
		Enabled:         true,
		MaxFailuresUser: 3, // per (user@IP)
		MaxFailuresIP:   5, // per IP
		LockoutBase:     time.Minute,
		LockoutMax:      15 * time.Minute,
		FailureWindow:   15 * time.Minute,
		MaxEntries:      1000,
	})
	l.Stop() // no janitor during tests; drive time manually
	l.now = func() time.Time { return *now }
	return l
}

func allow(t *testing.T, l *LoginLimiter, user, ip string) Decision {
	t.Helper()
	dec, err := l.Allow(context.Background(), user, ip)
	if err != nil {
		t.Fatal(err)
	}
	return dec
}

func TestThresholdLocksOut(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := testLimiter(&now)
	const ip = "1.2.3.4:5"
	// First MaxFailuresUser-1 failures still allow (same user+IP).
	for i := 0; i < 2; i++ {
		_ = l.RecordFailure(context.Background(), "admin", ip)
		if !allow(t, l, "admin", ip).Allowed {
			t.Fatalf("locked out too early after %d failures", i+1)
		}
	}
	// 3rd failure hits the user@IP threshold → locked for that user+IP.
	_ = l.RecordFailure(context.Background(), "admin", ip)
	dec := allow(t, l, "admin", ip)
	if dec.Allowed {
		t.Fatal("expected lockout at user@IP threshold")
	}
	if dec.RetryIn <= 0 {
		t.Fatalf("expected positive Retry-After, got %v", dec.RetryIn)
	}
}

func TestNoCrossIPAccountLockout(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := testLimiter(&now)
	// Attacker hammers "admin" from their own IP until locked.
	for i := 0; i < 3; i++ {
		_ = l.RecordFailure(context.Background(), "admin", "10.0.0.1:1")
	}
	if allow(t, l, "admin", "10.0.0.1:1").Allowed {
		t.Fatal("attacker's own IP should be locked for that account")
	}
	// The real admin logging in from a DIFFERENT IP must NOT be locked out —
	// this is the break-glass DoS the design deliberately avoids.
	if !allow(t, l, "admin", "203.0.113.7:9").Allowed {
		t.Fatal("admin from a fresh IP must not be locked by a remote attacker")
	}
}

func TestPerIPSprayLock(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := testLimiter(&now)
	// One IP sprays many distinct usernames → the per-IP counter (5) trips.
	for _, u := range []string{"a", "b", "c", "d", "e"} {
		_ = l.RecordFailure(context.Background(), u, "198.51.100.5:1")
	}
	if allow(t, l, "fresh-user", "198.51.100.5:1").Allowed {
		t.Fatal("a sprayed IP should be locked for any username")
	}
	// A different IP is unaffected.
	if !allow(t, l, "a", "203.0.113.1:1").Allowed {
		t.Fatal("unrelated IP should be allowed")
	}
}

func TestExponentialBackoffClamped(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := testLimiter(&now)
	var peak time.Duration
	for round := 0; round < 8; round++ {
		_ = l.RecordFailure(context.Background(), "u", "1.1.1.1:1")
		retry := allow(t, l, "u", "1.1.1.1:1").RetryIn
		if retry > 0 {
			if retry > 15*time.Minute {
				t.Fatalf("lockout %v exceeded LockoutMax", retry)
			}
			if retry > peak {
				peak = retry
			}
			now = now.Add(retry + time.Second) // move past the lock to try again
		}
	}
	if peak != 15*time.Minute {
		t.Fatalf("expected backoff to reach the 15m ceiling, got %v", peak)
	}
}

func TestIdleDecayResets(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := testLimiter(&now)
	const ip = "1.1.1.1:1"
	_ = l.RecordFailure(context.Background(), "u", ip)
	_ = l.RecordFailure(context.Background(), "u", ip)
	now = now.Add(20 * time.Minute) // beyond FailureWindow → decay
	_ = l.RecordFailure(context.Background(), "u", ip)
	if !allow(t, l, "u", ip).Allowed {
		t.Fatal("expected counter to decay after idle window, but locked")
	}
}

func TestResetOnSuccess(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := testLimiter(&now)
	const ip = "1.1.1.1:1"
	_ = l.RecordFailure(context.Background(), "u", ip)
	_ = l.RecordFailure(context.Background(), "u", ip)
	_ = l.RecordSuccess(context.Background(), "u", ip)
	// Two more failures should not lock (counter was cleared).
	_ = l.RecordFailure(context.Background(), "u", ip)
	if !allow(t, l, "u", ip).Allowed {
		t.Fatal("expected fresh counter after success")
	}
}

func TestIPKeyNormalization(t *testing.T) {
	l := testLimiter(&[]time.Time{time.Unix(1, 0)}[0])
	cases := map[string]string{
		"1.2.3.4:55":           "1.2.3.4/32",
		"1.2.3.4":              "1.2.3.4/32",
		"[2001:db8::1]:443":    "2001:db8::/64",
		"2001:db8:0:0:aaaa::1": "2001:db8::/64",
	}
	for in, want := range cases {
		if got := l.ipKey(in); got != want {
			t.Errorf("ipKey(%q)=%q want %q", in, got, want)
		}
	}
}

func TestDisabledAllowsAll(t *testing.T) {
	l := New(Options{Enabled: false})
	_ = l.RecordFailure(context.Background(), "u", "1.1.1.1:1")
	if !allow(t, l, "u", "1.1.1.1:1").Allowed {
		t.Fatal("disabled limiter must allow everything")
	}
}
