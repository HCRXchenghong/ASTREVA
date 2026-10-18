# AI 供应商 / 知识库 / 限额 与 客服策略 实施规格（批次 3–4）

本文件是「AI 统一配置 + 多知识库 + 频率限额」与「客服策略」的实施依据，逐条来自产品负责人确认。

## 一、AI 多供应商（统一配置）

### 数据模型
```
ai_providers
  id            文本主键
  name          显示名（如「OpenAI 主力」「国内中转」）
  kind          text | image            文本模型 / 生图模型
  api_type      chat_completions | responses | anthropic_messages | images
  base_url      上游地址
  api_key_cipher AES-256-GCM 密文（只回末四位掩码）
  models        JSONB 数组：该供应商已获取到的模型
  default_model 默认模型
  timeout_ms    请求超时
  allowed_hosts 出站白名单（逗号分隔，防 SSRF）
  enabled       是否启用
  sort_order    排序
  created_at/updated_at

ai_bindings          能力 → 供应商 + 模型
  capability    reception | translation | detection | seo | image
  provider_id
  model
  params        JSONB（温度、max_tokens、风格等）
```

### 行为
- 「一键获取模型」：`POST /site-admin-api/ai/providers/{id}/models` → 调上游 `/models`（或按协议探测）→ 结果写回 `models`，页面模型框变成**下拉**，可搜索、可任选；也允许手填。
- 「能力绑定」：接待 AI / 翻译 / 语言检测 / AI SEO / AI 生图 各选一个供应商 + 模型；未绑定时回退到旧设置（兼容现有 `ai_settings`、翻译上游、生图上游），迁移时自动生成一条默认绑定，保证旧行为不变。
- 出站白名单按供应商配置；保存时校验主机名，调用前二次校验（防 SSRF）。
- 密钥只写不读；`PATCH` 未传 `api_key` 表示不修改。

### 接口
```
GET    /site-admin-api/ai/providers
POST   /site-admin-api/ai/providers
PATCH  /site-admin-api/ai/providers/{id}
DELETE /site-admin-api/ai/providers/{id}
POST   /site-admin-api/ai/providers/{id}/models      一键获取模型
POST   /site-admin-api/ai/providers/{id}/test        连通性/最小请求测试
GET    /site-admin-api/ai/bindings
PUT    /site-admin-api/ai/bindings                   保存能力绑定
```

## 二、AI 知识库（多库，两种用途，按客服可关）

### 数据模型
```
ai_knowledge_bases
  id, name, description, enabled, sort_order, created_at/updated_at

ai_knowledge_entries
  id, base_id, title, content, keywords JSONB, category,
  priority INT, enabled, updated_at
  embedding  vector（预留，v2 语义检索用，当前 NULL）

agent_preferences 扩展
  knowledge_suggest_enabled   客服侧「AI 推荐发送内容」总开关（默认 true）
  knowledge_base_ids JSONB    该客服可用的知识库（默认全部）
```

### 两种用途（同一份知识，两个消费点）
1. **客服辅助（推荐发送）**：客服与访客对话过程中，AI 结合**当前对话 + 命中知识**生成**候选回复**，客服可一键插入/编辑后发送；**客服接管后 AI 不再直接回复**。
   - `POST /api/agent/suggest-reply {conversation_id}` → `{suggestions:[{text, sources:[entry_id], score}]}`
   - 每个客服可**自行关闭**（`knowledge_suggest_enabled=false`），关闭后不产生推荐、不消耗额度。
2. **AI 自主接待（知识约束回答）**：客服未接管时，AI 只依据命中知识回答；
   - 命中知识 → 生成回答并在消息上标注来源条目（内部可见）；
   - **未命中 → 按配置拒答或转人工**（`unknown_action: refuse | handoff`），拒答文案可配三语。

### 检索
- 第一版：**中文按字符 bigram + 英文/其他按词**建倒排，BM25 打分 + 标题/关键词加权 + 优先级加权；返回 top-N（默认 3）与分数。
- 分数低于阈值视为未命中（阈值可配）。
- 预留 embedding 列，后续可平滑升级为向量混合检索。

### 接口
```
GET/POST/PATCH/DELETE  /site-admin-api/ai/knowledge/bases
GET/POST/PATCH/DELETE  /site-admin-api/ai/knowledge/entries
POST                   /site-admin-api/ai/knowledge/search   检索预览（后台可直接试）
POST                   /site-admin-api/ai/knowledge/import   批量导入（CSV/JSON）
```

## 三、频率 / 次数 / 字数限额（后台可配）

```
ai_limits
  visits_per_message_chars       访客单条最大字数（默认 4000，已有）
  visitor_messages_per_minute    访客每分钟消息数（默认 20）
  visitor_messages_per_day       访客每天消息数（默认 300）
  ai_calls_per_conversation_day  AI 每天每会话调用数（默认 60）
  ai_reply_max_chars             AI 单次回复最大字数（默认 1200）
  exceed_message_i18n           超限提示（zh/en/ar/… 按访客语言）
  enabled
```
- 计数维度：`会话 + 访客ID + IP`，滑动窗口（内存 + 可选 Redis；重启后按秒级窗口重建）。
- 超限行为：返回配置文案（按访客语言），**不调用 AI**，并写系统日志（warning，动作 `ai.limit_exceeded`）。
- AI 回复截断：生成后按 `ai_reply_max_chars` 截断并保持句子完整。

## 四、客服策略

| 能力 | 规则（可配） |
|---|---|
| 排队与技能组 | 并发满时不丢弃：进入队列（按分组路由、按等待时长排序）；有客服空出即分配；访客可见「排队中，前方 N 位」 |
| SLA 与超时 | 首响超时（默认 60s）提醒客服；无回复 N 分钟（默认 5）自动回退：①交回 AI ②转其他在线客服（可配顺序） |
| 会话超时关闭 | 双方静默 N 分钟（默认 30）自动结束并触发评价邀请 |
| AI / 关键词优先级 | `keyword_first`（默认，现行为）或 `ai_first`（先 AI，AI 离线才用关键词）或 `keyword_only` / `ai_only` |
| AI 无人值守兜底 | 非营业时间/无客服在线：一次离线提示 + 留资引导（文案与开关可配）；恢复营业后可自动回访提醒 |
| 黑名单 | 按访客ID/IP/账号拉黑：拉黑后建会话被拒或进入只读；管理员可解封，操作写审计 |
| 新会话自动弹窗接管 | 访客发起即向在线客服 App 弹「接管」提示；**未点接管前 AI 持续接待**；客服可「只读围观」（此时**不显示已读**、不计入其会话数） |
| 提醒渠道 | 每客服可配 **1–3 个** Telegram Bot Token+ChatID / Bark Key / 邮箱；分配、转接、访客新消息按渠道推送；可分别开关 |

### 数据模型要点
```
conversation_queue       会话排队：conversation_id, group, enqueued_at, priority
agent_remark_channels    客服提醒渠道：agent_id, kind(telegram|bark|email), config_cipher, enabled
blacklist                kind(visitor|ip|account), value, reason, expires_at, created_by
agent_preferences 扩展    auto_takeover_popup, read_only_watch
conversations 扩展        sla_first_response_at, last_agent_reply_at, sla_state
```

## 五、验收与测试

- Go 单测：供应商 CRUD 与密钥掩码、白名单校验、限额计数与滑动窗口、知识检索打分（中文 bigram/英文词）、拒答与转人工分支、排队分配顺序、SLA 超时回退、黑名单拦截、提醒渠道 1–3 个上限与失败重试。
- 集成测试：控制台接口鉴权与审计；AI 接管 vs 未接管的知识约束回答；推荐发送在客服关闭后不产生。
- UI：控制台「AI 管理 → 模型供应商 / 知识库 / 限额」「系统 → 安全策略」「客服账号 → 提醒渠道」，App 的「接管弹窗 / 只读围观 / 推荐发送」。
- 全量：`scripts/verify-all.sh` 全绿。

## 六、批次 3 第一阶段的接口契约（已冻结，实现与 UI 均以此为准）

```
GET    /api/admin/ai-providers              → {"providers":[{id,name,kind,api_type,base_url,api_key_masked,models,default_model,timeout_ms,allowed_hosts,enabled,sort_order}]}
POST   /api/admin/ai-providers              body 含 api_key（仅写入）
PATCH  /api/admin/ai-providers/{id}         局部；api_key 不传=不改，传 ""=清除
DELETE /api/admin/ai-providers/{id}
POST   /api/admin/ai-providers/{id}/models  一键获取模型（写回 models）；上游失败 502
POST   /api/admin/ai-providers/{id}/test    → {ok,latency_ms,message,models_count}
GET    /api/admin/ai-providers/{id}/secret  → 解密配置（仅服务间调用，供 admin-server 读取）
GET    /api/admin/ai-bindings               → {"bindings":[{capability,provider_id,model,params}]}   # 数组（以真实实现为准；控制台前端兼容数组与对象映射）
PUT    /api/admin/ai-bindings               body {"bindings":[{capability,provider_id,model,params}]}
GET    /api/admin/agents/{id}/preferences   管理员查看某客服翻译偏好
PATCH  /api/admin/agents/{id}/preferences   管理员重置/修改
```

生效范围（本阶段）：`reception`（接待 AI）、`translation`（翻译与译文预览）由客服引擎按能力绑定解析上游，未绑定或供应商被禁用时**原样回退**到既有 `ai_settings`；`detection` 绑定只落库（语言检测仍为本地脚本+词表，不消耗上游）；`seo` 与 `image` 的绑定先落库，**admin-server 侧下轮通过 `/secret` 接口接入**（官网 SEO 生成、AI 生图、网站翻译目前仍用各自配置，行为不变）。

安全要点：
- API Key 使用 AES-256-GCM 加密入库，列表/详情只回末四位掩码，明文永不进日志。
- `allowed_hosts` 非空时，调用前必须校验目标主机，防 SSRF；为空表示不限制（默认）。
- `/secret` 属服务间接口，仅管理员会话可访问，调用方为受信后端（admin-server），需在审计中可追。

## 七、下一阶段（本文件已列出、尚未实现）

1. 知识库（多库 + 检索 + 客服推荐发送 + AI 接管按库回答 + 库外拒答 + 按客服关闭）。
2. 限额（访客每分钟/每天消息数、AI 每日调用数、AI 单次回复字数、超限三语文案）。
3. 客服策略（排队与技能组、SLA、超时关闭、优先级、无人值守兜底、黑名单、接管弹窗与只读围观、1–3 个提醒渠道）。
4. 开放 API 权限范围（scopes）。
5. 客户画像（GeoLite2 + 后台可配第三方 IP 库 + 双端展示）。

## 八、知识库实现补充（已完成，含分数归一化说明）

### 检索打分（重要）
- BM25（k1=1.2, b=0.75）在 title(×2)/keywords(×1.5)/content(×1) 分别计算后加权求和，再按优先级放大。
- **分数归一化到 (0,1]**：`maxPossible = Σ idf(t) × (k1+1) × (2+1.5+1)`，最终 `score / maxPossible`（再乘优先级系数并截断到 1）。
  这样做是必要的：原始 BM25 无上界（实测同一问题可达 19 分），后台配的 0.35 阈值会**完全失效**，
  只要有一个词片段重合就会被当作"命中知识"，AI 会据此乱回答。
- 默认阈值 0.35（可在后台调 0–1）。实测：强命中（标题+关键词+正文都命中）≈0.57；
  只沾一点边（仅无关条目正文出现该词）≈0.10 → 被阈值挡掉，AI 不会据此回答。
- `POST /api/admin/knowledge/search` 不做阈值过滤，便于运营对照调参；AI 回答与推荐发送走阈值过滤。

### 两种用途的落地语义
- **AI 自主接待**（客服未接管）：仅当"知识库总开关开 且 存在启用库"才进入知识模式（否则行为与升级前完全一致）。
  命中 → 注入资料并要求只依据资料回答，同时在消息上记录 `knowledge_entry_ids`（仅客服/后台可见，访客视图不下发）；
  未命中 → **不调用上游**，按配置 `handoff`（转人工并回复文案）或 `refuse`（只回复文案）。
- **客服辅助推荐**：`POST /api/agent/suggest-reply` 生成 1–3 条候选，**绝不落库、绝不直接发送**；
  要求**会话已分配给该客服**（否则 403，防跨客服泄露）；该客服关闭推荐或检索无命中时返回空（无命中带 `reason:"no_knowledge"`）；
  可按客服限定可用知识库（`knowledge_base_ids`，空=全部）。

### admin-server 也已接入统一供应商
- 新增 `ai-provider-client`：按能力解析（translation / seo / image），60 秒 TTL 缓存 + 保存配置后立即 `invalidate()`；
  Go 不可达或未配置绑定 → 返回 null，**完全回退原有本地配置**（升级零影响）。
- 网站翻译、AI SEO、AI 生图的上游（base_url / api_key / model / api_type）现在都由「AI 管理 → 模型供应商 + 能力绑定」决定；
  绑定域名与 `allowed_hosts` 会并入本地 SSRF 白名单。
- 注意：`/secret` 是唯一返回明文密钥的接口，仅受信后端调用；绕过 admin-server 直连 Go 改配置时，生效延迟最多 60 秒（TTL）。

---

# 附录 A：客服策略（批次 4）施工规格

> 目标：把"排队与技能组、SLA 与超时回退、会话超时关闭、AI/关键词优先级、无人值守兜底、黑名单、新会话接管弹窗与只读围观、每客服 1–3 个提醒渠道"落地为**可配置、可审计、可测试**的真实功能。
> 所有新表/列必须同时写进 `internal/store/postgres.go` 的 `ensureCoreTables()` 与 `migrations/*.sql`（运行时只跑前者）。

## A1 数据结构

```sql
-- 客服策略（单例，id=1）
CREATE TABLE IF NOT EXISTS service_policy (
  id SMALLINT PRIMARY KEY DEFAULT 1,
  queue_enabled BOOLEAN NOT NULL DEFAULT true,
  max_wait_seconds INT NOT NULL DEFAULT 600,          -- 排队最长等待，超过转无人值守兜底
  sla_first_response_seconds INT NOT NULL DEFAULT 60,  -- 首响超时提醒
  sla_idle_minutes INT NOT NULL DEFAULT 5,            -- 客服无回复多久触发回退
  sla_idle_action TEXT NOT NULL DEFAULT 'ai',         -- ai | reassign
  auto_close_idle_minutes INT NOT NULL DEFAULT 30,    -- 双方静默多久自动结束（0=不自动）
  ai_keyword_priority TEXT NOT NULL DEFAULT 'keyword_first', -- keyword_first | ai_first | keyword_only | ai_only
  unattended_enabled BOOLEAN NOT NULL DEFAULT true,   -- 非营业/无人在线时的兜底
  unattended_reply TEXT NOT NULL DEFAULT '',          -- 兜底话术（默认中文，按访客语言翻译）
  takeover_popup_enabled BOOLEAN NOT NULL DEFAULT true,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT service_policy_singleton CHECK (id=1)
);

-- 排队（会话进入队列的顺序与分组）
CREATE TABLE IF NOT EXISTS conversation_queue (
  conversation_id TEXT PRIMARY KEY,
  group_name TEXT NOT NULL DEFAULT '',
  priority INT NOT NULL DEFAULT 0,      -- 越大越优先
  enqueued_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  reason TEXT NOT NULL DEFAULT ''       -- capacity | offline | manual
);

-- 黑名单
CREATE TABLE IF NOT EXISTS service_blacklist (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,                   -- visitor | ip | account
  value TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  created_by TEXT NOT NULL DEFAULT '',
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (kind, value)
);

-- 客服提醒渠道（每客服 1–3 个）
CREATE TABLE IF NOT EXISTS agent_alert_channels (
  id TEXT PRIMARY KEY,
  agent_id TEXT NOT NULL,
  kind TEXT NOT NULL,                   -- telegram | bark | email
  config_ciphertext TEXT NOT NULL DEFAULT '',  -- AES-256-GCM：token/chat_id/key/address
  enabled BOOLEAN NOT NULL DEFAULT true,
  created_at/updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_agent_alert_channels_agent ON agent_alert_channels(agent_id);

-- 会话补充列
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS first_response_at TIMESTAMPTZ;
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS last_agent_reply_at TIMESTAMPTZ;
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS sla_state TEXT NOT NULL DEFAULT '';
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS queue_group TEXT NOT NULL DEFAULT '';

-- 客服偏好补充
ALTER TABLE agents ADD COLUMN IF NOT EXISTS preferences ...  -- 复用现有 JSONB
--  新增字段：takeover_popup_enabled(bool,默认true)、read_only_watch(bool,默认false)
```

## A2 行为规则（须逐条实现并测试）

1. **排队与技能组**：并发满/无人空闲时不丢弃会话，写 `conversation_queue`（`group_name` 取会话分组，`priority` 取分组权重）；有空闲客服（在线 + 未达并发 + 分组匹配）时按 `priority DESC, enqueued_at ASC` 出队分配；访客端可见「排队中，前方 N 位」；等待超过 `max_wait_seconds` → 转无人值守兜底并告知访客。
2. **SLA**：首次分配后 `first_response_at` 为空且超过 `sla_first_response_seconds` → 提醒负责客服（App 弹窗 + 提醒渠道），并写审计；`last_agent_reply_at` 超过 `sla_idle_minutes` → 按 `sla_idle_action` 执行：`ai`（交回 AI 接待，会话仍属该客服但 AI 接管回复）或 `reassign`（转其他在线客服）；两条路径都记审计与系统日志。
3. **会话超时关闭**：双方静默超过 `auto_close_idle_minutes` → 自动结束（状态 closed）+ 触发评价邀请；`0` 表示不自动关闭。
4. **AI/关键词优先级**：`keyword_first`（现行为）/ `ai_first`（先 AI，AI 不可用时才命中关键词）/ `keyword_only` / `ai_only`；切换后同一会话行为需可预测（有单测覆盖四种模式）。
5. **无人值守兜底**：非营业时间或没有在线客服时，先发 `unattended_reply`（按访客语言翻译），并引导留资；营业恢复或客服上线后，对等待中的会话按队列顺序分配；不得重复发送同一条兜底话术（每会话一次）。
6. **黑名单**：建会话/发消息前检查 `visitor|ip|account` 三类，命中则拒绝并返回配置文案；支持过期时间；管理员可增删查，操作写审计。
7. **接管弹窗与只读围观**：访客发起后立即向在线客服推送「接管」提示（App 弹窗，受 `takeover_popup_enabled` 控制）；**未点接管前 AI 继续接待**；客服可"只读围观"（`read_only_watch`）：可看消息但**不标记已读**（不递增已读计数、不触发 `MarkConversationRead`）、不计入其并发/分配。
8. **提醒渠道（每客服 1–3 个）**：`telegram`（bot token + chat id）、`bark`（key/url）、`email`（地址）；分配、转接、访客新消息按渠道推送；每个渠道可单独启停；发送失败重试 2 次并记日志；**上限 3 个**（超出 400）；渠道密钥加密入库、只回掩码。

## A3 接口

```
管理员：
GET/PATCH /api/admin/service-policy                策略单例（含 priority、SLA、超时、兜底、弹窗开关）
GET       /api/admin/service-queue                 排队列表（含等待时长与分组）
GET/POST  /api/admin/blacklist                     黑名单增删查（POST 含 kind/value/reason/expires_at）
DELETE    /api/admin/blacklist/{id}
GET/POST  /api/admin/agents/{id}/alert-channels     渠道列表/新增（≤3）
PATCH/DELETE /api/admin/agents/{id}/alert-channels/{channelId}
客服：
POST      /api/agent/conversations/{id}/takeover    接管（停止 AI、正式分配）
POST      /api/agent/conversations/{id}/watch       只读围观（不标记已读、不计入分配）
GET/POST  /api/agent/alert-channels                 自己的渠道（≤3，密钥只回掩码）
```

## A4 验收测试（必须真实执行）

- **排队**：并发满→入队；新客服上线→按 优先级/入队时间 出队；等待超时→兜底话术且只发一次。
- **SLA**：首响超时触发提醒（断言推送/审计）；空闲超时 `ai` 与 `reassign` 两条路径；`auto_close` 触发结束与评价邀请。
- **优先级**：四种模式各自的关键词/AI 调用顺序与上游调用次数断言。
- **黑名单**：三类命中拒绝、过期后放行、管理员操作写审计。
- **只读围观**：`MarkConversationRead` 调用次数为 0、`UnreadForAgent` 不变、并发计数不变。
- **接管**：接管后 AI 不再自动回复（上游 0 调用）、弹窗受开关控制。
- **提醒渠道**：1–3 个上限（第 4 个 400）、密钥掩码、发送失败重试、渠道停用后不推送。
- **Postgres**：新表建表/读写/级联清理 + 单例约束（真实容器跑 `TestPostgres*`）。
- **控制台/App**：策略页、排队页、黑名单页、渠道配置页与 App 接管弹窗/围观按钮的 Playwright/组件验证，console error=0。

## 九、限额与推荐发送的最终语义（已实现）

### AI 限额（`ai_limits` 单例）
| 字段 | 默认 | 说明 |
|---|---|---|
| `enabled` | true | 关闭时全部不限制 |
| `visitor_messages_per_minute` | 20 | 访客消息频率（滑动 60 秒窗口，按 会话+访客ID+IP），**0=不限制** |
| `visitor_messages_per_day` | 300 | 滚动 24 小时窗口 |
| `ai_calls_per_conversation_day` | 60 | 每会话每天 AI 调用次数 |
| `ai_reply_max_chars` | 1200 | AI 单次回复字数上限（按句末标点截断，找不到标点硬截断补省略号） |
| `exceed_message` | 空（内置中文兜底） | 超限提示，按访客语言自动翻译后下发 |

行为要点：
- **频率超限**：该条访客消息**不落库、不触发关键词/AI**，接口返回 429（开放 API 同样映射），访客收到提示文案，并写 `ai.limit_exceeded` 审计。
- **AI 调用达上限**：不请求上游，直接落库并回复提示文案（中文原文直接作为中文译文，避免二次翻译）。
- 被拒请求也计入窗口（防抖动）；进程重启窗口重建（审计独立落库）。
- 管理员接口 `GET/PATCH /api/admin/ai-limits`：越界（负数或超上限 600/20000/2000/20000）返回 **400**，不静默改写。

### AI 推荐发送的开关语义（重要）
`POST /api/agent/suggest-reply` 由**两个开关共同门控**，任一关闭都不生成候选、不调用上游，并返回 `reason:"disabled_by_preference"`：
- `ai_suggest_replies`：**AI 推荐发送总开关**（客服可自行关闭）
- `knowledge_suggest_enabled`：**是否使用知识库辅助推荐**（客服可自行关闭）

App 的「AI 建议」按钮同样按两者共同启用/禁用；控制台「客服账号 → 翻译偏好」可查看与重置这两项。

---

# 附录 B：开放 API 权限范围（批次 5）施工规格

## B1 现状
开放 API（`/open/v1/*`）现有端点：`GET /me`、`GET|POST /conversations`、`GET /conversations/{id}`、`GET|POST /conversations/{id}/messages`、`POST /conversations/{id}/close`、`GET /ws`。
鉴权按应用 Key（只存 SHA-256）+ 每应用令牌桶限流；**目前没有权限范围概念**——一个有 Key 的应用能调用全部接口。

## B2 权限范围定义
| scope | 覆盖端点 |
|---|---|
| `me:read` | `GET /open/v1/me`（默认给所有应用，不可取消） |
| `conversations:read` | `GET /open/v1/conversations`、`GET /open/v1/conversations/{id}` |
| `conversations:create` | `POST /open/v1/conversations` |
| `conversations:close` | `POST /open/v1/conversations/{id}/close` |
| `messages:read` | `GET /open/v1/conversations/{id}/messages` |
| `messages:write` | `POST /open/v1/conversations/{id}/messages` |
| `events:subscribe` | `GET /open/v1/ws` |

预置组合（控制台一键套用）：
- **全功能**：全部 7 个
- **只读订阅**：`conversations:read`、`messages:read`、`events:subscribe`
- **仅创建与发消息**：`conversations:create`、`messages:write`
- **客服机器人**：`conversations:read`、`messages:read`、`messages:write`、`conversations:close`

## B3 数据与兼容
- `api_apps` 增列 `scopes JSONB NOT NULL DEFAULT '[]'::jsonb`（`domain.APIApp.Scopes []string`）。
- **兼容策略**：`scopes` 为空数组 = **等同于全部范围**（保持既有应用不受影响）；控制台在应用详情里提示"未限制（建议按需收窄）"。
- 校验：保存时只接受上表内的 scope；`me:read` 自动补齐。

## B4 执行点与错误
- 在 `internal/openapi` 的每个 handler 入口统一走 `requireScope(app, "xxx")`；缺失时返回 **403** `{"error":"forbidden","message":"缺少权限范围 messages:write"}`，不泄露其他信息。
- `GET /open/v1/me` 返回 `{app:{id,name}, scopes:[...]}`，便于对接方自检。
- 每次因缺范围被拒都写审计（action `openapi.scope_denied`，含 app_id、scope、path），并计入系统日志。
- 控制台侧：`GET/PATCH /api/admin/api-apps/{id}` 增加 `scopes`；新建应用时默认「全功能」并在界面显式提示。

## B5 验收测试
1. 每个端点：缺少对应 scope → 403 且带 scope 名；有 scope → 正常。
2. 空 scopes 的旧应用 → 全部端点可用（兼容回归）。
3. 四套预置组合逐一断言可访问/被拒的端点集合。
4. `/me` 返回 scopes；保存非法 scope → 400。
5. 缺范围被拒写审计（断言审计条数增加，且不包含请求体敏感内容）。
6. 控制台：应用详情能看到/修改 scopes，预置一键套用，保存后立即生效（无需重启）。
7. 真实 PG：`api_apps.scopes` JSONB 往返与默认值（扩展 `TestPostgres*`）。

### A5 已实现后的语义修订（接线 2a）

1. **无人值守兜底只在"无人接待"时发送**：若 AI 正在接待（`AISettings.Enabled` 且模式非 `manual_only`），非营业时间也**不再额外发离线话术**（否则访客会同时收到 AI 回答和"当前离线"提示，自相矛盾）。即：无人值守兜底 = 人类客服不在线 **且** AI 不可用/未启用时的最后兜底；每会话只发一次（`conversations.unattended_notified_at` 落库，重启仍成立）。
2. **排队**：分配失败（无人在线/并发满）入队并给访客推送 `conversation.queue_position {position, waiting_seconds, group?}`；客服上线、会话结束、转接时统一由应用层 `drainQueue()` 按 `priority DESC, enqueued_at ASC` 出队；store 的静默分配会跳过"已在队列中"的会话以保证顺序；离线释放且**尚未首响**的会话重新入队。`queue_enabled=false` 时不入队，改走无人值守兜底。
3. **接管/围观**：接管 = 分配给自己 + `assigned` + 清空 SLA 状态 + 移出队列 + 广播；围观 = 进程内登记（重启失效），不分配、不计并发、**不标记已读**（已读入口统一走 `service.MarkConversationRead`，围观期间直接返回快照），围观者可只读拉取消息。
4. **接管提示**：`conversation.takeover_request` 只推给"在线 且 `takeover_popup_enabled != false`"的客服，且受全局策略 `takeover_popup_enabled` 约束；App 侧自己做去重与"稍后"。
5. **黑名单**：建会话前查 ip+account，发消息前查 visitor+ip+account；命中返回 403（错误码 `blacklisted`，文案不含被拉黑值），写审计 `blacklist.blocked`；为省上游调用，拦截文案不做翻译。
6. **提醒渠道派发**：新会话分配 / 转接 / 访客新消息 → 对负责客服 goroutine+recover 调 `notify.SendAlertChannels`（标题 + 访客名 + 消息摘要前 80 字 + conversation_id + `PUBLIC_ADMIN_URL` 链接）；渠道为空不开 goroutine。
7. **队列接口字段**：`GET /api/admin/service-queue` 每项含 `conversation_id, group_name, priority, enqueued_at, wait_seconds, reason, visitor_name`。
8. **渠道掩码字段**：渠道列表/详情同时返回字符串 `masked`（主密钥掩码）与 `config_masked`（各字段掩码），并带信封 `max_channels`。
9. `first_response_at` 的写入属 SLA 计时器接线（下一波 2b）；在那之前"未首响"等同于"无人工首响记录"。

### A6 SLA 巡检与优先级的实现语义（接线 2b）

- **巡检入口**：`RunPolicySweep(now)`（可单测、确定性），生产由 `StartPolicyLoop/StopPolicyLoop` 驱动，间隔取 `SERVICE_POLICY_TICK_SECONDS`（默认 30s、非法回落、最小 5s），服务关闭时优雅停止。
- **首响**：`first_response_at` 由客服**首次真实回复**写入（`AddAgentMessage` 内，幂等早退路径不写）；接管时会重置为空。首响起算点 = `max(创建时间, 最后访客消息时间)`（无 `assigned_at` 列，属已知近似）。
- **待回复（idle）**：以"最后一条未被回复的访客消息"计时（比 `last_agent_reply_at` 更严格，避免客服久前回复、访客刚发消息就误告警）。
- **幂等**：`sla_state` 记录 `first_response` / `idle`，**仅状态变化时**推送 `conversation.sla`、派发提醒渠道、写审计与日志。
- **idle 动作**：
  - `ai`：交回 AI 接待（`ai_serving`，保留 assigned 便于追溯）。**保留 `sla_state='idle'`**——若清空，`first_response_at` 仍为空会每轮重复首响告警；客服真实回复或接管时才清空。
  - `reassign`：同分组优先、排除原客服、转移并发；**无候选时入队**而非立即 drain（刚释放的并发会让原客服成为唯一候选、被立刻分回）。
- **自动关闭**：`auto_close_idle_minutes>0` 且双方静默超阈值 → 走既有结束流程（访客可评价），审计 `conversation.auto_closed`，不重复关闭。
- **优先级四模式**：判定集中在纯函数 `resolveAutoReplyPath(priority, aiEnabled, aiConfigured)`；`keyword_first` 完全保持历史行为。
  **已知限制**：`ai_first`/`ai_only` 判断"上游是否已配置"时看的是**全局 `ai_settings`**（store 层看不到能力绑定）；若只通过「AI 管理 → 能力绑定」提供上游（全局 Key 为空），这两种模式会退化为关键词。修复方向：由应用层把"能力绑定是否可用"传入该判定（列入后续优化）。

## 十、AI 故障转移的实时端到端验证（`scripts/verify-ai-failover.mjs`）

`node scripts/verify-ai-failover.mjs`（纯 Node，无新增依赖）对**真实运行中的**客服引擎执行一次可重复的故障转移验收：

1. 用种子超级管理员登录 Go 引擎后台（`POST /api/admin/login`，默认 `superadmin` / `123456`）。
2. 记录当前 `GET /api/admin/ai-binding-targets` 与 `ai_keyword_priority`，在 `finally` 中**原样恢复**（含其它能力的候选）。
3. 确保真实 MiMo 供应商存在（缺省 `https://api.xiaomimimo.com/v1` / `mimo-v2.6-flash`，密钥从 gitignored `.env` 的 `ASTREVA_E2E_MIMO_API_KEY` 读取），并用 `POST /api/admin/ai-providers/{id}/test` 验证连通。
4. 造一个必然失败的候选：`http://127.0.0.1:9/v1`，`timeout_ms=3000`，连接被立即拒绝。
5. 通过控制台同一接口 `PUT /api/admin/ai-binding-targets` 把 `reception` 链配成 `[broken@priority 0, mimo@priority 1]`。
6. 临时把 `ai_keyword_priority` 设为 `ai_only`，经开放 API（`POST /open/v1/conversations` + `POST /open/v1/conversations/{id}/messages`）投递真实访客消息，轮询直到 AI 回复落库（≤90s）。
7. 断言：确实产生 AI 回复；回复**不等于任何本地兜底文案**（从 Go 源码的 `fallbackReply` / `aiBusyText` / 拒答 / 超限文案提取，覆盖多语言）；`GET /api/admin/ai-providers/health` 中失败候选 `consecutive_failures >= 1`（或 `state=open`）且 `last_error` 指向死上游，MiMo 保持 `closed` 且 0 失败；任一断言失败则退出码非 0。
8. 输出中的密钥一律掩码为 `sk-****`，绝不打印明文。

幂等性：MiMo 供应商复用、绝不删除；失败候选每次重建（熔断器按 `provider_id` 计数，保证 priority 0 真的被尝试），结束时删除；临时开放 API 应用与测试会话一并删除；若上一次失败留下了熔断冷却，脚本会先等待冷却结束再发消息。可配置项：`ASTREVA_E2E_ENGINE_URL`、`ASTREVA_E2E_ADMIN_ACCOUNT`、`ASTREVA_E2E_ADMIN_PASSWORD`、`ASTREVA_E2E_MIMO_API_KEY`、`ASTREVA_E2E_MIMO_BASE_URL`、`ASTREVA_E2E_MIMO_MODEL`。
