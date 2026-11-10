#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEPLOY_DIR="$ROOT_DIR/deployments/ubuntu"
ENV_FILE="${ASTREVA_UBUNTU_ENV:-$DEPLOY_DIR/.env.ubuntu}"
COMPOSE_FILE="$DEPLOY_DIR/docker-compose.ubuntu.yml"
BACKUP_DIR="${ASTREVA_BACKUP_DIR:-$ROOT_DIR/backups/postgres}"
RETENTION_DAYS="${ASTREVA_BACKUP_RETENTION_DAYS:-30}"
STACK_NAME="${ASTREVA_STACK_NAME:-astreva-ubuntu}"
BACKUP_HELPER_IMAGE="${ASTREVA_BACKUP_HELPER_IMAGE:-alpine:3.20}"

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

# macOS 默认没有 sha256sum，只有 shasum -a 256；Linux 两者通常都有。
if command -v sha256sum >/dev/null 2>&1; then
  HASH_CMD=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then
  HASH_CMD=(shasum -a 256)
else
  echo "缺少 sha256sum / shasum，无法生成校验文件" >&2
  exit 1
fi

write_digest() {
  local file="$1"
  (cd "$(dirname "$file")" && "${HASH_CMD[@]}" "$(basename "$file")") > "$file.sha256"
}

docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" -p "$STACK_NAME" exec -T postgres \
  pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --format=plain --no-owner --no-privileges \
  | gzip -9 > "$TMP_FILE"
mv "$TMP_FILE" "$TARGET_FILE"

# 先确认 gzip 完整，再生成并校验 SHA-256 摘要文件，避免把写坏的备份当成有效备份。
# 注意：macOS 自带 /sbin/sha256sum 校验“没有合法摘要行”的文件时会返回 0，
# 所以这里直接校验摘要格式，而不是依赖 `sha256sum -c` 的退出码。
gzip -t "$TARGET_FILE"
write_digest "$TARGET_FILE"
BACKUP_DIGEST="$(awk 'NF>=1 {print $1; exit}' "$TARGET_FILE.sha256")"
if [[ ! "$BACKUP_DIGEST" =~ ^[0-9a-fA-F]{64}$ ]]; then
  echo "校验文件生成异常（摘要格式不正确）：$TARGET_FILE.sha256" >&2
  exit 1
fi

# 本地媒体卷：生产默认 ADMIN_MEDIA_DRIVER=local，数据库之外还必须备份素材。
# 对象存储（R2/S3）模式由供应商负责版本/生命周期，这里的本地卷可能是空目录，仍会安全归档。
backup_volume() {
  local name="$1"
  local volume="${STACK_NAME}_${name}"
  local archive="$BACKUP_DIR/astreva-${name}-${STAMP}.tar.gz"
  if ! docker volume inspect "$volume" >/dev/null 2>&1; then
    echo "提示：未找到本地卷 ${volume}，跳过 ${name}（S3/R2 模式或尚未初始化）。" >&2
    return 0
  fi
  if ! docker run --rm -v "$volume:/data:ro" -v "$BACKUP_DIR:/backup" "$BACKUP_HELPER_IMAGE" \
      sh -c "cd /data && tar czf /backup/$(basename "$archive") ."; then
    echo "本地卷备份失败：$volume" >&2
    return 1
  fi
  gzip -t "$archive"
  write_digest "$archive"
  echo "本地媒体卷备份完成：$archive"
}

backup_volume admin-uploads
backup_volume servicebridge-uploads

find "$BACKUP_DIR" -maxdepth 1 -type f \( -name 'astreva-postgres-*.sql.gz' -o -name 'astreva-postgres-*.sql.gz.sha256' -o -name 'astreva-*-uploads-*.tar.gz' -o -name 'astreva-*-uploads-*.tar.gz.sha256' \) -mtime "+$RETENTION_DAYS" -delete

# 可选：给 node_exporter textfile collector 写“最近一次成功备份时间”，
# 供 deployments/ubuntu/monitoring/alerts.yml 的 AstrevaBackupStale 告警使用。
# 例如 cron 中设置 ASTREVA_BACKUP_TEXTFILE=/var/lib/node_exporter/textfile/astreva_backup.prom
if [[ -n "${ASTREVA_BACKUP_TEXTFILE:-}" ]]; then
  TEXTFILE_TMP="$ASTREVA_BACKUP_TEXTFILE.tmp.$$"
  mkdir -p "$(dirname "$ASTREVA_BACKUP_TEXTFILE")"
  {
    echo "# HELP astreva_backup_last_success_timestamp_seconds Unix 时间戳：最近一次 PostgreSQL 备份成功的时间。"
    echo "# TYPE astreva_backup_last_success_timestamp_seconds gauge"
    echo "astreva_backup_last_success_timestamp_seconds $(date -u +%s)"
  } > "$TEXTFILE_TMP"
  mv "$TEXTFILE_TMP" "$ASTREVA_BACKUP_TEXTFILE"
fi

echo "PostgreSQL 备份完成：$TARGET_FILE"
echo "校验文件：$TARGET_FILE.sha256"
