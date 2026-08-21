# ASTREVA 独立站

本仓库包含 Astro 官网、统一管理后台、构建发布服务和 ServiceBridge 在线客服。后台统一使用 `1337` 端口和一次 2FA 登录，左侧分为内容管理、客服管理、工作台、系统设置；客服使用 Flutter “在线客服”App 接待访客。

当前发布标签：`v1.1.3`（2026-08-21）。本次版本把本地客服闭环、统一后台、App 更新和推送准备整理为正式发布，完整变更见 [CHANGELOG.md](CHANGELOG.md)。

## 目录

- `frontend/`：Astro + React + Tailwind 官网，构建产物位于 `frontend/dist/`。
- `admin-server/`：统一后台、官网内容、媒体、咨询单、2FA 和发布管理。
- `servicebridge/backend/`：Go API、WebSocket、PostgreSQL/Redis、AI 和上传服务。
- `servicebridge/apps/user-web/`：官网嵌入的访客聊天端。
- `servicebridge/apps/agent-mobile/`：iOS/Android 客服 App。
- `deployments/ubuntu/`：官网、后台和客服系统的生产 Compose。
- `data/`、`uploads/`：本地内容与上传文件，敏感运行数据不提交 Git。

## 本地运行

```bash
npm install
cp .env.example .env
npm run build:frontend
bash scripts/run-local-stack.sh
```

启动后访问：

- 官网：`http://127.0.0.1:4321/`
- 统一后台：`http://127.0.0.1:1337/site-admin/`
- 后台首次注册/登录：`http://127.0.0.1:1337/admin-2fa`
- 客服访客端：`http://127.0.0.1:5173/?embed=1`
- 客服 API：`http://127.0.0.1:8080/healthz`
- 构建服务：`http://127.0.0.1:8787/healthz`

首次进入后台时，由站点管理员创建自己的邮箱、密码并绑定 2FA。客服账号在“客服管理”中新建，随后用于登录“在线客服”App。`scripts/run-local-stack.sh` 默认使用内存客服仓库，并让客服 API 监听 `0.0.0.0:8080` 供同一局域网的真机访问。重启后本地客服账号和会话会清空；生产 Compose 使用 PostgreSQL。

## 内容与客服

前端构建从 `PUBLIC_ADMIN_URL/site-admin-api/public-content` 获取内容；接口不可用时使用 `frontend/src/data/fallback.ts`。后台保存只更新内容，点击“发布官网”才调用重建 webhook。

统一后台通过服务端代理访问客服引擎，浏览器不会保存客服引擎管理员密码。相关变量：

- `SERVICEBRIDGE_API_URL`：客服 Go API 内网地址。
- `SERVICEBRIDGE_ADMIN_ACCOUNT`、`SERVICEBRIDGE_ADMIN_PASSWORD`：统一后台代理使用的客服引擎管理员凭证。
- `PUBLIC_SERVICEBRIDGE_URL`：官网访客聊天公开地址。
- `ADMIN_2FA_SESSION_SECRET`：统一后台登录会话签名密钥。
- `SERVICEBRIDGE_CORS_ALLOWED_ORIGINS`：客服 API/WebSocket 允许来源。

生产环境必须使用强随机密码和密钥，不能沿用 `.env.example` 的本地示例值。内容闭环见 [docs/CMS_CLOSED_LOOP.md](docs/CMS_CLOSED_LOOP.md)，客服与 App 说明见 [servicebridge/README.md](servicebridge/README.md)。

## App 更新与推送

客服 App 通过自建通道更新：统一后台左侧“App 版本”上传 APK 并配置强制/可选更新；App 启动时调用客服引擎 `GET /api/app-update/latest` 检测，Android 下载后自动拉起系统安装。签名、发布与 FCM 推送接入步骤见 [docs/APP_RELEASE.md](docs/APP_RELEASE.md)。

## 验证

```bash
npm run build:frontend
node --check admin-server/server.mjs
cd servicebridge/backend && go test ./... && go vet ./...
cd ../apps/agent-mobile && dart analyze && flutter test
```

商业上线前仍需完成推送真机验证、浏览器/真机端到端自动化、备份监控和容量验收，统一记录在 [功能完善任务.md](功能完善任务.md)。
