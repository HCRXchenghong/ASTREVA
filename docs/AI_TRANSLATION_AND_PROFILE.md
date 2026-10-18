# 自动翻译与 AI 客户画像（实施规格）

本文件是「自动翻译 + 语种策略 + 客户画像」的唯一实施依据。决策来自产品负责人，实施时必须逐条对齐。

## 一、已确认的决策

| 议题 | 决策 |
|---|---|
| 译文未就绪时访客端 | **访客端显示「正在输入」**；客服端同时看到自己的**正文**与**翻译中/已翻译**状态；译文就绪后投递给访客，绝不把客服原文语言暴露给访客 |
| 方言粒度 | **由客服自行选择**：`标准语` 或 `含方言`（写入客服个人偏好，默认标准语） |
| 显式指令有效期 | 客户说「请用英语回答我」后**整个会话持续生效**，客服可手动改回 |
| IP 地理来源 | **CDN 头 + 本地 GeoLite2**（必选）；**后台可再添加第三方 IP 库（带 Key）提高准度**，作为补充源 |
| 画像中的 IP | 客服可看**完整 IP** |
| 译文是否可编辑 | **由客服自行配置**：`发送前review译文` 开关（默认开启），可改词后发送；关闭则全自动直接发送 |

客服个人偏好（账号级，默认值见括号）：
- `auto_translate_incoming` 访客消息自动译成中文（开）
- `auto_translate_outgoing` 我的消息自动译成访客语言（开）
- `review_translation_before_send` 发送前确认/编辑译文（开）
- `translation_dialect_mode` 目标语言风格：`standard`（默认）/ `dialect`
- `ai_suggest_replies` AI 推荐发送内容（开）

## 二、语言识别（三级）

1. **脚本级（本地，0 成本）**：Han / 假名 / 谚文 / 阿拉伯字母 / 西里尔 / 泰文 / 希伯来 / 天城文 / 希腊文 → 直接定位语言族；中文再判简繁，粤语标记（係/唔/咗/嘅/喺）→ `yue`。
2. **拉丁语系词表打分（本地）**：en/es/fr/de/pt/it/nl/id/vi/tr/pl/ro 的停用词与特征字符打分，输出候选与置信度。
3. **LLM 兜底（可选，低置信度才调用）**：严格 JSON `{"lang","dialect","confidence"}`，低温度，只输出 JSON；结果按文本哈希缓存。

输出结构：`{code, tag, dialect, confidence, method, candidates[]}`。`Translations` 以 **基础语言码**（zh/en/ar/es…）为键；方言只作为翻译提示与元数据，不参与键。

方言标记（有足够证据才给，否则回落标准语）：
- 阿拉伯语：海湾（شلون/وين/ابغى/مره）、埃及（ازيك/عايز/خالص/جامد）、黎凡特（كيفك/هلق/منيح/شو）、马格里布（بزاف/واش/دابا/كيفاش）
- 西班牙语：西班牙（vosotros/vale/coche/ordenador）、拉美（carro/platicar/ahorita）、阿根廷（vos/che/boludo）
- 葡萄牙语：巴西（você/celular/ônibus/time）、欧洲（telemóvel/autocarro/equipa）
- 英语：英式（colour/favourite/centre）与美式

## 三、会话语言策略（优先级从高到低）

1. **显式指令**（最高）：中/英/西/阿/法/德/葡/日/韩/俄 的"请用X回答我 / reply in X / responde en X / بالعربية"等句式，正则+语言名表解析；命中后写会话级覆盖（整会话生效），客服可清除。
2. **会话主语言**：最近 10 条访客消息的语种占比，取最高；**平局取最近一条**；占比 < 40% 回落初始语言。
3. **初始语言**：`Conversation.VisitorLang`（挂件/Accept-Language）。
4. 目标风格：客服偏好为 `dialect` 且识别到方言 → 目标提示为 `es-419`/`ar-Gulf` 这类 BCP-47；否则标准语。

结果写入会话 `LanguageState`（当前语言、方言、依据、置信度、统计、指令覆盖），会话详情可见且客服可手工覆盖。

## 四、消息翻译流水线

访客消息：入库原文（`Content` 永不覆盖）→ 检测语种 → 更新主语言统计 → 若 ≠ 中文，异步译成中文写 `Translations["zh"]` → 客服端默认显示中文，`OriginalContent` 带原文，附 `DetectedLang`，UI 有「原文」开关。

客服消息：入库原文与 `AuthorLang` → 目标语言由策略得出 → 若不同则异步翻译写 `Translations[target]` → **访客端先显示「正在输入」**，译文就绪后投递；客服端立即看到自己消息 + 状态（`翻译中` / `已翻译` / `翻译失败`）。

失败处理：翻译**绝不阻塞**、原文永久保留、`Translations` 与原文同记录（两侧都能看原文）、按 `server_msg_id` 幂等、按 `sha1(text+target+provider)` 缓存；失败时按配置①投原文并于客服端标红，或②保留占位并提示客服重试。

## 五、AI 客户画像

`VisitorProfile`（按访客聚合、跨会话）：
- 地理：CDN 头 → 本地 GeoLite2 → 后台配置的第三方 IP 库（补充）→ `Accept-Language`；输出国家/地区/时区/推测国籍 + 置信度 + 依据（UI 标注"推测"）。
- 语言画像：语种与占比、方言标记、code-switching。
- 对话画像：行业/需求/关注产品/预算与工期线索/情绪/是否决策人；AI 每 N 条增量更新"摘要 + 标签"，**带证据**（引用消息 id）。
- 展示：控制台与 App 的会话详情"访客画像"卡（含完整 IP、首次/最近访问、浏览页数，复用官网埋点数据）。
- 隐私：可整体关闭画像；完整 IP 属于个人信息，随访问日志保留策略过期；管理操作写审计。

## 六、分期

| 期 | 内容 | 验收 |
|---|---|---|
| P1 | 语言检测引擎（脚本+词表+置信度+方言标记+指令解析） | ✅ 已完成：21 语种 + 方言标记 + 13 条指令句式；`internal/ai/language_test.go` 82 断言全绿 |
| P2 | 访客消息→中文 + 客服端「原文」开关（控制台+App）+ 消息字段迁移 | ✅ 后端已完成：`domain.Message.DetectedLang/AuthorLang/TranslationStatus`、`conversations.detected_lang/reply_lang_override/language_stats` 迁移；App 已显示「查看原文」+ 语种标签；`internal/app/language_flow_test.go` 覆盖双视图 |
| P3 | 客服消息→访客语言 + 「正在输入」占位 + 失败策略 + 客服偏好开关 | ✅ 后端已完成（异步流水线）：`translation_jobs.go` 先投原文/输入指示、译文就绪原地更新、失败兜底原文；访客端原生显示「客服正在回复…」；偏好开关与预览接口进行中 |
| P4 | 会话语言策略 + 客服可编辑译文 + 方言模式选择 | 策略优先级单测 |
| P5 | AI 客户画像（地理/语言/对话）+ 双端展示 + 隐私开关 | 画像集成测试 |
| P6 | 翻译健康度面板、缓存与限额、成本统计 | 看板数据校验 |

依赖：P2 之后需要「AI 多供应商 + 能力绑定」（翻译/检测各绑定供应商与模型）；IP 库配置依赖「系统设置」新增第三方 IP 库表单。

---

# 附录：客户画像（P5）施工规格

## 1. 数据源与优先级
1. **CDN 头**：`CF-IPCountry`、`X-Vercel-IP-Country`、`X-Country-Code`（零成本、最准到国家）。
2. **本地 GeoLite2**（可选，推荐）：`geoip2` 库文件挂载到容器（`/data/GeoLite2-City.mmdb`），解析到国家/地区/城市/时区；文件缺失时自动降级。
3. **后台可配第三方 IP 库**（补充提高准度）：控制台「系统 → 访客画像」里配置 `enabled`、`url_template`（含 `{ip}` 占位）、`api_key`（加密）、`timeout_ms`、`cache_ttl_hours`。命中顺序：CDN 头 → 本地库 → 第三方；结果按 IP 缓存（默认 24h），第三方失败不影响会话。
4. `Accept-Language`、页面语言（`/en`、`/ar`）作为语言画像的兜底。

## 2. 数据模型
```sql
CREATE TABLE IF NOT EXISTS visitor_profiles (
  visitor_id TEXT PRIMARY KEY,
  country TEXT DEFAULT '', region TEXT DEFAULT '', city TEXT DEFAULT '', timezone TEXT DEFAULT '',
  nationality_guess TEXT DEFAULT '',      -- ISO 国家码（"推测"，附置信度与依据）
  nationality_confidence NUMERIC(3,2) DEFAULT 0,
  nationality_basis JSONB DEFAULT '[]',   -- ["ip_cdn","language","timezone"]
  ip_first TEXT DEFAULT '', ip_last TEXT DEFAULT '',
  languages JSONB DEFAULT '{}',           -- {"es":12,"en":3}
  dialect TEXT DEFAULT '',                -- es-419 / ar-Gulf ...
  code_switching BOOLEAN DEFAULT false,
  summary TEXT DEFAULT '',                -- AI 生成的画像摘要（增量更新）
  tags JSONB DEFAULT '[]',
  interests JSONB DEFAULT '[]',
  evidence JSONB DEFAULT '[]',            -- 摘要引用的消息 id
  visits INT DEFAULT 0, last_path TEXT DEFAULT '',
  ai_updated_at TIMESTAMPTZ, created_at/updated_at TIMESTAMPTZ DEFAULT now()
);
CREATE TABLE IF NOT EXISTS visitor_profile_settings (
  id SMALLINT PRIMARY KEY DEFAULT 1,
  enabled BOOLEAN NOT NULL DEFAULT true,
  geoip_db_path TEXT DEFAULT '', third_party_url TEXT DEFAULT '', third_party_key_ciphertext TEXT DEFAULT '',
  third_party_timeout_ms INT DEFAULT 2000, cache_ttl_hours INT DEFAULT 24,
  ai_summary_enabled BOOLEAN DEFAULT true, ai_summary_every_messages INT DEFAULT 10,
  show_full_ip BOOLEAN DEFAULT true, updated_at TIMESTAMPTZ DEFAULT now(),
  CONSTRAINT visitor_profile_settings_singleton CHECK (id=1)
);
-- 无 GeoLite2 库时仍可运行；启用第三方库需在控制台填 URL 与 Key。
```

## 3. 生成与更新时机
- **建会话/每次访客消息**：更新 `ip_last`、`country/region/city/timezone`（按需解析并缓存）、`languages` 计数、`dialect`（来自消息检测）、`visits/last_path`（来自官网埋点）。
- **AI 摘要**：每 `ai_summary_every_messages` 条访客消息触发一次（异步、复用 `aiSettingsFor("reception")` 上游）；输入=最近 N 条对话 + 已有画像；输出严格 JSON（`summary`/`tags`/`interests`/`evidence`），解析失败只记日志不覆盖旧值。
- **国籍推测**：只给"推测"结论：依据优先级 `ip_cdn > geoip > third_party > language > timezone`，置信度按证据数量加权（例如 IP 国别 0.6 + 语言一致 0.2 + 时区一致 0.2，上限 0.95）。**UI 必须标注"推测"**。

## 4. 接口
```
管理员：
GET/PATCH /api/admin/visitor-profile/settings      画像开关/GeoLite2 路径/第三方库/摘要频率/是否显示完整 IP
GET       /api/admin/visitor-profiles?q=&country=&tag=&limit=&offset=
GET       /api/admin/visitor-profiles/{visitor_id}  详情（含语言占比、标签、摘要、证据消息）
POST      /api/admin/visitor-profiles/{visitor_id}/refresh   立即重算（含 AI 摘要）
客服：
GET       /api/agent/conversations/{id}/profile     会话侧画像卡数据（越权 403）
```
隐私：`show_full_ip=false` 时客服侧只返回脱敏 IP（`1.2.3.*`）；`enabled=false` 时不采集、不返回画像；画像随访客数据保留策略清理。

## 5. 验收测试
1. 地理三级降级：只有 CDN 头 / 只有本地库（用测试 mmdb 或注入解析函数）/ 只有第三方（httptest 桩）各自正确；第三方超时不影响会话。
2. 语言画像：多语种计数与占比、方言标记（复用 `DetectLanguage`）、code-switching 判定。
3. 国籍推测：置信度与依据字段符合规则；UI 标注"推测"（快照断言文案）。
4. AI 摘要：每 N 条触发一次（上游桩计数）、JSON 解析失败不覆盖旧摘要、证据 id 指向真实消息。
5. 隐私：`show_full_ip=false` 返回脱敏 IP；`enabled=false` 时接口返回空/404 且不发起第三方请求。
6. 真实 PG：两张新表建表/读写/JSONB 往返/单例约束。
7. 控制台画像页与 App 画像卡：Playwright/组件验证，console error=0。
