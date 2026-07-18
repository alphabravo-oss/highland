package ratelimit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisOptions configures the shared HA login limiter (ADR-0005).
type RedisOptions struct {
	Options
	Addr           string
	Password       string
	DB             int
	KeyPrefix      string // includes installation/cluster identity
	UsernameSalt   string // hashes usernames in keys
	OutagePolicy   OutagePolicy
	DialTimeout    time.Duration
	Client         redis.Cmdable // optional inject for tests
}

// RedisLimiter implements Limiter with atomic dual-key updates.
type RedisLimiter struct {
	opt    RedisOptions
	client redis.Cmdable
	owned  *redis.Client
	now    func() time.Time
}

// NewRedis builds a Redis-backed limiter. When Client is nil it dials Addr.
func NewRedis(opt RedisOptions) (*RedisLimiter, error) {
	if opt.KeyPrefix == "" {
		opt.KeyPrefix = "highland:login"
	}
	if opt.OutagePolicy == "" {
		opt.OutagePolicy = OutageFailClosed
	}
	l := &RedisLimiter{opt: opt, now: time.Now}
	if opt.Client != nil {
		l.client = opt.Client
		return l, nil
	}
	if opt.Addr == "" {
		return nil, fmt.Errorf("redis limiter requires Addr")
	}
	timeout := opt.DialTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:         opt.Addr,
		Password:     opt.Password,
		DB:           opt.DB,
		DialTimeout:  timeout,
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
	})
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("redis limiter ping: %w", err)
	}
	l.client = rdb
	l.owned = rdb
	return l, nil
}

// Lua: atomic allow check for IP and user@IP lock TTLs.
var allowScript = redis.NewScript(`
local ipKey = KEYS[1]
local comboKey = KEYS[2]
local now = tonumber(ARGV[1])
local ipLock = redis.call('HGET', ipKey, 'locked_until')
local comboLock = redis.call('HGET', comboKey, 'locked_until')
local retry = 0
local locked = 0
if ipLock then
  local untilTs = tonumber(ipLock)
  if untilTs and untilTs > now then
    locked = 1
    retry = untilTs - now
  end
end
if comboLock then
  local untilTs = tonumber(comboLock)
  if untilTs and untilTs > now then
    locked = 1
    local d = untilTs - now
    if d > retry then retry = d end
  end
end
return {locked, retry}
`)

// Lua: atomic failure bump for both keys.
var failScript = redis.NewScript(`
local function bump(key, threshold, base, maxLock, window, now)
  local failures = tonumber(redis.call('HGET', key, 'failures') or '0')
  local last = tonumber(redis.call('HGET', key, 'last_seen') or '0')
  if window > 0 and last > 0 and (now - last) > window then
    failures = 0
    redis.call('HDEL', key, 'locked_until')
  end
  failures = failures + 1
  redis.call('HSET', key, 'failures', failures, 'last_seen', now)
  if failures >= threshold and threshold > 0 then
    local n = failures - threshold
    local d = base
    for i = 1, n do
      if maxLock > 0 and d >= maxLock then
        d = maxLock
        break
      end
      d = d * 2
    end
    if maxLock > 0 and d > maxLock then d = maxLock end
    redis.call('HSET', key, 'locked_until', now + d)
    redis.call('PEXPIRE', key, math.floor((window > 0 and window or maxLock) * 1000) + d * 1000)
  else
    redis.call('PEXPIRE', key, math.floor((window > 0 and window or 900) * 1000))
  end
end
local now = tonumber(ARGV[1])
local thrIP = tonumber(ARGV[2])
local thrUser = tonumber(ARGV[3])
local base = tonumber(ARGV[4])
local maxLock = tonumber(ARGV[5])
local window = tonumber(ARGV[6])
bump(KEYS[1], thrIP, base, maxLock, window, now)
bump(KEYS[2], thrUser, base, maxLock, window, now)
return 1
`)

func (l *RedisLimiter) Allow(ctx context.Context, username, remoteAddr string) (Decision, error) {
	if !l.opt.Enabled {
		return Decision{Allowed: true}, nil
	}
	now := l.now().Unix()
	ipk, combok := l.keys(username, remoteAddr)
	res, err := allowScript.Run(ctx, l.client, []string{ipk, combok}, now).Slice()
	if err != nil {
		return l.onOutage(err)
	}
	locked := toInt(res[0]) == 1
	retry := time.Duration(toInt(res[1])) * time.Second
	return Decision{Allowed: !locked, RetryIn: retry}, nil
}

func (l *RedisLimiter) RecordFailure(ctx context.Context, username, remoteAddr string) error {
	if !l.opt.Enabled {
		return nil
	}
	now := l.now().Unix()
	ipk, combok := l.keys(username, remoteAddr)
	base := int64(l.opt.LockoutBase.Seconds())
	if base <= 0 {
		base = 60
	}
	maxLock := int64(l.opt.LockoutMax.Seconds())
	window := int64(l.opt.FailureWindow.Seconds())
	err := failScript.Run(ctx, l.client, []string{ipk, combok},
		now, l.opt.MaxFailuresIP, l.opt.MaxFailuresUser, base, maxLock, window).Err()
	if err != nil {
		_, outErr := l.onOutage(err)
		return outErr
	}
	return nil
}

func (l *RedisLimiter) RecordSuccess(ctx context.Context, username, remoteAddr string) error {
	if !l.opt.Enabled {
		return nil
	}
	ipk, combok := l.keys(username, remoteAddr)
	err := l.client.Del(ctx, ipk, combok).Err()
	if err != nil {
		_, outErr := l.onOutage(err)
		return outErr
	}
	return nil
}

func (l *RedisLimiter) Health(ctx context.Context) Health {
	h := Health{Backend: "redis"}
	if err := l.client.Ping(ctx).Err(); err != nil {
		h.Status = "unavailable"
		h.Message = "ping failed"
		return h
	}
	h.Status = "ok"
	return h
}

func (l *RedisLimiter) Close() error {
	if l.owned != nil {
		return l.owned.Close()
	}
	return nil
}

func (l *RedisLimiter) onOutage(err error) (Decision, error) {
	if l.opt.OutagePolicy == OutageFailOpen {
		return Decision{Allowed: true}, nil
	}
	return Decision{}, fmt.Errorf("login limiter unavailable: %w", err)
}

func (l *RedisLimiter) keys(username, remoteAddr string) (string, string) {
	ip := normalizeIPKey(remoteAddr)
	userHash := hashUsername(username, l.opt.UsernameSalt)
	prefix := l.opt.KeyPrefix
	return prefix + ":ip:" + ip, prefix + ":combo:" + userHash + "@" + ip
}

func normalizeIPKey(remoteAddr string) string {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return host
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String() + "/32"
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

func hashUsername(username, salt string) string {
	sum := sha256.Sum256([]byte(salt + "\x00" + strings.ToLower(username)))
	return hex.EncodeToString(sum[:8])
}

func toInt(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case string:
		var x int64
		fmt.Sscan(n, &x)
		return x
	default:
		return 0
	}
}

var _ Limiter = (*RedisLimiter)(nil)
