# ADR-0005: Shared login limiter outage policy

Status: **Accepted**
Date: 2026-07-18
Decision: DEC-4

## Context

Local-login throttling is process-local. Attackers can hop replicas to reset
effective thresholds. HA mode needs a shared limiter and an explicit policy when
the backend is unavailable.

## Decision (DEC-4)

### Interface

```go
type LoginLimiter interface {
    Allow(ctx context.Context, username, clientKey string) (Decision, error)
    RecordFailure(ctx context.Context, username, clientKey string) error
    RecordSuccess(ctx context.Context, username, clientKey string) error
    Health(ctx context.Context) Health
    Close() error
}
```

### Implementations

- `MemoryLoginLimiter` — single-replica / development (existing semantics).
- `RedisLoginLimiter` — atomic Lua/transaction updates for IP and `user@IP`.

### Outage policy

| Profile | Backend required | On backend error |
|---|---|---|
| development / single-replica | no | N/A (memory) |
| default multi-replica without Redis | no | memory per process (documented limitation) |
| production HA | yes | **fail closed**: deny local login with 503-class limiter unavailable response; emit metric/alert |

Operator override `limiter.failOpen: true` is allowed only as an explicit,
audited break-glass setting that:

- logs a high-severity audit/metric event on each backend error path;
- surfaces degraded status in `/api/v1/status`;
- is documented as weakening brute-force protection.

IPv4 `/32` and IPv6 `/64` normalization, exponential backoff, failure windows,
and success reset are preserved. Passwords, cookies, MFA codes, and recovery
codes are never stored in limiter keys. Usernames in Redis keys are hashed with
a per-installation salt for privacy while remaining collision-safe for the
limiter purpose.

## Consequences

- HA profile chart values require Redis for login limiting.
- Status API exposes limiter backend identity and health without connection
  secrets.
