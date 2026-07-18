#!/usr/bin/env bash
# Chi route registrations vs OpenAPI path keys with optional STRICT gate.
#
# Exit codes:
#   0 — coverage ok (or report-only when STRICT=0)
#   1 — STRICT=1 and missing public paths (after allowlist)
#   2 — tooling / missing files
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ROUTER="${ROUTER:-$ROOT/apps/api/internal/handlers/router.go}"
STORAGE_HTTP="${STORAGE_HTTP:-$ROOT/apps/api/internal/storage}"
OPENAPI_ROOT="${OPENAPI_ROOT:-$ROOT/docs/openapi}"
HIGHLAND_OAPI="${HIGHLAND_OAPI:-$OPENAPI_ROOT/highland-v1.yaml}"
STORAGE_OAPI="${STORAGE_OAPI:-$OPENAPI_ROOT/storage-v1.yaml}"
ALLOWLIST="${ALLOWLIST:-$OPENAPI_ROOT/internal-allowlist.txt}"
STRICT="${STRICT:-0}"

if [[ ! -f "$ROUTER" || ! -f "$HIGHLAND_OAPI" ]]; then
  echo "ERROR: router or highland OpenAPI missing" >&2
  exit 2
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# Collect OpenAPI path keys from root + modular path fragments + storage-v1.
{
  grep -Eho '^[[:space:]]+/[A-Za-z0-9_{}/.*-]*:' \
    "$HIGHLAND_OAPI" \
    "$STORAGE_OAPI" \
    "$OPENAPI_ROOT"/paths/*.yaml 2>/dev/null \
    | sed -E 's/^[[:space:]]+//; s/:[[:space:]]*$//' \
    || true
} | sort -u >"$tmp/oapi.txt"

# Extract public Chi paths. Prefer Route()-aware extraction; ignore relative
# registrations that are only valid under a prefix (avoids /login false positives).
python3 - "$ROUTER" "$STORAGE_HTTP" >"$tmp/chi.txt" <<'PY'
import re, sys
from pathlib import Path

router = Path(sys.argv[1]).read_text()
storage_dir = Path(sys.argv[2])

def strip_comments(text: str) -> str:
    out = []
    for line in text.splitlines():
        if "//" in line:
            line = line[: line.index("//")]
        out.append(line)
    return "\n".join(out)

def extract_from_router(text: str) -> set[str]:
    text = strip_comments(text)
    paths: set[str] = set()
    # Top-level absolute registrations only (path starts with / and is not inside
    # a Route body for relative joins — we handle Route separately).
    # Mark Route body spans to skip for bare relative paths.
    route_spans = []
    for m in re.finditer(r'\br\.Route\(\s*"([^"]+)"\s*,', text):
        prefix = m.group(1).rstrip("/")
        start = text.find("{", m.end())
        if start < 0:
            continue
        depth = 0
        i = start
        end = None
        while i < len(text):
            if text[i] == "{":
                depth += 1
            elif text[i] == "}":
                depth -= 1
                if depth == 0:
                    end = i + 1
                    break
            i += 1
        if end is None:
            continue
        route_spans.append((start, end, prefix))
        body = text[start:end]
        for rm in re.finditer(
            r'\br\.(?:Get|Post|Put|Patch|Delete|Head|Options|Handle|HandleFunc)\(\s*"([^"]+)"',
            body,
        ):
            rel = rm.group(1)
            if not rel.startswith("/"):
                rel = "/" + rel
            full = prefix + rel
            # Normalize trailing wildcards
            if full.endswith("/*"):
                paths.add(full[:-2])
            else:
                paths.add(full.rstrip("/") if full != "/" else full)

    def in_route_body(pos: int) -> bool:
        return any(s <= pos < e for s, e, _ in route_spans)

    for m in re.finditer(
        r'\br\.(?:Get|Post|Put|Patch|Delete|Head|Options|Handle|HandleFunc)\(\s*"([^"]+)"',
        text,
    ):
        if in_route_body(m.start()):
            continue
        p = m.group(1)
        if not p.startswith("/"):
            continue
        if p.endswith("/*"):
            paths.add(p[:-2])
        else:
            paths.add(p)
    return paths

paths = extract_from_router(router)

# Storage / operations / policy Mount sites — absolute paths only.
for f in storage_dir.rglob("*.go"):
    text = strip_comments(f.read_text())
    for m in re.finditer(
        r'\br\.(?:Get|Post|Put|Patch|Delete|Handle|HandleFunc)\(\s*"([^"]+)"',
        text,
    ):
        p = m.group(1)
        if p.startswith("/api/"):
            if p.endswith("/*"):
                paths.add(p[:-2])
            else:
                paths.add(p.rstrip("/") if len(p) > 1 else p)

for p in sorted(paths):
    print(p)
PY

# Load allowlist
: >"$tmp/allow.txt"
if [[ -f "$ALLOWLIST" ]]; then
  grep -vE '^[[:space:]]*(#|$)' "$ALLOWLIST" | sed 's/[[:space:]]*$//' | sort -u >"$tmp/allow.txt" || true
fi

normalize() {
  # collapse {param} names for comparison
  sed -E 's/\{[A-Za-z0-9_]+\}/{}/g; s#/+#/#g; s#/$##'
}

normalize <"$tmp/chi.txt" | sort -u >"$tmp/chi.n"
normalize <"$tmp/oapi.txt" | sort -u >"$tmp/oapi.n"
normalize <"$tmp/allow.txt" | sort -u >"$tmp/allow.n"

# Missing = chi - oapi - allow
comm -23 "$tmp/chi.n" "$tmp/oapi.n" >"$tmp/missing0"
comm -23 "$tmp/missing0" "$tmp/allow.n" >"$tmp/missing"

chi_n=$(wc -l <"$tmp/chi.n" | tr -d ' ')
oapi_n=$(wc -l <"$tmp/oapi.n" | tr -d ' ')
covered=$(comm -12 "$tmp/chi.n" "$tmp/oapi.n" | wc -l | tr -d ' ')
missing=$(wc -l <"$tmp/missing" | tr -d ' ')
allow_n=$(wc -l <"$tmp/allow.n" | tr -d ' ')

echo "== OpenAPI route inventory =="
echo "Router:        $ROUTER"
echo "OpenAPI root:  $HIGHLAND_OAPI (+ storage-v1 + paths/*)"
echo "Chi public paths:           $chi_n"
echo "OpenAPI public path keys:   $oapi_n"
echo "Chi paths covered by OAPI:  $covered"
echo "Allowlisted (internal):     $allow_n"
echo "Missing (must document):    $missing"

if [[ "$missing" -gt 0 ]]; then
  echo
  echo "-- Chi public paths missing from OpenAPI (not allowlisted) --"
  cat "$tmp/missing" | sed 's/^/  /'
fi

if [[ "$STRICT" == "1" && "$missing" -gt 0 ]]; then
  echo "ERROR: STRICT=1 and $missing path(s) lack OpenAPI coverage" >&2
  exit 1
fi

echo "OK: inventory complete"
