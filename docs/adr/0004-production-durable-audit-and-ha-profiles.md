# ADR-0004: Production durable audit backend and HA profiles

Status: **Accepted**
Date: 2026-07-18
Decisions: DEC-1, DEC-2, DEC-3, DEC-5, DEC-6

## Context

Highland 0.3.x stores audit events in a process-local ring with optional JSONL
append. Append failures are ignored, list queries are replica-local, and shared
RWX file append is not a safe multi-writer HA design. Production readiness
requires an explicit `AuditSink` contract, failure policy, and deployment
profiles.

## Decision

### DEC-1 — Production durable audit backend

| Profile | Backend | Durable |
|---|---|---|
| single-replica development | `MemoryAuditSink` or optional JSONL | no / yes (JSONL) |
| default multi-replica | `MemoryAuditSink` unless durable audit configured | no |
| production HA | **PostgreSQL** (`PostgresAuditSink`) | yes |

PostgreSQL is selected over shared JSONL, CR-per-event, and single-writer PVC
because it provides atomic append, cursor pagination, multi-replica access,
backup tooling operators already know, and clear failure semantics. Shared RWX
JSONL is **not** an acceptable production HA design.

JSONL remains a **migration source and export format**, not the HA primary.

### DEC-2 — When durable audit is mandatory

Durable audit is **mandatory** when any of the following are true in a
production HA profile:

- storage writes (portable or provider-native) are enabled;
- admin policy control mutations are enabled;
- local user administration is enabled beyond bootstrap.

Read-only inventory/console installs may run without durable audit. Development
profiles never require PostgreSQL.

### DEC-3 — Required-audit failure policy by action class

| Class | Pre-event | Post-event | On append failure |
|---|---|---|---|
| storage mutations (create plan/operation) | **required** admission | best-effort + pending delivery on CR | **fail closed** — do not create operation |
| policy / identity admin mutations | **required** | best-effort | **fail closed** |
| login success/failure, denied authz | best-effort | n/a | log metric; do not block login response path beyond limiter |
| read-only API | optional | n/a | ignore |

Post-mutation audit outage must never retry the external provider mutation.
`StorageOperation` remains the durable workflow record; audit delivery may be
marked pending and reconciled.

### DEC-5 — Redis topology/support boundary

Redis is **optional** until an operator selects the production HA profile for
shared login limiting and/or centrally revocable sessions. Highland does not
bundle Redis; operators provide Redis (or compatible) with TLS, auth, and HA as
appropriate. Key namespaces include cluster identity and installation.

### DEC-6 — Default PDB and topology behavior

- Replicas = 1: **no PDB** rendered (avoid impossible budgets).
- Replicas ≥ 2: PDB with `maxUnavailable: 1` by default.
- Soft `topologySpreadConstraints` with `whenUnsatisfiable: ScheduleAnyway` and
  hostname anti-affinity preference (not hard requirement) for default installs.
- Production HA example values use stricter zone spreading when multi-zone
  capacity exists.

## Consequences

- Production HA documentation requires Postgres + Redis for full security-state
  parity across API replicas.
- Chart gains audit backend configuration and safe PDB/topology templates.
- Call sites classify audit append as required vs best-effort.

## Alternatives considered

1. **Kubernetes CR per audit event** — native but high API-server load and weak
   pagination at scale.
2. **Shared RWX JSONL** — multi-writer corruption risk; rejected for HA.
3. **Always require Postgres** — too heavy for read-only/dev installs; rejected.
