# ServiceBridge Go Backend

## 能力

- 管理员、客服、访客鉴权和权限边界。
- 会话分配、AI/人工状态、消息幂等、历史、撤回、关闭、转接和评价。
- WebSocket 实时投递、来源校验、会话撤销和 Redis 跨节点广播。
- 开放 API（`/open/v1/*`）：第三方平台用 `ast_live_` API Key 接入，创建会话、收发消息并实时订阅 AI/人工回复；Key 哈希存储、按应用限速、会话按应用隔离，统一后台「API 管理」授权。
- 内存/PostgreSQL 仓库、OpenAI 兼容调用、本地/S3 上传。
- 限流、安全响应头、可信代理、审计、CSV、健康/就绪/指标。

## 运行

```bash
GOCACHE=/tmp/astreva-go-cache go test ./...
STORE_DRIVER=memory HTTP_ADDR=127.0.0.1:8080 go run ./cmd/server
```

设置 `DATABASE_URL` 或 `STORE_DRIVER=postgres` 使用 PostgreSQL；设置 `REDIS_ADDR` 启用多节点事件。生产还必须设置强随机的 `DATA_ENCRYPTION_KEY`、`ADMIN_BOOTSTRAP_PASSWORD`、`AGENT_BOOTSTRAP_PASSWORD`、`METRICS_BEARER_TOKEN`，并配置明确的 `CORS_ALLOWED_ORIGINS` 和 `TRUSTED_PROXY_CIDRS`。

开放 API 变量：`OPEN_API_ENABLED`（默认 true）、`OPEN_API_DEFAULT_RATE_LIMIT`（默认每应用每分钟 300 次）。第三方接入指南见仓库根 `docs/API_INTEGRATION.md`。

上传变量包括 `UPLOAD_DRIVER`、`UPLOAD_DIR`、`UPLOAD_PUBLIC_BASE_URL`、`UPLOAD_MAX_BYTES` 以及 `S3_*`。AI 变量包括 `OPENAI_API_KEY`、`OPENAI_BASE_URL`、`OPENAI_MODEL`、`OPENAI_API_TYPE`；后台保存的数据库配置优先于启动默认值。

AI 供应商熔断变量：`AI_CIRCUIT_COOLDOWN_MS`（普通故障冷却，默认 30000，最小 5000）与 `AI_CIRCUIT_AUTH_COOLDOWN_MS`（401/403 冷却，默认 300000，最小 5000）。同一能力可挂多个供应商（`ai_binding_targets`，按 priority 升序尝试），连续失败 3 次后熔断跳过，冷却结束放行一次探测；管理端接口为 `GET/PUT /api/admin/ai-binding-targets` 与 `GET /api/admin/ai-providers/health`。

知识库检索变量：`KNOWLEDGE_INDEX_TTL_SECONDS`（每个知识库的进程内倒排索引 TTL，默认 60 秒，<=0 表示禁用缓存）与 `KNOWLEDGE_INDEX_MAX_ENTRIES`（索引缓存条目上限，默认 50000，超出后按最近使用淘汰；单个库自身超过上限时不做缓存，该次检索回退全表 BM25，结果不变）。索引在知识库/条目增删改（含 `POST /api/admin/knowledge/import`）后自动失效。

服务间接口：`INTERNAL_API_TOKEN` 是 `/api/internal/*` 的共享令牌，未配置时接口直接返回 503。`POST /api/internal/visitor-activity`（Header `X-Internal-Token`，Body `{"visitor_id","path","occurred_at?"}`）供 admin-server 上报访客浏览活动：更新画像 `last_path`、`visits+1`、`last_seen_at`；画像关闭返回 200 + `{"ok":false,"reason":"disabled"}`，画像不存在返回 200 + `{"ok":false,"reason":"no_profile"}`（不凭空创建）。

策略告警：`GET /api/admin/policy-alerts`（仅管理员）返回 `takeover` / `sla_idle` / `sla_first_response` 三类告警，按紧急度排序（takeover > idle > first_response，同级按等待时长倒序），供控制台轮询。

完整接口见 `../docs/API与WebSocket协议.md`。客服使用 `../apps/agent-mobile/` Flutter App，管理员使用仓库根 `admin-server/` 统一后台；推送、自动化端到端测试和生产验收状态见根目录 `功能完善任务.md`。
