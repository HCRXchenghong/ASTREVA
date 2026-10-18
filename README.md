# ASTREVA 独立站

本仓库包含 Astro 官网、统一管理后台、构建发布服务和 ServiceBridge 在线客服。后台统一使用 `1337` 端口和一次 2FA 登录，一级侧栏包含内容管理、网站翻译、R2 素材库、客服管理、工作台、系统设置和 API 管理（第三方平台接入客服引擎）；客服使用 Flutter “在线客服”App 接待访客。

当前发布标签：`v1.1.5`（2026-08-28）。本次版本增强网站翻译任务级容错（上游长窗口不可用自动重排、旧任务中断、pending 状态不报错），并重做官网语言切换器（导航栏内国旗下拉），完整变更见 [CHANGELOG.md](CHANGELOG.md)。

## 目录

- `frontend/`：Astro + React + Tailwind 官网，构建产物位于 `frontend/dist/`。
- `admin-server/`：统一后台、官网内容、媒体、咨询单、2FA 和发布管理。
- `servicebridge/backend/`：Go API、WebSocket、PostgreSQL/Redis、AI 和上传服务。
- `servicebridge/apps/user-web/`：官网嵌入的访客聊天端。
- `servicebridge/apps/agent-mobile/`：iOS/Android 客服 App。
- `deployments/ubuntu/`：官网、后台和客服系统的生产 Compose。
- `admin-server/seed/`：只读首次初始化种子；运行数据仅保存于 PostgreSQL 和对象存储。

## 本地运行

```bash
npm install
cp .env.example .env
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

前端构建从 `PUBLIC_ADMIN_URL/site-admin-api/public-content` 获取内容。后台“降级版本”可把当前已保存内容冻结为版本、启用指定版本或删除历史版本；构建前会校验当前启用版本。后台不可用时，已发布的静态官网持续对外提供服务，不依赖本地 JSON 快照。

官网仅把中文 `zh-CN` 作为源内容。后台“网站翻译”可配置 Chat Completions、Responses 或 Anthropic Messages 上游，API Key 使用 CMS 主密钥加密存入 PostgreSQL。中文内容每次保存后，英语 `en` 和阿拉伯语 `ar` 会按同一修订号进入翻译队列；过期、失败或未配置的译文不会对外伪装成成功。官网中的手动语言选择优先并保存在浏览器；首次访问才根据 Cloudflare/Vercel IP 国家头，其次根据 `Accept-Language` 推荐语言。阿拉伯语会同步启用 RTL。

生产 Compose 使用共享的只读静态发布卷：构建服务把新产物原子切换到该卷，Nginx 官网容器立即读取新版本；构建服务不再暴露到公网。

后台采用模块化单体：`auth-router`、`content-router`、`leads-router`、`media-router`、`publish-router`、`platform-store`、`media-store`、`publish-service`、`servicebridge-proxy` 与 OpenAPI 模块。所有环境均使用 PostgreSQL 保存后台数据、S3 兼容对象存储保存后台媒体；本地启动脚本会自动准备 Docker PostgreSQL 与 MinIO。运维、迁移、备份与监控见 [docs/OPERATIONS.md](docs/OPERATIONS.md)。

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
npm run test:admin
cd servicebridge/backend && go test ./... && go vet ./...
cd ../apps/agent-mobile && dart analyze && flutter test
```

商业上线前仍需完成推送真机验证、浏览器/真机端到端自动化、备份监控和容量验收，统一记录在 [功能完善任务.md](功能完善任务.md)。
