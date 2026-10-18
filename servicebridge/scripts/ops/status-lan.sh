#!/usr/bin/env bash
set -euo pipefail

BACKEND_PORT="${BACKEND_PORT:-8080}"
WEB_PORT="${WEB_PORT:-5173}"
BACKEND_SCREEN="customer-service-backend"
WEB_SCREEN="customer-service-user-web"

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

screen_pid() {
  local name="$1"
  (screen -ls 2>/dev/null || true) | awk -v name="$name" '$1 ~ "\\."name"$" { split($1, a, "."); print a[1]; exit }'
}

show_service() {
  local name="$1"
  local session_name="$2"
  local port="$3"
  local pid
  pid="$(screen_pid "$session_name")"
  if [[ -n "$pid" ]]; then
    echo "$name: running (screen pid $pid)"
    return
  fi
  pid="$(lsof -t -nP -iTCP:"$port" -sTCP:LISTEN 2>/dev/null | head -n 1 || true)"
  if [[ -n "$pid" ]]; then
    echo "$name: running (pid $pid, detected by port $port)"
    return
  fi
  echo "$name: stopped"
}

lan_ip="$(find_lan_ip)"
show_service "backend" "$BACKEND_SCREEN" "$BACKEND_PORT"
show_service "user-web" "$WEB_SCREEN" "$WEB_PORT"
echo "backend local: http://127.0.0.1:$BACKEND_PORT"
echo "user-web local: http://127.0.0.1:$WEB_PORT"
if [[ -n "$lan_ip" ]]; then
  echo "backend lan: http://$lan_ip:$BACKEND_PORT"
  echo "user-web lan: http://$lan_ip:$WEB_PORT"
fi
