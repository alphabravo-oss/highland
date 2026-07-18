# Bundled OpenAPI artifacts

Output of `hack/openapi-bundle.sh` (Redocly CLI `@redocly/cli@2.18.0`).

Default products:

| Source | Bundled file |
|--------|----------------|
| `docs/openapi/highland-v1.yaml` | `highland-v1.bundled.yaml` |
| `docs/openapi/storage-v1.yaml` | `storage-v1.bundled.yaml` |

Bundled files may be gitignored or regenerated in CI; generators
(`openapi-typescript`, `oapi-codegen`) should consume the bundled document so
external `$ref`s are already resolved.
