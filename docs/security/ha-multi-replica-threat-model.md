# HA multi-replica threat model extension

Extends [storage-threat-model.md](storage-threat-model.md) for multi-replica API
deployments (ADR-0004, ADR-0005).

## Assets

- Durable audit evidence for privileged mutations
- Login throttle state
- Local identity Secret and session-version revocation
- StorageOperation leader authority
- Session signing key and optional Redis session store

## Adversaries and scenarios

| Scenario | Risk | Control |
|---|---|---|
| Replica hopping on login | Bypass per-process throttle | Shared Redis limiter; fail closed when required |
| Partial audit backend outage | Silent loss of privileged evidence | Required pre-mutation admission fail closed |
| Split audit visibility | Operator sees incomplete history | Shared durable sink query path |
| Concurrent JSONL multi-writer | Corruption / lost lines | Forbidden in production HA; Postgres primary |
| Stale identity cache | Revoked user still authorized | Session version check; identity Secret sync |
| Operation leader loss mid-mutation | Duplicate external change | Existing lease + observation before retry |
| Disruption of all API pods | Control plane unavailable | PDB + topology for voluntary disruption |
| Limiter fail-open misconfig | Weakened brute-force defense | Explicit override + metrics + docs |
| Audit metadata leakage | Secrets in logs/metrics | Structured event builders; redaction tests |

## Trust boundaries

- Browser never holds kube/provider credentials (unchanged).
- API replicas share identity Secret and optional Redis/Postgres; network
  policies limit access.
- Trusted proxy CIDRs alone determine client IP for limiter and audit source.

## Recovery objectives (initial)

| State | RPO | RTO (objective) |
|---|---|---|
| Identity Secret | 0 (K8s etcd) | minutes (Secret restore) |
| Audit (Postgres) | last successful backup | restore + verify import |
| Policy / StorageOperation | 0 (K8s) | controller resync |
| Limiter counters | ephemeral acceptable | automatic on Redis recovery |

## Audit retention and privacy

- Privileged operation audit retention lower bound: 90 days (configurable, not
  below bound when writes enabled).
- Redact passwords, tokens, recovery codes, raw bodies, provider credentials.
- Deletion/export follows operator retention config; no silent purge of
  nonterminal operation linkage.
