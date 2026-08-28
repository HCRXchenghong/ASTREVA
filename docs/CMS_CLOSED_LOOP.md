# 官网内容闭环

## 入口与职责

- 登录：`http://127.0.0.1:1337/admin-2fa`
- 统一后台：`http://127.0.0.1:1337/site-admin/`
- 内容管理：统一后台左侧“内容管理”
- 客服账号和会话：统一后台左侧“客服管理”和“工作台”
- 公开内容：`GET /site-admin-api/public-content`
- 当前降级内容：`GET /site-admin-api/public-fallback-content`
- 咨询单：`POST /site-admin-api/leads`
- 发布：`POST /site-admin-api/publish`
- 构建状态：`GET /site-admin-api/status`
- 审计日志：`GET /site-admin-api/audit-events`
- 发布队列：`GET /site-admin-api/publish-jobs`
- OpenAPI：`GET /openapi.json`

`admin-server` 是当前唯一内容源，不依赖外部内容系统。第一次访问登录页时创建管理员并绑定一次性验证码应用，之后使用邮箱、密码和 6 位验证码登录。

## 编辑与发布

1. 在内容后台选择模块，修改并保存当前模块。
   - 日常编辑可使用左侧的首页、产品、服务、静态页面、媒体库等快捷模块。
   - 需要逐项核对时使用左侧第一项“全部内容字段”：它会按当前前端内容模型递归显示所有分类、产品、图片、视频、文字、SEO、导航和解决方案字段；新字段加入内容模型后也会自动出现。
2. 重新打开模块确认数据已持久化。
3. 如需调整后台不可用时使用的内容，在“降级版本”中从当前已保存内容创建版本，再启用目标版本。创建不会影响官网；启用后会自动发布。删除当前启用版本时，系统会先切换到另一版本并自动发布；最后一个版本不可删除。
4. 点击“发布官网”。后台携带 `REBUILD_WEBHOOK_SECRET` 请求 `FRONTEND_REBUILD_WEBHOOK_URL`。
5. 构建服务先校验当前启用的降级版本，再执行 `npm run build:frontend`，并将完整静态产物原子切换到官网容器共享的发布目录。
6. 在状态面板确认完成时间、退出码和构建摘要，再检查官网。

内容、管理员、咨询单、降级版本、审计事件与发布队列均保存于 PostgreSQL `site_*` 表，媒体保存于 S3 兼容对象存储。构建发布前会读取当前启用的降级版本；后台不可用时，已经发布的静态官网继续对外服务，不依赖本地 JSON 快照。旧存储切换与删除流程见 [OPERATIONS.md](OPERATIONS.md)。

`npm run build:frontend` 会先执行 `npm run cms:contract`：它根据 `frontend/src/lib/types.ts` 核对后台内容种子中的必填字段和字段类型，并拒绝页面组件中重新写入的中文文案或媒体地址。以后修改前端内容模型时，如果没有同步后台配置字段，构建会失败，避免前后端配置脱节。

## 故障定位

- 官网显示旧的降级内容：在后台“降级版本”确认当前启用版本，重新启用并发布；再检查构建服务日志。
- 保存成功但官网未变化：保存不会自动发布，检查构建状态和 webhook 日志。
- 客服入口地址错误：在“在线客服”模块或 `PUBLIC_SERVICEBRIDGE_URL` 中填写完整的 `http://` 或 `https://` 地址后重新发布。
- 后台被重定向到登录页：2FA 会话已过期，重新登录即可。
