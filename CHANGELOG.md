# 更新记录

## v1.1.5 - 2026-08-28

### 重点变更

- 网站翻译任务级容错：上游网关长窗口不可用（如账号池临时排除返回 502）时，任务自动重排退避（60s/120s/240s）而不是直接标记失败；批次内重试改为快速失败（1.5s/5s/15s），批次间隔 1.5 秒。
- 新保存的翻译配置会立即中断仍在执行的旧修订号任务（批次边界检查修订号），避免旧任务长时间占用队列导致新任务排队卡死。
- 重排期间译文状态保持 pending（排队中），页面不再把上游瞬时不可用显示为失败报错；仅当重排次数耗尽后才标记 failed。
- 上游错误信息保留 HTTP 状态码，且识别 “Service temporarily unavailable” 等网关文案为临时不可用，确保此类错误进入任务级重排而不是直接判为失败。
- 修复翻译后台「保存翻译配置」点击后弹出 “Cannot read properties of null” 报错：保存回调在 await 之后才读取 event.currentTarget（事件派发结束后已置空）；改为 await 前缓存表单引用，并对状态对象加空值保护。保存本身一直成功，此前为前端误报错误。
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
