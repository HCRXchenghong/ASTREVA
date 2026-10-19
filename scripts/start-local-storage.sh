#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="$ROOT_DIR/.env"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "缺少 .env；请先复制并填写 .env.example。" >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

for key in DATABASE_URL CMS_STORE_DRIVER ADMIN_MEDIA_DRIVER S3_ENDPOINT S3_BUCKET S3_ACCESS_KEY_ID S3_SECRET_ACCESS_KEY S3_PUBLIC_BASE_URL; do
  if [[ -z "${!key:-}" ]]; then
    echo "缺少 $key" >&2
    exit 1
  fi
done
if [[ "$CMS_STORE_DRIVER" != "postgres" || "$ADMIN_MEDIA_DRIVER" != "s3" ]]; then
  echo "本地存储必须使用 PostgreSQL 和 S3 兼容对象存储。" >&2
  exit 1
fi

db_password="${DATABASE_URL#postgres://astreva_cms:}"
db_password="${db_password%%@*}"

if ! docker inspect astreva-cms-postgres >/dev/null 2>&1; then
  docker run -d --name astreva-cms-postgres --restart unless-stopped \
    -p 127.0.0.1:5433:5432 \
    -e POSTGRES_DB=astreva_cms \
    -e POSTGRES_USER=astreva_cms \
    -e POSTGRES_PASSWORD="$db_password" \
    -v astreva-cms-postgres-data:/var/lib/postgresql/data \
    postgres:16-alpine >/dev/null
else
  docker start astreva-cms-postgres >/dev/null
fi

if ! docker inspect astreva-cms-minio >/dev/null 2>&1; then
  docker run -d --name astreva-cms-minio --restart unless-stopped \
    -p 127.0.0.1:10006:9000 \
    -p 127.0.0.1:10007:9001 \
    -e MINIO_ROOT_USER="$S3_ACCESS_KEY_ID" \
    -e MINIO_ROOT_PASSWORD="$S3_SECRET_ACCESS_KEY" \
    -v astreva-cms-minio-data:/data \
    minio/minio:RELEASE.2024-01-11T07-46-16Z server /data --console-address :9001 >/dev/null
else
  # 旧容器可能没有映射控制台端口；检测到就重建（数据在命名卷里不会丢）。
  if ! docker port astreva-cms-minio 9001 >/dev/null 2>&1; then
    echo "重建 MinIO 容器以暴露 Web 控制台（127.0.0.1:10007）..."
    docker rm -f astreva-cms-minio >/dev/null
    docker run -d --name astreva-cms-minio --restart unless-stopped \
      -p 127.0.0.1:10006:9000 \
      -p 127.0.0.1:10007:9001 \
      -e MINIO_ROOT_USER="$S3_ACCESS_KEY_ID" \
      -e MINIO_ROOT_PASSWORD="$S3_SECRET_ACCESS_KEY" \
      -v astreva-cms-minio-data:/data \
      minio/minio:RELEASE.2024-01-11T07-46-16Z server /data --console-address :9001 >/dev/null
  else
    docker start astreva-cms-minio >/dev/null
  fi
fi

for _ in $(seq 1 30); do
  docker exec astreva-cms-postgres pg_isready -U astreva_cms -d astreva_cms >/dev/null 2>&1 && break
  sleep 1
done
docker exec astreva-cms-postgres pg_isready -U astreva_cms -d astreva_cms >/dev/null

# Go 客服引擎用自己的库：它的 ensureCoreTables 会建 conversations/messages/sessions 等表，
# 与后台 CMS 的表混在同一个库里容易重名冲突，因此单独建 astreva_engine（幂等）。
if ! docker exec astreva-cms-postgres psql -U astreva_cms -d postgres -tAc \
  "SELECT 1 FROM pg_database WHERE datname='astreva_engine'" | grep -q 1; then
  echo "创建客服引擎数据库 astreva_engine（AI 供应商/绑定等数据持久化到这里）..."
  docker exec astreva-cms-postgres psql -U astreva_cms -d postgres -c "CREATE DATABASE astreva_engine" >/dev/null
fi

for _ in $(seq 1 30); do
  curl -fsS "$S3_ENDPOINT/minio/health/live" >/dev/null && break
  sleep 1
done
curl -fsS "$S3_ENDPOINT/minio/health/live" >/dev/null

cd "$ROOT_DIR"
node --input-type=module <<'NODE'
import { CreateBucketCommand, PutBucketPolicyCommand, S3Client } from '@aws-sdk/client-s3';
const client = new S3Client({
  endpoint: process.env.S3_ENDPOINT,
  region: process.env.S3_REGION || 'us-east-1',
  forcePathStyle: true,
  credentials: { accessKeyId: process.env.S3_ACCESS_KEY_ID, secretAccessKey: process.env.S3_SECRET_ACCESS_KEY }
});
const bucket = process.env.S3_BUCKET;
try { await client.send(new CreateBucketCommand({ Bucket: bucket })); } catch (error) {
  if (!['BucketAlreadyOwnedByYou', 'BucketAlreadyExists'].includes(error.name)) throw error;
}
await client.send(new PutBucketPolicyCommand({
  Bucket: bucket,
  Policy: JSON.stringify({ Version: '2012-10-17', Statement: [{ Sid: 'PublicReadSiteMedia', Effect: 'Allow', Principal: '*', Action: ['s3:GetObject'], Resource: [`arn:aws:s3:::${bucket}/*`] }] })
}));
NODE

echo "本地 PostgreSQL 和对象存储已就绪。"
