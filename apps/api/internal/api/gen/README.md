# Generated Go API types

**Do not hand-edit** `highland.gen.go`. Regenerate from the repo root:

```bash
./hack/openapi-generate.sh
# Full gate (lint + STRICT routes + regen + dirty-diff):
./hack/openapi-check.sh
```

| Item | Value |
|------|--------|
| Tool | `oapi-codegen` **v2.4.1** (pinned in `hack/openapi-generate.sh`) |
| Source | `docs/openapi/highland-v1.yaml` → bundled `docs/openapi/generated/highland-v1.bundled.yaml` |
| Output | `highland.gen.go` package `gen` |
| Mode | `-generate types` only (handlers stay handwritten) |

## Policy (ADR-0007 / DEC-11)

- Wire types only; domain models remain handwritten.
- CI fails on dirty diff after regenerate (`hack/openapi-check.sh`).
- Coverage tier `generated` is compile/test only.
