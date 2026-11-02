# ServiceBridge API 与 WebSocket

默认本地地址为 `http://127.0.0.1:8080`。JSON 接口拒绝未知字段和超过 1 MiB 的请求体；除公开接口外使用：

```text
Authorization: Bearer <token>
```

## 公开与访客接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/healthz` | 进程健康 |
| GET | `/readyz` | 存储就绪 |
| GET | `/api/contact-settings` | 欢迎语、联系方式、快捷问题 |
| GET | `/api/app-config` | 公开的 Firebase 客户端参数与推送开关，供客服 App 启动时初始化 |
| POST | `/api/visitor/conversations` | 创建访客和会话，body: `source` |
| GET | `/api/conversations/{id}/messages` | 历史分页，query: `limit`、`before` |
| POST | `/api/uploads` | 鉴权上传，multipart 字段 `file` |
| POST | `/api/visitor/conversations/{id}/rating` | 评价，body: `score`、`tags`、`comment` |
| GET | `/ws` | WebSocket 入口 |

创建会话返回 `token`、`visitor`、`conversation` 和 `initial_messages`。访客 token 只能访问其绑定的会话。

## 客服接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/agent/login` | 登录，body: `account`、`password` |
| POST | `/api/agent/status` | `online`、`busy`、`offline` |
| GET | `/api/agent/conversations` | 当前客服负责的会话 |
| POST | `/api/agent/avatar` | 更新客服头像 `avatar_url` |
| GET | `/api/agent/ratings/summary` | 当前客服好评率 |
| POST | `/api/agent/device-token` | 上报推送设备 token，body: `device_token`、`platform(android\|ios)`（空 token 解除绑定） |
| POST | `/api/account/password` | 修改当前账号密码 |
| POST | `/api/conversations/{id}/read` | 清除当前角色未读 |
| PATCH | `/api/conversations/{id}/remark` | 修改备注 |
| POST | `/api/conversations/{id}/messages/{message_id}/revoke` | 撤回消息 |
| POST | `/api/conversations/{id}/close` | 关闭会话 |

客服只能操作自己负责的会话。新密码至少 10 个字符且不能是明显弱口令；修改后旧 token 立即撤销。

## 管理接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/admin/login` | 管理员登录 |
| GET | `/api/admin/dashboard` | 数据概览 |
| GET | `/api/admin/system-status` | 运行状态 |
| GET | `/api/admin/conversations` | 全部会话 |
| GET | `/api/admin/conversations/export.csv` | 会话 CSV |
| DELETE | `/api/conversations/{id}` | 删除会话 |
| POST | `/api/admin/conversations/{id}/transfer` | 按 `agent_id` 或 `group` 转接 |
| GET/POST | `/api/admin/agents` | 列表/新增客服 |
| PATCH | `/api/admin/agents/{id}` | 修改客服 |
| POST | `/api/admin/agents/{id}/reset-password` | 重置密码 |
| POST | `/api/admin/agents/{id}/disable` | 禁用客服 |
| DELETE | `/api/admin/agents/{id}` | 删除客服 |
| GET/POST | `/api/admin/keyword-rules` | 列表/新增关键词规则 |
| PATCH | `/api/admin/keyword-rules/{id}` | 修改关键词规则 |
| GET/PATCH | `/api/admin/ai-settings` | 获取/保存 AI 配置 |
| POST | `/api/admin/ai-settings/models` | 按所选协议真实读取上游模型列表 |
| POST | `/api/admin/ai-settings/test` | 测试 AI 回复 |
| GET/PATCH | `/api/admin/business-hours` | 获取/保存营业时间 |
| GET/PATCH | `/api/admin/push-settings` | 获取/保存推送配置（Firebase 客户端参数 + FCM 服务账号/APNs 密钥，密钥加密存储、只回掩码） |
| POST | `/api/admin/push-settings/test` | 验证推送凭证：FCM 真实换取 OAuth2 令牌、APNs 真实签出 JWT |
| PATCH | `/api/admin/contact-settings` | 保存访客端联系方式和欢迎语 |
| GET | `/api/admin/ratings/summary` | 评价汇总 |
| GET | `/api/admin/ratings` | 最近评价，query: `limit` |
| GET | `/api/admin/audit-events` | 最近审计，query: `limit` |
| GET | `/api/admin/audit-events/export.csv` | 审计 CSV |

管理员可访问全部会话。账号、配置、转接、关闭和导出等关键操作会写审计记录。

## 开放接口（默认关闭，后台可开启）

开放 API 默认关闭，由后台「系统设置 → 安全策略 → 开放 API」控制；关闭时 `/open/v1/*` 返回 503。开启后仍需 `ast_live_` API Key 与 scope。完整接入说明见控制台「API 管理 → 接入说明」。

## 核心结构

会话状态：`waiting`、`assigned`、`ai_serving`、`human_requested`、`closed`。会话对象额外带只读字段 `assigned_agent_name`、`assigned_agent_avatar`，供访客端显示真实接待客服。

消息类型：`text`、`emoji`、`image`、`audio`、`contact_telephone`、`contact_whatsapp`、`contact_telegram`、`system`、`ai_text`、`handoff_request`、`revoked`。数据库仍兼容读取历史 `contact_phone`、`contact_wechat` 消息，但新接口不再产生旧类型。

AI 配置写入字段包括 `enabled`、`mode`、`base_url`、`api_key`、`model`、`api_type`、`temperature`、`max_output_tokens`、`timeout_seconds`、`system_prompt`。`timeout_seconds` 允许 0-120；AI 回复的外层超时按该值加翻译余量计算（上限 150 秒），不会再被固定的 25 秒截断。`api_type` 仅允许 `chat_completions`、`responses`、`anthropic_messages`。API Key 使用服务端主密钥加密保存，返回只包含 `api_key_masked`，不会回传明文 Key。

联系方式对外只返回 `telephone`、`whatsapp`、`telegram` 和 `entry_reply`，不再返回微信、QQ 或图片二维码配置。默认全部为空，必须由管理员在控制台配置（`PATCH` 为整体替换语义）。营业时间含 `timezone/start/end/enabled/offline_reply`：非营业时间 AI 继续接待，但不自动分配人工，并给访客发送一次 `offline_reply`（为空用内置文案）。

接待优先级为：人工已分配 / 人工排队 > 明确转人工 > 关键词规则 > AI。会话一旦由人工接管，即使客服离线或 AI 请求稍后返回，也只能转给其他人工或保持人工队列，不会重新触发关键词与 AI。

## WebSocket

连接：

```text
wss://service.example.com/ws?role=visitor&token=<visitor_token>&conversation_id=<conversation_id>
wss://service.example.com/ws?role=agent&token=<agent_token>
wss://service.example.com/ws?role=admin&token=<admin_token>
```

> 安全说明：浏览器 WebSocket 无法自定义 `Authorization` 头，因此这里保留 query token 兼容；
> 生产必须使用 `wss://`，并在反向代理关闭/脱敏含 `token` 的访问日志。服务端已限制单账号/全局
> 连接数、单连接消息大小（64 KiB）和最大生命周期；令牌过期或账号被重置/停用时连接会被主动断开。

客户端发送：

```json
{"event":"ping"}
{"event":"message.send","data":{"conversation_id":"conv_xxx","client_msg_id":"web_xxx","message_type":"text","content":"你好"}}
{"event":"conversation.close","data":{"conversation_id":"conv_xxx"}}
```

服务端主要事件：`connected`、`pong`、`message.ack`、`message.receive`、`message.revoked`、`conversation.created`、`conversation.assigned`、`conversation.status_changed`、`agent.notification`、`agent.status_changed`、`rating.created`、`session.revoked`、`error`。

`client_msg_id` 是发送重试幂等键。服务端每 30 秒发送协议 Ping，70 秒无活动关闭连接；客户端应指数退避重连并在重连后通过历史接口补消息。

## 运维接口

`GET /metrics` 输出 WebSocket 角色连接数和 Go 协程数；设置 `METRICS_BEARER_TOKEN` 后需要 Bearer token。生产安全相关变量和反代要求见 `生产部署与容量规划.md`。
