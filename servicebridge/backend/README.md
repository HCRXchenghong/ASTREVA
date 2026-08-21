# ServiceBridge Go Backend

## 能力

- 管理员、客服、访客鉴权和权限边界。
- 会话分配、AI/人工状态、消息幂等、历史、撤回、关闭、转接和评价。
- WebSocket 实时投递、来源校验、会话撤销和 Redis 跨节点广播。
- 内存/PostgreSQL 仓库、OpenAI 兼容调用、本地/S3 上传。
- 限流、安全响应头、可信代理、审计、CSV、健康/就绪/指标。

## 运行

```bash
GOCACHE=/tmp/astreva-go-cache go test ./...
STORE_DRIVER=memory HTTP_ADDR=127.0.0.1:8080 go run ./cmd/server
```

设置 `DATABASE_URL` 或 `STORE_DRIVER=postgres` 使用 PostgreSQL；设置 `REDIS_ADDR` 启用多节点事件。生产还必须设置强随机的 `DATA_ENCRYPTION_KEY`、`ADMIN_BOOTSTRAP_PASSWORD`、`AGENT_BOOTSTRAP_PASSWORD`、`METRICS_BEARER_TOKEN`，并配置明确的 `CORS_ALLOWED_ORIGINS` 和 `TRUSTED_PROXY_CIDRS`。

上传变量包括 `UPLOAD_DRIVER`、`UPLOAD_DIR`、`UPLOAD_PUBLIC_BASE_URL`、`UPLOAD_MAX_BYTES` 以及 `S3_*`。AI 变量包括 `OPENAI_API_KEY`、`OPENAI_BASE_URL`、`OPENAI_MODEL`、`OPENAI_API_TYPE`；后台保存的数据库配置优先于启动默认值。

完整接口见 `../docs/API与WebSocket协议.md`。客服使用 `../apps/agent-mobile/` Flutter App，管理员使用仓库根 `admin-server/` 统一后台；推送、自动化端到端测试和生产验收状态见根目录 `功能完善任务.md`。
