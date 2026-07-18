#!/usr/bin/env bash
# Optional breaking-change check against a baseline OpenAPI bundle.
# Baseline path (default): docs/openapi/generated/baseline/highland-v1.bundled.yaml
# When baseline is missing, exits 0 with a note (first setup).
# When oasdiff is unavailable, exits 0 with a note unless STRICT=1.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

CURRENT="${CURRENT:-docs/openapi/generated/highland-v1.bundled.yaml}"
BASELINE="${BASELINE:-docs/openapi/generated/baseline/highland-v1.bundled.yaml}"
STRICT="${STRICT:-0}"

if [[ ! -f "$CURRENT" ]]; then
  echo "ERROR: current bundle missing: $CURRENT (run ./hack/openapi-generate.sh)" >&2
  exit 2
fi

if [[ ! -f "$BASELINE" ]]; then
  echo "NOTE: no baseline at $BASELINE — skipping breaking-diff."
  echo "      After the first released contract, copy the bundle there to enable the gate."
  exit 0
fi

if ! command -v oasdiff >/dev/null 2>&1; then
  if [[ "$STRICT" == "1" ]]; then
    echo "ERROR: oasdiff required for STRICT breaking check (https://github.com/oasdiff/oasdiff)" >&2
    exit 1
  fi
  echo "NOTE: oasdiff not installed; skipping breaking-diff (set STRICT=1 to fail)."
  exit 0
fi

echo "== oasdiff breaking: $BASELINE → $CURRENT =="
oasdiff breaking "$BASELINE" "$CURRENT"
echo "OK: no breaking changes vs baseline"
