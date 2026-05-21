# API 与 WebSocket 协议

本文档记录当前后端已经落地的接口和实时事件。

默认后端地址：

- HTTP：`http://localhost:8080`
- WebSocket：`ws://localhost:8080/ws`

HTTP API 只接受 `Authorization: Bearer <token>` 认证头；为避免 token 进入日志或浏览器历史，普通 HTTP 接口不接受 `?token=`。WebSocket 握手仍通过 query token 传入。

JSON 请求体上限为 1MB，并按接口字段严格校验；多余字段、非法枚举、超长文本、非法时间格式会返回 `invalid_input` 或 `invalid_json`。

## 1. 登录

### 管理账号登录

`POST /api/admin/login`

```json
{
  "account": "superadmin",
  "password": "123456"
}
```

返回：

```json
{
  "token": "a_xxx",
  "admin": {
    "id": "admin_super",
    "account": "superadmin",
    "name": "超级管理员"
  }
}
```

### 客服账号登录

`POST /api/agent/login`

```json
{
  "account": "admin",
  "password": "123456"
}
```

返回：

```json
{
  "token": "a_xxx",
  "agent": {
    "id": "agent_lixue",
    "account": "admin",
    "name": "客服-李雪",
    "status": "offline"
  }
}
```

## 2. 访客会话

### 创建访客会话

`POST /api/visitor/conversations`

```json
{
  "source": "web_pc"
}
```

返回：

```json
{
  "token": "v_xxx",
  "visitor": {
    "id": "vis_xxx",
    "ip": "127.0.0.1",
    "remark": "127.0.0.1"
  },
  "conversation": {
    "id": "conv_xxx",
    "status": "ai_serving",
    "visitor_remark": "127.0.0.1"
  },
  "initial_messages": []
}
```

## 3. 客服接口

请求头：

```text
Authorization: Bearer <agent_token>
```

### 修改在线状态

`POST /api/agent/status`

```json
{
  "status": "online"
}
```

`status` 可选：

- `online`
- `busy`
- `offline`

### 注册 App 推送设备

`POST /api/agent/push-device`

```json
{
  "platform": "ios",
  "provider": "uni-push",
  "token": "client-push-token"
}
```

该接口用于 APNs、Android 厂商推送或 uni-push 接入。相同客服、平台、服务商和 token 会幂等更新。

客服端 uni-app 登录成功后会尝试调用 `uni.getPushClientId`，拿到 `cid` 后自动注册到该接口；H5 或模拟器没有推送能力时会静默跳过。

当后端配置 `PUSH_WEBHOOK_URL` 后，访客新消息触发客服提醒时，服务端会把通知和该客服已注册设备 POST 到推送网关。网关可以是 uni-push 云函数、APNs/Android 厂商推送适配服务。

### 修改当前登录密码

`POST /api/account/password`

管理员和客服账号共用。修改成功后服务端会撤销该账号已有登录 token，并向该账号已在线 WebSocket 推送 `session.revoked` 后断开连接；客户端应清理本地登录态并要求重新登录。
新密码必须至少 10 个字符，且不能使用 `123456`、`password` 等明显弱口令。

```json
{
  "current_password": "当前密码",
  "new_password": "NewPassword-0123"
}
```

### 获取自己负责的会话

`GET /api/agent/conversations`

返回：

```json
{
  "conversations": []
}
```

## 4. 管理接口

请求头：

```text
Authorization: Bearer <admin_token>
```

### 获取全部会话

`GET /api/admin/conversations`

### 导出全部会话 CSV

`GET /api/admin/conversations/export.csv`

返回 `text/csv` 附件，字段包含会话 ID、访客 ID、原始 IP、访客备注、状态、负责人、来源、最后消息、未读数和创建/更新时间。管理端“会话监控”页面的“导出”按钮调用该接口。

### 获取数据概览

`GET /api/admin/dashboard`

返回：

```json
{
  "total_conversations": 1284,
  "active_conversations": 42,
  "ai_serving": 18,
  "waiting": 6,
  "human_requested": 2,
  "assigned": 16,
  "closed": 1242,
  "online_agents": 5,
  "total_agents": 8,
  "rating": {
    "total": 320,
    "average": 4.7,
    "satisfaction_rate": 0.92
  }
}
```

### 获取客服列表

`GET /api/admin/agents`

返回：

```json
{
  "agents": [
    {
      "id": "agent_lixue",
      "account": "admin",
      "name": "客服-李雪",
      "group": "售前组",
      "status": "online",
      "max_conversations": 10,
      "current_conversations": 3
    }
  ]
}

```

### 新增客服

`POST /api/admin/agents`

初始密码必须满足和改密一致的强度要求。管理端默认生成临时密码，并要求客服首次登录后自行修改。

```json
{
  "account": "kf_003",
  "password": "Tmp-初始化临时密码",
  "name": "赵敏",
  "group": "售前组",
  "max_conversations": 10
}
```

### 修改客服资料

`PATCH /api/admin/agents/{agent_id}`

```json
{
  "name": "赵敏",
  "group": "售后组",
  "status": "offline",
  "max_conversations": 8
}
```

### 重置客服密码

`POST /api/admin/agents/{agent_id}/reset-password`

请求体可传入指定新密码；如果传 `{}`，服务端会生成临时强密码并在本次响应中返回。
重置成功后，该客服账号既有登录 token 会立即失效，在线 WebSocket 会收到 `session.revoked` 后断开。

```json
{}
```

返回：

```json
{
  "agent": {},
  "temporary_password": "Tmp-xxxxxxxxxxxxxxxx"
}
```

### 禁用客服

`POST /api/admin/agents/{agent_id}/disable`

禁用后该客服不能登录；既有登录 token 会立即失效，在线 WebSocket 会收到 `session.revoked` 后断开。未关闭会话会重新派单，若无可用客服则进入 AI 接待。

### 获取 AI 配置

`GET /api/admin/ai-settings`

返回字段说明：

- `enabled`：AI 总开关。
- `mode`：`human_first`、`always_ai`、`manual_only`。
- `base_url`：OpenAI 原生或兼容服务地址。
- `api_key_masked`：只读脱敏 Key，后端不会回传明文。
- `api_type`：`chat_completions` 或 `responses`。

### 更新 AI 配置

`PATCH /api/admin/ai-settings`

```json
{
  "enabled": true,
  "mode": "human_first",
  "base_url": "https://api.openai.com/v1",
  "api_key": "sk-xxx",
  "model": "gpt-4o-mini",
  "api_type": "chat_completions",
  "temperature": 0.7,
  "max_output_tokens": 512,
  "timeout_seconds": 20,
  "system_prompt": "你是一个专业的在线客服助手。",
  "agent_no_reply_timeout_seconds": 60
}
```

`api_key` 是写入字段；为空时保留原 Key。接口返回 `api_key_masked`，不返回明文 `api_key`。

### 测试 AI 接口联调

`POST /api/admin/ai-settings/test`

```json
{
  "input": "请用一句话回复：AI 接口联调是否正常。"
}
```

返回：

```json
{
  "reply": "AI 接口联调正常。"
}
```

### 获取营业时间

`GET /api/admin/business-hours`

### 更新营业时间

`PATCH /api/admin/business-hours`

```json
{
  "timezone": "Asia/Shanghai",
  "start": "09:00",
  "end": "18:00",
  "enabled": true
}
```

### 更新联系方式配置

`PATCH /api/admin/contact-settings`

```json
{
  "phone": "400-123-4567",
  "wechat": "Service999",
  "wechat_reply_type": "image",
  "wechat_image_url": "/uploads/20260513/xxx.png",
  "qq": "88888888",
  "qq_reply_type": "text",
  "qq_image_url": ""
}
```

### 获取关键词规则

`GET /api/admin/keyword-rules`

```json
{
  "rules": [
    {
      "id": "kw_phone",
      "keyword": "电话",
      "match_type": "contains",
      "reply": "客服电话：400-123-4567",
      "enabled": true,
      "priority": 90,
      "action": "phone"
    }
  ]
}
```

### 新增关键词规则

`POST /api/admin/keyword-rules`

```json
{
  "keyword": "售后",
  "match_type": "contains",
  "reply": "售后客服会尽快为您处理。",
  "enabled": true,
  "priority": 50,
  "action": "text"
}
```

### 修改关键词规则

`PATCH /api/admin/keyword-rules/{rule_id}`

字段同新增接口。

### 获取评价汇总

`GET /api/admin/ratings/summary`

### 获取最近评价

`GET /api/admin/ratings?limit=20`

### 获取管理审计日志

`GET /api/admin/audit-events?limit=50`

返回最近的管理操作审计，包括客服账号变更、关键词配置、AI 配置、营业时间、会话转接/关闭等关键动作。

### 导出管理审计 CSV

`GET /api/admin/audit-events/export.csv?limit=500`

返回 `text/csv` 附件，字段包含审计 ID、操作者类型、操作者 ID、动作、资源、资源 ID、IP、User-Agent、描述和创建时间。管理端“管理操作审计”页面的“导出”按钮调用该接口。

## 5. 通用会话接口

### 获取历史消息

`GET /api/conversations/{conversation_id}/messages`

可选查询参数：

- `limit`：每页数量，默认 50，最大 100。
- `before`：传入当前最早一条 `server_msg_id`，向前加载更早消息。

返回：

```json
{
  "messages": [],
  "has_more": true,
  "next_before": "msg_xxx"
}
```

访客、负责该会话的客服、管理账号可访问。

### 修改访客备注

`PATCH /api/conversations/{conversation_id}/remark`

```json
{
  "remark": "意向客户-王总"
}
```

普通客服只能修改自己负责会话的备注，管理账号可以修改任意会话备注。原始 IP 不会被覆盖。

### 关闭会话

`POST /api/conversations/{conversation_id}/close`

### 上传图片

`POST /api/uploads`

访客、客服、管理账号均可上传。使用 `multipart/form-data`，字段名为 `file`。当前支持 `jpg/png/gif/webp`。

用户端 Web、客服端 uni-app 的相册/拍照发送，以及管理端联系方式二维码上传均使用该接口。上传完成后，图片消息通过 WebSocket 以 `message_type=image` 发送，`content` 必须为上传返回的 `/uploads/...` 或公开 `http(s)` URL。

上传存储由 `UPLOAD_DRIVER` 决定：

- `local`：写入本地/共享卷，并通过 `/uploads/{path}` 访问。
- `s3`：写入 S3/MinIO 兼容对象存储，返回 `S3_PUBLIC_BASE_URL` 或 `UPLOAD_PUBLIC_BASE_URL` 拼出的公开 URL。

返回：

```json
{
  "url": "/uploads/20260513/xxx.png",
  "path": "20260513/xxx.png",
  "mime_type": "image/png",
  "size": 1024
}
```

### 运维指标

`GET /metrics`

返回 Prometheus 文本格式指标，包含当前 WebSocket 连接数和 Go 协程数。生产建议只允许内网监控系统访问该端点。

如果配置了 `METRICS_BEARER_TOKEN`，需要请求头：

```text
Authorization: Bearer <metrics_token>
```

### 访客提交服务评价

`POST /api/visitor/conversations/{conversation_id}/rating`

请求头：

```text
Authorization: Bearer <visitor_token>
```

```json
{
  "score": 5,
  "tags": ["非常满意"],
  "comment": "体验很好"
}
```

同一会话只能提交一次评价，重复提交返回 `409 conflict`。

### 管理员强制转接

`POST /api/admin/conversations/{conversation_id}/transfer`

按客服转接：

```json
{
  "agent_id": "agent_lixue"
}
```

按分组转接：

```json
{
  "group": "售后组"
}
```

## 6. WebSocket 连接

### 访客连接

```text
ws://localhost:8080/ws?role=visitor&token=<visitor_token>&conversation_id=<conversation_id>
```

### 客服连接

```text
ws://localhost:8080/ws?role=agent&token=<agent_token>
```

### 管理端连接

```text
ws://localhost:8080/ws?role=admin&token=<admin_token>
```

## 7. WebSocket 事件

### 心跳

发送：

```json
{
  "event": "ping"
}
```

返回：

```json
{
  "event": "pong",
  "data": {
    "time": "2026-05-12T12:00:00Z"
  }
}
```

### 发送消息

```json
{
  "event": "message.send",
  "data": {
    "conversation_id": "conv_xxx",
    "client_msg_id": "client_xxx",
    "message_type": "text",
    "content": "你好"
  }
}
```

`client_msg_id` 用于客户端重试幂等。相同会话、发送方和 `client_msg_id` 的消息只会写入一次。

`message_type` 支持 `text`、`emoji`、`image`、`contact_phone`、`contact_wechat`。图片消息的 `content` 使用上传接口返回的 `url`。

访客连接只能绑定并写入当前 token 对应的会话；如果访客端在连接或发送时传入其它 `conversation_id`，服务端会返回 `forbidden` 错误并拒绝。

### 消息确认

```json
{
  "event": "message.ack",
  "data": {
    "server_msg_id": "msg_xxx",
    "client_msg_id": "client_xxx"
  }
}
```

### 接收消息

```json
{
  "event": "message.receive",
  "data": {
    "server_msg_id": "msg_xxx",
    "conversation_id": "conv_xxx",
    "sender_type": "ai",
    "message_type": "ai_text",
    "content": "您好，请问有什么可以帮您？"
  }
}
```

### 会话状态变化

```json
{
  "event": "conversation.status_changed",
  "data": {
    "id": "conv_xxx",
    "status": "assigned",
    "assigned_agent_id": "agent_lixue"
  }
}
```

### 会话被分配

```json
{
  "event": "conversation.assigned",
  "data": {
    "id": "conv_xxx",
    "status": "assigned"
  }
}
```

### 客服端提醒

```json
{
  "event": "agent.notification",
  "data": {
    "conversation_id": "conv_xxx",
    "title": "114.25.10.22",
    "body": "人工客服",
    "status": "human_requested"
  }
}
```

### 登录态被撤销

管理员或客服账号改密、客服密码被重置、客服账号被禁用时，相关在线连接会收到该事件，随后服务端断开连接。
客服端和管理端收到该事件后应清理本地 token、停止自动重连并返回登录页。

```json
{
  "event": "session.revoked",
  "data": {
    "reason": "password_changed"
  }
}
```

`reason` 可能为 `password_changed`、`password_reset`、`account_disabled`。多后端实例部署时，该事件会通过 Redis Pub/Sub 广播到其它节点。

### 关闭会话

访客连接只能关闭自己的会话；客服只能关闭自己负责的会话；管理端可关闭任意会话。

```json
{
  "event": "conversation.close",
  "data": {
    "conversation_id": "conv_xxx"
  }
}
```

## 8. 生产安全相关环境变量

- `DATA_ENCRYPTION_KEY`：配置后，PostgreSQL 中 `ai_settings.api_key_ciphertext` 会以 `enc:v1:` 前缀加密存储。
- `ADMIN_BOOTSTRAP_PASSWORD` / `AGENT_BOOTSTRAP_PASSWORD`：首次初始化默认管理员和默认客服的密码；生产环境必须设置为非默认强密码。
- `CORS_ALLOWED_ORIGINS`：生产域名白名单，多个域名用英文逗号分隔。
- WebSocket 浏览器连接会复用 `CORS_ALLOWED_ORIGINS` 校验 `Origin`；原生 App 或无 `Origin` 的客户端不受影响。
- `TRUSTED_PROXY_CIDRS`：可信反代 / 负载均衡 CIDR；仅可信代理传入的 `X-Forwarded-For` / `X-Real-IP` 会作为真实访客 IP。
- `RATE_LIMIT_ENABLED` / `RATE_LIMIT_RPS` / `RATE_LIMIT_BURST`：节点级限流配置。
- `METRICS_BEARER_TOKEN`：监控指标访问 token。
- `STORE_DRIVER=postgres`：启用 PostgreSQL 仓库。
- `REDIS_ADDR`：启用 Redis 跨节点 WebSocket 事件广播。
- `UPLOAD_DIR`：本地上传目录，容器部署时需要挂共享卷。
- `UPLOAD_PUBLIC_BASE_URL`：上传文件公开域名，例如 `https://service.example.com`。
