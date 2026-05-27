# Backend

Go 后端服务入口。

当前已实现：

- `GET /healthz`
- `GET /readyz`
- 优雅退出
- 基础请求日志
- 管理、客服、访客 HTTP API
- 管理员 / 客服自助修改登录密码
- 改密、重置密码、禁用账号后的登录 token 撤销与在线 WebSocket 断开
- WebSocket Origin 白名单、访客会话绑定和访客关闭会话权限校验
- JSON 请求体上限、未知字段拒绝、强密码和配置字段边界校验
- 数据概览聚合和服务评价
- 图片上传、本地 `/uploads/` 静态访问、S3/MinIO 兼容对象存储
- `/metrics` Prometheus 文本指标
- 节点级限流、登录接口独立限流、安全响应头、管理操作审计
- 会话监控 CSV 导出、管理审计 CSV 导出
- 飞书直连客服：访客消息入飞书、飞书回复回到访客
- WebSocket 实时消息
- OpenAI 原生/兼容接口适配
- 内存仓库：本地演示和单元测试
- PostgreSQL 仓库：账号、会话、消息、配置、关键词、推送设备持久化
- Redis Pub/Sub：多后端实例之间广播 WebSocket 投递事件和登录态撤销事件
- bcrypt：PostgreSQL 新建/重置客服密码哈希
- `DATA_ENCRYPTION_KEY`：PostgreSQL AI Key 可选 AES-GCM 加密落库

本地运行：

```bash
cd backend
go run ./cmd/server
```

环境变量：

- `HTTP_ADDR`：监听地址，默认 `:8080`。
- `APP_ENV`：运行环境，默认 `local`。
- `LOG_LEVEL`：日志级别，默认 `info`。
- `OPENAI_API_KEY`：OpenAI 或兼容服务 Key。
- `OPENAI_BASE_URL`：OpenAI 或兼容服务地址，默认 `https://api.openai.com/v1`。
- `OPENAI_MODEL`：模型名称，默认 `gpt-4o-mini`。
- `OPENAI_API_TYPE`：`chat_completions` 或 `responses`，默认 `chat_completions`。
- `STORE_DRIVER`：`memory` 或 `postgres`；设置 `DATABASE_URL` 时会自动使用 PostgreSQL。
- `DATABASE_URL`：商业部署数据库连接串。
- `REDIS_ADDR`：商业部署 Redis 地址。
- `REDIS_CHANNEL`：跨节点事件频道，默认 `customer-service:ws-events`。
- `NODE_ID`：后端节点 ID，默认使用主机名和启动时间生成。
- `DATA_ENCRYPTION_KEY`：敏感配置加密密钥，生产必须配置。
- `ADMIN_BOOTSTRAP_PASSWORD`：首次初始化 `superadmin` 的密码，生产必须设置为非默认强密码。
- `AGENT_BOOTSTRAP_PASSWORD`：首次初始化默认客服账号的密码，生产必须设置为非默认强密码；飞书主链路使用虚拟客服 `agent_feishu`。
- `CORS_ALLOWED_ORIGINS`：允许跨域的前端域名，默认 `*` 仅用于开发。
- `TRUSTED_PROXY_CIDRS`：可信反代 / 负载均衡 CIDR，只有来自这些地址的 `X-Forwarded-For` / `X-Real-IP` 会被采信。
- `SECURITY_HEADERS`：是否输出基础安全响应头，默认 `true`。
- `RATE_LIMIT_ENABLED`：是否启用节点级限流，默认 `true`。
- `RATE_LIMIT_RPS` / `RATE_LIMIT_BURST`：单 IP 令牌桶限流参数，默认 `20 / 60`。
- `METRICS_BEARER_TOKEN`：`/metrics` 保护 token；为空时不校验。
- `UPLOAD_DRIVER`：`local` 或 `s3`，默认 `local`。
- `UPLOAD_DIR`：上传文件保存目录，默认 `uploads`。
- `UPLOAD_PUBLIC_BASE_URL`：上传文件公开域名，返回绝对 URL 时使用。
- `UPLOAD_MAX_BYTES`：单文件上传上限，默认 `10485760`。
- `S3_ENDPOINT`：S3 兼容服务地址；AWS S3 可留空。
- `S3_REGION`：S3 区域，默认 `us-east-1`。
- `S3_BUCKET`：对象存储桶名，`UPLOAD_DRIVER=s3` 时必填。
- `S3_ACCESS_KEY_ID` / `S3_SECRET_ACCESS_KEY` / `S3_SESSION_TOKEN`：对象存储凭证；也可使用运行环境默认凭证链。
- `S3_FORCE_PATH_STYLE`：MinIO 等服务通常设为 `true`。
- `S3_KEY_PREFIX`：对象 key 前缀，默认 `uploads`。
- `S3_PUBLIC_BASE_URL`：对象/CDN 公开访问域名。
- `FEISHU_*`：飞书直连客服首次启动兜底配置。生产建议通过 `PATCH /api/admin/integrations/feishu` 在后台保存到数据库。

测试：

```bash
go test ./...
```

Redis 集成测试：

```bash
REDIS_ADDR=localhost:16379 go test ./internal/realtime -run TestRedisBusIntegration -count=1
```

压测工具：

```bash
go run ./cmd/wsload -http http://localhost:8080 -ws ws://localhost:8080 -n 100 -duration 30s -interval 1s -messages-per-conn 5
go run ./cmd/wsload -message-type image -content /uploads/20260513/demo.png -n 10 -messages-per-conn 1
```
