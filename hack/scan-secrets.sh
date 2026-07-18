#!/usr/bin/env bash
# Local / CI secret scan for high-confidence credential patterns in source.
# Excludes vendored and generated trees. No network required.
#
# Exit 0 if clean; exit 1 if potential secrets are found.
# Policy: docs/security/supply-chain-policy.md (ADR-0006).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# High-confidence patterns only (avoid noisy generic password matches).
# Each line: <label>|<basic-ERE compatible with git grep -E / grep -E>
PATTERNS=(
  'AWS Access Key ID|AKIA[0-9A-Z]{16}'
  'Private key block|-----BEGIN (RSA |EC |OPENSSH |DSA |PGP )?PRIVATE KEY-----'
  'GitHub PAT|gh[pousr]_[A-Za-z0-9_]{36,}'
  'GitHub fine-grained PAT|github_pat_[A-Za-z0-9_]{20,}'
  'Slack token|xox[baprs]-[0-9A-Za-z-]{10,}'
  'Google API key|AIza[0-9A-Za-z_-]{35}'
  'Stripe secret key|sk_live_[0-9a-zA-Z]{20,}'
)

# Pathspecs excluded from the scan (deps / build output / packages).
GIT_EXCLUDES=(
  ':(exclude)apps/web/node_modules'
  ':(exclude)apps/web/dist'
  ':(exclude)apps/web/storybook-static'
  ':(exclude)apps/web/test-results'
  ':(exclude)apps/web/playwright-report'
  ':(exclude)chart/charts'
  ':(exclude)*.png'
  ':(exclude)*.jpg'
  ':(exclude)*.jpeg'
  ':(exclude)*.gif'
  ':(exclude)*.webp'
  ':(exclude)*.ico'
  ':(exclude)*.woff'
  ':(exclude)*.woff2'
  ':(exclude)*.map'
  ':(exclude)*.tgz'
  ':(exclude)*.gz'
  ':(exclude)*.zip'
)

found=0

echo "== secret scan (pattern-based) =="

if ! command -v git >/dev/null 2>&1 || ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "ERROR: scan-secrets.sh requires a git working tree" >&2
  exit 2
fi

for entry in "${PATTERNS[@]}"; do
  label="${entry%%|*}"
  regex="${entry#*|}"
  # git grep -E: tracked files only; -I skip binary; -n line numbers.
  # Exit codes: 0=match, 1=no match, >=2 error.
  set +e
  hits="$(git grep -nIE -e "$regex" -- . "${GIT_EXCLUDES[@]}" 2>/dev/null)"
  rc=$?
  set -e
  if [[ "$rc" -ge 2 ]]; then
    echo "ERROR: git grep failed for pattern: $label" >&2
    exit 2
  fi
  if [[ "$rc" -eq 0 && -n "$hits" ]]; then
    while IFS= read -r hit; do
      [[ -z "$hit" ]] && continue
      file="${hit%%:*}"
      rest="${hit#*:}"
      lineno="${rest%%:*}"
      printf 'SECRET_SCAN: [%s] %s:%s\n' "$label" "$file" "$lineno"
      found=1
    done <<<"$hits"
  fi
done

if [[ "$found" -ne 0 ]]; then
  echo
  echo "Secret scan FAILED: potential credentials detected."
  echo "Remove secrets from the tree, rotate any exposed credentials, and re-run."
  echo "See docs/security/supply-chain-policy.md for exception process (ADR-0006)."
  exit 1
fi

echo "OK: no high-confidence secret patterns found"
exit 0
