# 在线客服系统

轻量、高效、纯自研在线客服系统。当前方向已调整为：网站访客在 Web 聊天窗口咨询，人工客服直接在飞书里接待和回复，不再维护独立客服 App / 管理 App。

当前已确定：

- 用户端：Web，支持手机浏览器和电脑浏览器。
- 人工客服端：飞书群 / 飞书应用消息事件。
- 主通信：访客侧 WebSocket，客服侧飞书事件回调。
- 后端：Go。
- 状态与跨节点投递：Redis Pub/Sub。
- 数据库：PostgreSQL。
- 上传存储：本地/共享卷或 S3/MinIO 兼容对象存储。
- AI：OpenAI 原生 API 和 OpenAI 兼容协议适配。
- 并发目标：不考虑带宽瓶颈时，按 5000 WebSocket 并发连接容灾设计。

核心文档：

- `客服系统边界说明.md`
- `项目总计划.md`
- `docs/API与WebSocket协议.md`
- `docs/飞书直连客服.md`

项目目录：

- `backend/`：Go 后端。
- `apps/user-web/`：用户端 Web。
- `deployments/`：部署配置。
- `scripts/`：脚本。
- `docs/`：技术文档。

## 本地运行

后端：

```bash
cd backend
go run ./cmd/server
```

用户端 Web：

```bash
cd apps/user-web
python3 -m http.server 5173
```

访问：

- 用户端：`http://localhost:5173`
- 后端健康检查：`http://localhost:8080/healthz`
- 运维指标：`http://localhost:8080/metrics`

## 飞书直连

启用飞书前，需要在飞书开放平台创建自建应用，开启机器人能力和消息事件订阅，并把回调地址配置为：

```text
https://service.example.com/api/integrations/feishu/events
```

飞书配置保存在 ServiceBridge 后端数据库中，环境变量只做首次启动兜底。管理员登录后调用后台接口保存：

```http
PATCH /api/admin/integrations/feishu
```

访客发送消息后，后端会把消息推到默认飞书群。客服直接回复该飞书消息，或在任意消息里带上 `#conv_xxx`，后端会把飞书回复落库为客服消息并推回网站访客。

## 商业部署样例

生产镜像构建：

```bash
docker build -t customer-service-backend:latest ./backend
```

生产 Compose 样例：

```bash
cp deployments/.env.example deployments/.env
# 修改 deployments/.env 中的数据库密码、DATA_ENCRYPTION_KEY、
# ADMIN_BOOTSTRAP_PASSWORD、AGENT_BOOTSTRAP_PASSWORD、CORS 域名、可信代理网段、OpenAI Key 和飞书应用配置
scripts/ops/preflight-prod.sh deployments/.env
docker compose --env-file deployments/.env -f deployments/docker-compose.prod.example.yml up -d --build
```

局域网运行：

```bash
make lan-start
make lan-status
make lan-stop
```

工程验收快捷命令：

```bash
make test
make compose-check
make build
make preflight-prod

# 后端已启动后执行
HTTP_BASE=http://localhost:8080 WS_BASE=ws://localhost:8080 make smoke
```

商业交付总验收：

```bash
make commercial-acceptance
```

上线前按 `docs/商业上线验收清单.md` 逐项确认生产资源、配置底线、飞书回调和 5000 WebSocket 容量验收。
