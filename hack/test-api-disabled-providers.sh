#!/usr/bin/env bash
# Process-level regression: Highland must start and serve its core endpoints
# with Longhorn, Rook/Ceph, Kubernetes inventory, and mutating benchmarks off.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PORT="${HIGHLAND_DISABLED_SMOKE_PORT:-18089}"
tmp="$(mktemp -d)"
pid=""
cleanup() {
  if [[ -n "$pid" ]]; then
    kill -TERM "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT INT TERM

(cd "$ROOT/apps/api" && go build -o "$tmp/highland-api" ./cmd/highland-api)
env \
  HIGHLAND_LISTEN_ADDR="127.0.0.1:$PORT" \
  HIGHLAND_ADMIN_USER=admin \
  HIGHLAND_ADMIN_PASSWORD=disabled-provider-smoke \
  HIGHLAND_SESSION_SECRET=0123456789abcdef0123456789abcdef \
  HIGHLAND_LONGHORN_ENABLED=false \
  HIGHLAND_LONGHORN_REQUIRED=false \
  HIGHLAND_STORAGE_ENABLED=false \
  HIGHLAND_ROOK_CEPH_ENABLED=false \
  HIGHLAND_KUBERNETES_BENCHMARK_ENABLED=false \
  "$tmp/highland-api" >"$tmp/api.log" 2>&1 &
pid=$!

for _ in $(seq 1 100); do
  if curl --fail --silent "http://127.0.0.1:$PORT/healthz" >"$tmp/health.json"; then
    break
  fi
  if ! kill -0 "$pid" 2>/dev/null; then
    cat "$tmp/api.log" >&2
    exit 1
  fi
  sleep 0.1
done
grep -q 'highland-api' "$tmp/health.json"
curl --fail --silent "http://127.0.0.1:$PORT/readyz" >"$tmp/ready.json"
grep -q 'ready' "$tmp/ready.json"
if grep -Eqi 'panic|nil pointer' "$tmp/api.log"; then
  cat "$tmp/api.log" >&2
  exit 1
fi
kill -TERM "$pid"
wait "$pid"
pid=""
echo "OK: API starts with every managed provider disabled"
