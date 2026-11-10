# 更新记录

## v1.4.4 - 2026-11-10

### 生产链路修复与清理

- 修复本地媒体模式：`ADMIN_MEDIA_DRIVER=local` 现在真正切换 media-store，而非仍按 S3 初始化；媒体运行时旧配置不能覆盖部署级驱动。
- 修复自定义 `ADMIN_UPLOAD_DIR` 下 `/uploads/site-admin/` 的公开读取路径；新增自定义上传目录回归测试。
- 修复草稿预览路由未注入 `bodyJson` 导致的 503；新增预览路由回归测试。
- 生产 Compose 移除已不可稳定拉取的内置 MinIO/mc；默认使用本地卷或外部 R2/S3，避免部署时拉取失败。
- 删除未使用的旧后台 HTML/JS 静态页面、旧内容渲染函数、`watch-translation.sh`、`legacy-frame.vue` 和过期任务文档/截图。
- 更新功能状态、交接、安全基线与上线剩余事项文档；页面巡检更新为 15/15。
- 修复 HTTP API 集成测试固定端口导致的偶发重复咨询单误判。

## v1.4.3 - 2026-10-30

### 一键商业化部署

- 新增 `scripts/install.sh`：支持 GitHub 一行命令拉取、环境检查、Docker/Compose 安装与版本校验、GitHub/Docker/Registry 多渠道测速、域名询问、ACME 邮箱、SSL 自动签发与续期。
- 旧版本升级：先 `pg_dump` 备份 PostgreSQL，再原地 `up -d --build`；不执行 `down -v`，不重建数据库卷，保留 `.env.ubuntu` 与数据加密密钥。
- 部署后自动检查官网、后台、客服 API、容器健康、TLS 证书；全部正常才退出 0，否则保留服务并打印排查命令。
- 新增 [一键部署文档](docs/ONE_CLICK_DEPLOY.md)；首次安装默认本地媒体卷，已有 S3 配置不会被覆盖。

## v1.4.2 - 2026-10-27

### 开放 API 关闭（业务要求）

- 开放 API 改为后台「系统设置 → 安全策略 → 开放 API」总开关，默认关闭；关闭时 `/open/v1/*` 返回 503，不泄露数据。
- Caddy 始终转发 `/open/*` 到 Go 引擎，由后台开关决定是否可用；不再需要在环境变量里改 `OPEN_API_ENABLED`。
- 删除 `scripts/verify-content-api-stub.mjs`；`verify-all.sh` 第 5 步改为临时启动真实 admin-server（JSON 存储 + seed），通过真实 `/site-admin-api/public-content` 构建，不再用桩内容 API。
- 控制台恢复「API 管理」用于管理授权，但总开关默认关闭；撤下独立第三方接入指南，后台保留静态接入说明。
- AI 不再伪造回复：未配置 API Key 时 `Reply` 返回 `ErrNoAPIKey`，后台测试/建议接口真实报错，接待链路走明确的 `aiBusyText`，不再返回本地伪装的 AI 文案。
- 客服围观范围也改为后台可选（仅本人/本组/未分配，或任意客服）；草稿预览有效期可在后台选择 5–120 分钟。

## v1.4.1 - 2026-10-24

### 安全加固（代码 + 文档）

- 首次初始化：新增一次性 `ADMIN_SETUP_TOKEN`，`setup/verify` 重新校验“仍无管理员”，旧挑战不能覆盖已建账号；初始化接口独立限速。
- 后台会话：写请求新增 CSRF 双提交令牌（`X-CSRF-Token` + `astreva_admin_csrf` Cookie），会话 Cookie 改 `SameSite=Strict`；后台写 Origin 与官网读/提交白名单分离；重置密码/2FA、重建账号会撤销全部旧会话。
- 富文本：重写 HTML 消毒（未闭合标签、`javascript:`/`//host` 链接），翻译 AI 输出与草稿预览同样消毒；JSON-LD 使用 script 上下文安全序列化；公网站点改用 Astro `security.csp` 生成带哈希的严格 CSP（无 `script-src 'unsafe-inline'`），Caddy 负责 `frame-ancestors`/XFO。页面已无内联 `style` 属性；客服页外置 `chat.css` 后，`style-src`/`style-src-attr` 也收紧为 `'self'`/`'none'`。
- 草稿预览：`/__preview/<token>/` 由 Caddy `forward_auth` 向 admin-server 校验构建 token + 10 分钟 HttpOnly 预览 Cookie，未登录/链接外泄均无法直接读取。
- 访客聊天：Caddy 增加 `frame-ancestors`；聊天页只向白名单父 Origin 发送会话 token，不再使用 `'*'`；内容模型里的客服地址不能指向非配置域名。
- 上传：访客上传禁止 APK，拒绝 HTML 伪装；增加每访客与全局每日上传配额。
- 本地默认口令：Go 引擎不再内置 `superadmin/123456`；未配置 bootstrap 口令时生成本进程随机口令，生产环境强制显式强口令。
- 客服引擎：只读围观默认只允许本人/本组/未分配会话（后台安全策略可改为 `all`），接管/转接/结束/停用立即清理；Bark 自定义地址禁止私网/回环/链路本地与重定向；WebSocket 增加全局/单账号连接上限、24h 生命周期，API Key 重置/停用/删除与账号过期会断开连接；CSV 导出防公式注入；默认密码在切生产时自动替换；HTTP 读写/空闲超时。
- 部署：admin-server 注入 `APP_ENV=production` 并校验弱密钥；Go/admin-server/rebuild-webhook 镜像非 root；生产 Caddy 不再暴露 `/metrics`；`INTERNAL_API_TOKEN` 贯通 AI 供应商内部读取与访客画像转发。
- 移动端：正式包禁止明文 HTTP；Android `usesCleartextTraffic=false`，iOS ATS 仅允许本地网络；重建入口与发布构建以非 root 运行。
- 文档：同步更新安全设计、等保自评、基线加固、日志留存、上线清单、预览 token、默认口令与 Compose 文件名；移除文档中的真实 macOS 路径和个人姓名。

## v1.4.0 - 2026-10-21

### 重点变更

- 官网前端整体替换：旧 Astro + Tailwind v3 站点下线，新前端（Astro 7 + Tailwind CSS v4 + Preline UI 5 + React islands）成为唯一实现；路由、内容接口、多语言（zh-CN / en / ar + RTL）与 SEO 产物保持一致，共 67 个静态页面。
- 视觉重设计：蓝图网格、金色索引、描边序号、非对称版式与滚动揭示，形成统一的「精密工程感」设计语言；品牌色与圆角全部收敛到 `frontend/src/styles/themes/astreva.css` 令牌，页面内零硬编码品牌色。
- 图标系统：CMS 里的字符图标（✓ ▶ 工 滴 盾 …）统一映射为内联 SVG，带上下文映射与兜底，编辑字段不变。
- 性能：导航与 Hero 主 CTA 改为服务端渲染；Preline 只引入用到的 accordion 插件。整站 JS 从约 155 KB 降到 75 KB（gzip），CSS 13 KB（gzip）。
- 工程化：接入 ESLint（flat config）+ Prettier（含 Astro 插件），新增 `npm run check` / `npm run verify`；构建期内容缓存；`postinstall` 自动修复 npm 可选依赖导致的 rolldown 原生绑定缺失。
- 内容模型：新增 `home.partnersHidden`（隐藏合作伙伴区块）与 `home.partnerLogos`（可上传 Logo，未上传回退名称），后台首页可编辑。
- 文档：新增 `docs/PRODUCTION_CHECKLIST.md` 上线检查清单；`frontend/README.md` 记录设计语言、图标系统、Tailwind v3→v4 迁移坑与部署模型。

## v1.3.0 - 2026-09-01

### 重点变更

- 新增 API 管理（统一后台左侧「API 管理」）：可为第三方平台创建/编辑/禁用/删除 API 授权，支持按平台设置限速与备注，查看累计调用量与最近使用时间；Key 采用 `ast_live_` 前缀，系统只保存 SHA-256 哈希，完整 Key 仅在创建/重置时显示一次；授权的增删改与重置均写入审计日志。
- 新增开放接口（Open API），第三方平台可通过 API 接入客服能力并实时对话：`/open/v1/conversations` 创建/列出会话（可携带外部用户标识与语言），`/open/v1/conversations/{id}/messages` 收发消息（支持幂等、关键词与转人工流程与官网一致），`/open/v1/conversations/{id}/close` 结束会话；接入会话在客服工作台正常显示并标注来源。
- 实时传输：`/open/v1/ws` WebSocket 只读订阅，第三方平台实时接收 AI 回复、人工客服回复、系统消息、正在输入与会话状态变化，事件与官网访客端一致；消息按会话语言本地化后推送。
- 开放接口安全边界：API Key 鉴权 + 按应用令牌桶限速（默认 300 次/分钟，可单独配置，超限 429）；会话按应用严格隔离，跨应用访问返回 404；订阅连接只读，不暴露访客令牌；可通过 `OPEN_API_ENABLED=false` 整体关闭。
- 存储与部署：PostgreSQL 新增 `api_apps` 表（迁移 003），内存仓库同步实现；创建会话接口扩展访客备注参数，第三方平台用户标识直接显示为客服侧访客备注。
- 文档：新增 `docs/API_INTEGRATION.md` 第三方接入指南（鉴权、接口、WebSocket、curl/JS 示例与常见问题）；`servicebridge/docs/API与WebSocket协议.md` 补充开放接口与授权管理章节；统一后台「API 管理」提供独立的「接入说明」页面（需登录），可一键复制接入文档发给接入方。

## v1.2.0 - 2026-09-01

### 重点变更

- 人工客服无缝接管：会话可在 AI 与人工坐席之间切换，访客与坐席消息三语互译；翻译上游失败支持断点续传，批次串行化避免限流（低推理强度 + 小批次提升代理可靠性）。
- AI 回复携带当前会话历史上下文（30 条 / 6000 字上限），按会话严格隔离；访客切换网站语言后同步会话语言，AI 回复与翻译跟随新语言；欢迎语即时三语，无中文闪现。
- 智能缓存：保存 AI 配置时自动逐个实测模型缓存支持，支持的按最高效方式启用（responses 加 prompt_cache_key / Anthropic 加 cache_control），不支持保持原样；后台显示探测结果并支持手动重测。
- 聊天窗口新增「正在回复」typing 指示（AI 与人工坐席均支持），头部指示器不再截断；修复刷新丢会话（主站一级存储桥）；访客 IP 防伪造（CF-Connecting-IP / 右到左 XFF）、IPv6 归一化与来源标记。
- 新增 AI 生成 SEO：后台一键按批次（12 条/批）为全站内容生成 title / description / keywords，生成结果可复核后保存。
- 全站 head 补齐 SEO：爬虫 robots / googlebot / bingbot / baiduspider 指令，六大搜索引擎站点验证码（Google / Bing / 百度 / 360 / 搜狗 / Yandex），og:locale 与多语言 alternate、hreflang、theme-color，og:image 统一转为绝对地址。
- robots.txt 逐一放行 20+ 搜索引擎与 AI 爬虫（GPTBot、OAI-SearchBot、ChatGPT-User、ClaudeBot、PerplexityBot、Google-Extended、CCBot 等），并输出 Host 与 sitemap。
- 新增 llms.txt AI 发现页：按内容系统自动生成站点介绍、核心页面与产品分类索引，面向大模型爬虫。
- 语言切换器增强：译文按行级 / HTML 片段级建立词典提升命中率，品牌名归一化，所选语言写入 localStorage；页头内联脚本恢复语言与排版方向，杜绝切换闪烁；导航语言按钮初始状态跟随文档语言，样式更柔和。
- 访客聊天页头像升级为 PNG（客服 + 中/英/阿三语访客头像），全局隐藏滚动条但保留滚动能力；user-web 资源文件名带内容哈希，规避 1 年不可变 CDN 缓存导致更新不生效。
- 生产数据库备份不再纳入仓库（backups/ 已忽略）。

## v1.1.5 - 2026-08-28

### 重点变更

- 网站翻译任务级容错：上游网关长窗口不可用（如账号池临时排除返回 502）时，任务自动重排退避（60s/120s/240s）而不是直接标记失败；批次内重试改为快速失败（1.5s/5s/15s），批次间隔 1.5 秒。
- 新保存的翻译配置会立即中断仍在执行的旧修订号任务（批次边界检查修订号），避免旧任务长时间占用队列导致新任务排队卡死。
- 重排期间译文状态保持 pending（排队中），页面不再把上游瞬时不可用显示为失败报错；仅当重排次数耗尽后才标记 failed。
- 上游错误信息保留 HTTP 状态码，且识别 “Service temporarily unavailable” 等网关文案为临时不可用，确保此类错误进入任务级重排而不是直接判为失败。
- 修复翻译后台「保存翻译配置」点击后弹出 “Cannot read properties of null” 报错：保存回调在 await 之后才读取 event.currentTarget（事件派发结束后已置空）；改为 await 前缓存表单引用，并对状态对象加空值保护。保存本身一直成功，此前为前端误报错误。
- 前台全站翻译补齐（后台除外）：在线客服弹窗新增三语语言包（assets/i18n.js，中/英/阿 70+ 条文案），静态元素以 data-i18n 标注、动态 JS 文案统一走 t()，弹窗通过 URL lang 参数取语言；官网聊天挂件随当前语言向弹窗传 lang，并在切换语言时同步。官网其余文案均来自内容系统，由既有译文机制覆盖。
- 翻译任务可视化：重译/保存后语言卡片立即显示「正在翻译」与实时进度条（已完成条数/总条数 + 百分比），后端按批次上报进度；有任务进行时后台每 2 秒自动刷新状态，无需手动刷新。
- 官网语言切换器重做：移入导航栏（桌面在电话按钮左侧、移动端在汉堡按钮左侧），显示当前语言国旗，点击下拉弹出另外两个语言；顺序为 English（美国旗）→ العربية（沙特旗）→ 中文；不再悬浮遮挡页面内容。

### 发布验证

- `npm run test:admin`：20 通过 / 0 失败。
- 生产环境实测：翻译任务在上游 502 抖动下自动重排并最终完成；语言切换器在桌面/移动端导航栏正常渲染。

## v1.1.4 - 2026-08-28

### 重点变更

- 网站翻译可靠性修复：上游请求同时携带 `Authorization` 与 `x-api-key` 双凭证，规避部分网关剥离 `Authorization` 头导致的 401。
- 翻译上游请求新增指数退避重试（1.5s/4s/10s/20s/60s），覆盖 408/429/5xx 与网络错误，批次之间增加 1 秒停顿，降低网关高频抖动下的失败率。
- 翻译配置切换为 Chat Completions 通道 + 20 条/批 + 300 秒超时，实测稳定完成英语/阿拉伯语全站翻译；英语译文质量已抽查验证。
- 新增 `scripts/reset-admin.mjs`：忘记后台账号密码时可重置管理员邮箱/密码并重新生成 2FA 密钥（含登录自测与二维码）。
- 新增 `scripts/watch-translation.sh`：本地监视翻译任务状态变化。
- 补齐统一后台模块化单体（内容、翻译、媒体、系统设置）、运维文档与备份脚本等 v1.1.3 之后的本地演进内容。

## v1.1.3 - 2026-08-21

本版本基于本地工作区相对 `origin/main` / `v1.1.2` 的差异整理。`main` 与 `origin/main` 的提交基线一致，本地新增和修改内容作为发布提交进入此 tag。

### 重点变更

- 统一后台切换为单一控制台资源，覆盖内容管理、客服账号、工作台、关键词、评价、审计和 App 版本管理。
- ServiceBridge 移除废弃外部接待直连接入，补齐自研客服的会话、评价、审计、关键词、上传、App 版本发布和 PostgreSQL/内存存储能力。
- 新增 Flutter `online_customer_service` 客服 App，支持 iOS/Android、账号登录、会话接待、图片消息、更新检测、Android 安装引导和 FCM 客户端接入。
- 后端新增通知抽象和 FCM HTTP v1 推送通道，支持客服设备 token 上报，并在会话分配和访客新消息时触发推送。
- Ubuntu/Compose、Nginx、环境变量和运维脚本按官网、统一后台、客服引擎和访客端一体化部署刷新。
- 文档同步到当前商业上线边界，新增 App 发布、更新与推送说明，并更新功能完善清单。

### 发布验证

以下检查已在本地通过：

- `node --check admin-server/server.mjs`
- `npm run build:frontend`
- `cd servicebridge/backend && go test ./...`
- `cd servicebridge/backend && go vet ./...`
- `cd servicebridge/apps/agent-mobile && dart analyze`
- `cd servicebridge/apps/agent-mobile && flutter test`
