# ASTREVA 独立站

本仓库包含 Astro 官网、Arco Design Pro Vue 统一控制台、构建发布服务和 ServiceBridge 在线客服。

统一控制台使用 `1337` 端口和一次 2FA 登录，侧栏包含工作台、客服管理（账号 / 服务评价）、运营（咨询单 / 关键词 / 审计日志）、应用管理（App 版本 / API 管理）、内容管理（官网内容 / 网站翻译 / R2 素材库）和系统设置（营业时间与联系方式 / AI / 推送）。客服使用 Flutter「在线客服」App 接待访客。

## 总清单

- [二期功能总清单：已实现 / 需你侧配置 / 未做与已知限制](docs/FEATURE_STATUS.md)

## 安全与合规文档

- [安全设计说明书](docs/security/安全设计说明书.md)
- [等保三级自评表](docs/security/等保三级自评表.md)
- [基线加固清单](docs/security/基线加固清单.md)
- [日志留存与审计规范](docs/security/日志留存与审计规范.md)
- [自动翻译与 AI 客户画像实施规格](docs/AI_TRANSLATION_AND_PROFILE.md)

## 目录

- `frontend/`：Astro + React + Tailwind 官网，构建产物位于 `frontend/dist/`；中文在根路径，英语/阿拉伯语在 `/en/`、`/ar/`。
- `admin-web/`：基于 Arco Design Pro Vue（MIT）的统一控制台 SPA，构建产物 `admin-web/dist/`，由 `admin-server` 同源托管在 `/site-admin/`。
- `admin-server/`：控制台 API、官网内容、媒体、咨询单、2FA、推送配置与发布管理。
- `servicebridge/backend/`：Go API、WebSocket、PostgreSQL/Redis、AI、推送（FCM/APNs）和上传服务。
- `servicebridge/apps/user-web/`：官网嵌入的访客聊天端。
- `servicebridge/apps/agent-mobile/`：iOS/Android 客服 App。
- `deployments/ubuntu/`：官网、控制台和客服系统的生产 Compose（Caddy 直接终止 TLS）。
- `admin-server/seed/`：只读首次初始化种子；运行数据仅保存于 PostgreSQL 和对象存储。

## 本地运行

```bash
npm install                 # 依赖装在项目内缓存，避免污染系统 npm 缓存
cp .env.example .env
npm run build:admin-web     # 首次需要；run-local-stack 也会在缺失时自动构建
bash scripts/run-local-stack.sh
```

启动后访问：

- 官网：`http://127.0.0.1:4321/`
- 统一控制台：`http://127.0.0.1:1337/site-admin/`
- 控制台登录：`http://127.0.0.1:1337/site-admin/login`（旧地址 `/admin-2fa` 会 302 到这里）
- 客服访客端：`http://127.0.0.1:5173/?embed=1`
- 客服 API：`http://127.0.0.1:8080/healthz`
- 构建服务：`http://127.0.0.1:8787/healthz`

开发控制台时可以用热更新：`npm run dev:admin-web`（Vite 起在 `5174`，把 `/site-admin-api` 与 `/site-admin-auth` 代理到 `1337`）。

首次进入控制台时，由站点管理员创建自己的邮箱、密码并绑定 2FA。客服账号在「客服管理 → 客服账号」中新建，随后用于登录「在线客服」App。本地脚本默认使用内存客服仓库，并让客服 API 监听 `0.0.0.0:8080` 供同一局域网的真机访问；重启后本地客服账号和会话会清空，生产 Compose 使用 PostgreSQL。

## 内容、翻译与客服

前端构建从 `PUBLIC_ADMIN_URL/site-admin-api/public-content` 获取内容；`?locale=en|ar` 取对应译文，译文未就绪时接口返回 409，构建会跳过该语言而不是失败。

官网以中文 `zh-CN` 为唯一源内容。控制台「网站翻译」可配置 Chat Completions / Responses / Anthropic Messages 上游，API Key 使用主密钥加密存入 PostgreSQL。中文内容每次保存后，英语和阿拉伯语按同一修订号进入翻译队列；过时、失败或未配置的译文不会伪装成成功。

控制台「内容管理 → 官网内容」里可以保存草稿并发布，也可以点击「预览草稿」把当前未保存内容构建到官网的 `/__preview/` 路径下核对，不影响线上。

统一后台通过服务端代理访问客服引擎，浏览器不会保存客服引擎管理员密码。相关变量：

- `SERVICEBRIDGE_API_URL`：客服 Go API 内网地址。
- `SERVICEBRIDGE_ADMIN_ACCOUNT`、`SERVICEBRIDGE_ADMIN_PASSWORD`：控制台代理使用的客服引擎管理员凭证。
- `PUBLIC_SERVICEBRIDGE_URL`：官网访客聊天公开地址。
- `ADMIN_2FA_SESSION_SECRET`：控制台登录会话签名密钥。
- `SERVICEBRIDGE_CORS_ALLOWED_ORIGINS`：客服 API/WebSocket 允许来源。

生产环境必须使用强随机密码和密钥，不能沿用 `.env.example` 的本地示例值。内容闭环见 [docs/CMS_CLOSED_LOOP.md](docs/CMS_CLOSED_LOOP.md)，客服与 App 说明见 [servicebridge/README.md](servicebridge/README.md)。

## 推送配置（后台可配）

推送不再依赖构建期参数或环境变量：登录控制台 → **系统设置 → 推送设置**，填入 Firebase 客户端参数（`api_key` / `app_id` / `messaging_sender_id` / `project_id` / `storage_bucket`）与 Firebase 服务账号 JSON（用于自动换取 OAuth2 令牌），即可让 Android 生效；iOS 还需填入 APNs `.p8`、Key ID、Team ID 与 Bundle ID，并在 Xcode 打开 Push Notifications capability。

App 启动时调用 `GET /api/app-config` 获取公开的 Firebase 参数；控制台提供「验证凭证」按钮（FCM 实际换取令牌、APNs 实际签名）。环境变量 `FCM_ACCESS_TOKEN` / `FCM_ENDPOINT` 仅作为数据库无配置时的兜底。

## 验证

```bash
bash scripts/verify-all.sh        # 一键跑完下面所有检查
npm run build:admin-web
node --check admin-server/server.mjs
npm run test:admin
cd servicebridge/backend && go test ./... && go vet ./...
cd ../apps/agent-mobile && dart analyze && flutter test
```

商业上线前仍需完成真机推送验证、正式签名发布、浏览器/真机端到端自动化、备份恢复演练和容量验收；统一记录在 [功能完善任务.md](功能完善任务.md)。
