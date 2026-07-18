# ADR-0007: OpenAPI toolchain, qualification evidence, and coverage tiers

Status: **Accepted**
Date: 2026-07-18
Decisions: DEC-10, DEC-11, DEC-12, DEC-13, DEC-14

## Context

Compatibility claims are not machine-enforced. OpenAPI covers only storage.
Coverage is global and does not reflect security risk.

## Decision

### DEC-10 — Compatibility evidence freshness and ancestry

- Result states: `passed`, `failed`, `flaky`, `skipped`, `blocked`, `not-run`.
- `skipped` / `not-run` **never** satisfy a release gate.
- Freshness: PR smoke = current commit; nightly health ≤ 7 days; production
  promotion requires evidence for the **exact candidate digests** (or an
  ancestor commit that changed only documentation/metadata as proven by a
  machine-checkable path filter).
- Invalidation: changes under `apps/api/internal/storage`, providers, chart
  CRDs/RBAC, or operation planner invalidate provider release gates.

### DEC-11 — OpenAPI generator toolchain

- Canonical layout: modular OpenAPI 3.1 under `docs/openapi/` with root
  `highland-v1.yaml`.
- Lint/bundle: Redocly CLI (pinned version + checksum in CI).
- TypeScript wire types: `openapi-typescript` (pinned).
- Go wire types: `oapi-codegen` (pinned) into `apps/api/internal/api/gen/`.
- Domain models remain handwritten; mapping functions bridge wire ↔ domain.
- Generation is deterministic and offline after tool install; CI fails on dirty
  diff.

### DEC-12 — Public API and deprecation policy

- Public surface: `/auth/*`, `/api/v1/*`, health/readiness as documented.
- Metrics may be excluded from public client contracts.
- Breaking change: remove/rename path or field, retype field, change authz
  semantics, or change error code meaning without dual-support window.
- Deprecation: mark in OpenAPI + changelog; retain ≥ one minor release; emit
  `Deprecation` / `Sunset` headers where practical.

### DEC-13 — Coverage tier thresholds

| Tier | Packages (examples) | Initial gate |
|---|---|---|
| critical | auth, middleware CSRF/RBAC, audit, policy, operations planner/execution, identity, app bootstrap | ≥ 80% statements after migration; no unexplained drop |
| high | provider clients, inventory correlation, readiness, chart security tests | ≥ 70% |
| standard | handlers presentation, insights, UI helpers | track; no hard fail initially |
| generated | `**/gen/**`, openapi generated TS | compile/test only; excluded from handwritten % |
| excluded | mocks, fixtures, pure docs | n/a |

Thresholds start from measured baselines and only ratchet upward with approval.

### DEC-14 — Rolling version skew

Supported skew: **current and previous** Highland chart/app minor within the
same release line for API↔web (N and N-1). Greater skew is unsupported.

## Consequences

- `qualification/` holds schemas, profiles, runner, and result format.
- OpenAPI route inventory must match Chi registration for public routes.
- Coverage CI publishes tier reports without untrusted token exposure.
