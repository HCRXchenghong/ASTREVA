#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
RUN_DIR="$ROOT_DIR/.run"
BACKEND_LOG="$RUN_DIR/backend.log"
WEB_LOG="$RUN_DIR/user-web.log"
BACKEND_BIN="$RUN_DIR/customer-service-server"
BACKEND_PORT="${BACKEND_PORT:-8080}"
WEB_PORT="${WEB_PORT:-5173}"
BACKEND_SCREEN="customer-service-backend"
WEB_SCREEN="customer-service-user-web"

mkdir -p "$RUN_DIR" "$RUN_DIR/uploads"

find_lan_ip() {
  if command -v ipconfig >/dev/null 2>&1; then
    for iface in en0 en1; do
      ip="$(ipconfig getifaddr "$iface" 2>/dev/null || true)"
      if [[ -n "$ip" ]]; then
        printf '%s' "$ip"
        return
      fi
    done
  fi
  ifconfig | awk '
    /^[a-z0-9].*:/ { iface=$1; sub(":", "", iface) }
    /inet / && $2 !~ /^127\./ && iface !~ /^utun/ { print $2; exit }
  '
}

is_running() {
  local pid="$1"
  [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null
}

screen_running() {
  local name="$1"
  (screen -ls 2>/dev/null || true) | grep -q "[0-9][0-9]*\\.${name}[[:space:]]"
}

screen_pid() {
  local name="$1"
  (screen -ls 2>/dev/null || true) | awk -v name="$name" '$1 ~ "\\."name"$" { split($1, a, "."); print a[1]; exit }'
}

ensure_port_free() {
  local port="$1"
  local name="$2"
  if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
    echo "$name port $port is already in use" >&2
    lsof -nP -iTCP:"$port" -sTCP:LISTEN >&2 || true
    exit 1
  fi
}

wait_http() {
  local url="$1"
  for _ in $(seq 1 80); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.25
  done
  return 1
}

rm -f "$RUN_DIR/backend.pid" "$RUN_DIR/user-web.pid"

if ! screen_running "$BACKEND_SCREEN"; then
  ensure_port_free "$BACKEND_PORT" "backend"
  (
    cd "$ROOT_DIR/backend"
    go build -o "$BACKEND_BIN" ./cmd/server
  )
  screen -dmS "$BACKEND_SCREEN" bash -lc "cd '$ROOT_DIR/backend' && exec env HTTP_ADDR='0.0.0.0:$BACKEND_PORT' STORE_DRIVER=memory RATE_LIMIT_ENABLED=true RATE_LIMIT_RPS=50 RATE_LIMIT_BURST=100 LOG_LEVEL=info UPLOAD_DIR='$RUN_DIR/uploads' '$BACKEND_BIN' >>'$BACKEND_LOG' 2>&1"
fi

if ! screen_running "$WEB_SCREEN"; then
  ensure_port_free "$WEB_PORT" "user web"
  screen -dmS "$WEB_SCREEN" bash -lc "cd '$ROOT_DIR/apps/user-web' && exec python3 -m http.server '$WEB_PORT' --bind 0.0.0.0 >>'$WEB_LOG' 2>&1"
fi

wait_http "http://127.0.0.1:$BACKEND_PORT/readyz"
wait_http "http://127.0.0.1:$WEB_PORT/index.html"

lan_ip="$(find_lan_ip)"

backend_pid="$(screen_pid "$BACKEND_SCREEN")"
web_pid="$(screen_pid "$WEB_SCREEN")"

echo "backend pid: $backend_pid"
echo "user-web pid: $web_pid"
echo "backend ready: http://127.0.0.1:$BACKEND_PORT/readyz"
echo "user-web local: http://127.0.0.1:$WEB_PORT"
if [[ -n "$lan_ip" ]]; then
  echo "user-web lan: http://$lan_ip:$WEB_PORT"
  echo "backend lan: http://$lan_ip:$BACKEND_PORT"
fi
