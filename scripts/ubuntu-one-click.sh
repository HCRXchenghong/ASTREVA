#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEPLOY_DIR="$ROOT_DIR/deployments/ubuntu"
COMPOSE_FILE="$DEPLOY_DIR/docker-compose.ubuntu.yml"
ENV_FILE="${ASTREVA_UBUNTU_ENV:-$DEPLOY_DIR/.env.ubuntu}"
GENERATED_CADDYFILE="$DEPLOY_DIR/Caddyfile"
STACK_NAME="${ASTREVA_STACK_NAME:-astreva-ubuntu}"

ACTION="${1:-up}"

compose_cmd() {
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    docker compose "$@"
    return
  fi
  if command -v docker-compose >/dev/null 2>&1; then
    docker-compose "$@"
    return
  fi
  echo "未检测到 docker compose，请先安装 Docker Compose" >&2
  exit 1
}

require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "缺少命令: $cmd" >&2
    exit 1
  fi
}

need_var() {
  local name="$1"
  local value="${!name:-}"
  if [[ -z "${value// /}" ]]; then
    echo "缺少环境变量: $name" >&2
    echo "请先在 $ENV_FILE 中补齐再执行" >&2
    exit 1
  fi
}

looks_like_placeholder() {
  local value="${1,,}"
  [[ "$value" == *change-me* || "$value" == *changeme* || "$value" == *example.com* || "$value" == *placeholder* ]]
}

value_too_short() {
  local value="${1:-}"
  local min="${2:-1}"
  [[ "${#value}" -lt "$min" ]]
}

validate_secret() {
  local name="$1"
  local min_len="$2"
  local value="${!name:-}"

  if value_too_short "$value" "$min_len"; then
    echo "${name} 长度不能少于 ${min_len} 个字符" >&2
    exit 1
  fi
  if [[ "$name" == "ADMIN_BOOTSTRAP_PASSWORD" || "$name" == "AGENT_BOOTSTRAP_PASSWORD" ]]; then
    if [[ "${value,,}" == *password* || "${value,,}" == *123456* || "$value" == "00000000" ]]; then
      echo "${name} 不能使用明显弱口令" >&2
      exit 1
    fi
  fi
  if looks_like_placeholder "$value"; then
    echo "${name} 仍然是示例占位值" >&2
    exit 1
  fi
}

strip() {
  local value="${1:-}"
  value="$(echo "$value" | tr -d '[:space:]')"
  value="${value#https://}"
  value="${value#http://}"
  value="${value%%/}"
  echo "$value"
}

write_caddyfile() {
  cat > "$GENERATED_CADDYFILE" <<EOF
{
  email $ACME_EMAIL
}

http:// {
  redir https://{host}{uri}
}

$SITE_DOMAIN {
  encode zstd gzip
  header {
    X-Content-Type-Options "nosniff"
    Referrer-Policy "strict-origin-when-cross-origin"
    Permissions-Policy "camera=(), microphone=(), geolocation=()"
  }

  reverse_proxy frontend:80
}

$ADMIN_DOMAIN {
  encode zstd gzip
  header {
    X-Content-Type-Options "nosniff"
    Referrer-Policy "strict-origin-when-cross-origin"
    Permissions-Policy "camera=(), microphone=(), geolocation=()"
  }

  reverse_proxy admin-server:1337
}

$SERVICE_DOMAIN {
  encode zstd gzip
  header {
    X-Content-Type-Options "nosniff"
    Referrer-Policy "strict-origin-when-cross-origin"
    Permissions-Policy "camera=(), microphone=(), geolocation=()"
  }

  handle /api/* {
    reverse_proxy servicebridge-backend-1:8080 servicebridge-backend-2:8080
  }

  handle /api {
    reverse_proxy servicebridge-backend-1:8080 servicebridge-backend-2:8080
  }

  handle /ws* {
    reverse_proxy servicebridge-backend-1:8080 servicebridge-backend-2:8080
  }

  handle /uploads/* {
    reverse_proxy servicebridge-backend-1:8080 servicebridge-backend-2:8080
  }

  handle /readyz {
    reverse_proxy servicebridge-backend-1:8080 servicebridge-backend-2:8080
  }

  handle /healthz {
    reverse_proxy servicebridge-backend-1:8080 servicebridge-backend-2:8080
  }

  handle /metrics {
    reverse_proxy servicebridge-backend-1:8080 servicebridge-backend-2:8080
  }

  handle {
    reverse_proxy servicebridge-user-web:80
  }
}
EOF
}

wait_for_http() {
  local label="$1"
  local url="$2"
  local i=0

  while ((i < 30)); do
    if curl -fsS -H "Host: $label" --max-time 3 "$url" >/dev/null; then
      return 0
    fi
    sleep 2
    ((i += 1))
  done
  echo "等待超时: $label" >&2
  return 1
}

compose_action() {
  local sub="$1"
  shift || true
  compose_cmd -f "$COMPOSE_FILE" --env-file "$ENV_FILE" -p "$STACK_NAME" "$sub" "$@"
}

print_usage() {
  cat <<EOF
用法: scripts/ubuntu-one-click.sh [up|down|status|logs|help]
  up: 启动全部服务（默认）
  down: 停止并清理容器
  status: 查看服务状态
  logs [service]: 查看日志（默认全部）
  help: 显示帮助
EOF
}

if [[ ! -f "$COMPOSE_FILE" ]]; then
  echo "未找到 compose 配置: $COMPOSE_FILE" >&2
  exit 1
fi

case "$ACTION" in
  down|stop)
    compose_action down
    echo "stack 已停止: $STACK_NAME"
    exit 0
    ;;
  status)
    compose_action ps
    exit 0
    ;;
  logs)
    if [[ "${2:-}" == "" ]]; then
      compose_action logs -f
    else
      compose_action logs -f "$2"
    fi
    exit 0
    ;;
  help|-h|--help)
    print_usage
    exit 0
    ;;
  up|start|"")
    ;;
  *)
    echo "未知命令: $ACTION" >&2
    print_usage
    exit 1
    ;;
esac

if [[ ! -f "$ENV_FILE" ]]; then
  cp "$DEPLOY_DIR/.env.ubuntu.example" "$ENV_FILE"
  echo "已创建环境文件: $ENV_FILE"
  echo "请先填写域名和密钥后再次执行 scripts/ubuntu-one-click.sh up"
  exit 1
fi

require_cmd docker
require_cmd curl

set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

SITE_DOMAIN="$(strip "$SITE_DOMAIN")"
ADMIN_DOMAIN="$(strip "$ADMIN_DOMAIN")"
SERVICE_DOMAIN="$(strip "$SERVICE_DOMAIN")"
ACME_EMAIL="$(echo "${ACME_EMAIL:-}" | tr -d '[:space:]')"

need_var SITE_DOMAIN
need_var ADMIN_DOMAIN
need_var SERVICE_DOMAIN
need_var ACME_EMAIL
need_var POSTGRES_USER
need_var POSTGRES_PASSWORD
need_var POSTGRES_DB
need_var DATA_ENCRYPTION_KEY
need_var ADMIN_BOOTSTRAP_PASSWORD
need_var AGENT_BOOTSTRAP_PASSWORD
need_var CORS_ALLOWED_ORIGINS
need_var TRUSTED_PROXY_CIDRS
need_var REBUILD_WEBHOOK_SECRET
need_var ADMIN_2FA_SESSION_SECRET
need_var METRICS_BEARER_TOKEN

if [[ "${CORS_ALLOWED_ORIGINS}" == "*" || "${TRUSTED_PROXY_CIDRS}" == "*" ]]; then
  echo "CORS_ALLOWED_ORIGINS / TRUSTED_PROXY_CIDRS 不能使用 *，请配置具体网段/域名" >&2
  exit 1
fi

validate_secret DATA_ENCRYPTION_KEY 32
validate_secret ADMIN_BOOTSTRAP_PASSWORD 12
validate_secret AGENT_BOOTSTRAP_PASSWORD 12
validate_secret REBUILD_WEBHOOK_SECRET 16
validate_secret ADMIN_2FA_SESSION_SECRET 24
validate_secret METRICS_BEARER_TOKEN 24

POSTGRES_DB="${POSTGRES_DB:-customer_service}"
POSTGRES_USER="${POSTGRES_USER:-customer_service}"
REDIS_ADDR="${REDIS_ADDR:-redis:6379}"
REBUILD_WEBHOOK_PORT="${REBUILD_WEBHOOK_PORT:-8787}"
RATE_LIMIT_ENABLED="${RATE_LIMIT_ENABLED:-true}"
RATE_LIMIT_RPS="${RATE_LIMIT_RPS:-20}"
RATE_LIMIT_BURST="${RATE_LIMIT_BURST:-60}"
SECURITY_HEADERS="${SECURITY_HEADERS:-true}"
UPLOAD_DRIVER="${UPLOAD_DRIVER:-local}"
UPLOAD_PUBLIC_BASE_URL="${UPLOAD_PUBLIC_BASE_URL:-https://$SERVICE_DOMAIN}"
UPLOAD_MAX_BYTES="${UPLOAD_MAX_BYTES:-10485760}"
S3_FORCE_PATH_STYLE="${S3_FORCE_PATH_STYLE:-false}"
OPENAI_API_KEY="${OPENAI_API_KEY:-}"
OPENAI_BASE_URL="${OPENAI_BASE_URL:-https://api.openai.com/v1}"
OPENAI_MODEL="${OPENAI_MODEL:-gpt-4o-mini}"
OPENAI_API_TYPE="${OPENAI_API_TYPE:-chat_completions}"
ADMIN_2FA_ISSUER="${ADMIN_2FA_ISSUER:-星渡官网账号}"
ADMIN_2FA_SESSION_TTL_MS="${ADMIN_2FA_SESSION_TTL_MS:-43200000}"
FEISHU_ENABLED="${FEISHU_ENABLED:-false}"
FEISHU_APP_ID="${FEISHU_APP_ID:-}"
FEISHU_APP_SECRET="${FEISHU_APP_SECRET:-}"
FEISHU_VERIFICATION_TOKEN="${FEISHU_VERIFICATION_TOKEN:-}"
FEISHU_ENCRYPT_KEY="${FEISHU_ENCRYPT_KEY:-}"
FEISHU_DEFAULT_CHAT_ID="${FEISHU_DEFAULT_CHAT_ID:-}"
FEISHU_AGENT_ID="${FEISHU_AGENT_ID:-agent_feishu}"
FEISHU_TIMEOUT_SECONDS="${FEISHU_TIMEOUT_SECONDS:-8}"
S3_PUBLIC_BASE_URL="${S3_PUBLIC_BASE_URL:-}"
S3_ENDPOINT="${S3_ENDPOINT:-}"
S3_REGION="${S3_REGION:-us-east-1}"
S3_BUCKET="${S3_BUCKET:-}"
S3_ACCESS_KEY_ID="${S3_ACCESS_KEY_ID:-}"
S3_SECRET_ACCESS_KEY="${S3_SECRET_ACCESS_KEY:-}"
S3_SESSION_TOKEN="${S3_SESSION_TOKEN:-}"
FRAME_ANCESTORS="${FRAME_ANCESTORS:-https://$SITE_DOMAIN}"
SITE_ORIGIN="https://$SITE_DOMAIN"
ADMIN_ORIGIN="https://$ADMIN_DOMAIN"
SERVICE_ORIGIN="https://$SERVICE_DOMAIN"
DATABASE_URL="${DATABASE_URL:-postgres://$POSTGRES_USER:$POSTGRES_PASSWORD@postgres:5432/$POSTGRES_DB?sslmode=disable}"

write_caddyfile

export PUBLIC_SITE_URL="$SITE_ORIGIN"
export PUBLIC_ADMIN_URL="$ADMIN_ORIGIN"
export PUBLIC_SERVICEBRIDGE_URL="$SERVICE_ORIGIN"
export SITE_ORIGIN
export ADMIN_ORIGIN
export SERVICE_ORIGIN
export REDIS_ADDR
export DATABASE_URL
export DATA_ENCRYPTION_KEY
export ADMIN_BOOTSTRAP_PASSWORD
export AGENT_BOOTSTRAP_PASSWORD
export CORS_ALLOWED_ORIGINS
export FRAME_ANCESTORS
export TRUSTED_PROXY_CIDRS
export SECURITY_HEADERS
export RATE_LIMIT_ENABLED
export RATE_LIMIT_RPS
export RATE_LIMIT_BURST
export METRICS_BEARER_TOKEN
export OPENAI_API_KEY
export OPENAI_BASE_URL
export OPENAI_MODEL
export OPENAI_API_TYPE
export UPLOAD_DRIVER
export UPLOAD_PUBLIC_BASE_URL
export UPLOAD_MAX_BYTES
export S3_FORCE_PATH_STYLE
export S3_ENDPOINT
export S3_REGION
export S3_BUCKET
export S3_ACCESS_KEY_ID
export S3_SECRET_ACCESS_KEY
export S3_SESSION_TOKEN
export S3_PUBLIC_BASE_URL
export S3_KEY_PREFIX="${S3_KEY_PREFIX:-uploads}"
export FEISHU_ENABLED
export FEISHU_APP_ID
export FEISHU_APP_SECRET
export FEISHU_VERIFICATION_TOKEN
export FEISHU_ENCRYPT_KEY
export FEISHU_DEFAULT_CHAT_ID
export FEISHU_AGENT_ID
export FEISHU_TIMEOUT_SECONDS
export REBUILD_WEBHOOK_SECRET
export REBUILD_WEBHOOK_PORT
export ADMIN_2FA_SESSION_SECRET
export ADMIN_2FA_ISSUER
export ADMIN_2FA_SESSION_TTL_MS
export STRAPI_API_TOKEN="${STRAPI_API_TOKEN:-}"

compose_action up -d --build

echo "等待反代入口可访问..."
wait_for_http "$SITE_DOMAIN" "http://127.0.0.1/"
wait_for_http "$ADMIN_DOMAIN" "http://127.0.0.1/admin-2fa"
wait_for_http "$SERVICE_DOMAIN" "http://127.0.0.1/readyz"
wait_for_http "$SERVICE_DOMAIN" "http://127.0.0.1/?embed=1"

echo "启动完成"
echo "主站: $SITE_ORIGIN"
echo "后台: $ADMIN_ORIGIN/site-admin/"
echo "客服: $SERVICE_ORIGIN/?embed=1"
echo "客服接口: $SERVICE_ORIGIN/api"
echo "证书: Caddy 自动签发并自动续签（需域名 DNS 指向本机 80/443）"
echo "停止: scripts/ubuntu-one-click.sh down"
echo "查看: scripts/ubuntu-one-click.sh logs [service_name]"
