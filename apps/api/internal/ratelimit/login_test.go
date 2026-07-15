package ratelimit

import (
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

func TestThresholdLocksOut(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := testLimiter(&now)
	const ip = "1.2.3.4:5"
	// First MaxFailuresUser-1 failures still allow (same user+IP).
	for i := 0; i < 2; i++ {
		l.RecordFailure("admin", ip)
		if ok, _ := l.Allow("admin", ip); !ok {
			t.Fatalf("locked out too early after %d failures", i+1)
		}
	}
	// 3rd failure hits the user@IP threshold → locked for that user+IP.
	l.RecordFailure("admin", ip)
	ok, retry := l.Allow("admin", ip)
	if ok {
		t.Fatal("expected lockout at user@IP threshold")
	}
	if retry <= 0 {
		t.Fatalf("expected positive Retry-After, got %v", retry)
	}
}

func TestNoCrossIPAccountLockout(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := testLimiter(&now)
	// Attacker hammers "admin" from their own IP until locked.
	for i := 0; i < 3; i++ {
		l.RecordFailure("admin", "10.0.0.1:1")
	}
	if ok, _ := l.Allow("admin", "10.0.0.1:1"); ok {
		t.Fatal("attacker's own IP should be locked for that account")
	}
	// The real admin logging in from a DIFFERENT IP must NOT be locked out —
	// this is the break-glass DoS the design deliberately avoids.
	if ok, _ := l.Allow("admin", "203.0.113.7:9"); !ok {
		t.Fatal("admin from a fresh IP must not be locked by a remote attacker")
	}
}

func TestPerIPSprayLock(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := testLimiter(&now)
	// One IP sprays many distinct usernames → the per-IP counter (5) trips.
	for _, u := range []string{"a", "b", "c", "d", "e"} {
		l.RecordFailure(u, "198.51.100.5:1")
	}
	if ok, _ := l.Allow("fresh-user", "198.51.100.5:1"); ok {
		t.Fatal("a sprayed IP should be locked for any username")
	}
	// A different IP is unaffected.
	if ok, _ := l.Allow("a", "203.0.113.1:1"); !ok {
		t.Fatal("unrelated IP should be allowed")
	}
}

func TestExponentialBackoffClamped(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := testLimiter(&now)
	var peak time.Duration
	for round := 0; round < 8; round++ {
		l.RecordFailure("u", "1.1.1.1:1")
		_, retry := l.Allow("u", "1.1.1.1:1")
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
	l.RecordFailure("u", ip)
	l.RecordFailure("u", ip)
	now = now.Add(20 * time.Minute) // beyond FailureWindow → decay
	l.RecordFailure("u", ip)
	if ok, _ := l.Allow("u", ip); !ok {
		t.Fatal("expected counter to decay after idle window, but locked")
	}
}

func TestResetOnSuccess(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := testLimiter(&now)
	const ip = "1.1.1.1:1"
	l.RecordFailure("u", ip)
	l.RecordFailure("u", ip)
	l.RecordSuccess("u", ip)
	// Two more failures should not lock (counter was cleared).
	l.RecordFailure("u", ip)
	if ok, _ := l.Allow("u", ip); !ok {
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
	l.RecordFailure("u", "1.1.1.1:1")
	if ok, _ := l.Allow("u", "1.1.1.1:1"); !ok {
		t.Fatal("disabled limiter must allow everything")
	}
}
