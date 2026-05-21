#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
IMAGE="${IMAGE:-customer-service-backend:acceptance}"
SKIP_DOCKER_BUILD="${SKIP_DOCKER_BUILD:-false}"
SMOKE_PORT="${SMOKE_PORT:-}"
server_pid=""
tmp_env=""

cleanup() {
  if [[ -n "$server_pid" ]] && kill -0 "$server_pid" 2>/dev/null; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  if [[ -n "$tmp_env" ]]; then
    rm -f "$tmp_env"
  fi
}
trap cleanup EXIT

section() {
  printf '\n== %s ==\n' "$1"
}

pick_port() {
  if [[ -n "$SMOKE_PORT" ]]; then
    printf '%s' "$SMOKE_PORT"
    return
  fi
  python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
}

wait_ready() {
  local base="$1"
  for _ in $(seq 1 80); do
    if curl -fsS "$base/readyz" >/dev/null 2>&1; then
      return
    fi
    sleep 0.25
  done
  echo "backend did not become ready: $base" >&2
  return 1
}

section "backend unit tests"
(cd "$ROOT_DIR/backend" && go test ./...)

section "frontend syntax checks"
(cd "$ROOT_DIR" && node --check apps/admin-app/main.js)
(cd "$ROOT_DIR" && node --check apps/admin-app/common/api.js)
(cd "$ROOT_DIR" && node --check apps/admin-app/common/realtime.js)
(cd "$ROOT_DIR" && node --check apps/agent-app/main.js)
(cd "$ROOT_DIR" && node --check apps/agent-app/common/api.js)
(cd "$ROOT_DIR" && node --check apps/agent-app/common/realtime.js)

section "docker compose config"
docker compose -f "$ROOT_DIR/deployments/docker-compose.dev.yml" config >/tmp/customer-service-compose-dev-acceptance.out
docker compose --env-file "$ROOT_DIR/deployments/.env.example" -f "$ROOT_DIR/deployments/docker-compose.prod.example.yml" config >/tmp/customer-service-compose-prod-acceptance.out

section "production preflight"
tmp_env="$(mktemp -t customer-service-prod.XXXXXX.env)"
acceptance_pg_secret="local-acceptance-postgres-2026"
acceptance_data_key="local-acceptance-data-key-32-bytes"
acceptance_admin_secret="LocalAcceptanceAdmin2026!"
acceptance_agent_secret="LocalAcceptanceAgent2026!"
acceptance_metrics_token="local-acceptance-metrics-token-2026"
cat > "$tmp_env" <<'EOF'
POSTGRES_DB=customer_service
POSTGRES_USER=customer_service
POSTGRES_PASSWORD=__POSTGRES_PASSWORD__
DATABASE_URL=postgres://customer_service:__POSTGRES_PASSWORD__@postgres:5432/customer_service?sslmode=disable
REDIS_ADDR=redis:6379
DATA_ENCRYPTION_KEY=__DATA_ENCRYPTION_KEY__
ADMIN_BOOTSTRAP_PASSWORD=__ADMIN_BOOTSTRAP_PASSWORD__
AGENT_BOOTSTRAP_PASSWORD=__AGENT_BOOTSTRAP_PASSWORD__
CORS_ALLOWED_ORIGINS=https://service.acme.test,https://admin.acme.test
TRUSTED_PROXY_CIDRS=127.0.0.1/32,::1/128,172.16.0.0/12
SECURITY_HEADERS=true
RATE_LIMIT_ENABLED=true
RATE_LIMIT_RPS=20
RATE_LIMIT_BURST=60
METRICS_BEARER_TOKEN=__METRICS_BEARER_TOKEN__
OPENAI_API_KEY=
OPENAI_BASE_URL=https://api.openai.com/v1
OPENAI_MODEL=gpt-4o-mini
OPENAI_API_TYPE=chat_completions
UPLOAD_DRIVER=local
UPLOAD_PUBLIC_BASE_URL=https://service.acme.test
UPLOAD_MAX_BYTES=10485760
S3_ENDPOINT=
S3_REGION=us-east-1
S3_BUCKET=
S3_ACCESS_KEY_ID=
S3_SECRET_ACCESS_KEY=
S3_SESSION_TOKEN=
S3_FORCE_PATH_STYLE=false
S3_KEY_PREFIX=uploads
S3_PUBLIC_BASE_URL=
PUSH_WEBHOOK_URL=
PUSH_WEBHOOK_BEARER_TOKEN=
PUSH_WEBHOOK_TIMEOUT_SECONDS=5
EOF
sed -i '' \
  -e "s#__POSTGRES_PASSWORD__#$acceptance_pg_secret#g" \
  -e "s#__DATA_ENCRYPTION_KEY__#$acceptance_data_key#g" \
  -e "s#__ADMIN_BOOTSTRAP_PASSWORD__#$acceptance_admin_secret#g" \
  -e "s#__AGENT_BOOTSTRAP_PASSWORD__#$acceptance_agent_secret#g" \
  -e "s#__METRICS_BEARER_TOKEN__#$acceptance_metrics_token#g" \
  "$tmp_env"
"$ROOT_DIR/scripts/ops/preflight-prod.sh" "$tmp_env"
rm -f "$tmp_env"

if [[ "$SKIP_DOCKER_BUILD" != "true" ]]; then
  section "backend production image"
  docker build -t "$IMAGE" "$ROOT_DIR/backend"
fi

section "local smoke"
port="$(pick_port)"
(
  cd "$ROOT_DIR/backend"
  HTTP_ADDR=":$port" \
  STORE_DRIVER=memory \
  RATE_LIMIT_ENABLED=true \
  RATE_LIMIT_RPS=50 \
  RATE_LIMIT_BURST=100 \
  LOG_LEVEL=error \
  UPLOAD_DIR="/tmp/customer-service-uploads-acceptance-$port" \
  go run ./cmd/server
) &
server_pid="$!"
wait_ready "http://localhost:$port"
HTTP_BASE="http://localhost:$port" \
WS_BASE="ws://localhost:$port" \
SMOKE_WS_CONNECTIONS="${SMOKE_WS_CONNECTIONS:-5}" \
SMOKE_DURATION="${SMOKE_DURATION:-3s}" \
"$ROOT_DIR/scripts/smoke/local-smoke.sh"

section "acceptance complete"
echo "commercial acceptance passed"
