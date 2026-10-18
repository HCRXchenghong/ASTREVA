# 生产运维：CMS 数据、媒体、备份与监控

## 存储切换

生产 Compose 固定 `CMS_STORE_DRIVER=postgres` 和 `ADMIN_MEDIA_DRIVER=s3`。旧 JSON 运行数据不再由后台自动读取。首次升级使用下方一次性切换命令，核验通过后会删除旧 JSON 与本地媒体；新实例仅使用镜像内只读种子初始化空数据库。

`DATA_ENCRYPTION_KEY` 会同时用于后台 TOTP 密钥的数据库加密。不得在上线后直接更换该值；如需轮换，先完成专门的数据重加密迁移。

后台媒体默认使用 S3 兼容对象存储。上线前必须设置：

- `ADMIN_MEDIA_DRIVER=s3`
- `S3_BUCKET`
- `S3_PUBLIC_BASE_URL`
- 对应的 S3 endpoint、region 与访问凭据（如供应商不使用实例角色）

这些环境变量只用于首次启动种子。后台首次启动会将 R2/S3 与 AI 生图配置迁入 PostgreSQL 的 `site_runtime_settings`；之后以“统一控制台 → 内容管理 → R2 素材库 → 系统设置”里保存的配置为准。Access Key、Secret Access Key、Session Token 与 AI API Key 使用 `CMS_DATA_ENCRYPTION_KEY` 做 AES-256-GCM 加密，读取接口只返回是否已配置和末四位掩码。

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

AI 生图初始密钥由服务端读取 `ADMIN_OPENAI_IMAGE_API_KEY`，迁入 PostgreSQL 后可在“R2 素材库 → 系统设置 → AI 生图”中更新。自定义兼容 API 时，还必须把主机名显式写入请求主机白名单；这个白名单用于阻止 SSRF。保存新密钥时会真实请求模型接口验证，明文密钥不会返回浏览器。未配置密钥时，媒体库其他功能正常使用，页面会明确标记 AI 功能不可用。

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

### 备份范围

| 对象 | 备份方式 | 是否在本仓库脚本内 |
| --- | --- | --- |
| PostgreSQL（内容、管理员、降级版本、素材记录、发布队列、审计） | `scripts/backup-production.sh` 每日全量 `pg_dump` + gzip + SHA-256 | 是 |
| 对象存储媒体文件（R2 / S3 / MinIO） | 供应商侧 bucket versioning、跨区复制、生命周期归档 | 否，必须在供应商侧开启 |
| `.env.ubuntu`（密钥、域名、口令） | 离线加密保管（密码管理器/密管系统），不进 Git | 否 |
| Caddy 证书与配置卷 | 由 Caddy 自动签发/续期，无需备份；自定义 Caddyfile 已入 Git | 否 |

### 每日备份

在生产主机上设置一个非根目录的 `ASTREVA_BACKUP_DIR`，并用 cron 每日执行：

```bash
0 2 * * * ASTREVA_BACKUP_DIR=/srv/astreva-backups /path/to/ASTREVA-main/scripts/backup-production.sh
```

脚本行为：

- 生成 PostgreSQL `pg_dump --format=plain` 的 gzip 文件与同名 `.sha256` 校验文件，默认保留 30 天（`ASTREVA_BACKUP_RETENTION_DAYS`）。
- 写完先 `gzip -t` 校验压缩完整性，再生成 `.sha256` 并校验摘要格式（64 位十六进制），避免把截断的校验文件当成有效备份。
- 校验命令自动选择 `sha256sum`（Linux）或 `shasum -a 256`（macOS 默认）；恢复时重新计算摘要逐字节比对，不依赖 `-c` 的退出码。
- 可选：设置 `ASTREVA_BACKUP_TEXTFILE=/var/lib/node_exporter/textfile/astreva_backup.prom` 会额外写出
  `astreva_backup_last_success_timestamp_seconds`，供监控的 `AstrevaBackupStale` 告警判断备份新鲜度。
- 备份失败（非 0 退出）时 cron 会发邮件；请确保 `MAILTO` 或外部告警通道可用。

对象存储需在供应商侧启用 bucket versioning、跨区复制或生命周期归档，因为媒体不在 PostgreSQL 备份内。

### RPO / RTO 目标模板

按业务实际容忍度填写并签字确认；下表是可直接使用的起点模板。

| 数据/能力 | 恢复手段 | RPO（可丢多久） | RTO（多久恢复） | 验收方式 |
| --- | --- | --- | --- | --- |
| PostgreSQL（全站内容与后台数据） | 每日全量备份 + 恢复演练 | ≤ 24 小时 | ≤ 2 小时 | `scripts/restore-production.sh` 通过 |
| 对象存储媒体 | bucket versioning + 生命周期归档 | ≈ 0（版本可回滚） | ≤ 1 小时 | 版本回滚 + 抽查对象可访问 |
| 运行时配置与密钥 | 离线加密副本 | 随每次变更 | ≤ 1 小时 | 用副本重建 `.env.ubuntu` 并启动 |
| 整体站点 | 上述组合 | ≤ 24 小时 | ≤ 4 小时 | 端到端演练（含官网与后台登录） |

### 恢复演练

**频率**：至少每季度一次；备份方案、PostgreSQL 大版本或对象存储供应商发生变更后必须立即补做一次。演练记录（时间、备份文件、校验值、结果、耗时）归档到运维记录。

**步骤**（演练在隔离库进行，默认 `customer_service_restore`，不会覆盖生产库）：

1. 选定备份文件，与 `.sha256` 放在同一目录，并确认真实大小非 0。
2. 执行非交互演练：

   ```bash
   scripts/restore-production.sh --yes /srv/astreva-backups/astreva-postgres-20260101T020000Z.sql.gz
   ```

   脚本会先校验 SHA-256 与 gzip 完整性，再删除并重建目标库，然后
   `gunzip -c <备份> | docker compose exec -T postgres psql` 导入。
3. 脚本自动验收以下各项，任一结构性失败则非零退出：

   - 4 张关键表存在：`site_content`、`site_admin_users`、`site_fallback_versions`、`site_media`；
   - `site_content` 恰好 1 行，`content` JSONB 含 `site` / `home`，`revision` 正常；
   - `site_admin_users` 非空（为空只告警，但意味着恢复后无法登录后台）；
   - `site_media` / `site_fallback_versions` 行数被记录（为 0 会告警）；
   - `site_fallback_state` 引用的降级版本在 `site_fallback_versions` 中存在（引用完整性）；
   - 打印最近一次发布记录（`site_publish_jobs` 最新一条的 id / status / 时间）。

4. 人工补充核验（脚本无法自动完成）：

   - 用恢复库启动一个**临时**后台实例，确认能登录并看到最新内容与最近一次发布结果；
   - 抽查若干 `site_media` 记录对应的对象存储 URL 仍可访问（备份不含对象存储本体）；
   - 恢复耗时对照 RTO 目标，超标则记录改进项。

5. 演练结束后清理隔离库：

   ```bash
   docker compose --env-file deployments/ubuntu/.env.ubuntu \
     -f deployments/ubuntu/docker-compose.ubuntu.yml \
     exec -T postgres sh -c 'dropdb -U "$POSTGRES_USER" --if-exists customer_service_restore'
   ```

**生产库保护**：`restore-production.sh` 默认拒绝把备份导回 `POSTGRES_DB`。只有在灾难恢复场景下才允许
`--force-production`，且必须手工输入生产库名二次确认（`--yes` 不会跳过这一步）。

## 监控与告警

### 启用方式

监控栈（Prometheus + Grafana）在 Compose 中以 profile `monitoring` 定义，**默认不启动**：

```bash
# 1. 确认 .env.ubuntu 已设置 METRICS_BEARER_TOKEN 与 GRAFANA_ADMIN_PASSWORD
# 2. 启动监控（不影响现有业务容器）
docker compose --env-file deployments/ubuntu/.env.ubuntu --profile monitoring up -d

# 查看状态 / 日志
docker compose --env-file deployments/ubuntu/.env.ubuntu --profile monitoring ps
docker compose --env-file deployments/ubuntu/.env.ubuntu --profile monitoring logs -f prometheus

# 停止监控（业务容器不受影响）
docker compose --env-file deployments/ubuntu/.env.ubuntu --profile monitoring down
```

- Prometheus：`http://127.0.0.1:9090`（`PROMETHEUS_HOST_PORT`，默认只绑回环）。
- Grafana：`http://127.0.0.1:3000`（`GRAFANA_HOST_PORT`，默认只绑回环）。数据源与《ASTREVA 运维总览》仪表盘自动加载。
- 需要从外部访问请用 SSH 隧道（`ssh -L 3000:127.0.0.1:3000 user@host`），**不要**把 9090/3000 暴露到公网。
- 数据持久化在 `prometheus-data` / `grafana-data` 卷；保留时间由 `PROMETHEUS_RETENTION` 控制（默认 `15d`）。
- Prometheus 的 Bearer token 由容器启动时从 `${METRICS_BEARER_TOKEN}` 写入容器内临时凭据文件，仓库不存任何真实 token。
- Grafana 默认管理员为 `GRAFANA_ADMIN_USER` / `GRAFANA_ADMIN_PASSWORD`；首次登录后请立即修改密码。

### 指标含义

| 指标 | 来源 | 含义 |
| --- | --- | --- |
| `up{job=...}` | Prometheus 自身 | 抓取是否成功。`0` 覆盖进程退出、网络隔离，以及 `/metrics` 返回 401/404/5xx。 |
| `customer_service_ws_connections{role="visitor"\|"agent"\|"admin"}` | Go 后端 | 当前各角色的 WebSocket 长连接数。 |
| `customer_service_go_goroutines` | Go 后端 | 当前 Go 协程数，用于发现泄漏或过载。 |
| `probe_success`（可选） | blackbox_exporter | 对外 HTTP(S) 探测是否成功，需要按 `prometheus.yml` 注释部署 blackbox。 |
| `node_filesystem_avail_bytes` / `node_filesystem_size_bytes`（可选） | node_exporter | 磁盘可用比例。 |
| `container_start_time_seconds`（可选） | cAdvisor | 容器启动时间变化即视为重启。 |
| `astreva_backup_last_success_timestamp_seconds`（可选） | node_exporter textfile | 最近一次 PostgreSQL 备份成功时间。 |

后台自身还暴露：

- 后台指标：`GET /metrics`，需 `Authorization: Bearer $ADMIN_METRICS_BEARER_TOKEN`（未配置时返回 404）。
- 后台 OpenAPI：`GET /openapi.json`。
- 审计日志：`GET /site-admin-api/audit-events`（需要管理员会话）。
- 发布队列：`GET /site-admin-api/publish-jobs`（需要管理员会话）。

### 告警阈值

规则文件 `deployments/ubuntu/monitoring/alerts.yml`，阈值是起点，请按本环境 2–4 周基线回调：

| 告警 | 表达式（摘要） | 阈值/时长 | 级别 |
| --- | --- | --- | --- |
| `AstrevaTargetDown` | `up == 0` | 1 分钟 | critical |
| `AstrevaMetricsScrapeMisconfigured` | `up{job=~"servicebridge-backend\|admin-server"} == 0` | 5 分钟 | warning |
| `AstrevaHttpEndpointUnavailable` | `probe_success == 0` | 2 分钟 | critical |
| `AstrevaScrapeTargetsMissing` | `absent(up{job=~"..."})` | 5 分钟 | warning |
| `AstrevaWebSocketConnectionsDropped` | 总量 < 近 15 分钟峰值的 50%，且峰值 ≥ 20 | 5 分钟 | critical |
| `AstrevaVisitorConnectionsZero` | 访客连接为 0，且 1 小时内峰值 ≥ 5 | 10 分钟 | warning |
| `AstrevaGoroutineCountElevated` | `customer_service_go_goroutines > 3000` | 10 分钟 | warning |
| `AstrevaGoroutineLeakSuspected` | 当前 > 近 1 小时均值 3 倍，且 > 1000 | 15 分钟 | critical |
| `AstrevaDiskSpaceLow` / `Critical` | 可用比例 < 15% / < 5% | 15 / 5 分钟 | warning / critical |
| `AstrevaContainerRestarted` | `changes(container_start_time_seconds[30m]) > 0` | 立即 | warning |
| `AstrevaTargetFlapping` | `changes(up[30m]) >= 3` | 立即 | warning |
| `AstrevaBackupStale` | 距上次成功备份 > 36 小时 | 1 小时 | critical |

`probe_success`、`node_*`、`container_start_time_seconds`、`astreva_backup_*` 相关的规则在未部署对应
exporter 时不会产生序列，因此不会误报；部署后即自动生效。告警接入（Alertmanager、邮件、IM）建议在
Prometheus 之外单独配置，并将 `docs/OPERATIONS.md` 作为 runbook 入口。

### 审计与容量

- 审计日志应接入集中日志系统并设置保存周期；发布队列与审计事件接口见上表。
- 客服长连接的容量结论必须来自 `servicebridge/scripts/loadtest/README.md` 的阶梯压测实测记录；
  有压测工具不代表已经达到容量。

## 真实 PostgreSQL 集成测试（可选，建议上线前执行一次）

Go 客服引擎的 Postgres 存储会**在启动时自动补齐新增列**（`ensureCoreTables` 里的 `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`），
不依赖 `migrations/001_init.sql`。为避免"只在内存存储跑过、上线才发现列缺失"这类问题，提供了一个真实库集成测试：

```bash
docker run --rm -d --name astreva-pg-test -e POSTGRES_PASSWORD=test \
  -e POSTGRES_DB=astreva_test -p 55432:5432 postgres:16-alpine
sleep 3
cd servicebridge/backend
TEST_DATABASE_URL='postgres://postgres:test@127.0.0.1:55432/astreva_test?sslmode=disable' \
  GOCACHE="${TMPDIR:-/tmp}/astreva-go-cache" go test ./internal/store/ -run TestPostgresAgentPreferences -count=1 -v
docker rm -f astreva-pg-test
```

覆盖：客服偏好 JSONB 读写与默认值、会话语言状态（detected_lang/reply_lang_override/language_stats）、
消息语种元数据、消息历史分页（`PagedMessages`）以及历史库自动升级路径。

## AI 上游与自动翻译链路（运维视角）

### 配置
- 控制台 **AI 管理 → 模型供应商**：可建多个上游（每组 URL + Key），保存后点「一键获取模型」把该上游可用模型拉下来，模型框变成下拉可任选；「测试连接」显示延迟与模型数。
- **AI 管理 → 能力绑定**：接待 AI / 翻译 / 语言检测 / AI SEO / AI 生图 各自选一个供应商 + 模型。
- 未绑定或供应商被禁用时，自动**回退到「系统设置」里的原有上游**，因此升级不会中断线上服务。
- 语言检测默认在本地完成（脚本 + 词表，21 语种 + 方言标记），不消耗上游额度；`detection` 绑定只落库备用。

### 密钥与安全
- API Key 一律 AES-256-GCM 加密入库，控制台只显示末四位掩码；日志中不落明文。
- 供应商可配「出站主机白名单」：非空时只允许名单内主机，用于防 SSRF；新建自定义中转地址时建议填写。
- 明文只在服务间接口 `/api/admin/ai-providers/{id}/secret` 返回（管理员会话，供受信后端读取），该调用可在审计日志中追溯。

### 翻译链路行为（排障要点）
- **访客消息**：原文立即落库并下发；若语种非中文，异步翻译成中文，客服端先看到原文 + `翻译中…`，译好原地更新为 `已翻译`；失败显示 `翻译失败（已显示原文）`。访客端始终只看到自己语言的原文。
- **客服消息**：访客端先看到「客服正在回复…」，译文就绪后投递；**翻译失败兜底发送原文**，绝不丢消息；客服端状态在同一处可见。
- 客服可对自己账号关闭入向/出向自动翻译、选择是否发送前确认译文、选择目标语言风格（标准语/含方言）；管理员可在「运营中心 → 客服账号 → 翻译偏好」查看与重置。
- 异步任务失败只影响该条消息的译文，不影响会话与消息投递。

### 常见问题
| 现象 | 排查 |
|---|---|
| 客服端一直显示「翻译中…」 | 上游不可达/超时（默认 8s 窗口）；看服务日志 `translate message failed` 与供应商「测试连接」 |
| 访客端长时间「客服正在回复…」 | 同上；失败会自动补发原文并关闭输入指示 |
| 消息历史分页为空（Postgres 部署） | 二期曾修复 `PagedMessages` 嵌套查询问题；升级后重启服务即可，必要时跑真实库集成测试（见上文） |
| 模型下拉为空 | 该上游不支持 `/models`（如部分 Anthropic 兼容端点）→ 手填模型名即可 |
