# Ubuntu 一键部署说明

目录 `/deployments/ubuntu` 提供了生产向的 Docker Compose 一键配置。

## 一、准备

1. 把域名 A 记录指向 Ubuntu 服务器公网 IP（至少 `SITE_DOMAIN`、`ADMIN_DOMAIN`、`SERVICE_DOMAIN`）：
   - 建议先做 `site/admin/service` 三个子域名指向同一服务器。
2. 确认服务器安全组/防火墙放通 `80`、`443`。
3. 编辑环境变量文件：
   - `cp deployments/ubuntu/.env.ubuntu.example deployments/ubuntu/.env.ubuntu`
   - 把密码、密钥替换为真实值，不能保留 `change-me`、`example.com`。

## 二、启动（Ubuntu）

```bash
scripts/ubuntu-one-click.sh up
```

脚本会：

- 校验环境变量和关键口令长度。
- 自动生成 `deployments/ubuntu/Caddyfile`。
- 创建并启动 Caddy、主站、管理后台、servicebridge 后端、客服前端、Redis/PostgreSQL、Webhook 容器。
- Caddy 会自动签发 Let’s Encrypt 证书，并自动续签。

## 三、常用命令

- 停止：`scripts/ubuntu-one-click.sh down`
- 查看状态：`scripts/ubuntu-one-click.sh status`
- 查看日志：`scripts/ubuntu-one-click.sh logs`（或 `scripts/ubuntu-one-click.sh logs admin-server`）

## 四、启动地址

- 主站：`https://SITE_DOMAIN`
- 后台登录：`https://ADMIN_DOMAIN/admin-2fa`
- 后台管理：`https://ADMIN_DOMAIN/site-admin/`
- 客服入口：`https://SERVICE_DOMAIN/?embed=1`
- 客服 API 健康检查：`https://SERVICE_DOMAIN/readyz`

## 五、为什么客服用 `/?embed=1`

`servicebridge` 客服前端会在 `?embed=1` 下进入“嵌入模式”，去掉默认外壳边框，方便以 iframe 的方式嵌进主站对话窗口，避免主站点击体验出现双重边框。
