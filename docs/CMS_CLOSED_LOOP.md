# 官网内容闭环

> 版本：v1.4.5 · 更新时间：2026-11-10

## 入口与职责

- 控制台登录：`http://127.0.0.1:1337/site-admin/login`（旧地址 `/admin-2fa` 会 302 到此）
- 统一控制台：`http://127.0.0.1:1337/site-admin/`
- 内容编辑：控制台左侧「内容管理 → 官网内容」
- 网站翻译：控制台左侧「内容管理 → 网站翻译」
- 素材库：控制台左侧「内容管理 → 素材库」
- 客服账号与会话：控制台左侧「客服管理」和「工作台」
- 公开内容：`GET /site-admin-api/public-content`（`?locale=en|ar` 取译文，未就绪返回 409）
- 当前降级内容：`GET /site-admin-api/public-fallback-content`
- 咨询单：`POST /site-admin-api/leads`
- 草稿预览：`POST /site-admin-api/preview`，构建到官网 `/__preview/<随机 token>/`，由 Caddy forward_auth 向 admin-server 校验构建 token + 后台领取的短时预览 Cookie
- 发布：`POST /site-admin-api/publish?source=current|fallback`
- 构建状态：`GET /site-admin-api/status`
- 审计日志：`GET /site-admin-api/audit-events`
- 发布队列：`GET /site-admin-api/publish-jobs`

`admin-server` 是当前唯一内容源，不依赖外部内容系统。第一次访问登录页时创建管理员并绑定一次性验证码应用，之后使用邮箱、密码和 6 位验证码登录。控制台前端是 `admin-web/`（Arco Design Pro Vue），由 `admin-server` 同源托管。

## 编辑与发布

1. 在控制台「内容管理 → 官网内容」按分组选择模块进行编辑。
   - 常规编辑使用「页面 / 产品 / 全局组件 / SEO」分组；
   - 需要逐项核对时使用「全部内容」，它会按前端内容模型递归显示所有分类、产品、图片、视频、文字、SEO、导航和解决方案字段；新字段加入内容模型后也会自动出现。
2. 保存草稿（不会影响线上），或点击「保存并发布」。
3. 想在发布前核对效果，点击「预览草稿」：后台把当前编辑器内容（包括未保存修改）通过构建服务构建到官网的 `/__preview/<随机 token>/` 路径，线上 `current` 不受影响。
4. 如需调整后台不可用时使用的内容，在「发布中心 → 降级版本」中从当前已保存内容创建版本，再启用目标版本。创建不影响官网；启用后会自动发布；删除当前启用版本时会先切换到另一版本并自动发布，最后一个版本不可删除。
5. 点击「保存并发布」后，后台携带 `REBUILD_WEBHOOK_SECRET` 请求 `FRONTEND_REBUILD_WEBHOOK_URL`。
6. 构建服务先校验内容源（当前内容或启用的降级版本），再执行 `npm run build:frontend`，并把静态产物原子切换到官网容器共享的发布目录（`releases/` + `current` 符号链接）。
7. 在「发布状态」确认完成时间、退出码与构建摘要，再检查官网；失败时可再次点击发布重试。

内容、管理员、咨询单、降级版本、审计事件与发布队列均保存于 PostgreSQL `site_*` 表，媒体保存于本地 Docker 卷或外部 S3/R2。后台不可用时，已经发布的静态官网继续对外服务。

`npm run build:frontend` 会先执行 `npm run cms:contract`：它根据 `frontend/src/lib/types.ts` 核对后台内容种子中的必填字段和字段类型，并拒绝页面组件中重新写入的中文文案或媒体地址。以后修改前端内容模型时，如果没有同步后台配置字段，构建会失败，避免前后端配置脱节。

## 多语言

- 中文是唯一源内容，官网根路径为中文；
- 英语/阿拉伯语有真实路由 `/en/`、`/ar/`，构建期从 `/site-admin-api/public-content?locale=…` 生成；某语言译文未就绪（409）时该语言不生成，构建不会失败；
- 页面输出正确的 `lang`/`dir`、canonical 与 hreflang（含 `x-default`），语言切换器是真实跳转而非客户端替换文本。

## 故障定位

- 官网显示旧的降级内容：在「发布中心」确认当前启用版本，重新启用并发布；再检查构建服务日志。
- 保存成功但官网未变化：保存不会自动发布，检查发布状态和 webhook 日志。
- 客服入口地址错误：在「系统设置 / 内容管理 → 全局组件 → 在线客服」中填写完整的 `http://` 或 `https://` 地址后重新发布；`PUBLIC_SERVICEBRIDGE_URL` 会覆盖内容里的值。
- 控制台白屏或提示「控制台尚未构建」：运行 `npm run build:admin-web`（生产镜像构建时已自动完成）。
- 后台被重定向到登录页：2FA 会话已过期，重新登录即可。
