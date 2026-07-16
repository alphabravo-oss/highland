#!/usr/bin/env bash
# Capture and sanitize Rook CRD fixtures. Dashboard JSON files supplied as
# additional arguments are sanitized with the same policy; this script never
# authenticates to Ceph and therefore cannot record a JWT or password itself.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${1:-$ROOT/apps/api/internal/providers/rookceph/testdata/captured}"
NAMESPACE="${ROOK_NAMESPACE:-rook-ceph}"
shift || true

command -v kubectl >/dev/null || { echo "kubectl is required" >&2; exit 1; }
command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }
mkdir -p "$OUT"

sanitize() {
  jq '
    def redact_string:
      gsub("[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}"; "[redacted-id]")
      | gsub("([0-9]{1,3}\\.){3}[0-9]{1,3}"; "[redacted-ip]")
      | if test("^eyJ[A-Za-z0-9_-]+\\.") then "[redacted-token]" else . end;
    walk(
      if type == "object" then
        with_entries(select((.key | ascii_downcase | test("password|token|authorization|credential|secret|private.?key")) | not))
      elif type == "string" then redact_string
      else . end
    )' "$1"
}

for resource in cephclusters cephblockpools cephfilesystems cephrbdmirrors; do
  raw="$(mktemp)"
  kubectl -n "$NAMESPACE" get "$resource.ceph.rook.io" -o json >"$raw"
  sanitize "$raw" >"$OUT/$resource.json"
  rm -f "$raw"
done

for input in "$@"; do
  [[ -f "$input" ]] || { echo "fixture input not found: $input" >&2; exit 1; }
  sanitize "$input" >"$OUT/$(basename "$input")"
done

if rg -n --ignore-case 'bearer |password|authorization|credential|secret|eyJ[A-Za-z0-9_-]+\.|([0-9]{1,3}\.){3}[0-9]{1,3}' "$OUT"; then
  echo "fixture redaction validation failed" >&2
  exit 1
fi

echo "Sanitized fixtures written to $OUT; manually review hostnames and usernames before commit."
