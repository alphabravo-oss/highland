#!/usr/bin/env bash
# Bundle modular OpenAPI into a single document with Redocly CLI.
# Usage:
#   hack/openapi-bundle.sh                 # bundle highland-v1 → stdout path default
#   hack/openapi-bundle.sh highland-v1     # same
#   hack/openapi-bundle.sh storage-v1      # bundle storage-v1
#   OUT=path.yaml hack/openapi-bundle.sh   # custom output
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

DOC="${1:-highland-v1}"
case "$DOC" in
  highland-v1|highland) ENTRY="docs/openapi/highland-v1.yaml" ;;
  storage-v1|storage)   ENTRY="docs/openapi/storage-v1.yaml" ;;
  *)
    if [[ -f "$DOC" ]]; then
      ENTRY="$DOC"
    elif [[ -f "docs/openapi/${DOC}.yaml" ]]; then
      ENTRY="docs/openapi/${DOC}.yaml"
    else
      echo "ERROR: unknown OpenAPI entry '${DOC}'" >&2
      echo "Usage: $0 [highland-v1|storage-v1|path/to.yaml]" >&2
      exit 2
    fi
    ;;
esac

BASENAME="$(basename "$ENTRY" .yaml)"
OUT="${OUT:-docs/openapi/generated/${BASENAME}.bundled.yaml}"
mkdir -p "$(dirname "$OUT")"

REDOCLY_VERSION="${REDOCLY_VERSION:-2.18.0}"
REDOCLY=(npx --yes "@redocly/cli@${REDOCLY_VERSION}")

if ! command -v npx >/dev/null 2>&1; then
  echo "ERROR: npx is required to run @redocly/cli@${REDOCLY_VERSION}" >&2
  exit 1
fi

echo "== openapi bundle: ${ENTRY} → ${OUT} (redocly@${REDOCLY_VERSION}) =="
"${REDOCLY[@]}" bundle "$ENTRY" \
  --config redocly.yaml \
  --output "$OUT"

echo "OK: wrote ${OUT}"
