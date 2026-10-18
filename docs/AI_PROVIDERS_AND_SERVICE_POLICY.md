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
