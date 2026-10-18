# 第三方平台 API 接入指南

ASTREVA 客服引擎提供开放 API（Open API），允许第三方平台（独立站、ERP、App、小程序等）通过 API 授权接入客服能力：

- 在第三方平台上为用户创建客服会话；
- 代用户收发消息，与官网访客一样进入 AI 接待、关键词规则与人工客服流程；
- 通过 WebSocket 实时接收 AI 与人工客服的回复，实现跨平台实时对话。

授权与 Key 管理入口：统一控制台 → **应用管理 → API 管理**。

## 1. 获取 API Key

1. 管理员登录统一控制台，进入「应用管理 → API 管理」，点击「新增授权」。
2. 填写平台名称、备注与限速（次/分钟，0 表示使用默认 300 次/分钟）。
3. 保存后系统生成形如 `ast_live_xxxxxxxx…` 的 API Key。**完整 Key 只显示这一次**，请立即复制保存；系统只存储 Key 的 SHA-256 哈希，控制台与数据库均无法再次查看。
4. Key 泄露时可随时「重置 Key」（旧 Key 立即失效），也可「禁用」或删除授权。

所有请求携带以下任一鉴权信息：

```
Authorization: Bearer ast_live_xxxxxxxx…
X-API-Key: ast_live_xxxxxxxx…
```

WebSocket 订阅额外支持 query 参数 `key=ast_live_…`（浏览器 WebSocket 无法自定义请求头）。

## 2. 服务地址

开放接口部署在客服引擎（ServiceBridge）域名下，即官网「全站设置 → ServiceBridge 地址」中配置的地址，记为 `{BASE}`。管理端查询、审计与限速统计均与客服引擎共用。

## 3. 接口一览

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/open/v1/me` | 连通性测试，返回当前授权应用信息 |
| POST | `/open/v1/conversations` | 创建会话 |
| GET | `/open/v1/conversations` | 列出本应用创建的会话（最多 200 条，按更新时间倒序） |
| GET | `/open/v1/conversations/{id}` | 查询单个会话 |
| GET | `/open/v1/conversations/{id}/messages?limit=50&before={server_msg_id}` | 拉取历史消息（访客视角，已按会话语言本地化，支持翻页） |
| POST | `/open/v1/conversations/{id}/messages` | 代访客发送消息 |
| POST | `/open/v1/conversations/{id}/close` | 结束会话 |
| GET | `/open/v1/ws?conversation_id={id}` | WebSocket 实时订阅该会话 |

通用约定：

- 请求与响应均为 `application/json; charset=utf-8`。
- 错误响应形如 `{"error": "..."}`，状态码：400 参数错误、401 Key 无效或已禁用、404 会话不存在或不属于当前应用、429 超过限速、503 开放接口被管理员关闭。
- 会话归属：每个应用只能访问自己通过 API 创建的会话（创建时自动写入来源标记 `api:<app_id>`），跨应用访问返回 404。
- 限速：令牌桶，默认每分钟 300 次，可在控制台按应用单独设置；超限返回 429，稍后重试即可。
- 第三方平台调用同样计入访客真实 IP 解析（支持 Cloudflare / 反向代理头），控制台可审计每一次授权的创建、修改、重置与删除。

## 4. 创建会话

```bash
curl -X POST '{BASE}/open/v1/conversations' \
  -H 'Authorization: Bearer ast_live_xxx' \
  -H 'Content-Type: application/json' \
  -d '{"external_user_id": "user-1001", "lang": "en"}'
```

请求体（均可选）：

| 字段 | 说明 |
| --- | --- |
| `external_user_id` | 第三方平台的用户标识（≤128 字符），会显示为客服工作台中的访客备注，便于人工客服识别 |
| `lang` | 会话语言：`zh` / `en` / `ar`，影响 AI 回复语言与消息本地化，默认 `zh` |

响应 `201`：

```json
{
  "conversation": { "id": "conv_xxx", "status": "waiting", "visitor_remark": "user-1001", "source": "api:api_xxx", "...": "..." },
  "initial_messages": [ { "server_msg_id": "msg_xxx", "sender_type": "ai", "content": "您好，欢迎咨询在线客服…", "...": "..." } ]
}
```

创建会话后即进入官网一致的接待流程：欢迎语、关键词规则、AI 接待或转人工。

## 5. 发送消息

```bash
curl -X POST '{BASE}/open/v1/conversations/{conversation_id}/messages' \
  -H 'Authorization: Bearer ast_live_xxx' \
  -H 'Content-Type: application/json' \
  -d '{"content": "Hello, I want a quote", "message_type": "text", "client_msg_id": "msg-0001"}'
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `content` | 是 | 消息内容，≤4000 字符；`image` 类型填图片 URL |
| `message_type` | 否 | `text`（默认）/ `emoji` / `image` |
| `client_msg_id` | 否 | 客户端幂等 ID（≤128 字符），重复发送同一 ID 不会产生重复消息 |

响应 `201`：

```json
{
  "message": { "server_msg_id": "msg_xxx", "sender_type": "visitor", "content": "Hello, I want a quote", "...": "..." },
  "generated": [ { "sender_type": "system", "content": "已为您转接人工客服…", "...": "..." } ],
  "conversation": { "status": "ai_serving", "...": "..." }
}
```

- `generated` 为本次触发的即时回复（关键词自动回复、转人工系统消息等）；
- AI 回复异步生成，通过 WebSocket 或轮询 `GET messages` 获取；
- 消息发送后与官网挂件完全一致：分配人工客服、推送通知、记录审计与未读数。

## 6. 实时接收回复（WebSocket）

对每个进行中的会话建立一条订阅连接：

```
GET {BASE_WS}/open/v1/ws?conversation_id={conversation_id}&key=ast_live_xxx
```

（`{BASE_WS}` 为 `{BASE}` 的 ws/wss 版本；服务端调用也可直接用请求头携带 Key。）

连接成功后收到 `connected` 事件，随后实时推送：

| 事件 | 说明 |
| --- | --- |
| `connected` | 连接建立，附带会话信息 |
| `message.ack` | 本应用刚发送的消息回执 |
| `message.receive` | 新消息：AI 回复、人工客服回复、系统消息（`sender_type` 区分） |
| `conversation.typing` | 客服/AI「正在输入」指示，可用于渲染动效 |
| `conversation.status_changed` | 会话状态变化（等待中 / AI 接待 / 人工服务 / 已结束等） |
| `pong` | 心跳响应（每 30-60 秒发送 `{"event":"ping"}` 保活，70 秒无活动连接会断开） |
| `error` | 上行事件被拒绝等错误提示 |

消息事件中的 `content` 已按会话语言本地化，可直接渲染到第三方平台的聊天界面。

JavaScript 示例：

```js
const ws = new WebSocket(
  'wss://service.example.com/open/v1/ws?conversation_id=' + conversationId + '&key=' + apiKey
);
ws.onmessage = (event) => {
  const { event: name, data } = JSON.parse(event.data);
  if (name === 'message.receive') {
    renderMessage(data); // data.sender_type: ai / agent / system
  }
};
setInterval(() => ws.readyState === 1 && ws.send('{"event":"ping"}'), 30000);
```

> 该连接为**只读订阅**：发送消息请使用 `POST /open/v1/conversations/{id}/messages`。

## 7. 结束会话与历史

```bash
curl -X POST '{BASE}/open/v1/conversations/{conversation_id}/close' \
  -H 'Authorization: Bearer ast_live_xxx'
```

结束后会话进入只读状态，仍可通过 `GET messages` 拉取完整历史；如需再次咨询，重新创建会话即可。

## 8. 常见问题

- **401 unauthorized**：Key 错误、已被重置或授权被禁用；在控制台确认授权状态。
- **404 not found**：会话不存在，或该会话由其他应用创建（会话按应用隔离）。
- **429 rate limit**：降低调用频率或联系管理员调高该应用的限速。
- **收不到人工回复**：确认 WebSocket 仍连接（70 秒无心跳会被断开）；也可用 `GET messages?before=` 轮询兜底。
- **多节点部署**：客服引擎多副本 + Redis 时，事件经 Redis 总线分发，订阅任意节点均可收到全部事件。
