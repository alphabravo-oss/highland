#!/usr/bin/env bash
# Workstream C qualification scaffolding gate.
# 1) Validate profiles.yaml presence and required providers/profiles
# 2) Run Go unit tests + aggregator against fixtures (skipped/not-run cannot pass)
# 3) Ensure docs/compatibility.yaml qualificationProfiles IDs map into profiles.yaml
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
QUAL="$ROOT/qualification"
SCRIPTS="$QUAL/scripts"
FIXTURES="$QUAL/fixtures"
COMPAT="$ROOT/docs/compatibility.yaml"
PROFILES="$QUAL/profiles.yaml"

fail() { echo "ERROR: $*" >&2; exit 1; }

echo "== qualification scaffolding =="

[[ -f "$PROFILES" ]] || fail "missing $PROFILES"
[[ -f "$QUAL/schema.json" ]] || fail "missing schema.json"
[[ -f "$QUAL/results.schema.json" ]] || fail "missing results.schema.json"
[[ -f "$FIXTURES/sample-passed.json" ]] || fail "missing sample-passed.json"
[[ -f "$FIXTURES/sample-skipped.json" ]] || fail "missing sample-skipped.json"
[[ -f "$FIXTURES/sample-not-run.json" ]] || fail "missing sample-not-run.json"
[[ -f "$COMPAT" ]] || fail "missing $COMPAT"

echo "-- validate profiles.yaml --"
python3 "$SCRIPTS/validate.py"

# Required provider keys must appear as profile provider: values.
for provider in generic-csi longhorn rook-ceph openebs linstor highland; do
  if ! grep -qE "^[[:space:]]*provider:[[:space:]]*${provider}[[:space:]]*$" "$PROFILES"; then
    fail "profiles.yaml missing provider ${provider}"
  fi
done

echo "-- go test aggregate --"
(
  cd "$SCRIPTS"
  go test . -count=1
)

echo "-- build aggregate --"
TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT
AGG="$TMP_ROOT/aggregate"
(
  cd "$SCRIPTS"
  go build -o "$AGG" .
)

echo "-- fixtures: skipped must not pass production --"
TMP_SKIP="$TMP_ROOT/skipped"
mkdir -p "$TMP_SKIP"
cp "$FIXTURES/sample-skipped.json" "$TMP_SKIP/"
set +e
"$AGG" -dir "$TMP_SKIP" -gate production -require linstor-drbd >"$TMP_ROOT/agg-skipped.json" 2>"$TMP_ROOT/agg-skipped.err"
skip_rc=$?
set -e
if [[ "$skip_rc" -eq 0 ]]; then
  fail "aggregate exited 0 for skipped production profile (must fail)"
fi
if ! grep -qE 'notPassed|rejectedNonPassEvidence|did not pass|skipped' "$TMP_ROOT/agg-skipped.json"; then
  cat "$TMP_ROOT/agg-skipped.json" >&2 || true
  cat "$TMP_ROOT/agg-skipped.err" >&2 || true
  fail "aggregate output did not indicate skipped non-pass handling"
fi
echo "  OK: skipped rejected for production"

echo "-- fixtures: not-run must not pass production --"
TMP_NOTRUN="$TMP_ROOT/notrun"
mkdir -p "$TMP_NOTRUN"
cp "$FIXTURES/sample-not-run.json" "$TMP_NOTRUN/"
set +e
"$AGG" -dir "$TMP_NOTRUN" -gate production -require longhorn-current >"$TMP_ROOT/agg-notrun.json" 2>"$TMP_ROOT/agg-notrun.err"
notrun_rc=$?
set -e
if [[ "$notrun_rc" -eq 0 ]]; then
  fail "aggregate exited 0 for not-run production profile (must fail)"
fi
echo "  OK: not-run rejected for production"

echo "-- fixtures: missing required production profiles fail --"
TMP_PASS="$TMP_ROOT/passed-only"
mkdir -p "$TMP_PASS"
cp "$FIXTURES/sample-passed.json" "$TMP_PASS/"
set +e
"$AGG" -dir "$TMP_PASS" -gate production -require linstor-drbd,ha-multi-replica >"$TMP_ROOT/agg-missing.json" 2>"$TMP_ROOT/agg-missing.err"
missing_rc=$?
set -e
if [[ "$missing_rc" -eq 0 ]]; then
  fail "aggregate exited 0 when required production profiles missing"
fi
echo "  OK: missing required production profiles fail closed"

echo "-- fixtures: redaction of sensitive fields --"
if grep -q 'must-be-redacted-by-aggregator' "$TMP_ROOT/agg-skipped.json"; then
  fail "aggregator summary leaked redacted secret material"
fi
if ! grep -q '\[REDACTED\]' "$TMP_ROOT/agg-skipped.json"; then
  fail "expected [REDACTED] markers in aggregate summary redactedSample"
fi
echo "  OK: sensitive fields redacted"

echo "-- compatibility.yaml qualificationProfiles linkage --"
python3 - "$PROFILES" "$COMPAT" <<'PY'
import re
import sys
from pathlib import Path

profiles_path = Path(sys.argv[1])
compat_path = Path(sys.argv[2])

profile_ids = set()
for line in profiles_path.read_text(encoding="utf-8").splitlines():
    m = re.match(r"^\s+-\s+id:\s*([a-z0-9-]+)\s*$", line)
    if m:
        profile_ids.add(m.group(1))

if not profile_ids:
    print("ERROR: no profile ids parsed from profiles.yaml", file=sys.stderr)
    sys.exit(1)

compat = compat_path.read_text(encoding="utf-8")

# Collect all qualificationProfiles list entries.
linked: list[str] = []
in_qp = False
qp_indent = None
for line in compat.splitlines():
    if re.search(r"qualificationProfiles:\s*$", line):
        in_qp = True
        qp_indent = len(line) - len(line.lstrip(" "))
        continue
    if not in_qp:
        continue
    if not line.strip() or line.lstrip().startswith("#"):
        continue
    indent = len(line) - len(line.lstrip(" "))
    m = re.match(r"^\s+-\s+([a-z0-9-]+)\s*$", line)
    if m and indent > (qp_indent or 0):
        linked.append(m.group(1))
        continue
    # left the list
    in_qp = False
    qp_indent = None

if not linked:
    print("ERROR: no qualificationProfiles entries found in compatibility.yaml", file=sys.stderr)
    sys.exit(1)

missing = sorted({p for p in linked if p not in profile_ids})
if missing:
    print(
        "ERROR: compatibility.yaml qualificationProfiles not present in profiles.yaml:",
        file=sys.stderr,
    )
    for p in missing:
        print(f"  - {p}", file=sys.stderr)
    sys.exit(1)

# Each managed surface must declare qualificationProfiles.
required_markers = [
    ("providers.longhorn", r"(?m)^  longhorn:\n(?:.*\n)*?      qualificationProfiles:"),
    ("providers.rookCeph", r"(?m)^  rookCeph:\n(?:.*\n)*?      qualificationProfiles:"),
    ("providers.openebs", r"(?m)^  openebs:\n(?:.*\n)*?      qualificationProfiles:"),
    ("providers.linstor", r"(?m)^  linstor:\n(?:.*\n)*?      qualificationProfiles:"),
    ("genericCsi", r"(?m)^genericCsi:\n(?:.*\n)*?    qualificationProfiles:"),
    ("highlandPlatform", r"(?m)^highlandPlatform:\n(?:.*\n)*?    qualificationProfiles:"),
]
for name, pattern in required_markers:
    if not re.search(pattern, compat):
        print(f"ERROR: {name} missing qualificationProfiles block", file=sys.stderr)
        sys.exit(1)

print(f"compatibility links: {len(linked)} profile id(s) -> profiles.yaml OK")
print(f"  linked={sorted(set(linked))}")
PY

echo "OK: qualification scaffolding gate passed"
