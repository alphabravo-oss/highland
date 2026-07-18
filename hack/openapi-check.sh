#!/usr/bin/env bash
# Full OpenAPI gate: lint, STRICT route inventory, regenerate, fail on dirty diff.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

echo "== 1) lint =="
bash ./hack/check-openapi.sh

echo "== 2) STRICT route coverage =="
STRICT=1 bash ./hack/openapi-route-inventory.sh

echo "== 3) regenerate wire artifacts =="
bash ./hack/openapi-generate.sh

echo "== 4) drift check (git) =="
# Only check generated + openapi sources that must stay in sync with codegen.
paths=(
  docs/openapi/generated/highland-v1.bundled.yaml
  apps/api/internal/api/gen/highland.gen.go
  apps/web/src/api/generated/highland-v1.ts
)
if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "WARN: not a git worktree; skipping dirty-diff (artifacts written)"
  exit 0
fi

dirty=0
for p in "${paths[@]}"; do
  if [[ ! -f "$p" ]]; then
    echo "ERROR: missing generated artifact $p" >&2
    dirty=1
  fi
done

# Determinism: regenerate twice and ensure tracked files match the working tree.
# Modified tracked files after regen mean the commit is out of date.
status="$(git status --porcelain -- "${paths[@]}" || true)"
if echo "$status" | grep -E '^[ MADRCU][ MADRCU]|^[MADRCU]' | grep -v '^??' | grep -q .; then
  echo "ERROR: OpenAPI generated artifacts differ from the git index/HEAD after regenerate:" >&2
  echo "$status" >&2
  echo "Run ./hack/openapi-generate.sh and commit the result." >&2
  dirty=1
fi
if echo "$status" | grep -q '^??'; then
  echo "NOTE: generated artifacts are untracked (first commit of codegen output):"
  echo "$status" | grep '^??' || true
  echo "Commit them so CI drift checks compare against HEAD."
fi

if [[ "$dirty" -ne 0 ]]; then
  exit 1
fi

echo "OK: OpenAPI lint + route coverage + generation drift clean"
