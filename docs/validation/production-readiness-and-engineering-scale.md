# Validation: Production readiness and engineering scale

Status: **Automated program bar complete; live/signing deferred honestly**
Program plan: [../plans/production-readiness-and-engineering-scale.md](../plans/production-readiness-and-engineering-scale.md)
Baseline commit (pre-program): `58e6572`
Validation date: 2026-07-18

## Architecture decisions (DEC-1…14)

| DEC | Record |
|-----|--------|
| DEC-1…3,5,6 | [ADR-0004](../adr/0004-production-durable-audit-and-ha-profiles.md) |
| DEC-4 | [ADR-0005](../adr/0005-shared-login-limiter-outage-policy.md) |
| DEC-7…9 | [ADR-0006](../adr/0006-software-supply-chain-policy.md) |
| DEC-10…14 | [ADR-0007](../adr/0007-openapi-qualification-coverage.md) |

Threat model: [../security/ha-multi-replica-threat-model.md](../security/ha-multi-replica-threat-model.md)

## Automated validation (latest run)

| Command | Result |
|---------|--------|
| `go test ./... -count=1` (apps/api) | pass |
| `go vet ./...` | pass |
| `go build ./cmd/highland-api` | pass |
| `SKIP_DEPENDENCY_BUILD=1 hack/test-chart.sh` | pass (PDB 1/2/3, digests, production HA) |
| `hack/openapi-check.sh` | pass (lint + STRICT routes 0 missing + generate) |
| `hack/check-qualification.sh` | pass (skip/not-run cannot pass production) |
| `hack/scan-secrets.sh` | pass |
| web typecheck / lint / unit (89) / build | pass |

### Security / HA shipped-path tests

- Storage submit fail-closed: `TestHTTPSubmitBlocksWhenDurableAuditAdmissionFails`
- Policy apply fail-closed: `TestPolicyApplyFailClosedWhenDurableAuditUnavailable`
- Identity create/update/delete fail-closed: `TestIdentity*FailClosedWhenDurableAuditUnavailable`
- Security policy + OIDC config admit when durable
- Shared limiter: `TestAlternatingReplicasShareThreshold`, `TestFailClosedOutagePolicy`
- Planner exhaustiveness: `TestActionRegistrationExhaustive`
- App Build: fakes, durable required, Postgres DSN failure path
- OpenAPI wire smoke: Go `gen` package + web `highland-v1.consumer.test.ts`

### Supply chain

- fio default: `ghcr.io/aksakalli/fio:3.39` (chart + `benchmark/k8s.go` fallback; no `latest`)
- Chart image digest fields + render tests
- CI: govulncheck, secret scan, openapi-check
- Release: Syft SBOM best-effort; Cosign keyless scaffolded (`if: false` until OIDC)

### Qualification

- 10 profiles linked from `docs/compatibility.yaml`
- Aggregator rejects `skipped` / `not-run` / missing required production profiles

## Deferred (owner / reason / target / impact)

| Item | Owner | Reason | Target | Impact |
|------|-------|--------|--------|--------|
| Live multi-node HA drain | maintainers | no lab | lab env | promotion claims |
| Live provider release gates | maintainers | no provider labs | lab env | promotion claims |
| Cosign always-on | maintainers | OIDC not enabled | release job flip | signatures optional |
| Full inventory/UI split | maintainers | size/equivalence | next minors | DX |
| Delete all handwritten DTOs | maintainers | incremental | call-site PRs | wire types ready |

## Key docs

- Production HA values: `chart/examples/values-production-ha.yaml`
- Artifact verification: `docs/security/artifact-verification.md`
- Runbooks: `docs/runbooks/{durable-audit,login-limiter-outage,ha-availability}.md`
- OpenAPI migration: `docs/openapi/MIGRATION.md`
