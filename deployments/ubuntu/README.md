# Ubuntu 生产部署

该目录部署官网、自研后台、重建服务、ServiceBridge 访客端、两实例 Go 后端、PostgreSQL、Redis 和 Caddy。

## 准备

1. 将 `SITE_DOMAIN`、`ADMIN_DOMAIN`、`SERVICE_DOMAIN` 的 DNS A/AAAA 记录指向服务器。
2. 放通 TCP `80` 和 `443`，安装 Docker Engine 与 Compose。
3. 复制并填写配置：

```bash
cp deployments/ubuntu/.env.ubuntu.example deployments/ubuntu/.env.ubuntu
```

所有 `change-me`、`example.com` 必须替换。三个域名要与 `CORS_ALLOWED_ORIGINS`、`FRAME_ANCESTORS` 和 `UPLOAD_PUBLIC_BASE_URL` 一致。

## 操作

```bash
scripts/ubuntu-one-click.sh up
scripts/ubuntu-one-click.sh status
scripts/ubuntu-one-click.sh logs
scripts/ubuntu-one-click.sh down
```

脚本会校验关键变量，运行时生成 `deployments/ubuntu/Caddyfile`，构建镜像并等待四个公开入口通过检查。Caddy 自动签发和续期证书。

## 地址

- 官网：`https://SITE_DOMAIN/`
- 后台：`https://ADMIN_DOMAIN/site-admin/`
- 客服：`https://SERVICE_DOMAIN/?embed=1`
- 客服就绪：`https://SERVICE_DOMAIN/readyz`

后台使用 `SERVICEBRIDGE_API_URL` 通过容器内网访问客服引擎，并以 `SERVICEBRIDGE_ADMIN_ACCOUNT` 和 `ADMIN_BOOTSTRAP_PASSWORD` 代理管理员操作；凭证不会发送到浏览器。`?embed=1` 会隐藏客服独立页外壳，供官网 iframe 使用。

移动 App 生产构建应把 `SERVICEBRIDGE_URL` 指向 `https://SERVICE_DOMAIN`。上线后还要完成推送、数据库备份、对象存储、日志采集、监控告警和容量验收，详见 `servicebridge/docs/商业上线验收清单.md`。
