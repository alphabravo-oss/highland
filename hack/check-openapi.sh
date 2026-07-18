#!/usr/bin/env bash
# Lint storage-v1 and highland-v1 OpenAPI documents with Redocly CLI.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

REDOCLY_VERSION="${REDOCLY_VERSION:-2.18.0}"
REDOCLY=(npx --yes "@redocly/cli@${REDOCLY_VERSION}")
CONFIG="${REDOCLY_CONFIG:-redocly.yaml}"

if ! command -v npx >/dev/null 2>&1; then
  echo "ERROR: npx is required to run @redocly/cli@${REDOCLY_VERSION}" >&2
  exit 1
fi

docs=(
  docs/openapi/storage-v1.yaml
  docs/openapi/highland-v1.yaml
)

echo "== OpenAPI lint (redocly@${REDOCLY_VERSION}) =="
for doc in "${docs[@]}"; do
  if [[ ! -f "$doc" ]]; then
    echo "ERROR: missing $doc" >&2
    exit 1
  fi
  echo "-- lint $doc --"
  "${REDOCLY[@]}" lint "$doc" --config "$CONFIG"
done

echo "OK: OpenAPI documents lint clean"
