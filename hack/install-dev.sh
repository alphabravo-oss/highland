#!/usr/bin/env bash
# Local dev: mock manager (or real via HIGHLAND_MANAGER_URL) + API + web.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
API_DIR="$ROOT/apps/api"
WEB_DIR="$ROOT/apps/web"
BIN="$ROOT/hack/.bin"
USE_MOCK="${USE_MOCK:-1}"
MOCK_PORT="${MOCK_LH_PORT:-9500}"
API_PORT="${HIGHLAND_API_PORT:-8080}"
WEB_PORT="${HIGHLAND_WEB_PORT:-5173}"

PIDS=()
cleanup() {
  for pid in "${PIDS[@]:-}"; do
    kill "$pid" 2>/dev/null || true
  done
}
trap cleanup EXIT INT TERM

mkdir -p "$BIN"
(
  cd "$API_DIR"
  go build -o "$BIN/mock-longhorn-manager" ./cmd/mock-longhorn-manager
  go build -o "$BIN/highland-api" ./cmd/highland-api
)

if [[ "$USE_MOCK" == "1" ]]; then
  export MOCK_LH_ADDR=":$MOCK_PORT"
  "$BIN/mock-longhorn-manager" &
  PIDS+=($!)
  export HIGHLAND_MANAGER_URL="http://127.0.0.1:$MOCK_PORT"
  echo "[install-dev] mock manager on :$MOCK_PORT"
else
  export HIGHLAND_MANAGER_URL="${HIGHLAND_MANAGER_URL:-http://127.0.0.1:9500}"
  echo "[install-dev] using manager $HIGHLAND_MANAGER_URL"
fi

export HIGHLAND_LISTEN_ADDR=":$API_PORT"
export HIGHLAND_ADMIN_USER="${HIGHLAND_ADMIN_USER:-admin}"
export HIGHLAND_ADMIN_PASSWORD="${HIGHLAND_ADMIN_PASSWORD:-highland}"
export HIGHLAND_COOKIE_SECURE=false
"$BIN/highland-api" &
PIDS+=($!)
echo "[install-dev] API on :$API_PORT"

cd "$WEB_DIR"
if [[ ! -d node_modules ]]; then
  npm install
fi
export VITE_API_PROXY="http://127.0.0.1:$API_PORT"
export HIGHLAND_WEB_PORT="$WEB_PORT"
echo "[install-dev] web on http://127.0.0.1:$WEB_PORT (admin / highland)"
npx vite --host 127.0.0.1 --port "$WEB_PORT" --strictPort &
PIDS+=($!)
wait
