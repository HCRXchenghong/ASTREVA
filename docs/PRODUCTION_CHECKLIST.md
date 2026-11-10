# 生产上线检查清单

> 版本：v1.4.5 · 更新时间：2026-11-10
> 适用于“今天/明天就要发版”的场景。任何一步失败都不要继续。配置类细节见 [ONE_CLICK_DEPLOY.md](ONE_CLICK_DEPLOY.md)，非配置遗留项见 [PRODUCTION_READINESS.md](PRODUCTION_READINESS.md)。

## 1. 依赖与代码质量

```bash
npm ci
npm run check
npm run verify        # = bash scripts/verify-all.sh
```

期望：

- `npm run check` 全绿；
- `verify-all.sh` 为 **11 PASS / 0 FAIL / 0 SKIP**；
- `npm run test:admin` 为 **111 tests / 106 pass / 0 fail / 5 skip**；
- 不需要手工补跑 `npm run format:check` 才能上线；若团队要求格式门禁，另行执行。

## 2. 构建

```bash
npm run build:frontend
```

期望：

- 中文内容：**27 page(s) built**；
- 配置并完成 `en`/`ar` 翻译后，生成对应语言页面（不是无条件 67 页；未就绪的译文返回 409 并按设计跳过）；
- `frontend/dist` 非空，`sitemap-index.xml`、`robots.txt`、`llms.txt` 存在。

## 3. 内容模型迁移

如果生产库是旧内容结构：

1. 先备份数据库（见第 6 节）；
2. 打开后台「内容管理 → 官网内容」；
3. 不修改内容，点一次「仅保存」；
4. 确认保存 200、revision 增加，刷新后字段完整。

如果生产库已经是当前结构，可跳过。

## 4. 安全配置自检

- `.env.ubuntu` 已设置强随机 `INTERNAL_API_TOKEN`（≥32 位）、`DATA_ENCRYPTION_KEY`、`ADMIN_2FA_SESSION_SECRET`；
- `APP_ENV=production` 已注入 admin-server；弱密钥/示例值会拒绝启动；
- 首次初始化使用启动日志中的 `ADMIN_SETUP_TOKEN`；初始化完成后确认 `GET /site-admin-api/setup/start` 返回 409；
- 后台写请求带 `X-CSRF-Token`，浏览器已领取 `astreva_admin_csrf` Cookie；
- 官网 CSP 无 `script-src 'unsafe-inline'`；Caddy 输出 `frame-ancestors` + XFO；
- 访客上传只允许图片/音频；APK 仅管理员 App 发布路径；
- `/metrics` 只在受保护的管理/内网入口，不暴露在公网 service 域名。

## 5. 发布前页面自检

### 中文

- `/`、`/products/`、`/solutions/`、`/support/`、`/support/faq/`、`/about/`、`/privacy/`、`/agreement/` 返回 200；
- `sitemap-index.xml`、`robots.txt`、`llms.txt` 可访问；
- 导航下拉、移动端菜单、Cookie 同意、表单提交、客服 iframe 均可用；
- 浏览器控制台无报错。

### 英文/阿拉伯语

- 仅在翻译已就绪后检查 `/en/`、`/ar/`；
- `/ar/` 的 `<html dir="rtl">`；
- 译文未就绪时接口 409、构建跳过是预期行为，不要用中文兜底。

## 6. 备份

生产默认 `ADMIN_MEDIA_DRIVER=local`，备份必须同时覆盖数据库和本地媒体卷。

```bash
# 项目名默认 astreva-ubuntu；安装器使用其他名字时显式传
ASTREVA_STACK_NAME=astreva-ubuntu \
ASTREVA_BACKUP_DIR=/srv/astreva-backups \
bash scripts/backup-production.sh
```

期望生成：

- `astreva-postgres-*.sql.gz` + `.sha256`；
- `astreva-admin-uploads-*.tar.gz` + `.sha256`；
- `astreva-servicebridge-uploads-*.tar.gz` + `.sha256`；
- 全部 `gzip -t` 通过、SHA-256 校验通过。

对象存储（R2/S3）模式还需要在供应商侧开启版本/生命周期；本脚本不会替代供应商备份。

## 7. 部署

### 全新/升级（推荐）

```bash
curl -fsSL https://raw.githubusercontent.com/HCRXchenghong/ASTREVA/main/scripts/install.sh \
  | sudo bash -s -- \
      --repo https://github.com/HCRXchenghong/ASTREVA.git \
      --domain www.example.com \
      --email ops@example.com
```

安装器会检查 DNS/端口、生成强密钥、保留旧数据库密钥、更新前备份、启动 Compose 并做 HTTPS 健康检查。

### 手工发布静态站

```bash
PUBLISH_DIR=/usr/share/nginx/site node scripts/build-frontend.mjs
```

或控制台「保存并发布」（走同一个 `buildFrontend()`）。

## 8. 上线后验收

- `https://SITE_DOMAIN/`、`https://ADMIN_DOMAIN/site-admin/`、`https://SERVICE_DOMAIN/readyz` 均可访问；
- 管理员 2FA 登录成功，普通客服登录成功；
- 访客创建会话、发消息、转人工、客服接管、结束、评价全链路通过；
- 后台上传一张新图，前台发布后可见；
- 检查 `docker compose ps` 全部 healthy，Caddy 证书有效期正常；
- 检查 `backups/` 最新备份时间与校验文件。

## 9. 回滚

- 代码：`git revert <commit>`，重新部署；
- 静态站：`releases/` 下保留最近版本，把 `current` symlink 指回上一版；
- 数据库：使用最近一次 `astreva-postgres-*.sql.gz` 恢复；本地媒体使用同时间戳归档 + `scripts/restore-media.sh` 恢复；
- 回滚后重新跑第 8 节验收。

## 10. 已知非阻塞项

- 英文/阿语依赖真实翻译 AI；
- R2/S3、FCM/APNs、真实 AI 需要生产凭证；
- 本地媒体恢复使用 `scripts/restore-media.sh`（会短暂停止写入服务），仍建议在隔离环境先演练；
- 移动 App 正式签名/商店发布、真实 VPS 安装、压测和监控告警接入需要发布环境执行。

完整清单见 [PRODUCTION_READINESS.md](PRODUCTION_READINESS.md)。
