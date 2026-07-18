# Generated TypeScript API types

**Do not hand-edit** `highland-v1.ts`. Regenerate from the repo root:

```bash
./hack/openapi-generate.sh
./hack/openapi-check.sh
```

| Item | Value |
|------|--------|
| Tool | `openapi-typescript` **7.6.1** (pinned in `hack/openapi-generate.sh`) |
| Source | Bundled `docs/openapi/generated/highland-v1.bundled.yaml` |
| Output | `highland-v1.ts` (`paths` / `components` types) |

## Policy (ADR-0007 / DEC-11)

- Wire types only; feature hooks and transport stay handwritten.
- CI fails on dirty diff after regenerate.
- Consumer smoke: `highland-v1.consumer.test.ts`.
