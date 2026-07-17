#!/usr/bin/env bash
# Run the same checks as CI locally (api unit, web unit/build, e2e, helm).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

echo "== parity matrix =="
"$ROOT/hack/check-parity.sh"

echo "== go test =="
(cd "$ROOT/apps/api" && go test ./... -count=1)

echo "== disabled-provider process smoke =="
"$ROOT/hack/test-api-disabled-providers.sh"

echo "== web typecheck/test/build =="
(cd "$ROOT/apps/web" && npm run typecheck && npm run lint && npm test && npm run build && npm run build-storybook)

echo "== storage OpenAPI =="
(cd "$ROOT" && npx --yes @redocly/cli@2.18.0 lint docs/openapi/storage-v1.yaml --config redocly.yaml)

echo "== playwright e2e =="
(cd "$ROOT/apps/web" && npx playwright install chromium && CI=true npm run test:e2e)

if command -v helm >/dev/null 2>&1; then
  echo "== helm dependency/lint/render =="
  "$ROOT/hack/test-chart.sh"
else
  echo "== helm skipped (not installed) =="
fi

echo "OK: all local CI checks passed"
