#!/usr/bin/env bash
# Validate highland/docs/parity-matrix.yaml and gate on P0 status.
# - Default: fail if any P0 item is status=not_started
# - STRICT_P0=1: fail if any P0 is not done|na
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
FILE="${PARITY_MATRIX:-$ROOT/docs/parity-matrix.yaml}"
STRICT_P0="${STRICT_P0:-0}"

if [[ ! -f "$FILE" ]]; then
  echo "ERROR: parity matrix not found: $FILE" >&2
  exit 1
fi

python3 - "$FILE" "$STRICT_P0" <<'PY'
import sys
from pathlib import Path

path = Path(sys.argv[1])
strict = sys.argv[2] == "1"

try:
    import yaml  # type: ignore
except ImportError:
    yaml = None

text = path.read_text()
if yaml is not None:
    data = yaml.safe_load(text)
else:
    # Minimal subset parser for our matrix format (no PyYAML required)
    items = []
    cur = None
    for line in text.splitlines():
        if line.startswith("  - id:"):
            if cur:
                items.append(cur)
            cur = {"id": line.split(":", 1)[1].strip()}
        elif cur is not None and line.startswith("    "):
            if ":" in line:
                k, v = line.strip().split(":", 1)
                cur[k.strip()] = v.strip().strip('"')
    if cur:
        items.append(cur)
    data = {"items": items}

items = data.get("items") or []
if not items:
    print("ERROR: no items in parity matrix", file=sys.stderr)
    sys.exit(1)

allowed = {"not_started", "partial", "done", "na", "blocked"}
errors = []
counts = {s: 0 for s in allowed}
p0_bad = []

for it in items:
    iid = it.get("id", "?")
    st = str(it.get("status", "")).strip()
    pr = str(it.get("priority", "")).strip()
    if st not in allowed:
        errors.append(f"{iid}: invalid status {st!r}")
        continue
    counts[st] = counts.get(st, 0) + 1
    if pr == "P0":
        if st == "not_started":
            p0_bad.append(f"{iid}: P0 not_started")
        if strict and st not in ("done", "na"):
            p0_bad.append(f"{iid}: P0 status={st} (STRICT_P0 requires done|na)")

print(f"parity-matrix: {path}")
print(f"  items={len(items)} counts={counts}")
p0 = [i for i in items if str(i.get('priority')) == 'P0']
p0_done = sum(1 for i in p0 if i.get('status') in ('done', 'na'))
print(f"  P0: {p0_done}/{len(p0)} done|na ({100*p0_done/max(len(p0),1):.0f}%)")

if errors:
    print("ERRORS:", file=sys.stderr)
    for e in errors:
        print(f"  - {e}", file=sys.stderr)
    sys.exit(1)

if p0_bad:
    print("P0 gate failures:", file=sys.stderr)
    for e in p0_bad:
        print(f"  - {e}", file=sys.stderr)
    sys.exit(1)

print("OK: parity matrix gate passed" + (" (STRICT_P0)" if strict else ""))
PY
