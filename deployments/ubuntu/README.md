# Ubuntu 生产部署

该目录部署官网、自研后台、重建服务、ServiceBridge 访客端、两实例 Go 后端、PostgreSQL、Redis 和 Caddy。

## 准备

1. 将 `SITE_DOMAIN`、`ADMIN_DOMAIN`、`SERVICE_DOMAIN` 的 DNS A/AAAA 记录指向服务器（Caddy 需要公网可达才能完成 ACME 校验）。
2. 放通 TCP `80`、`443`（以及 UDP `443` 启用 HTTP/3），并确保这两个端口没有被宿主机上的 nginx/apache 等占用；安装 Docker Engine 与 Compose。
3. 复制并填写配置：

```bash
cp deployments/ubuntu/.env.ubuntu.example deployments/ubuntu/.env.ubuntu
```

所有 `change-me`、`example.com` 必须替换。三个域名要与 `CORS_ALLOWED_ORIGINS`、`FRAME_ANCESTORS` 和 `UPLOAD_PUBLIC_BASE_URL` 一致。`MINIO_ROOT_USER`、`MINIO_ROOT_PASSWORD` 也是必填项（即使使用外部对象存储，避免空口令容器）。

后台素材使用对象存储：`S3_ENDPOINT` 必须同时被服务器和管理员浏览器访问（预签名直传由浏览器直接 PUT），生产推荐 Cloudflare R2 / OSS / S3 等外部存储；仓库内置的 MinIO 只监听宿主机回环，仅适合本机验证。

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

后台使用 `SERVICEBRIDGE_API_URL` 通过容器内网访问客服引擎，并以 `SERVICEBRIDGE_ADMIN_ACCOUNT` 和 `ADMIN_BOOTSTRAP_PASSWORD` 代理管理员操作；凭证不会发送到浏览器。`?embed=1` 会隐藏客服独立页外壳，供官网 iframe 使用。

移动 App 生产构建应把 `SERVICEBRIDGE_URL` 指向 `https://SERVICE_DOMAIN`。上线后还要完成推送、数据库备份、对象存储、日志采集、监控告警和容量验收，详见 `servicebridge/docs/商业上线验收清单.md`。
