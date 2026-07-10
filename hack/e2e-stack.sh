#!/usr/bin/env bash
# Start mock Longhorn manager + Highland API + Vite for Playwright e2e.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
API_DIR="$ROOT/apps/api"
WEB_DIR="$ROOT/apps/web"
BIN="$ROOT/hack/.bin"

MOCK_PORT="${MOCK_LH_PORT:-19500}"
API_PORT="${HIGHLAND_API_PORT:-18080}"
WEB_PORT="${HIGHLAND_WEB_PORT:-5173}"

PIDS=()
cleanup() {
  for pid in "${PIDS[@]:-}"; do
    kill "$pid" 2>/dev/null || true
  done
}
trap cleanup EXIT INT TERM

free_port() {
  local port=$1
  local pids
  pids=$(lsof -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)
  if [[ -n "${pids:-}" ]]; then
    # shellcheck disable=SC2086
    kill $pids 2>/dev/null || true
    sleep 0.2
  fi
}

free_port "$MOCK_PORT"
free_port "$API_PORT"
free_port "$WEB_PORT"

mkdir -p "$BIN"
echo "[e2e-stack] building mock-longhorn-manager + highland-api..."
(
  cd "$API_DIR"
  go build -o "$BIN/mock-longhorn-manager" ./cmd/mock-longhorn-manager
  go build -o "$BIN/highland-api" ./cmd/highland-api
)

export MOCK_LH_ADDR=":$MOCK_PORT"
"$BIN/mock-longhorn-manager" &
PIDS+=($!)

export HIGHLAND_LISTEN_ADDR=":$API_PORT"
export HIGHLAND_MANAGER_URL="http://127.0.0.1:$MOCK_PORT"
export HIGHLAND_ADMIN_USER="${HIGHLAND_ADMIN_USER:-admin}"
export HIGHLAND_ADMIN_PASSWORD="${HIGHLAND_ADMIN_PASSWORD:-highland}"
export HIGHLAND_COOKIE_SECURE=false
export HIGHLAND_DEV_ROLES="${HIGHLAND_DEV_ROLES:-1}"
export HIGHLAND_OIDC_MOCK="${HIGHLAND_OIDC_MOCK:-1}"
export HIGHLAND_METRICS_INTERVAL="${HIGHLAND_METRICS_INTERVAL:-2s}"
"$BIN/highland-api" &
PIDS+=($!)

for _ in $(seq 1 50); do
  if curl -sf "http://127.0.0.1:$API_PORT/healthz" | grep -q highland-api; then
    break
  fi
  sleep 0.1
done
curl -sf "http://127.0.0.1:$API_PORT/healthz" | grep -q highland-api

echo "[e2e-stack] starting vite on :$WEB_PORT (proxy → $API_PORT)..."
cd "$WEB_DIR"
export VITE_API_PROXY="http://127.0.0.1:$API_PORT"
export HIGHLAND_WEB_PORT="$WEB_PORT"
npx vite --host 127.0.0.1 --port "$WEB_PORT" --strictPort &
PIDS+=($!)

for _ in $(seq 1 100); do
  if curl -sf "http://127.0.0.1:$WEB_PORT/" >/dev/null; then
    break
  fi
  sleep 0.1
done
curl -sf "http://127.0.0.1:$WEB_PORT/" >/dev/null

echo "[e2e-stack] ready web=http://127.0.0.1:$WEB_PORT api=http://127.0.0.1:$API_PORT mock=:$MOCK_PORT"
wait
