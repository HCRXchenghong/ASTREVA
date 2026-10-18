#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
ENV_FILE="${1:-$ROOT_DIR/deployments/.env}"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "env file not found: $ENV_FILE" >&2
  echo "copy deployments/.env.example to deployments/.env and replace every placeholder first" >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

failures=()

require() {
  local name="$1"
  local value="${!name:-}"
  if [[ -z "${value// }" ]]; then
    failures+=("$name is required")
  fi
}

reject_placeholder() {
  local name="$1"
  local value="${!name:-}"
  local lowered
  lowered="$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]')"
  if [[ "$lowered" == *"change-me"* || "$lowered" == *"changeme"* || "$lowered" == *"example.com"* || "$lowered" == *"placeholder"* ]]; then
    failures+=("$name still contains a placeholder value")
  fi
}

strong_password() {
  local name="$1"
  local value="${!name:-}"
  local lowered
  lowered="$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]')"
  if [[ "${#value}" -lt 12 || "$value" == "123456" || "$lowered" == *"password"* || "$lowered" == *"change-me"* ]]; then
    failures+=("$name must be a non-default password with at least 12 chars")
  fi
}

for name in \
  POSTGRES_PASSWORD DATABASE_URL REDIS_ADDR DATA_ENCRYPTION_KEY \
  ADMIN_BOOTSTRAP_PASSWORD AGENT_BOOTSTRAP_PASSWORD CORS_ALLOWED_ORIGINS \
  TRUSTED_PROXY_CIDRS METRICS_BEARER_TOKEN; do
  require "$name"
  reject_placeholder "$name"
done

strong_password ADMIN_BOOTSTRAP_PASSWORD
strong_password AGENT_BOOTSTRAP_PASSWORD

if [[ "${CORS_ALLOWED_ORIGINS:-}" == "*" ]]; then
  failures+=("CORS_ALLOWED_ORIGINS must not be * in production")
fi

if [[ "${TRUSTED_PROXY_CIDRS:-}" == "*" ]]; then
  failures+=("TRUSTED_PROXY_CIDRS must not be * in production")
fi

if [[ "${RATE_LIMIT_ENABLED:-true}" != "true" ]]; then
  failures+=("RATE_LIMIT_ENABLED must be true in production")
fi

if [[ "${SECURITY_HEADERS:-true}" != "true" ]]; then
  failures+=("SECURITY_HEADERS must be true in production")
fi

if [[ "${UPLOAD_DRIVER:-local}" == "s3" ]]; then
  require S3_BUCKET
  reject_placeholder S3_BUCKET
fi

if ((${#failures[@]} > 0)); then
  printf 'production preflight failed:\n' >&2
  for failure in "${failures[@]}"; do
    printf ' - %s\n' "$failure" >&2
  done
  exit 1
fi

docker compose --env-file "$ENV_FILE" -f "$ROOT_DIR/deployments/docker-compose.prod.example.yml" config >/tmp/customer-service-compose-prod-preflight.out

echo "production preflight passed"
