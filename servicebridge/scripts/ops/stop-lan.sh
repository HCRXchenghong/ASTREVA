#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
RUN_DIR="$ROOT_DIR/.run"
BACKEND_PORT="${BACKEND_PORT:-8080}"
WEB_PORT="${WEB_PORT:-5173}"
BACKEND_SCREEN="customer-service-backend"
WEB_SCREEN="customer-service-user-web"

stop_screen() {
  local name="$1"
  if (screen -ls 2>/dev/null || true) | grep -q "[0-9][0-9]*\\.${name}[[:space:]]"; then
    screen -S "$name" -X quit || true
  fi
}

stop_port_listener() {
  local port="$1"
  local pids
  pids="$(lsof -t -nP -iTCP:"$port" -sTCP:LISTEN 2>/dev/null | sort -u || true)"
  if [[ -z "$pids" ]]; then
    return
  fi
  for pid in $pids; do
    kill "$pid" 2>/dev/null || true
  done
}

stop_screen "$BACKEND_SCREEN"
stop_screen "$WEB_SCREEN"
rm -f "$RUN_DIR/backend.pid" "$RUN_DIR/user-web.pid"
stop_port_listener "$BACKEND_PORT"
stop_port_listener "$WEB_PORT"

echo "lan services stopped"
