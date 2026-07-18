// Package ratelimit provides an in-memory brute-force limiter for local login.
//
// It is keyed by client IP and by (username @ client IP) — NOT by username
// alone. A standalone global per-username lock is deliberately avoided: it would
// let an unauthenticated attacker who merely knows the admin username hold the
// break-glass account locked from anywhere, a self-inflicted DoS. Tying the
// account counter to the source IP means an attacker can only throttle logins
// from their own IP, never lock out an admin coming from a different address.
//
//   - per-IP counter (higher threshold, tolerates NAT): stops one source
//     spraying many usernames.
//   - per-(user@IP) counter (lower threshold): stops one source hammering one
//     account.
//
// A request is denied if EITHER key is locked. Lockouts grow exponentially up
// to a ceiling and reset on the next successful login from that key.
//
// The client IP MUST be trustworthy: callers pass the RemoteAddr resolved by
// the ClientIP middleware, which only honors forwarding headers from configured
// trusted proxies. State is per-process; a shared store behind the same
// interface is the HA follow-up.
package ratelimit

import (
	"context"
	"net"
	"sync"
	"time"
)

// Options configures a LoginLimiter. Zero MaxEntries disables the cap.
type Options struct {
	Enabled         bool
	MaxFailuresUser int // per (user@IP)
	MaxFailuresIP   int // per IP
	LockoutBase     time.Duration
	LockoutMax      time.Duration
	FailureWindow   time.Duration
	MaxEntries      int
}

type entry struct {
	failures    int
	lockedUntil time.Time
	lastSeen    time.Time
}

// LoginLimiter tracks failed-login state keyed by client IP and by user@IP.
type LoginLimiter struct {
	opt   Options
	mu    sync.Mutex
	ip    map[string]*entry
	combo map[string]*entry // key: username + "@" + ipKey
	now   func() time.Time  // injectable clock for tests
	stop  chan struct{}
}

// New builds a LoginLimiter. When Enabled it starts a background janitor that
// evicts expired/idle entries. A disabled limiter allows everything.
func New(opt Options) *LoginLimiter {
	l := &LoginLimiter{
		opt:   opt,
		ip:    make(map[string]*entry),
		combo: make(map[string]*entry),
		now:   time.Now,
		stop:  make(chan struct{}),
	}
	if opt.Enabled && opt.FailureWindow > 0 {
		go l.janitor()
	}
	return l
}

// Allow implements Limiter.
func (l *LoginLimiter) Allow(ctx context.Context, username, remoteAddr string) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	if !l.opt.Enabled {
		return Decision{Allowed: true}, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	ipk := l.ipKey(remoteAddr)
	var retry time.Duration
	locked := false
	for _, e := range []*entry{l.ip[ipk], l.combo[username+"@"+ipk]} {
		if e != nil && now.Before(e.lockedUntil) {
			locked = true
			if d := e.lockedUntil.Sub(now); d > retry {
				retry = d
			}
		}
	}
	return Decision{Allowed: !locked, RetryIn: retry}, nil
}

// RecordFailure implements Limiter.
func (l *LoginLimiter) RecordFailure(ctx context.Context, username, remoteAddr string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !l.opt.Enabled {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	ipk := l.ipKey(remoteAddr)
	l.bump(l.ip, ipk, l.opt.MaxFailuresIP, now)
	l.bump(l.combo, username+"@"+ipk, l.opt.MaxFailuresUser, now)
	return nil
}

// RecordSuccess implements Limiter.
func (l *LoginLimiter) RecordSuccess(ctx context.Context, username, remoteAddr string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !l.opt.Enabled {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	ipk := l.ipKey(remoteAddr)
	delete(l.ip, ipk)
	delete(l.combo, username+"@"+ipk)
	return nil
}

// Health implements Limiter.
func (l *LoginLimiter) Health(ctx context.Context) Health {
	_ = ctx
	if l == nil {
		return Health{Status: "unavailable", Backend: "memory"}
	}
	if !l.opt.Enabled {
		return Health{Status: "ok", Backend: "memory", Message: "disabled"}
	}
	return Health{Status: "ok", Backend: "memory"}
}

// Close implements Limiter.
func (l *LoginLimiter) Close() error {
	l.Stop()
	return nil
}

// Ensure LoginLimiter satisfies Limiter.
var _ Limiter = (*LoginLimiter)(nil)

// Stop halts the janitor goroutine (safe to call more than once).
func (l *LoginLimiter) Stop() {
	select {
	case <-l.stop:
	default:
		close(l.stop)
	}
}

// bump applies idle-decay then increments, locking with backoff at threshold.
// Caller holds the mutex.
func (l *LoginLimiter) bump(m map[string]*entry, key string, threshold int, now time.Time) {
	if key == "" || threshold <= 0 {
		return
	}
	e := m[key]
	if e == nil {
		l.ensureCapacity(m, now)
		e = &entry{}
		m[key] = e
	}
	// Idle beyond the failure window → treat as a fresh streak.
	if l.opt.FailureWindow > 0 && !e.lastSeen.IsZero() && now.Sub(e.lastSeen) > l.opt.FailureWindow {
		e.failures = 0
		e.lockedUntil = time.Time{}
	}
	e.failures++
	e.lastSeen = now
	if e.failures >= threshold {
		e.lockedUntil = now.Add(l.backoff(e.failures - threshold))
	}
}

// backoff = LockoutBase * 2^n, clamped to LockoutMax. Computed by iterative
// doubling with an early exit so a large n cannot overflow the duration.
func (l *LoginLimiter) backoff(n int) time.Duration {
	d := l.opt.LockoutBase
	if d <= 0 {
		d = time.Minute
	}
	for i := 0; i < n; i++ {
		if l.opt.LockoutMax > 0 && d >= l.opt.LockoutMax {
			return l.opt.LockoutMax
		}
		d <<= 1
	}
	if l.opt.LockoutMax > 0 && d > l.opt.LockoutMax {
		d = l.opt.LockoutMax
	}
	return d
}

// ensureCapacity keeps a map under MaxEntries by first sweeping expired entries,
// then evicting the oldest unlocked entry, and — only if everything is locked —
// the oldest locked entry, so the cap is a hard bound. Caller holds the mutex.
func (l *LoginLimiter) ensureCapacity(m map[string]*entry, now time.Time) {
	if l.opt.MaxEntries <= 0 || len(m) < l.opt.MaxEntries {
		return
	}
	for k, e := range m {
		if l.expired(e, now) {
			delete(m, k)
		}
	}
	if len(m) < l.opt.MaxEntries {
		return
	}
	// Prefer evicting the oldest unlocked entry; fall back to the oldest overall
	// so a fully-locked map can never grow past the cap.
	var unlockedKey, oldestKey string
	var unlockedSeen, oldestSeen time.Time
	haveUnlocked, haveOldest := false, false
	for k, e := range m {
		if !haveOldest || e.lastSeen.Before(oldestSeen) {
			oldestKey, oldestSeen, haveOldest = k, e.lastSeen, true
		}
		if now.Before(e.lockedUntil) {
			continue
		}
		if !haveUnlocked || e.lastSeen.Before(unlockedSeen) {
			unlockedKey, unlockedSeen, haveUnlocked = k, e.lastSeen, true
		}
	}
	if haveUnlocked {
		delete(m, unlockedKey)
	} else if haveOldest {
		delete(m, oldestKey)
	}
}

func (l *LoginLimiter) expired(e *entry, now time.Time) bool {
	if now.Before(e.lockedUntil) {
		return false
	}
	return l.opt.FailureWindow > 0 && now.Sub(e.lastSeen) > l.opt.FailureWindow
}

func (l *LoginLimiter) janitor() {
	t := time.NewTicker(l.opt.FailureWindow)
	defer t.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-t.C:
			l.mu.Lock()
			now := l.now()
			for _, m := range []map[string]*entry{l.ip, l.combo} {
				for k, e := range m {
					if l.expired(e, now) {
						delete(m, k)
					}
				}
			}
			l.mu.Unlock()
		}
	}
}

// ipKey normalizes a RemoteAddr to a stable bucket: IPv4 to /32, IPv6 to /64
// (so an attacker cannot rotate through a /64 to dodge the per-IP counter).
func (l *LoginLimiter) ipKey(remoteAddr string) string {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return host // fall back to the raw string rather than dropping the key
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String() + "/32"
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}
