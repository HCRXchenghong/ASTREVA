#!/usr/bin/env zsh
set -u

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
LOG_DIR="$ROOT_DIR/.local-logs"
mkdir -p "$LOG_DIR"

cd "$ROOT_DIR"

if [[ -f "$ROOT_DIR/.env" ]]; then
  set -a
  source "$ROOT_DIR/.env"
  set +a
fi

export PUBLIC_SITE_URL="${PUBLIC_SITE_URL:-http://127.0.0.1:4321}"
export PUBLIC_ADMIN_URL="${PUBLIC_ADMIN_URL:-http://127.0.0.1:1337}"
export PUBLIC_SERVICEBRIDGE_URL="${PUBLIC_SERVICEBRIDGE_URL:-http://127.0.0.1:5173}"
export SERVICEBRIDGE_CORS_ALLOWED_ORIGINS="${SERVICEBRIDGE_CORS_ALLOWED_ORIGINS:-http://127.0.0.1:4321,http://127.0.0.1:5173}"
export CORS_ALLOWED_ORIGINS="${CORS_ALLOWED_ORIGINS:-$SERVICEBRIDGE_CORS_ALLOWED_ORIGINS}"
export FRAME_ANCESTORS="${FRAME_ANCESTORS:-http://127.0.0.1:4321}"
export STORE_DRIVER="${STORE_DRIVER:-memory}"
export HTTP_ADDR="${HTTP_ADDR:-127.0.0.1:8080}"

pids=()

start_service() {
  local name="$1"
  shift
  local log_file="$LOG_DIR/$name.log"
  printf '[astreva] starting %s -> %s\n' "$name" "$log_file"
  (
    cd "$ROOT_DIR"
    "$@"
  ) >"$log_file" 2>&1 &
  pids+=("$!")
}

cleanup() {
  printf '\n[astreva] stopping local stack...\n'
  for pid in "${pids[@]}"; do
    kill "$pid" >/dev/null 2>&1 || true
  done
}

trap cleanup INT TERM EXIT

start_service frontend-static node scripts/static-server.mjs
start_service admin npm run dev:admin
start_service rebuild-webhook npm run rebuild:webhook
start_service servicebridge-backend env -u DATABASE_URL STORE_DRIVER="$STORE_DRIVER" HTTP_ADDR="$HTTP_ADDR" CORS_ALLOWED_ORIGINS="$CORS_ALLOWED_ORIGINS" FRAME_ANCESTORS="$FRAME_ANCESTORS" npm run servicebridge:backend
start_service servicebridge-user-web python3 -m http.server 5173 --bind 127.0.0.1 --directory "$ROOT_DIR/servicebridge/apps/user-web"

printf '[astreva] local stack launched. Logs are in %s\n' "$LOG_DIR"
printf '[astreva] frontend: http://127.0.0.1:4321/\n'
printf '[astreva] admin login: http://127.0.0.1:1337/admin-2fa\n'
printf '[astreva] site admin: http://127.0.0.1:1337/site-admin/\n'
printf '[astreva] servicebridge user web: http://127.0.0.1:5173/?embed=1\n'

while true; do
  sleep 3600
done
