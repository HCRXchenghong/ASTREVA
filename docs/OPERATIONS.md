# 生产运维：CMS 数据、媒体、备份与监控

## 存储切换

生产 Compose 固定 `CMS_STORE_DRIVER=postgres` 和 `ADMIN_MEDIA_DRIVER=s3`。旧 JSON 运行数据不再由后台自动读取。首次升级使用下方一次性切换命令，核验通过后会删除旧 JSON 与本地媒体；新实例仅使用镜像内只读种子初始化空数据库。

`DATA_ENCRYPTION_KEY` 会同时用于后台 TOTP 密钥的数据库加密。不得在上线后直接更换该值；如需轮换，先完成专门的数据重加密迁移。

后台媒体默认使用 S3 兼容对象存储。上线前必须设置：

- `ADMIN_MEDIA_DRIVER=s3`
- `S3_BUCKET`
- `S3_PUBLIC_BASE_URL`
- 对应的 S3 endpoint、region 与访问凭据（如供应商不使用实例角色）

这些环境变量只用于首次启动种子。后台首次启动会将 R2/S3 与 AI 生图配置迁入 PostgreSQL 的 `site_runtime_settings`；之后以“统一后台 → 系统设置”保存的配置为准。Access Key、Secret Access Key、Session Token 与 AI API Key 使用 `CMS_DATA_ENCRYPTION_KEY` 做 AES-256-GCM 加密，读取接口只返回是否已配置和末四位掩码。

R2 配置保存前会真实执行 `HeadBucket`。素材库已有记录时，后台禁止直接修改 Endpoint、Bucket、对象前缀或 Public URL，避免旧记录对应的对象被错误删除或替换；切换存储身份必须先执行对象迁移。凭据、Region、寻址方式和服务端加密策略可以在线更新并立即生效。

R2 素材库支持智能 WebP 上传、原文件上传、稳定 URL 替换、批量删除与预签名 PUT 直传。可用以下变量调整图片策略：

- `ADMIN_UPLOAD_MAX_BYTES`：后台上传和直传确认的大小上限。
- `ADMIN_IMAGE_VARIANT_WIDTHS`：逗号分隔的 WebP 变体宽度。
- `ADMIN_IMAGE_WEBP_QUALITY`：WebP 质量（40–100）。
- `ADMIN_IMAGE_MAX_PIXELS`：解码图片的像素数安全上限。

在浏览器使用 R2 预签名直传前，Bucket 必须允许后台域名的 `PUT` CORS，例如 Cloudflare R2：

```json
[
  {
    "AllowedOrigins": ["https://admin.example.com"],
    "AllowedMethods": ["PUT"],
    "AllowedHeaders": ["Content-Type"],
    "ExposeHeaders": ["ETag"],
    "MaxAgeSeconds": 3600
  }
]
```

预签名确认接口会通过 `HeadObject` 和对象前 128 KiB 内容复核真实存在性、大小、Content-Type 与文件魔数；校验失败的对象会被删除且不入库。

AI 生图初始密钥由服务端读取 `ADMIN_OPENAI_IMAGE_API_KEY`，迁入 PostgreSQL 后可在系统设置中更新。自定义兼容 API 时，还必须把主机名显式写入请求主机白名单；这个白名单用于阻止 SSRF。保存新密钥时会真实请求模型接口验证，明文密钥不会返回浏览器。未配置密钥时，媒体库其他功能正常使用，页面会明确标记 AI 功能不可用。

旧 JSON 与本地媒体使用以下命令一次性迁移并清理：

```bash
CMS_CUTOVER_PURGE=confirmed npm run cms:cutover
```

命令会导入内容、管理员、咨询单、降级版本、审计事件和发布任务，再比对内容哈希、活动版本 ID 与各表记录数；本地媒体逐个写入对象存储。仅全部核验通过时，才删除 `data/` 中的旧 JSON 和 `uploads/site-admin/` 中的旧媒体。损坏、无法解析的 JSON 会列入 `discardedUnmigratable` 并按本次切换策略删除。

本地开发首次运行使用：

```bash
cp .env.example .env
bash scripts/start-local-storage.sh
```

## 备份与恢复

在生产主机上设置一个非根目录的 `ASTREVA_BACKUP_DIR`，并用 cron 每日执行：

```bash
0 2 * * * ASTREVA_BACKUP_DIR=/srv/astreva-backups /path/to/ASTREVA-main/scripts/backup-production.sh
```

脚本会生成 PostgreSQL `pg_dump` 的 gzip 文件及 SHA-256 校验文件，默认保留 30 天；可用 `ASTREVA_BACKUP_RETENTION_DAYS` 修改。对象存储需在供应商侧启用 bucket versioning、跨区复制或生命周期归档，因为媒体不在 PostgreSQL 备份内。

恢复演练至少每季度一次：恢复到隔离 PostgreSQL，校验 `site_content`、`site_admin_users`、`site_fallback_versions`、`site_media` 与最近一次发布内容，再人工检查官网。

## 监控与审计

- 后台指标：`GET /metrics`，需 `Authorization: Bearer $METRICS_BEARER_TOKEN`。
- 后台 OpenAPI：`GET /openapi.json`。
- 审计日志：`GET /site-admin-api/audit-events`（需要管理员会话）。
- 发布队列：`GET /site-admin-api/publish-jobs`（需要管理员会话）。

告警至少覆盖：后台健康检查失败、静态构建失败、数据库不可用、备份失败、对象存储错误及磁盘空间不足。审计日志应接入集中日志系统并设置保存周期。
