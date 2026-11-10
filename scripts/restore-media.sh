#!/usr/bin/env bash
#
# 恢复 backup-production.sh 生成的本地媒体卷归档。
#
# 用法：
#   scripts/restore-media.sh --yes --volume admin-uploads <astreva-admin-uploads-....tar.gz>
#   scripts/restore-media.sh --yes --volume servicebridge-uploads <astreva-servicebridge-uploads-....tar.gz>
#
# 该脚本会先停止会写该卷的服务，清空目标 Docker 卷，解包并重启服务。
# 生产库恢复仍由 scripts/restore-production.sh 负责；本脚本只恢复媒体卷。
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEPLOY_DIR="$ROOT_DIR/deployments/ubuntu"
COMPOSE_FILE="$DEPLOY_DIR/docker-compose.ubuntu.yml"
ENV_FILE="${ASTREVA_UBUNTU_ENV:-$DEPLOY_DIR/.env.ubuntu}"
STACK_NAME="${ASTREVA_STACK_NAME:-astreva-ubuntu}"
VOLUME_NAME="admin-uploads"
ARCHIVE=""
ASSUME_YES=0

usage() {
  cat <<'USAGE'
恢复本地媒体卷（Docker volume）。

用法：
  scripts/restore-media.sh --yes --volume admin-uploads <archive.tar.gz>
  scripts/restore-media.sh --yes --volume servicebridge-uploads <archive.tar.gz>

选项：
  -y, --yes                 确认清空目标卷（必须显式给出）
      --volume NAME         admin-uploads（默认）或 servicebridge-uploads
      --env-file PATH       生产环境文件，默认 deployments/ubuntu/.env.ubuntu
      --compose-file PATH   Compose 文件
  -h, --help                显示帮助

环境变量：
  ASTREVA_STACK_NAME        Compose 项目名，默认 astreva-ubuntu
  ASTREVA_RESTORE_HELPER_IMAGE  解包镜像，默认 alpine:3.20
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -y|--yes) ASSUME_YES=1; shift ;;
    --volume)
      [[ $# -ge 2 && -n "${2:-}" ]] || { echo "--volume 需要一个值" >&2; exit 2; }
      VOLUME_NAME="$2"; shift 2 ;;
    --env-file)
      [[ $# -ge 2 && -n "${2:-}" ]] || { echo "--env-file 需要一个路径" >&2; exit 2; }
      ENV_FILE="$2"; shift 2 ;;
    --compose-file)
      [[ $# -ge 2 && -n "${2:-}" ]] || { echo "--compose-file 需要一个路径" >&2; exit 2; }
      COMPOSE_FILE="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    --) shift; break ;;
    -*) echo "未知参数：$1" >&2; usage >&2; exit 2 ;;
    *)
      [[ -z "$ARCHIVE" ]] || { echo "只能指定一个归档文件" >&2; exit 2; }
      ARCHIVE="$1"; shift ;;
  esac
done

[[ "$ASSUME_YES" -eq 1 ]] || { echo "拒绝执行：必须显式加 --yes（会清空目标卷）。" >&2; exit 2; }
case "$VOLUME_NAME" in
  admin-uploads|servicebridge-uploads) ;;
  *) echo "不支持的卷：$VOLUME_NAME（只允许 admin-uploads 或 servicebridge-uploads）" >&2; exit 2 ;;
esac
[[ -n "$ARCHIVE" && -f "$ARCHIVE" ]] || { echo "归档文件不存在：${ARCHIVE:-<空>}" >&2; exit 2; }
[[ -f "$ARCHIVE.sha256" ]] || { echo "缺少校验文件：$ARCHIVE.sha256" >&2; exit 1; }
[[ -f "$ENV_FILE" ]] || { echo "未找到环境文件：$ENV_FILE" >&2; exit 1; }
[[ -f "$COMPOSE_FILE" ]] || { echo "未找到 Compose 文件：$COMPOSE_FILE" >&2; exit 1; }

if command -v sha256sum >/dev/null 2>&1; then
  HASH_CMD=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then
  HASH_CMD=(shasum -a 256)
else
  echo "缺少 sha256sum / shasum" >&2; exit 1
fi
(
  cd "$(dirname "$ARCHIVE")"
  "${HASH_CMD[@]}" -c "$(basename "$ARCHIVE").sha256"
)
gzip -t "$ARCHIVE"

VOLUME="${STACK_NAME}_${VOLUME_NAME}"
docker volume inspect "$VOLUME" >/dev/null 2>&1 || { echo "目标卷不存在：$VOLUME" >&2; exit 1; }

compose() { docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" -p "$STACK_NAME" "$@"; }
case "$VOLUME_NAME" in
  admin-uploads) SERVICES=(admin-server) ;;
  servicebridge-uploads) SERVICES=(servicebridge-backend-1 servicebridge-backend-2) ;;
esac
started=0
cleanup() {
  if [[ "$started" -eq 1 ]]; then
    compose start "${SERVICES[@]}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT
compose stop "${SERVICES[@]}" >/dev/null 2>&1 || true
started=1

HELPER_IMAGE="${ASTREVA_RESTORE_HELPER_IMAGE:-alpine:3.20}"
docker run --rm \
  -v "$VOLUME:/data" \
  -v "$(cd "$(dirname "$ARCHIVE")" && pwd):/backup:ro" \
  "$HELPER_IMAGE" \
  sh -c 'find /data -mindepth 1 -maxdepth 1 -exec rm -rf {} +; tar xzf "/backup/'"$(basename "$ARCHIVE")"'" -C /data'

echo "媒体卷已恢复：$VOLUME"
echo "来源：$ARCHIVE"
