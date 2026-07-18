# OpenAPI contract migration ledger

Status: **Phase 4 foundation complete** (full public path coverage + generated wire types + CI drift).

## Canonical documents

| Document | Role |
|----------|------|
| `docs/openapi/highland-v1.yaml` | Root public contract (modular `$ref`s) |
| `docs/openapi/storage-v1.yaml` | Storage control-plane (still independently lintable) |
| `docs/openapi/paths/*` | Path modules |
| `docs/openapi/schemas/*` | Shared schemas |
| `docs/openapi/internal-allowlist.txt` | Chi paths intentionally excluded from client contract |
| `docs/openapi/generated/highland-v1.bundled.yaml` | Bundled artifact for generators |

## Generators (pinned in `hack/openapi-generate.sh`)

| Target | Tool | Version |
|--------|------|---------|
| Go types | `oapi-codegen` | v2.4.1 |
| TypeScript | `openapi-typescript` | 7.6.1 |
| Lint/bundle | `@redocly/cli` | 2.18.0 |

```bash
./hack/openapi-generate.sh   # regen
./hack/openapi-check.sh      # lint + STRICT routes + regen + dirty-diff
STRICT=1 ./hack/openapi-breaking.sh  # optional vs baseline/
```

## Allowlisted (not Highland-owned client APIs)

- `/metrics` — Prometheus scrape
- `/api/v1/metrics` — operational summary (not stable client contract)
- `/api/v1/lh`, `/api/v1/lh/*` — Longhorn manager proxy (upstream schema)

## Incremental consumer migration (remaining)

Wire types are generated; **handwritten web DTOs and Go domain models remain** until call sites migrate:

1. Auth + account + users (high value for identity)
2. Platform status/benchmarks
3. Storage families already partially typed in storage-v1
4. Delete superseded manual DTOs only after call-site migration

Tracking: feature PRs should prefer `apps/web/src/api/generated/highland-v1` and `apps/api/internal/api/gen` for new code.
