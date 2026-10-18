#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEPLOY_DIR="$ROOT_DIR/deployments/ubuntu"
ENV_FILE="${ASTREVA_UBUNTU_ENV:-$DEPLOY_DIR/.env.ubuntu}"
COMPOSE_FILE="$DEPLOY_DIR/docker-compose.ubuntu.yml"
BACKUP_DIR="${ASTREVA_BACKUP_DIR:-$ROOT_DIR/backups/postgres}"
RETENTION_DAYS="${ASTREVA_BACKUP_RETENTION_DAYS:-30}"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "未找到生产环境文件：$ENV_FILE" >&2
  exit 1
fi
if [[ "$BACKUP_DIR" == "/" || -z "${BACKUP_DIR// }" ]]; then
  echo "备份目录不能是根目录" >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

mkdir -p "$BACKUP_DIR"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
TMP_FILE="$BACKUP_DIR/.astreva-postgres-$STAMP.sql.gz.tmp"
TARGET_FILE="$BACKUP_DIR/astreva-postgres-$STAMP.sql.gz"

docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" exec -T postgres \
  pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --format=plain --no-owner --no-privileges \
  | gzip -9 > "$TMP_FILE"
mv "$TMP_FILE" "$TARGET_FILE"
sha256sum "$TARGET_FILE" > "$TARGET_FILE.sha256"

find "$BACKUP_DIR" -maxdepth 1 -type f \( -name 'astreva-postgres-*.sql.gz' -o -name 'astreva-postgres-*.sql.gz.sha256' \) -mtime "+$RETENTION_DAYS" -delete
echo "PostgreSQL 备份完成：$TARGET_FILE"
