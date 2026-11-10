# Ubuntu 生产部署

> 版本：v1.4.5 · 更新时间：2026-11-10

该目录部署官网、自研后台、重建服务、ServiceBridge 访客端、两实例 Go 后端、PostgreSQL、Redis 和 Caddy。

## 准备

1. 将 `SITE_DOMAIN`、`ADMIN_DOMAIN`、`SERVICE_DOMAIN` 的 DNS A/AAAA 记录指向服务器（Caddy 需要公网可达才能完成 ACME 校验）。
2. 放通 TCP `80`、`443`（以及 UDP `443` 启用 HTTP/3），并确保这两个端口没有被宿主机上的 nginx/apache 等占用；安装 Docker Engine 与 Compose。
3. 复制并填写配置：

```bash
cp deployments/ubuntu/.env.ubuntu.example deployments/ubuntu/.env.ubuntu
```

所有 `change-me`、`example.com` 必须替换。三个域名要与 `CORS_ALLOWED_ORIGINS`、`FRAME_ANCESTORS` 和 `UPLOAD_PUBLIC_BASE_URL` 一致。生产 Compose 不再内置 MinIO；使用本地卷或外部 R2/S3。

`INTERNAL_API_TOKEN` 必须是 32 位以上随机值（admin-server 与 Go 引擎共用，用于 AI 供应商明文配置和访客画像转发）。`ADMIN_SETUP_TOKEN` 生产必须显式设置（`scripts/ubuntu-one-click.sh` 会自动生成并在终端打印一次）；首次进入控制台必须填写。不要把它写进容器日志或公开渠道。生产 compose 已为 admin-server 设置 `APP_ENV=production`，弱密钥/示例值会拒绝启动。草稿预览的短时 Cookie 默认写入 `ADMIN_DOMAIN`/`SITE_DOMAIN` 的公共父域；如两个域名不在同一注册域，需用 `PREVIEW_COOKIE_DOMAIN` 指定可共享的父域，否则预览会返回 401（这是有意的安全限制）。

后台素材默认使用本地 Docker 卷（`ADMIN_MEDIA_DRIVER=local`）；如切到外部 R2/OSS/S3，`S3_ENDPOINT` 必须同时被服务器和管理员浏览器访问（预签名直传由浏览器直接 PUT）。生产 Compose 不再内置 MinIO。

## 操作

```bash
scripts/ubuntu-one-click.sh up
scripts/ubuntu-one-click.sh status
scripts/ubuntu-one-click.sh logs
scripts/ubuntu-one-click.sh down
```

脚本会校验关键变量与 80/443 端口占用，构建镜像并等待三个域名的 HTTPS 入口通过检查。`deployments/ubuntu/Caddyfile` 使用环境变量（`{$SITE_DOMAIN}` 等）生成站点，由 Caddy 直接终止 TLS 并自动签发、续期证书；仓库不再在运行时改写该文件，避免配置漂移。

## 地址

- 官网：`https://SITE_DOMAIN/`
- 后台：`https://ADMIN_DOMAIN/site-admin/`
- 客服：`https://SERVICE_DOMAIN/?embed=1`
- 客服就绪：`https://SERVICE_DOMAIN/readyz`

首次部署仍需在 `.env.ubuntu` 填写 `SITE_DOMAIN`，供 Caddy 配置 HTTPS 入口。部署后可以在后台「AI SEO → 官网正式域名」修改 SEO 使用的域名；设置保存在数据库并自动重建三语页面的 canonical、hreflang、robots.txt 和 sitemap。更换实际访问域名时，还需同步更新 DNS、Caddy 的 `SITE_DOMAIN` 与证书入口；构建会拒绝 `example.com` 示例域名。

后台使用 `SERVICEBRIDGE_API_URL` 通过容器内网访问客服引擎，并以 `SERVICEBRIDGE_ADMIN_ACCOUNT` 和 `ADMIN_BOOTSTRAP_PASSWORD` 代理管理员操作；凭证不会发送到浏览器。`?embed=1` 会隐藏客服独立页外壳，供官网 iframe 使用。

移动 App 生产构建应把 `SERVICEBRIDGE_URL` 指向 `https://SERVICE_DOMAIN`。上线后还要完成推送、数据库备份、对象存储、日志采集、监控告警和容量验收，详见 `servicebridge/docs/商业上线验收清单.md`。

## 监控（可选）

监控栈（Prometheus + Grafana）以 Compose profile `monitoring` 提供，默认不启动，端口仅绑定宿主机回环：

```bash
docker compose --env-file deployments/ubuntu/.env.ubuntu --profile monitoring up -d
```

启用前请在 `.env.ubuntu` 中设置 `GRAFANA_ADMIN_PASSWORD`（以及已有的 `METRICS_BEARER_TOKEN`）。指标含义、告警阈值、RPO/RTO 与恢复演练见 `docs/OPERATIONS.md`。备份与隔离恢复演练使用 `scripts/backup-production.sh`、`scripts/restore-production.sh` 与 `scripts/restore-media.sh`。备份脚本会同时归档 PostgreSQL、`admin-uploads` 与 `servicebridge-uploads` 本地卷；Compose 项目名不是默认 `astreva-ubuntu` 时设置 `ASTREVA_STACK_NAME`。

## 一键部署

全新安装或升级可执行仓库根目录的 `scripts/install.sh`，它会检查 Docker/Compose、测速选择下载渠道、询问域名、自动签发 SSL、备份数据库与本地媒体卷并做最终健康检查。详见 [一键部署文档](../../docs/ONE_CLICK_DEPLOY.md)。
