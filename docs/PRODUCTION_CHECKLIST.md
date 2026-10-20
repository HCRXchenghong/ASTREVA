# 上线检查清单

面向「明天就要发版」的场景。按顺序执行，任何一步失败都不要继续。

## 1. 依赖与代码质量

```bash
npm ci                 # 会触发 postinstall：缺失的 rolldown 原生绑定自动补装
npm run check          # typecheck + ESLint + 内容契约
npm run format:check   # 格式检查（不通过则 npm run format 后重新提交）
```

期望：`check` 全绿；`format:check` 无输出。

## 2. 构建

```bash
npm run build:frontend
```

期望：`67 page(s) built`，且 `frontend/dist` 非空。

## 3. 全量验证

```bash
npm run verify         # = bash scripts/verify-all.sh
```

期望：`PASS 11 / FAIL 0 / SKIP 0`。含后台单测、admin-web 构建、Go 测试、Compose/Caddy 校验、PostgreSQL 集成。

## 4. 内容模型变更（本次必须做一次）

这次新增了合作伙伴 Logo 与隐藏开关，生产库里的内容是旧结构：

1. 打开后台「内容管理 → 官网内容 → 首页」。
2. 不做任何修改直接 **保存一次**。
3. 之后 `home.partnersHidden`（默认 false = 显示）与 `home.partnerLogos` 才会写进 PostgreSQL。

> 注意：`partnersHidden` 的语义是「隐藏」。不勾选即显示，不会因为空值误隐藏。

## 5. 发布前自检

- `/`、`/products/`、`/solutions/`、`/support/`、`/support/faq/`、`/about/` 均返回 200
- `/en/`、`/ar/` 正常，`/ar/` 的 `<html dir="rtl">`
- `sitemap-index.xml`、`robots.txt`、`llms.txt` 可访问
- 导航在下拉、移动端菜单、语言切换均可用
- Cookie 同意、表单提交、客服 iframe 均可用（表单需要后台在线）
- 浏览器控制台无报错

## 6. 部署

```bash
# 与现有发布流程一致：构建 → 原子发布 → current symlink 切换
PUBLISH_DIR=/usr/share/nginx/site node scripts/build-frontend.mjs
```

或使用控制台的「保存并发布」（走同一个 `buildFrontend()`）。

## 7. 回滚

- 静态产物：`releases/` 下保留最近 3 个版本，把 `current` 指回上一个即可。
- 代码：`git revert <commit>`，重新发布。
- 内容：后台有发布前的快照（`backups/`），关闭开关或回填内容字段即可。

## 客服联调注意

访客端会按 iframe 所在主机推导 API 地址（`<host>:8080`），因此**用哪个主机打开官网，就要把对应来源加进白名单**：

```
SERVICEBRIDGE_CORS_ALLOWED_ORIGINS=http://127.0.0.1:4321,http://127.0.0.1:5173,http://localhost:4321,http://localhost:5173
```

漏配时的表现是访客端弹出「连接客服系统失败，请确认后端已启动」，控制台里能看到
`Access to fetch at http://localhost:8080/api/visitor/conversations ... blocked by CORS`。
修改 `.env` 后需要**重启 ServiceBridge 后端**才生效。

## 客服后端的数据库指向

`servicebridge/backend` 读取的是 `DATABASE_URL`，不是 `SERVICEBRIDGE_DATABASE_URL`。
`scripts/run-local-stack.sh` 已经用引擎库覆盖了它，但**手动启动时**如果直接
`source .env`，`DATABASE_URL` 指向的是 CMS 库（`astreva_cms`），后端会在那里建一套
引擎表——表现就是「AI 供应商、历史会话全都消失了」。

手动启动请显式指定：

```bash
DATABASE_URL="postgres://…/astreva_engine?sslmode=disable" go run ./cmd/server
```

## AI 能力接入（客服接待）

1. 后台创建/更新供应商：`base_url`、`api_key`、模型列表、默认模型。
2. 把 `reception` 能力绑定到该供应商（同一能力可挂多个供应商，按优先级故障转移）。
3. 推理类模型（如 MiMo v2.6）会先消耗 reasoning token，必须把
   `max_output_tokens` 提到 **≥800**、`timeout_seconds` 提到 **≥60**，
   否则会出现「回复为空」或「请求超时」。
4. 联调：`POST /api/admin/ai-settings/test` 用当前配置发一条真实请求；
   `GET /api/admin/ai-providers/health` 看熔断状态与连续失败次数。

## 已知限制（非阻塞）

- `npm ci` 在 macOS 上仍可能因 npm 可选依赖 bug 漏装 rolldown 绑定；postinstall 会自动补装，失败时会打印手动命令。
- `ChatWidget` / `CookieConsent` 是 `client:only`，不参与服务端渲染（非关键路径）。
- 英文/阿拉伯语内容依赖后台译文；译文未就绪时构建会跳过对应路由（409），不会回退中文。
