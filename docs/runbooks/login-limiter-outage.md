# Runbook: Shared login limiter outage

## Symptoms

- Alert `HighlandSharedLimiterUnavailable`
- Local login returns 503 "login protection temporarily unavailable"
- Redis ping failures from API pods

## Immediate actions

1. Confirm Redis Service endpoints and NetworkPolicy allow API → Redis.
2. Check Redis auth/TLS settings match `HIGHLAND_LOGIN_LIMITER_REDIS_*`.
3. **Do not** enable fail-open unless an incident commander authorizes break-glass
   (`HIGHLAND_LOGIN_LIMITER_FAIL_OPEN=true`); it weakens brute-force protection.

## Recovery

1. Restore Redis; counters are ephemeral and may reset (documented).
2. Verify alternating failed logins across API pods share one threshold.
3. Disable fail-open after recovery.

## Related

- ADR-0005
- `chart/examples/values-production-ha.yaml`
