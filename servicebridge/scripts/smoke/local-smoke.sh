#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
HTTP_BASE="${HTTP_BASE:-http://localhost:8080}"
WS_BASE="${WS_BASE:-ws://localhost:8080}"

tmp_png="$(mktemp -t customer-service-smoke.XXXXXX.png)"
trap 'rm -f "$tmp_png"' EXIT

printf 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=' | base64 -d > "$tmp_png"

curl -fsS "$HTTP_BASE/readyz" >/dev/null
curl -fsS "$HTTP_BASE/metrics" | grep -q 'customer_service_ws_connections'

visitor_json="$(
  curl -fsS -X POST "$HTTP_BASE/api/visitor/conversations" \
    -H 'Content-Type: application/json' \
    -d '{"source":"smoke"}'
)"
visitor_token="$(node -e 'const fs=require("fs"); const d=JSON.parse(fs.readFileSync(0,"utf8")); process.stdout.write(d.token)' <<<"$visitor_json")"

upload_json="$(
  curl -fsS -X POST "$HTTP_BASE/api/uploads" \
    -H "Authorization: Bearer $visitor_token" \
    -F "file=@$tmp_png;type=image/png"
)"
upload_url="$(node -e 'const fs=require("fs"); const d=JSON.parse(fs.readFileSync(0,"utf8")); process.stdout.write(d.url)' <<<"$upload_json")"

if [[ "$upload_url" == /uploads/* ]]; then
  curl -fsSI "$HTTP_BASE$upload_url" | grep -qi 'content-type: image/png'
fi

(
  cd "$ROOT_DIR/backend"
  go run ./cmd/wsload \
    -http "$HTTP_BASE" \
    -ws "$WS_BASE" \
    -n "${SMOKE_WS_CONNECTIONS:-5}" \
    -duration "${SMOKE_DURATION:-3s}" \
    -interval 1s \
    -messages-per-conn 1
)

echo "local smoke passed"
