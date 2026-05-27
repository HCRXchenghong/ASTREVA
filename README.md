# ASTREVA 独立站（Astreva Independent Site）

这是一个“前台独立站 + 自研官网后台 + 可选在线客服”组合仓库。

- `frontend/`：Astro + React + Tailwind 的静态官网（当前仓库内置中文内容示例可直接预览）。
- `admin-server/`：Node 纯后端，提供官网内容配置、媒体库、咨询单管理和重建触发。
- `servicebridge/`：独立客服体系（可选启用，默认本地也可单独联调）。
- `scripts/`：本地一键启动脚本与重建 webhook。

## 仓库结构

```text
.
├─ frontend/                  前端站点代码与构建配置
├─ admin-server/              自研管理后台服务（Node）
├─ servicebridge/             在线客服子系统（可选）
├─ scripts/                   本地/运维辅助脚本
├─ deployments/               部署模板
└─ data / uploads/            后台数据持久化（本地默认示例与演示数据）
```

## 本地快速开始（推荐）

### 1）安装依赖

```bash
npm install
```

### 2）准备环境变量

```bash
cp .env.example .env
```

然后按实际环境调整 `.env`。

### 3）启动独立站（开发模式）

建议用两个终端并行启动：

```bash
node admin-server/server.mjs
```

```bash
npm run dev:frontend
```

也可以使用 npm 脚本：

```bash
npm run dev:admin
npm run dev:frontend
```

如果你只想一次性拉起前端静态服务（基于 `frontend/dist`）、后台、重建服务和客服联调栈，使用：

```bash
bash scripts/run-local-stack.sh
```

### 4）检查服务是否可用

- 前台独立站：`http://127.0.0.1:4321/`
- 后台登录（2FA）：`http://127.0.0.1:1337/admin-2fa`
- 后台编辑页：`http://127.0.0.1:1337/site-admin/`
- 后台接口健康：`http://127.0.0.1:1337/healthz`
- 重建服务：`http://127.0.0.1:8787/healthz`
- ServiceBridge 用户端（若启用）：`http://127.0.0.1:5173/?embed=1`

第一次登录后台时会要求先创建管理员帐号并绑定 Microsoft Authenticator（2FA）。

## 管理后台和前端闭环说明

前端运行时会从 `PUBLIC_ADMIN_URL/site-admin-api/public-content` 拉取内容。

- 保存内容后先点击**保存当前模块**，确认无误后点击**发布官网**。
- 后台会请求 `FRONTEND_REBUILD_WEBHOOK_URL`，调用 `scripts/rebuild-webhook.mjs` 执行 `npm run build:frontend`。
- 构建完成后，前端会展示更新后的静态内容。

`PUBLIC_ADMIN_URL`、`PUBLIC_SITE_URL`、`PUBLIC_SERVICEBRIDGE_URL` 和 `/site-admin-api/` 相关参数都在运行时按环境变量注入。

## 生产构建与预览

```bash
npm run build:frontend   # 生成 frontend/dist
npm run preview:frontend # 预览生产构建
```

生产镜像使用 `frontend/Dockerfile` 与 `deployments/*` 目录中的 compose/样例。
`scripts/run-local-stack.sh` 依赖本地已有 `frontend/dist`；开发环境建议先执行一次 `npm run build:frontend`。

## 常用环境变量

### 前端

| 变量 | 说明 |
| --- | --- |
| `PUBLIC_SITE_URL` | 站点公开 URL（默认值如 `https://www.example.com`）；用于 sitemap、robots、站点元信息 |
| `PUBLIC_ADMIN_URL` | 管理后台根地址（如 `http://127.0.0.1:1337`）。前端内容接口与表单提交都基于此地址 |
| `PUBLIC_SERVICEBRIDGE_URL` | ServiceBridge 用户端地址，前端/后台聊天窗口会引用 |
| `CONTENT_API_TOKEN` | 可选：如果你给后台接口加了鉴权，可放在这里 |

### `admin-server`（后台服务）

| 变量 | 说明 |
| --- | --- |
| `PORT` | 管理后台监听端口，默认 `1337` |
| `HOST` | 管理后台监听主机，默认 `127.0.0.1` |
| `PUBLIC_ADMIN_URL` | 后台对外公开域名（例如生产环境） |
| `PUBLIC_SITE_URL` | 前台站点域名 |
| `PUBLIC_SERVICEBRIDGE_URL` | 聊天入口域名 |
| `FRONTEND_REBUILD_WEBHOOK_URL` | 重建 webhook 地址（默认 `http://127.0.0.1:8787/rebuild`） |
| `REBUILD_WEBHOOK_SECRET` | 重建 webhook 鉴权密钥；前端后台发布时会带 `Authorization: Bearer <secret>` |
| `ADMIN_2FA_SESSION_SECRET` | 2FA 会话签名密钥 |
| `ADMIN_2FA_ISSUER` | 2FA 二维码显示名称（默认 `星渡官网账号`） |
| `ADMIN_2FA_SESSION_TTL_MS` | 2FA 会话有效时长（毫秒） |
| `SERVICEBRIDGE_CORS_ALLOWED_ORIGINS` | 供前端脚本启动时透传给客服相关服务 |

### 其他启动变量

- `REBUILD_WEBHOOK_SECRET` 与 `REBUILD_WEBHOOK_PORT`：用于 `scripts/rebuild-webhook.mjs`。
- 若联调 ServiceBridge：见 `servicebridge` 下的 README 与 `servicebridge/deployments/.env.example`。

## 本地数据存储说明

- `data/site-content.json`：网站内容快照（可编辑）
- `data/leads.json`：客户咨询记录
- `data/admin-users.json`：管理员账户与 2FA 信息
- `uploads/site-admin/`：后台上传资源（图片/视频/PDF/压缩包/CAD）

## 健康检查入口

- `GET /healthz`：admin-server 健康检查
- `GET /site-admin-api/public-content`：前端内容接口（未配置时自动回退到 seed 数据）
- `GET /site-admin-api/status`：重建状态面板
- `POST /site-admin-api/publish`：触发前端构建
- `POST /site-admin-api/leads`：前端提交咨询单

可直接参考更完整的闭环文档： [docs/CMS_CLOSED_LOOP.md](docs/CMS_CLOSED_LOOP.md)

## 常见问题

- **为什么前台会出现示例内容？**
  - 通常是 `PUBLIC_ADMIN_URL` 未配置或后端未启动。前端会自动降级到内置示例内容避免白屏。

- **为什么发布后不生效？**
  - 确认 `scripts/rebuild-webhook.mjs` 是否在 8787 端口启动。
  - 确认后台点击“发布官网”后，`/site-admin-api/status` 显示构建结果。

- **我只想跑独立站前台+后台，不要 servicebridge？**
  - 可以只启动 `admin-server` 与 `frontend`，跳过 `scripts/run-local-stack.sh`（它会同时起客服相关服务）。
