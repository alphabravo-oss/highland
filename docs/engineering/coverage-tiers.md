# Risk-tiered coverage (ENG-E6 / ADR-0007 DEC-13)

Highland measures statement coverage by **risk tier**, not a single global
percentage. Thresholds start from measured baselines and only ratchet upward
with review (ADR-0007).

## Tiers

| Tier | Intent | Initial gate | Examples |
|---|---|---|---|
| **critical** | Security, authz, audit, planning/execution | ≥ 80% statements after migration; no unexplained drop | `internal/auth`, `internal/audit`, `internal/middleware`, `internal/policy`, `internal/storage/operations`, `internal/ratelimit` |
| **high** | Provider clients, inventory, readiness, chart RBAC | ≥ 70% | provider adapters, inventory correlation, readiness paths |
| **standard** | Presentation, helpers, insights | Track; no hard fail initially | handlers presentation, UI helpers, insights |
| **generated** | Code generated from OpenAPI/CRDs | Compile/test only; **excluded** from handwritten % | `**/gen/**`, openapi-generated TypeScript |
| **excluded** | Mocks, fixtures, pure docs | n/a | fixtures, mocks, documentation-only trees |

## Critical package inventory (baseline script)

The local reporter `hack/coverage-tiers.sh` exercises these Go packages under
`apps/api`:

| Package path | Role |
|---|---|
| `internal/auth` | Sessions, tokens, RBAC roles, identity |
| `internal/audit` | Append-only audit sink and durability |
| `internal/middleware` | Auth, CSRF, RBAC, security headers |
| `internal/policy` | Storage write policy and ceilings |
| `internal/storage/operations` | Action catalogue, plan, execute, confirm |
| `internal/ratelimit` | Login and shared rate limiting |

## How to run

From the repository root:

```bash
./hack/coverage-tiers.sh
```

Behavior:

1. Runs `go test` with `-coverprofile` for each critical package.
2. Prints per-package statement coverage percentages.
3. Excludes paths matching `gen/` from aggregation.
4. **Exits 0** while baselines are being established (does not hard-fail on
   thresholds yet). Optional `COVERAGE_FAIL=1` may enable soft threshold
   messaging in future revisions.

## Policy notes

- **Fail-closed paths** need branch/behavior tests, not only statement hits
  (ENG-E6.8).
- **Generated code** is compiled and tested separately; it must not inflate
  handwritten coverage targets (ENG-E6.4).
- Publish tier reports without exposing repository tokens (ENG-E6.12).
- Ratchet only with an explicit baseline decision (ENG-E6.5–E6.7).

## Related

- [ADR-0007](../adr/0007-openapi-qualification-coverage.md) — DEC-13 coverage tiers
- [Production readiness plan](../plans/production-readiness-and-engineering-scale.md) — workstream E6
- [Validation log](../validation/production-readiness-and-engineering-scale.md)
