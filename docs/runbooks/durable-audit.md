# Runbook: Durable audit unavailable

## Symptoms

- Alert `HighlandDurableAuditUnavailable` or `HighlandAuditAppendFailures`
- Privileged mutations return `AUDIT_REQUIRED_UNAVAILABLE` (503)
- Status shows audit backend degraded

## Immediate actions

1. Confirm API pods still ready; do **not** disable storage writes as a silent bypass.
2. Check Postgres (or configured durable sink) connectivity from an API pod.
3. Verify credentials/DSN in the Secret referenced by the chart HA profile.
4. If JSONL single-replica path: check PVC mount and filesystem space.

## Recovery

1. Restore backend availability.
2. Confirm `GET /api/v1/audit` returns recent events from **any** replica.
3. Resume privileged operations only after a successful test admission event.
4. If events were quarantined during import, review counts from the import tool.

## Rollback

Never delete the durable audit database to recover application code. Roll back
API/chart digests while retaining the audit store read-only if needed.

## Related

- ADR-0004, ADR-0005
- `docs/INSTALL.md` production HA profile
