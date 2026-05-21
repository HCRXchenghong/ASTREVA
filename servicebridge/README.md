# 客服系统

轻量、高效、纯自研在线客服系统。

当前已确定：

- 用户端：Web，支持手机浏览器和电脑浏览器。
- 客服端：uni-app，支持 Android 和 iOS App。
- 管理端：uni-app，支持 Android 和 iOS App。
- 主通信：WebSocket。
- 后端：Go。
- 状态与跨节点投递：Redis Pub/Sub。
- 数据库：PostgreSQL。
- 上传存储：本地/共享卷或 S3/MinIO 兼容对象存储。
- AI：OpenAI 原生 API 和 OpenAI 兼容协议适配。
- 并发目标：不考虑带宽瓶颈时，按 5000 WebSocket 并发连接容灾设计。
- 商业管理能力：服务评价、管理审计、会话/审计 CSV 导出、客服 App 图片发送和推送提醒。

核心文档：

- `客服系统边界说明.md`
- `项目总计划.md`

UI 基准：

- `用户端前端UI示例.html`
- `客服端前端UI示例.html`
- `管理端前端UI示例.html`

项目目录：

- `backend/`：Go 后端。
- `apps/user-web/`：用户端 Web。
- `apps/agent-app/`：客服端 uni-app。
- `apps/admin-app/`：管理端 uni-app。
- `deployments/`：部署配置。
- `scripts/`：脚本。
- `docs/`：技术文档。
- `docs/生产部署与容量规划.md`：5000 并发、容灾和上线容量说明。

## 商业部署样例

生产镜像构建：

```bash
docker build -t customer-service-backend:latest ./backend
```

生产 Compose 样例：

```bash
cp deployments/.env.example deployments/.env
# 修改 deployments/.env 中的数据库密码、DATA_ENCRYPTION_KEY、
# ADMIN_BOOTSTRAP_PASSWORD、AGENT_BOOTSTRAP_PASSWORD、CORS 域名、可信代理网段和 OpenAI Key
scripts/ops/preflight-prod.sh deployments/.env
docker compose --env-file deployments/.env -f deployments/docker-compose.prod.example.yml up -d --build
```

## 当前本地运行

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

局域网运行：

```bash
make lan-start
make lan-status
make lan-stop
```

说明：

- `make lan-start` 会把后端绑定到 `0.0.0.0:8080`，把用户端 Web 绑定到 `0.0.0.0:5173`。
- `make lan-status` 会输出当前机器的局域网 IP 和可访问地址。
- 客服端 App 和管理端 App 登录页的“服务”输入框可直接填写局域网 API 地址，例如 `http://192.168.1.113:8080`。

小规模压测：

```bash
cd backend
go run ./cmd/wsload -n 30 -duration 5s -interval 1s -messages-per-conn 3
```

工程验收快捷命令：

```bash
make test
make compose-check
make build
# 生产 .env 准备好后执行
make preflight-prod

# 后端已启动后执行
HTTP_BASE=http://localhost:8080 WS_BASE=ws://localhost:8080 make smoke
```

商业交付总验收：

```bash
make commercial-acceptance
```

上线前按 `docs/商业上线验收清单.md` 逐项确认生产资源、配置底线和 5000 WebSocket 容量验收。

PostgreSQL + Redis 本地集成：

```bash
POSTGRES_PORT=15432 REDIS_PORT=16379 docker compose -f deployments/docker-compose.dev.yml up -d postgres redis

cd backend
STORE_DRIVER=postgres \
DATABASE_URL='postgres://customer_service:customer_service_dev@localhost:15432/customer_service?sslmode=disable' \
REDIS_ADDR='localhost:16379' \
DATA_ENCRYPTION_KEY='local-dev-secret' \
go run ./cmd/server
```
