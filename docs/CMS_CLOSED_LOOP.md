# 官网内容闭环

## 入口与职责

- 登录：`http://127.0.0.1:1337/admin-2fa`
- 统一后台：`http://127.0.0.1:1337/site-admin/`
- 内容管理：统一后台左侧“内容管理”
- 客服账号和会话：统一后台左侧“客服管理”和“工作台”
- 公开内容：`GET /site-admin-api/public-content`
- 咨询单：`POST /site-admin-api/leads`
- 发布：`POST /site-admin-api/publish`
- 构建状态：`GET /site-admin-api/status`

`admin-server` 是当前唯一内容源，不依赖外部内容系统。第一次访问登录页时创建管理员并绑定一次性验证码应用，之后使用邮箱、密码和 6 位验证码登录。

## 编辑与发布

1. 在内容后台选择模块，修改并保存当前模块。
2. 重新打开模块确认数据已持久化。
3. 点击“发布官网”。后台携带 `REBUILD_WEBHOOK_SECRET` 请求 `FRONTEND_REBUILD_WEBHOOK_URL`。
4. 构建服务执行 `npm run build:frontend`，原子替换 `frontend/dist`。
5. 在状态面板确认完成时间、退出码和构建摘要，再检查官网。

内容保存在 `data/site-content.json`，媒体位于 `uploads/site-admin/`，咨询单位于 `data/leads.json`。生产环境必须为这些目录挂载持久卷并备份。

## 故障定位

- 官网显示内置示例：检查 `PUBLIC_ADMIN_URL` 和后台健康接口，然后重新构建。
- 保存成功但官网未变化：保存不会自动发布，检查构建状态和 webhook 日志。
- 客服入口地址错误：在“在线客服”模块或 `PUBLIC_SERVICEBRIDGE_URL` 中填写完整的 `http://` 或 `https://` 地址后重新发布。
- 后台被重定向到登录页：2FA 会话已过期，重新登录即可。
