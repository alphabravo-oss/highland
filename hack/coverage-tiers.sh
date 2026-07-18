#!/usr/bin/env bash
# Risk-tier coverage reporter (ENG-E6 / ADR-0007 DEC-13).
# Runs go test -coverprofile for critical packages, prints per-package %,
# excludes gen/ paths, and exits 0 while baselines are established.
#
# Usage: ./hack/coverage-tiers.sh
# Env:
#   COVERAGE_FAIL=1  — print threshold guidance (still non-fatal unless
#                      COVERAGE_STRICT=1 is also set)
#   COVERAGE_STRICT=1 — with COVERAGE_FAIL=1, exit non-zero if any critical
#                       package is below CRITICAL_MIN (default 80)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
API="${ROOT}/apps/api"
OUT_DIR="${COVERAGE_OUT:-${TMPDIR:-/tmp}/highland-coverage-tiers}"
mkdir -p "$OUT_DIR"

CRITICAL_MIN="${CRITICAL_MIN:-80}"
COVERAGE_FAIL="${COVERAGE_FAIL:-0}"
COVERAGE_STRICT="${COVERAGE_STRICT:-0}"

# Critical packages relative to apps/api (ADR-0007 / ENG-E6.2).
CRITICAL_PACKAGES=(
  "./internal/auth"
  "./internal/audit"
  "./internal/middleware"
  "./internal/policy"
  "./internal/storage/operations"
  "./internal/ratelimit"
)

# shellcheck disable=SC2016
echo "Highland coverage tiers (critical baseline)"
echo "  api module: ${API}"
echo "  profiles:   ${OUT_DIR}"
echo "  excludes:   paths matching gen/"
echo "  thresholds: not hard-failing by default (COVERAGE_FAIL=${COVERAGE_FAIL} COVERAGE_STRICT=${COVERAGE_STRICT})"
echo

failed_tests=0
below_threshold=0
declare -a RESULTS=()

go_cover_pct() {
  local profile="$1"
  # go tool cover -func prints "total: (statements) XX.X%"
  go tool cover -func="$profile" 2>/dev/null | awk '
    /total:/ {
      for (i = 1; i <= NF; i++) {
        if ($i ~ /%$/) {
          gsub(/%/, "", $i)
          print $i
          exit
        }
      }
    }
  '
}

filter_gen_paths() {
  # Drop coverage entries under gen/ so generated code does not inflate %.
  local src="$1"
  local dst="$2"
  # coverprofile header is "mode: set|count|atomic"
  head -n 1 "$src" >"$dst"
  # Keep only non-gen body lines
  tail -n +2 "$src" | grep -v '/gen/' | grep -v '[\\/]gen[\\/]' >>"$dst" || true
}

cd "$API"

for pkg in "${CRITICAL_PACKAGES[@]}"; do
  safe_name="${pkg#./}"
  safe_name="${safe_name//\//_}"
  raw_profile="${OUT_DIR}/${safe_name}.raw.out"
  profile="${OUT_DIR}/${safe_name}.out"

  echo "==> go test -coverprofile ${pkg}"
  if ! go test "${pkg}" -count=1 -coverprofile="${raw_profile}" -covermode=atomic >"${OUT_DIR}/${safe_name}.log" 2>&1; then
    echo "    FAIL: tests failed for ${pkg} (see ${OUT_DIR}/${safe_name}.log)"
    failed_tests=$((failed_tests + 1))
    RESULTS+=("${pkg}	FAIL	n/a")
    continue
  fi

  if [[ ! -s "${raw_profile}" ]]; then
    echo "    WARN: empty coverprofile for ${pkg}"
    RESULTS+=("${pkg}	ok	n/a")
    continue
  fi

  filter_gen_paths "${raw_profile}" "${profile}"
  pct="$(go_cover_pct "${profile}" || true)"
  if [[ -z "${pct}" ]]; then
    pct="0.0"
  fi

  status="ok"
  if awk -v p="${pct}" -v m="${CRITICAL_MIN}" 'BEGIN { exit !(p+0 < m+0) }'; then
    status="below-${CRITICAL_MIN}%"
    below_threshold=$((below_threshold + 1))
  fi

  printf "    coverage: %s%% (%s)\n" "${pct}" "${status}"
  RESULTS+=("${pkg}	${status}	${pct}%")
done

echo
echo "Critical tier summary"
echo "---------------------"
printf "%-40s %-16s %s\n" "PACKAGE" "STATUS" "COVERAGE"
for row in "${RESULTS[@]}"; do
  IFS=$'\t' read -r pkg status cov <<<"${row}"
  printf "%-40s %-16s %s\n" "${pkg}" "${status}" "${cov}"
done
echo
echo "Baseline note: thresholds are observational until ENG-E6.5 baseline decision."
echo "Docs: docs/engineering/coverage-tiers.md"

# Test failures always fail the script (broken suite, not coverage gate).
if [[ "${failed_tests}" -gt 0 ]]; then
  echo "ERROR: ${failed_tests} package(s) failed tests" >&2
  exit 1
fi

if [[ "${COVERAGE_FAIL}" == "1" && "${below_threshold}" -gt 0 ]]; then
  echo "WARNING: ${below_threshold} critical package(s) below ${CRITICAL_MIN}%" >&2
  if [[ "${COVERAGE_STRICT}" == "1" ]]; then
    exit 1
  fi
fi

exit 0
