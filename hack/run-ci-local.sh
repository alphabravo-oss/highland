#!/usr/bin/env bash
# Run the same checks as CI locally (api unit, web unit/build, e2e, helm).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

echo "== parity matrix =="
"$ROOT/hack/check-parity.sh"

echo "== go test =="
(cd "$ROOT/apps/api" && go test ./... -count=1)

echo "== web typecheck/test/build =="
(cd "$ROOT/apps/web" && npm run typecheck && npm test && npm run build)

echo "== playwright e2e =="
(cd "$ROOT/apps/web" && npx playwright install chromium && CI=true npm run test:e2e)

if command -v helm >/dev/null 2>&1; then
  echo "== helm dependency/lint/render =="
  "$ROOT/hack/test-chart.sh"
else
  echo "== helm skipped (not installed) =="
fi

echo "OK: all local CI checks passed"
