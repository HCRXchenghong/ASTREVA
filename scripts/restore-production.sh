#!/usr/bin/env bash
#
# 从 `scripts/backup-production.sh` 生成的 .sql.gz 备份恢复到“隔离数据库”，用于恢复演练。
#
# 设计原则：
#   1. 先校验 SHA-256，再校验 gzip 完整性，最后才做任何破坏性动作。
#   2. 默认目标库是 customer_service_restore，绝不碰生产库。
#   3. 只有在显式 --force-production 且二次确认时才允许覆盖生产库。
#   4. --yes 只跳过“隔离库删除重建”的一次确认，不会跳过生产库的二次确认。
#
# 用法：
#   scripts/restore-production.sh [--yes] [--db NAME] [--env-file PATH] [--compose-file PATH] <backup.sql.gz>
#   scripts/restore-production.sh --help
#
# 示例：
#   # 演练（默认恢复 customer_service_restore，非交互）
#   scripts/restore-production.sh --yes /srv/astreva-backups/astreva-postgres-20260101T020000Z.sql.gz
#
#   # 指定隔离库名
#   scripts/restore-production.sh --yes --db cms_restore_2026q1 /srv/astreva-backups/astreva-postgres-....sql.gz
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEPLOY_DIR="$ROOT_DIR/deployments/ubuntu"
ENV_FILE="${ASTREVA_UBUNTU_ENV:-$DEPLOY_DIR/.env.ubuntu}"
COMPOSE_FILE="$DEPLOY_DIR/docker-compose.ubuntu.yml"
DEFAULT_TARGET_DB="customer_service_restore"

TARGET_DB="$DEFAULT_TARGET_DB"
ASSUME_YES=0
FORCE_PRODUCTION=0
BACKUP_FILE=""

usage() {
  cat <<'EOF'
从 PostgreSQL 逻辑备份恢复到隔离数据库（恢复演练用）。

用法：
  scripts/restore-production.sh [选项] <backup.sql.gz>

选项：
  -y, --yes                 跳过隔离库“删除+重建”的确认（生产库仍需二次确认）
      --db NAME             目标数据库名，默认 customer_service_restore
      --force-production    允许覆盖生产库（必须再手工输入库名二次确认）
      --env-file PATH       生产环境文件，默认 deployments/ubuntu/.env.ubuntu
      --compose-file PATH   Compose 文件，默认 deployments/ubuntu/docker-compose.ubuntu.yml
  -h, --help                显示帮助

行为：
  * 校验 <backup.sql.gz>.sha256（sha256sum 或 macOS 的 shasum -a 256）。
  * 校验 gzip 完整性。
  * 删除并重建目标数据库，然后用 `gunzip -c | docker compose exec -T postgres psql` 导入。
  * 恢复后校验表 site_content / site_admin_users / site_fallback_versions / site_media、
    当前内容 JSONB 结构、最近一次发布记录与降级版本引用完整性。
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -y|--yes)
      ASSUME_YES=1
      shift
      ;;
    --force-production)
      FORCE_PRODUCTION=1
      shift
      ;;
    --db|--database)
      if [[ $# -lt 2 || -z "${2:-}" ]]; then
        echo "--db 需要一个数据库名" >&2
        exit 2
      fi
      TARGET_DB="$2"
      shift 2
      ;;
    --env-file)
      if [[ $# -lt 2 || -z "${2:-}" ]]; then
        echo "--env-file 需要一个路径" >&2
        exit 2
      fi
      ENV_FILE="$2"
      shift 2
      ;;
    --compose-file)
      if [[ $# -lt 2 || -z "${2:-}" ]]; then
        echo "--compose-file 需要一个路径" >&2
        exit 2
      fi
      COMPOSE_FILE="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      break
      ;;
    -*)
      echo "未知参数：$1" >&2
      usage >&2
      exit 2
      ;;
    *)
      if [[ -n "$BACKUP_FILE" ]]; then
        echo "只能指定一个备份文件（已给出：${BACKUP_FILE}）" >&2
        exit 2
      fi
      BACKUP_FILE="$1"
      shift
      ;;
  esac
done

if [[ -z "$BACKUP_FILE" ]]; then
  echo "缺少备份文件参数。用 --help 查看用法。" >&2
  exit 2
fi
if [[ ! -f "$BACKUP_FILE" ]]; then
  echo "备份文件不存在：$BACKUP_FILE" >&2
  exit 1
fi
if [[ ! -f "$ENV_FILE" ]]; then
  echo "未找到生产环境文件：$ENV_FILE" >&2
  exit 1
fi
if [[ ! -f "$COMPOSE_FILE" ]]; then
  echo "未找到 Compose 文件：$COMPOSE_FILE" >&2
  exit 1
fi

# shellcheck disable=SC1090
set -a
source "$ENV_FILE"
set +a

if [[ -z "${POSTGRES_USER:-}" || -z "${POSTGRES_DB:-}" ]]; then
  echo "环境文件缺少 POSTGRES_USER 或 POSTGRES_DB：$ENV_FILE" >&2
  exit 1
fi

if [[ ! "$TARGET_DB" =~ ^[A-Za-z_][A-Za-z0-9_]{0,62}$ ]]; then
  echo "数据库名不合法（只允许字母、数字、下划线，且不以数字开头）：$TARGET_DB" >&2
  exit 1
fi
case "$TARGET_DB" in
  postgres|template0|template1)
    echo "拒绝操作 PostgreSQL 系统数据库：$TARGET_DB" >&2
    exit 1
    ;;
esac

# ---------------------------------------------------------------- 生产库保护（尽早拒绝）
if [[ "$TARGET_DB" == "$POSTGRES_DB" && "$FORCE_PRODUCTION" -ne 1 ]]; then
  printf '\n拒绝执行：目标数据库 %s 正是当前生产库（POSTGRES_DB）。\n' "$TARGET_DB" >&2
  printf '恢复演练必须使用隔离库（默认 %s）。\n' "$DEFAULT_TARGET_DB" >&2
  printf '确实要覆盖生产库时，请显式加 --force-production，并按提示二次确认。\n' >&2
  exit 1
fi

compose() {
  docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" "$@"
}

psql_admin() {
  compose exec -T postgres psql -U "$POSTGRES_USER" -d postgres -v ON_ERROR_STOP=1 "$@"
}

query() {
  compose exec -T postgres psql -U "$POSTGRES_USER" -d "$TARGET_DB" -v ON_ERROR_STOP=1 -tA -c "$1"
}

FAILURES=()

note() { printf '%s\n' "$*"; }
fail_check() {
  FAILURES+=("$1")
  printf '  [FAIL] %s\n' "$1" >&2
}
warn_check() { printf '  [WARN] %s\n' "$1"; }
ok_check() { printf '  [ OK ] %s\n' "$1"; }

on_error() {
  local code=$?
  printf '\n恢复流程失败（退出码 %s，第 %s 行）。目标数据库：%s\n' "$code" "${BASH_LINENO[0]:-?}" "$TARGET_DB" >&2
  printf '排查：docker compose -f %s --env-file %s ps postgres；确认容器在运行且磁盘有足够空间。\n' "$COMPOSE_FILE" "$ENV_FILE" >&2
}
trap on_error ERR

# ---------------------------------------------------------------- 校验阶段（无破坏性）
note "== 1/5 校验备份文件 =="
CHECKSUM_FILE="$BACKUP_FILE.sha256"
if [[ ! -f "$CHECKSUM_FILE" ]]; then
  echo "缺少校验文件：$CHECKSUM_FILE" >&2
  echo "拒绝在无法校验完整性的情况下恢复。" >&2
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  HASH_CMD=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then
  HASH_CMD=(shasum -a 256)
else
  echo "缺少 sha256sum / shasum，无法校验备份。" >&2
  exit 1
fi

# 不能只依赖 `sha256sum -c` / `shasum -c` 的退出码：macOS 自带 /sbin/sha256sum
# 在“校验文件里没有一行合法摘要”时只会告警并返回 0。这里显式校验摘要格式，
# 再逐字节比较，避免被截断或写坏的 .sha256 文件骗过。
recorded_digest="$(awk 'NF>=1 {print $1; exit}' "$CHECKSUM_FILE")"
if [[ ! "$recorded_digest" =~ ^[0-9a-fA-F]{64}$ ]]; then
  echo "校验文件不含合法的 SHA-256 摘要：$CHECKSUM_FILE" >&2
  exit 1
fi
actual_digest="$("${HASH_CMD[@]}" "$BACKUP_FILE" | awk 'NF>=1 {print $1; exit}')"
actual_digest="$(printf '%s' "$actual_digest" | tr 'A-F' 'a-f')"
recorded_digest="$(printf '%s' "$recorded_digest" | tr 'A-F' 'a-f')"
if [[ "$actual_digest" != "$recorded_digest" ]]; then
  echo "SHA-256 校验失败：$BACKUP_FILE" >&2
  echo "  期望：$recorded_digest" >&2
  echo "  实际：$actual_digest" >&2
  exit 1
fi
ok_check "SHA-256 校验通过"

if ! gzip -t "$BACKUP_FILE"; then
  echo "gzip 完整性校验失败：$BACKUP_FILE" >&2
  exit 1
fi
ok_check "gzip 完整性校验通过"

if ! compose ps -q postgres | grep -q .; then
  echo "postgres 容器未运行，无法恢复。先执行：docker compose -f $COMPOSE_FILE --env-file $ENV_FILE up -d postgres" >&2
  exit 1
fi
if ! compose exec -T postgres pg_isready -q -U "$POSTGRES_USER" -d postgres; then
  echo "postgres 容器未就绪（pg_isready 失败）" >&2
  exit 1
fi
ok_check "postgres 容器就绪"

# ---------------------------------------------------------------- 生产库二次确认
if [[ "$TARGET_DB" == "$POSTGRES_DB" ]]; then
  printf '\n！！！危险操作：即将删除并重建生产数据库 %s，并用备份 %s 覆盖。\n' "$TARGET_DB" "$BACKUP_FILE"
  printf '该操作不可自动回滚。请输入生产数据库名（%s）以确认：' "$TARGET_DB"
  answer=""
  read -r answer || true
  if [[ "$answer" != "$TARGET_DB" ]]; then
    echo "确认不匹配，已取消。" >&2
    exit 1
  fi
  if [[ "$ASSUME_YES" -eq 1 ]]; then
    printf '再次确认：--yes 不会跳过生产库冒险操作，请输入 RESTORE PRODUCTION 继续：'
    second=""
    read -r second || true
    if [[ "$second" != "RESTORE PRODUCTION" ]]; then
      echo "二次确认失败，已取消。" >&2
      exit 1
    fi
  fi
fi

# ---------------------------------------------------------------- 隔离库确认
if [[ "$ASSUME_YES" -ne 1 && "$TARGET_DB" != "$POSTGRES_DB" ]]; then
  printf '\n将把 %s 恢复到数据库 %s（该库会被删除后重建）。继续？[y/N] ' "$BACKUP_FILE" "$TARGET_DB"
  confirm=""
  read -r confirm || true
  case "$confirm" in
    y|Y|yes|YES|Yes) ;;
    *)
      echo "已取消。" >&2
      exit 1
      ;;
  esac
fi

# ---------------------------------------------------------------- 重建目标库
note ""
note "== 2/5 重建隔离数据库 =="
psql_admin -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '$TARGET_DB' AND pid <> pg_backend_pid();" >/dev/null
psql_admin -c "DROP DATABASE IF EXISTS \"$TARGET_DB\";" >/dev/null
psql_admin -c "CREATE DATABASE \"$TARGET_DB\";" >/dev/null
ok_check "已重建数据库 $TARGET_DB"

# ---------------------------------------------------------------- 导入
note ""
note "== 3/5 导入备份（gunzip -c | psql）=="
if ! gunzip -c "$BACKUP_FILE" | compose exec -T postgres psql -U "$POSTGRES_USER" -d "$TARGET_DB" -v ON_ERROR_STOP=1 -q; then
  echo "导入失败：$BACKUP_FILE" >&2
  exit 1
fi
ok_check "导入完成"

# ---------------------------------------------------------------- 校验
note ""
note "== 4/5 校验恢复结果 =="

for table in site_content site_admin_users site_fallback_versions site_media; do
  exists="$(query "SELECT to_regclass('public.$table') IS NOT NULL;")"
  if [[ "$exists" == "t" ]]; then
    ok_check "表存在：$table"
  else
    fail_check "缺少表：$table"
  fi
done

if [[ ${#FAILURES[@]} -gt 0 ]]; then
  note ""
  note "== 5/5 结果 =="
  printf '恢复校验未通过，缺少关键表。\n' >&2
  exit 1
fi

content_rows="$(query 'SELECT count(*) FROM site_content;')"
admin_rows="$(query 'SELECT count(*) FROM site_admin_users;')"
fallback_rows="$(query 'SELECT count(*) FROM site_fallback_versions;')"
media_rows="$(query 'SELECT count(*) FROM site_media;')"
revision="$(query 'SELECT COALESCE(max(revision), 0) FROM site_content;')"
content_shape_ok="$(query "SELECT ((content ? 'site') AND (content ? 'home')) FROM site_content WHERE id = 1;")"

# site_publish_jobs / site_fallback_state 属于后续迁移新增的表：老备份里可能没有，
# 缺失时降级为提示，而不是让整个校验崩掉。
has_publish_jobs="$(query "SELECT to_regclass('public.site_publish_jobs') IS NOT NULL;")"
if [[ "$has_publish_jobs" == "t" ]]; then
  latest_job="$(query "SELECT COALESCE((SELECT id || ' | ' || status || ' | ' || to_char(requested_at, 'YYYY-MM-DD HH24:MI:SS') FROM site_publish_jobs ORDER BY requested_at DESC LIMIT 1), '(无发布记录)');")"
else
  latest_job="(备份中无 site_publish_jobs 表)"
fi

has_fallback_state="$(query "SELECT to_regclass('public.site_fallback_state') IS NOT NULL;")"
if [[ "$has_fallback_state" == "t" ]]; then
  orphan_fallbacks="$(query 'SELECT count(*) FROM site_fallback_state s LEFT JOIN site_fallback_versions v ON v.id = s.active_version_id WHERE v.id IS NULL;')"
else
  orphan_fallbacks="0"
fi

note ""
note "恢复结果摘要："
printf '  site_content           行数=%s（revision=%s）\n' "$content_rows" "$revision"
printf '  site_admin_users       行数=%s\n' "$admin_rows"
printf '  site_fallback_versions 行数=%s\n' "$fallback_rows"
printf '  site_media             行数=%s\n' "$media_rows"
printf '  最近一次发布记录       %s\n' "$latest_job"
printf '  当前降级版本引用异常    %s\n' "$orphan_fallbacks"

[[ "$content_rows" == "1" ]] && ok_check "site_content 恰好一行（当前内容）" || fail_check "site_content 行数应为 1，实际 $content_rows"
[[ "$content_shape_ok" == "t" ]] && ok_check "site_content.content 含 site/home 字段" || fail_check "site_content.content 结构异常（缺少 site 或 home）"
if [[ "$admin_rows" == "0" ]]; then
  warn_check "site_admin_users 为空：恢复后无法登录后台，请确认备份来源是否包含管理员"
else
  ok_check "site_admin_users 非空（$admin_rows 个管理员）"
fi
if [[ "$fallback_rows" == "0" ]]; then
  warn_check "site_fallback_versions 为空：没有可用的降级版本（内容发布降级保护不可用）"
else
  ok_check "site_fallback_versions 非空（$fallback_rows 个版本）"
fi
if [[ "$media_rows" == "0" ]]; then
  warn_check "site_media 为空：素材库记录为 0（对象存储里的文件仍需单独核验）"
else
  ok_check "site_media 非空（$media_rows 条记录）"
fi
if [[ "$orphan_fallbacks" == "0" ]]; then
  ok_check "当前启用的降级版本在 site_fallback_versions 中存在"
else
  fail_check "site_fallback_state 引用了不存在的降级版本（$orphan_fallbacks 条）"
fi

note ""
note "== 5/5 结果 =="
if [[ ${#FAILURES[@]} -gt 0 ]]; then
  printf '恢复校验未通过（%d 项失败）：\n' "${#FAILURES[@]}" >&2
  for item in "${FAILURES[@]}"; do printf '  - %s\n' "$item" >&2; done
  exit 1
fi

cat <<EOF
恢复演练通过：$BACKUP_FILE → 数据库 $TARGET_DB
验收点：4 张关键表齐全、site_content 结构与 revision 正常、降级版本引用完整。
后续手工核验（脚本无法自动完成）：
  1. 用恢复库启动一个临时后台实例，确认能登录并看到最新内容；
  2. 抽查 site_media 记录对应的对象存储文件仍可访问（备份不含对象存储本体）；
  3. 记录本次演练时间、备份时间戳与校验值，归档到运维记录。
清理隔离库：docker compose -f $COMPOSE_FILE --env-file $ENV_FILE exec -T postgres \\
  psql -U $POSTGRES_USER -d postgres -c 'DROP DATABASE "$TARGET_DB";'
EOF
